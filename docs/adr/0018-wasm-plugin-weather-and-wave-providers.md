# ADR 0018: Sandboxed WASM Plugin Weather and Wave Forecast Providers

## Status
Accepted

## Context
Weather (`fetchWeatherKitData`, `fetchWeatherKitForecastBundleData`) and wave/swell forecasting (`fetchOpenMeteoMarineForecast`, `fetchOpenMeteoSeaTemperatureF`) were hardcoded directly into `backend/weather_tide.go`, the same problem ADR-0017 already solved for tides: a single maintainer-chosen vendor per domain, no way for an operator to swap providers without a Go rebuild, and — specific to weather — a real single-point-of-failure risk, since Apple WeatherKit requires a paid developer account and per-operator credentials that a fresh install simply doesn't have. `weatherToday` masked that failure mode with a hardcoded `72°F / "Partly Cloudy"` fallback state, and a wave-fetch failure inside `fetchWeatherKitForecastBundleData` was logged and silently swallowed into an empty `HourlyWave`/`WaveSummary` rather than surfaced — both violations of this codebase's fail-fast policy, not just missed features.

ADR-0017's WASM plugin machinery (`backend/wasm_tide_provider.go`, Extism/wazero) was the obvious template, but two open questions had to be answered before reusing it:
1. Would a third (and now fourth) copy of that ~500-line adapter just duplicate itself twice more, or does the pattern generalize?
2. Apple WeatherKit authenticates with an ES256-signed JWT built from a PEM private key — does TinyGo, compiled to `wasip1`, actually support `crypto/ecdsa`/`crypto/x509` well enough to sign inside the sandboxed guest, or does this need a host-side signing escape hatch (a new, security-relevant seam ADR-0017's design deliberately avoided needing for tides)?

## Decision

### Two sibling registries, not one
Weather and wave/marine forecasting became **two separate plugin types** (`weatherProvider` in `backend/weather_providers.go`, `waveProvider` in `backend/wave_providers.go`), discovered from separate directories (`plugins/weather/`, `plugins/waves/`, overridable via `PLUGINS_WEATHER_DIR`/`PLUGINS_WAVES_DIR`), not one combined "forecast provider" type. They're usually different upstream domains entirely — a point-weather API (WeatherKit, Open-Meteo's general forecast endpoint) versus a marine/wave model (Open-Meteo Marine, NOAA WaveWatch III) — and operators plausibly want to mix them (e.g. WeatherKit for point weather, Open-Meteo Marine for swell), which a single combined interface would prevent.

