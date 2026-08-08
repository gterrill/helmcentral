# ADR 0035: Weather Local Day Boundaries and the Absent-Precipitation Sentinel

## Status
Accepted

Applies the AGENTS.md Fallback Policy ("prefer fail-fast; do not add graceful fallback behavior that masks upstream data/source problems") to the weather-provider path introduced by the weather plugin system (`backend/weather_providers.go`).

Changes the `fetch_forecast` guest contract: a new required `timezone` input, and a `-1` sentinel meaning "absent" on every `precipitation_chance_pct` field. Weather plugin authors must read both sections below.

## Context

The forecast tab reported **0% chance of rain for today** while it was drizzling on deck at Mackay. Investigation found one upstream data problem and three separate code defects that between them made a wrong number look authoritative.

The upstream problem is not ours to fix. A raw capture of Apple's response for the vessel's position (saved as `docs/examples/weather-plugins/weatherkit/testdata/weatherkit_response_dry_nearterm.json`) shows WeatherKit explicitly sending `precipitationChance: 0.0` for days 0–5, then real values (`0.43`, `0.58`) from day 6 — with normal metadata, correct coordinates and no degradation flag. Open-Meteo, queried for the same coordinates in the same minute, reported light drizzle falling and a 91% daily maximum. WeatherKit's near-term data was simply wrong there; the plugin mapped it faithfully.

What made that undiagnosable was ours:

**Day boundaries came from the wrong timezone.** The weatherkit plugin hardcoded `timezone=UTC` in its request URL, so `forecastStart` landed on UTC midnight. The host buckets and labels its hourly series on the vessel's *local* date (`buildHourlySeriesByDay`, `vesselLocalLocation`). At UTC+10 the two disagreed by ten hours: the card labelled "today" actually summarised 10:00 today → 10:00 tomorrow, and the daily record covering local midnight → 10:00 was dropped outright by the `dayKey < localTodayKey` filter — precisely the window the rain fell in. Open-Meteo had a quieter version of the same bug via `timezone=auto`, which picks the civil IANA zone and so disagrees with the host's longitude-derived offset anywhere the two differ (eastern Spain, western China).

**Absence was indistinguishable from zero.** `precipitation_chance_pct` defaulted to Go's zero value in the plugin, passed through the host unexamined, and hit `typeof x === 'number' ? x : 0` in the frontend hook. A provider that said nothing and a provider forecasting a dry day rendered identically. `mapCurrentWeather` was worse: when `precipitationChance` was absent — which the captured payload shows really happens — it fell back to `precipitationIntensity`, writing an **mm/hr rainfall rate into a percentage field**, then to other time windows' values.

**Staleness was invisible.** `isCached`, `updatedAt` and `ttlSeconds` were declared on `ForecastDrawerProps` and passed from `App.tsx`, but never destructured or rendered — dead props. Combined with the stale-on-error cache fallback in `wasmWeatherProvider.FetchForecast`, a days-old forecast looked exactly like a live one.

Separately, `resolveGNSSPosition` returns `-1, -1` when GNSS is untrusted with no prior fix, and every handler validated position with a bare lat/lon range check that `-1,-1` passes. The weather cache had accumulated a real `-1.0,-1.0,1` entry: a forecast for the Gulf of Guinea, fetched, cached and displayed as local weather.

## Decision

### 1. The host names the timezone; plugins must use it

`fetch_forecast` gains a required `"timezone"` input carrying an IANA zone identifier, produced by `vesselLocalTimezoneName` (`backend/weather_tide.go`). Any plugin whose upstream API rolls hourly data into daily summaries must pass it through rather than hardcoding or letting the API guess.

The zone is a **fixed offset** (`Etc/GMT-10` for UTC+10 — note the POSIX sign inversion, which is why this is a tested helper and not an inline `Sprintf`). Fixed offsets are deliberate: they mirror `vesselLocalLocation`'s own longitude-derived offset exactly, so the provider's rollup boundary and the host's bucketing can never disagree. Adopting real civil zones would be a strictly larger change — it would mean giving the host a longitude→IANA lookup and changing `vesselLocalLocation` too, since the mismatch is between the *two* of them, not with reality.