### Both are WASM-only — no native built-in, unlike tides
Tides kept Storm Glass as a native built-in provider (ADR-0017). Weather and waves have **no native provider at all** — `weatherProviderRegistry`/`waveProviderRegistry` start empty and only WASM plugins ever populate them. This is a deliberate asymmetry: Storm Glass's native status predates the plugin mechanism and was never revisited, whereas weather and waves were designed plugin-first from day one, and both reference implementations (Open-Meteo, Open-Meteo Marine, WeatherKit) are equally well-suited to being plugins — none needed Go-only capabilities like BOM's `p.(*bomTideProvider)` concrete-type assertion (itself since removed, see ADR-0017's "Update" section).

**Open-Meteo (weather) and Open-Meteo Marine (waves) are the defaults** (`ui.weather_provider`/`ui.wave_provider` in `settings.yaml`, defaulting to `"open-meteo"`/`"open-meteo-marine"` when unset) specifically because both are free and keyless — a fresh Helmcentral install gets a fully working forecast dashboard with zero configuration, the same "useful out of the box" goal that motivated this ADR's default choices from the start. **Apple WeatherKit** ships as a second reference weather plugin (`docs/examples/weather-plugins/weatherkit/`) for operators who prefer Apple's data and are willing to get a paid Apple Developer account — see that plugin's README for how to obtain WeatherKit credentials (adapted from a community writeup on migrating off the discontinued Dark Sky API).

### Shared host layer, factored out this time
Rather than copy `wasm_tide_provider.go` twice more, its generic machinery was extracted into `backend/wasm_plugin.go`: `wasmPluginBase` (id/name/ttl + compiled module + `call()`), `wasmPluginCache[T]` (a generic TTL/stale/disk-persisted cache, replacing the tide-specific `wasmTideCache`), `manifestForWasmPlugin`/`configForWasmPlugin`/`allowedHostsForWasmPlugin` (companion-file loaders), `newWasmPluginBase` (compile + validate the universal `id()`/`name()`/`ttl_seconds()` contract), and `loadWasmPluginsFromDir` (the generic scan-discover-register loop). `wasm_tide_provider.go` was refactored onto this base in the same change — `wasm_weather_provider.go` and `wasm_wave_provider.go` are now genuinely thin (each under 150 lines: a cache-of-the-right-type plus one contract-specific fetch method), and any future plugin type (e.g. a marine-traffic or fuel-price provider) inherits allowlisting, timeouts, panic containment, and caching for free instead of re-implementing them a fourth time.

One accepted side effect: the tide plugin cache's on-disk envelope changed shape (from a flat per-entry struct to a generic `{"value": ..., "cached_at": ...}` wrapper). `wasmPluginCache[T].loadFromDisk` detects the old flat format (by checking for the `"value"` key) and discards the whole file with a logged warning rather than silently loading zero-valued entries into the stale-on-error fallback path — a real behavior a fail-fast codebase would insist on, not a masking fallback. Net effect for an operator upgrading: one stale tide cache file is dropped and rebuilt on first fetch after the upgrade; no functional regression.

### Plugin contracts
Both follow the tide contract's shape: `id()`, `name()`, `ttl_seconds()` (optional, default 3600), plus one fetch export. JSON in/out, SI units, RFC3339 timestamps (host does all local-day bucketing via the existing `vesselLocalLocation(longitude)`, so a plugin never needs to know the vessel's timezone).

```
// weather plugin
fetch_forecast({"lat": float64, "lon": float64, "days": int}) -> {
  "current": {time, temperature_c, condition, wind_speed_ms, wind_gust_ms, wind_direction_deg, precipitation_chance_pct},
  "days":    [{start, condition, temp_max_c, temp_min_c, wind_speed_ms, wind_gust_ms, wind_direction_deg,
               precipitation_chance_pct, sunrise (optional), sunset (optional)}, ...],
  "hourly":  [{time, temperature_c, condition, wind_speed_ms, wind_gust_ms, wind_direction_deg,
               precipitation_chance_pct, precipitation_mm, uv_index, is_daylight}, ...]
}

// wave plugin
fetch_waves({"lat": float64, "lon": float64, "days": int}) -> {
  "hourly": [{time, wave_height_m, wave_period_s, wave_direction_deg, wind_wave_height_m, swell_wave_height_m}, ...],
  "sea_surface_temperature_c": float64 (optional — omitted entirely when the model has no sea-temp data for this location)
}
```

`condition` uses the **existing WeatherKit condition-code vocabulary** (`clear`, `cloudy`, `mostlycloudy`, `partlycloudy`, `rain`, `heavyrain`, `snow`, `thunderstorms`, `mixedrainandsnow`, etc. — the full list is documented in `backend/weather_providers.go`'s top comment and enforced by the existing `formatWeatherConditionAt` in `main.go`), not a newly invented one. This was a deliberate reuse, not laziness: the WeatherKit plugin needs zero condition-mapping code (its `conditionCode` values already match), and `formatWeatherConditionAt`'s display logic (including the `mostlyclear` day/night split) needed no changes at all. The Open-Meteo plugin maps WMO weather codes onto this vocabulary itself, with an explicit fallback to `"cloudy"` for any WMO code the mapping table doesn't cover — documented as a deliberate reference-plugin simplification, not a claim of exhaustive WMO coverage.

### Host-side derivation, including a new one: moon phase
Same philosophy as `classifyTidalPhase`/`hasDoubleTide` in ADR-0017: anything that should behave identically across every provider is computed once, host-side, never by individual plugins. Unit conversion (°C→°F, m/s→kts), condition-label formatting, day-bucketing, and the wind/wave/precipitation summary sentences all moved (or already lived) host-side. **Moon phase is new to this list** — WeatherKit previously supplied it directly (`forecastDaily.days[].moonPhase`), but Open-Meteo has no equivalent field, so a host-side `moonPhase(t time.Time) string` (synodic-cycle approximation, referenced against a known new-moon epoch) now computes it identically for every weather provider, emitting the same 8-value vocabulary (`new`, `waxingCrescent`, `firstQuarter`, `waxingGibbous`, `full`, `waningGibbous`, `lastQuarter`, `waningCrescent`) the frontend already renders — zero frontend changes needed.

### Config/secrets: a generic mechanism, not a WeatherKit special case
WeatherKit is the only plugin (so far) needing operator-supplied secrets — a Key ID, Team ID, Service ID, and PEM private key. Rather than build WeatherKit-specific config plumbing, a companion `<name>.config.json` file (symmetric with `<name>.allowed_hosts.json`) was added to the shared host layer, usable by any future plugin: a flat JSON object of string values, each expanded via `os.Expand` against the backend process's environment (so `weatherkit.config.json` ships as a template containing `"key_id": "${WEATHERKIT_KEY_ID}"` etc., and an operator sets real values as container env vars rather than editing the file). A `${VAR}` referencing an *unset* environment variable causes that whole key to be **dropped from the map entirely** — never substituted with an empty string — so a plugin's own "is this key present?" check behaves correctly for operators who haven't set config that plugin doesn't need. A missing `<name>.config.json` file is the normal default (empty config, no error) — Open-Meteo and Open-Meteo Marine need none.

Missing required config surfaces **at call time, inside the guest**, not at plugin-load time: `weatherkit`'s `fetch_forecast` checks all four keys up front and returns a `pdk.SetErrorString` naming exactly which keys are missing and which env vars to set, which the host adapter propagates as a 502 with that same message. This means a default, keyless install has zero boot-time noise about WeatherKit being unconfigured (it loads fine — `id()`/`name()`/`ttl_seconds()` need no config), and an operator who explicitly selects "Apple WeatherKit" in Settings without having configured it gets an immediate, actionable, specific error the moment they try to use it — not a silent failure and not a refusal to load.

### ES256 JWT signing: proven feasible in-guest, no host escape hatch needed
Before committing to this design, a disposable feasibility spike (`backend/testdata/wasm_plugins/src/es256sign/`, `backend/wasm_es256_spike_test.go`) confirmed TinyGo 0.41.1 targeting `wasip1` compiles and correctly runs `crypto/ecdsa`, `crypto/elliptic` (P-256), `crypto/x509.ParsePKCS8PrivateKey`, `crypto/sha256`, and `crypto/rand` (satisfied by WASI's `random_get`, already enabled via `wazero`'s `EnableWasi: true`) inside the sandboxed guest, with a real cryptographic round-trip (sign in-guest, verify host-side against the same key) — not just "it compiled." The `weatherkit` plugin's own JWT construction (`weatherkit.go`) uses this exact approach: parse the PKCS8 PEM, `ecdsa.Sign`, pack `r‖s` into the 64-byte fixed-width JWS format per RFC 7518, base64url-join `header.claims.signature`. **No host-side signing host-function was needed** — the alternative design (a generic `es256_sign` Extism host function, sketched as a fallback before the spike ran) was never built, keeping the trust boundary exactly where ADR-0017 put it: plugins do their own work inside the sandbox, the host stays a thin, generic dispatcher.

### `/api/wave-forecast` is a new, separate endpoint
Wave data used to be embedded inside `/api/weather-forecast`'s per-day response (`hourly_wave`, `wave_summary`) and populated by a second, silently-swallowed-on-error fetch inside the WeatherKit forecast function — exactly the fail-fast violation described in Context. It is now `GET /api/wave-forecast`, a fully independent endpoint with its own provider resolution, its own 502 on failure, and its own per-day response shape keyed by `day_key` (a vessel-local `"2006-01-02"` string computed identically by both the weather and wave handlers via the same `vesselLocalLocation`, so the frontend can always join a weather day to its corresponding wave day even though the two forecasts come from independent upstream providers with no inherent shared indexing). A wave-provider outage is now a visible, distinct error state instead of an empty, unexplained gap in the weather forecast.

### Deletion
`fetchWeatherKitData`, `fetchWeatherKitForecastBundleData`, `fetchWeatherKitForecastData`, `generateWeatherKitJWT`, `buildDailyWindSeries`/`buildDailyPrecipitationSeries`/`buildDailyUVSeries`/`buildDailyCloudSeries` (the WeatherKit-raw-JSON-specific bucketing functions), `weatherKitForecastLocation`, `defaultWeatherForecastDays` (the hardcoded-placeholder fallback), `fetchOpenMeteoMarineForecast`, `fetchOpenMeteoSeaTemperatureF`, and their JSON parsers are gone from the backend entirely. That logic now lives only inside the `weatherkit` and `open-meteo-marine` plugins, ported near-verbatim into TinyGo. `github.com/golang-jwt/jwt` dropped to an indirect (transitive, via `echo/v4/middleware`) dependency in `go.mod` as a result.

## Consequences

Positive:
- Zero-rebuild extensibility for both domains, matching tides — a new weather or wave source is a `.wasm` file (+ allowlist, + optional config template) dropped into `plugins/weather/` or `plugins/waves/`, picked up on restart with zero frontend changes beyond what already exists generically (`use-weather-providers.ts`/`use-wave-providers.ts`, cloned from `use-tide-providers.ts`).
- A fresh install is useful immediately: Open-Meteo/Open-Meteo Marine need no API key at all, so the forecast dashboard works out of the box — previously WeatherKit-only installs with no credentials configured got a fake `72°F` tile forever.
- Two real fail-fast bugs fixed as a side effect of this migration, not just new features: the hardcoded weather-today placeholder state and the silently-swallowed wave-fetch error inside the old WeatherKit bundle fetch.
- The shared `wasm_plugin.go` layer means a fifth plugin type (should one ever be needed) costs a fraction of what the third one did — this ADR paid down that factoring debt rather than deferring it again.
- Apple WeatherKit is still fully supported, with clear, tested, documented credential-acquisition instructions, for operators who want it — nothing was removed, only un-hardcoded.

Negative / explicitly deferred:
- The tide plugin disk-cache format change (see "Shared host layer" above) means every operator upgrading to this version loses one cached tide-chart fetch on first boot — cosmetic, self-healing on the next fetch, but real.
- No dynamic entries were added to `/api/caches`/`listCaches` for the new per-plugin weather/wave cache files (`cache/weather_wasm_<id>_cache.json`, `cache/wave_wasm_<id>_cache.json`) — they exist and work identically to tide's per-plugin caches, just aren't yet surfaced in the caches-admin UI. Deferred as a nice-to-have, not core to pluggability.
- No plugin hot-reload, same limitation ADR-0017 already accepted for tides — `plugins/weather/` and `plugins/waves/` are scanned once at startup.
- WeatherKit's in-guest JWT signing and JSON parsing is, like BOM's HTML scraping (ADR-0017), harder to debug inside the WASM sandbox than equivalent native Go would be if Apple's API contract ever drifts — the same accepted tradeoff, extended to a second plugin.

## Related
- ADR-0017: Sandboxed WASM Plugin Tide Providers — the template this ADR generalizes and factors out a shared layer from; also the origin of the `wasm_plugin.go` extraction's tide-side consequence (cache format change).