The timezone is folded into `weatherWasmCacheKey`, because a bundle rolled up on different boundaries is different data.

An absent timezone is rejected by `validateFetchForecastInput` in both reference plugins rather than defaulted to UTC. Defaulting is what caused this bug; a loud failure is cheaper than a plausible wrong forecast.

### 2. `-1` means absent; `0` means zero

`precipitation_chance_pct` is `-1` when the upstream provider reported nothing, and consumers must render it as unavailable rather than as a number.

This deliberately **breaks** the convention the neighbouring sentinels use. `sentinelSpeedKts`, `sentinelTemperatureF` and `sentinelDirection` all treat exactly-zero as "no data", which is defensible for wind and temperature. For precipitation it is not: 0% is a legitimate and extremely common reading, so zero cannot double as the absent marker. Absence therefore has to be signalled explicitly by the plugin, and `sentinelPrecipitationPct` normalises any negative to `-1` while leaving a real `0` untouched.

The frontend maps `-1` (and a missing field) to `null`, and `WeatherForecastDay.precipitation` / `WeatherHourlyPrecipPoint.precipChancePct` are typed `number | null` so the compiler finds every display site. The precipitation chance, and the humidity and visibility figures derived from it, render as `—`. On the hourly chart a null leaves a gap in the chance line rather than pinning it to the 0% baseline.

### 3. The `mapCurrentWeather` fallback cascade is removed, not narrowed

The old chain (`currentWeather.precipitationChance` → `precipitationIntensity` → `forecastDaily.days[0]` → `forecastHourly.hours[0]`) is gone entirely. The intensity leg was a unit error. The remaining legs reported a *different time window's* number as current conditions, which is inventing data the provider never gave for this instant.

The prior code carried a comment acknowledging the mm/hr quirk as "pre-existing, not introduced here". The captured payload shows it firing in production. Absence is now reported as absence.

### 4. Provenance is always on screen

The forecast drawer renders provider, cache state and refresh age, mirroring the line `forecast-tide-section.tsx` already shows and reusing its `formatRefreshAge` helper. The stale-on-error cache fallback is retained — a stale forecast beats no forecast at sea — but it is no longer silent, which is what the fallback policy actually requires.

### 5. One position predicate, not a range check per handler

`hasUsableVesselPosition` replaces the open-coded lat/lon range checks in every geolocated handler (weather, wave, forecast-warnings, tide, tide auto-update, geonames). It rejects out-of-range values plus the two sentinel pairs, `-1,-1` and `0,0`, that mean "no fix" rather than a location. Positions that merely *contain* a `-1` or `0` component (51.5,-1.0) stay usable.

The tide handlers previously checked latitude only, so `-1,-1` reached `nearestStation` unchallenged.

## Consequences

- **Plugin authors must handle `timezone`.** Any third-party weather plugin ignoring it will roll days up on its own boundary and drift against the host's labels. Both reference plugins now fail loudly if the host omits it.
- **`-1` must not be displayed as a number.** Any new consumer of `precipitation_chance_pct` has to branch on the sentinel. The frontend types force this; a future non-TS consumer would not be protected.
- **Existing weather cache entries are invalidated once** by the cache-key change (the key gained a timezone field). Wave keys gained a trailing empty field for the same reason and refetch once too.
- **WeatherKit's near-term precipitation for this location remains wrong.** None of the above fixes that; it makes the failure legible rather than authoritative. Switching `ui.weather_provider` to `open-meteo` remains available and was demonstrably accurate here.
- **Open-Meteo still has a latent false zero.** Its response arrays are `[]int`, so a JSON `null` (which Open-Meteo does send at the edge of its forecast window) silently decodes to `0` — the same defect class this ADR closes elsewhere. Fixing it requires making the anonymous response structs nullable, which was left out of scope; it is a known follow-up.

## Related

- [ADR 0033: Removing Storm Glass — Tides Become Plugin-Only](0033-remove-storm-glass-tides-plugin-only.md) — the plugin-only provider model this contract sits in
- [ADR 0034: Surfacing Dashboard Save Failures](0034-surfacing-dashboard-save-failures.md) — the same fallback policy applied to the dashboard mutation path
- `AGENTS.md` § Fallback Policy
