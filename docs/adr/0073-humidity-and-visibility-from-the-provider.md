# ADR 0073: Humidity and Visibility Come From the Provider

## Status
Accepted

Applies the AGENTS.md Fallback Policy to two figures that were not fetched at all, but computed in the browser from an unrelated measurement and displayed with the authority of a reading.

Changes the `fetch_forecast` guest contract: two new hourly fields, `humidity_pct` and `visibility_m`, carried as nullable on the host so that an omitted field is distinguishable from a real zero. Weather plugin authors must read section 1.

## Context

The forecast details row showed Humidity and Visibility in the same run of chips as Wind, Gusts and the 500mb height. Neither was fetched. Both were arithmetic on the chance of precipitation, performed in the component:

```ts
const humidityPct  = precipitationPct === null ? null
  : Math.max(35, Math.min(95, Math.round(45 + (precipitationPct * 0.4))))
const visibilityNm = precipitationPct === null ? null
  : Math.max(1, 12 - (precipitationPct * 0.06))
```

A 40% chance of rain rendered as "Humidity 61%" and "Visibility 9.6 nm". Nothing measured or forecast either number. The constants have no source.

Visibility is the one that matters. It is a navigation figure, it is quoted in nautical miles, and it sits two chips away from a wind speed that came off a real model run. A skipper reading "Visibility 9.6 nm" has been told something, and what they were told was a rescaling of the rain chance.

Two things let this survive review.

**It was documented as a consequence of something else.** ADR 0035 says, of the absent-precipitation sentinel: "The precipitation chance, and the humidity and visibility figures derived from it, render as `—`." That sentence is accurate about the null handling and silent about whether the numbers should exist. Reading it, the derivation looks like a settled decision rather than an unexamined one.

**It was tested.** `frontend/src/test/forecast-drawer.test.tsx` carried a case named "does not derive humidity or visibility from an unavailable precipitation chance". The absence path was reasoned about carefully and covered, which made the presence path look considered too. A test on the edge case is not a test of whether the value belongs on screen.

The numbers were never unavailable. Both fields come from both shipped providers:

| | Open-Meteo (default, keyless) | WeatherKit (keyed) |
|---|---|---|
| Hourly humidity | `relative_humidity_2m`, percent | `humidity`, 0 to 1 fraction |
| Hourly visibility | `visibility`, metres | `visibility`, metres |
| Daily humidity | `relative_humidity_2m_mean/max/min` | none |
| Daily visibility | `visibility_mean/max/min`, undocumented | none |

WeatherKit is the sharper case: `humidity` and `visibility` are required fields on `HourWeatherConditions`, the plugin already requests `forecastHourly`, and both values arrive on every response. They were being discarded by `encoding/json` because `weatherKitHourForecast` did not declare them.

## Decision

### 1. Two new hourly fields, nullable on the host

`fetch_forecast` hourly entries gain `humidity_pct` (0 to 100) and `visibility_m` (metres). SI on the wire, matching `wind_speed_ms` and `temperature_c`; metres convert to nautical miles host-side the same way m/s converts to knots.

The host structs declare both as `*float64`, and this is the point of the whole change rather than a detail of it. A missing JSON field decodes to Go's zero value. Zero percent humidity is physically near impossible and would be obvious nonsense to anyone reading closely, but 0.0 nm visibility is not nonsense: it means you cannot see the bow. It is the single most consequential reading a helm display can show, and it is exactly what an un-rebuilt plugin would produce by saying nothing at all.

So absence is carried in the type. Two distinct absences resolve to the same `-1` sentinel:

- The field is missing entirely, because the plugin predates this contract. The pointer is nil.
- The field is present and the provider had no value for that hour. The plugin sends a negative.

`sentinelHumidityPct` and `sentinelVisibilityNm` normalise both, alongside `sentinelPrecipitationPct`. Like that one, they deliberately break the zero-means-absent convention that `sentinelSpeedKts`, `sentinelTemperatureF` and `sentinelDirection` use. This is ADR 0035's Convention B, extended to the two fields that need it most.

`wasmWeatherDayOutput` is unchanged. See section 2.

### 2. The daily figure is reduced host-side, not asked of the provider

The established pattern for a per-day number is to take the provider's own daily aggregate: the Wind and Gusts chips trace back to `wind_speed_10m_max` and `wind_gusts_10m_max`, not to any host reduction. That pattern is not available here.

Open-Meteo does serve `relative_humidity_2m_mean` and `visibility_min`, and the visibility aggregates return correct values with correct units. They are not in the documentation. An undocumented endpoint can be withdrawn without a changelog entry, and the failure would be silent. WeatherKit has no daily humidity or visibility at all, documented or otherwise.

So both are reduced from the hourly series in `buildDayData`, which is where `buildWindSummary` and `buildPrecipitationSummary` already do the same job. One code path, no dependency on an undocumented aggregate, and the only option that works for both providers.

### 3. Humidity takes the mean, visibility takes the minimum

The reduction follows the split the codebase already uses. `buildWindSummary` takes a range for sustained speed and a maximum for gust. `buildWaveSummary` takes a mean for period and a range for height. Hazard-shaped quantities reduce toward the hazard; characteristic-shaped ones reduce to the middle.

Humidity is characteristic-shaped and takes the mean. Visibility is hazard-shaped in the low direction and takes the minimum. The worst visibility of the day is the number a passage plan turns on, and a mean would smooth a two-hour fog bank at 0.5 nm into a comfortable-looking eight.

Both reducers skip negative samples and report absent when no sample qualifies, rather than reporting zero.

### 4. The derivation is deleted, not kept as a fallback

Retaining the computed values for the case where a provider omits the field would leave the display exactly as untrustworthy as it is now, while making it harder to notice: a real absence and a real reading would render identically, and the only way to tell them apart would be to read the source. That is the defect this ADR closes, reintroduced one layer down.

The chips render `—` when the provider said nothing. That path already existed for precipitation and is correct. It will fire more often now, which is the honest outcome and not a regression.

## Consequences

- **Visibility will be absent more often than expected.** It is model-dependent. ICON Global, Meteo-France, JMA and GEM do not carry it, and Open-Meteo answers with HTTP 200 and a null array rather than an error, with the unit string reported as `undefined`. Under `best_match`, which is what the reference plugin uses, live probes returned real values for both Paris and Tokyo despite their native regional models lacking the field. A 16-day window at the vessel's own position went null from index 475 onward. The em dash is a normal state here, not an error state.
- **Both reference plugins must be rebuilt.** `packaging/build-plugins.sh` does this on every `make dev`. An un-rebuilt third-party plugin is not merely non-crashing: its omitted fields land on nil, then on `-1`, then on the em dash, which is the correct rendering. That correctness is what the pointer buys and is the reason not to use a bare `float64` here.
- **The new Open-Meteo arrays are `[]*float64`.** The latent false zero ADR 0035 recorded as a known follow-up, where the existing `[]int` and `[]float64` response arrays decode a JSON null to zero, is not fixed by this change. It is not extended by it either. Visibility would have been the worst possible field to add to that pile, since its nulls are routine rather than an artefact of the window edge.
- **The UI still cannot say why a value is missing.** "Your provider does not publish this" and "the fetch failed" both render as `—`. The upper-air panel makes exactly this distinction and the wave panel does not; this change leaves the forecast row on the wrong side of it. Recorded as a gap rather than fixed, because the fix is a contract question about how a provider reports an unsupported field, not a display question.
- **Weather cache entries written before this change refill on TTL.** No key change was needed, so entries fetched under the old contract carry no humidity or visibility and land on absent until they expire.

## Verification

`go test -short -count=1 ./...` passes for the backend, both weather plugins pass their own suites, and 1445 frontend tests across 146 files pass. All eight reference plugins build under TinyGo 0.41.1 via `packaging/build-plugins.sh`.

The fixture rule applies and was followed. The Open-Meteo parse is pinned to a response captured live from `api.open-meteo.com` over a full 16-day window rather than an assumed shape: 384 hourly samples, with five trailing nulls in both `relative_humidity_2m` and `visibility`. The null tail is in the fixture, so the case that decodes wrong under a bare `[]float64` is exercised by every run rather than reasoned about.

The boundary case that a genuine 0.0 nm visibility survives, rather than being read as absent, is asserted at all three layers it could be lost in: `sentinelVisibilityNm` returning 0 for a real zero metre reading, `reduceVisibilityNm` letting that zero win as the day's minimum, and the drawer rendering it as `0.0 nm` rather than as the em dash. It is the most important assertion in the change and the one most likely to be broken by a later refactor that "simplifies" a nil check into a falsiness check.

Not yet done: an end-to-end check against the running install on the boat, including a location whose model omits visibility so the em-dash path is exercised against a live response rather than a fixture. WeatherKit's fixture was built from the documented schema rather than captured, because credentials were not available in this environment; the two fields it adds are required fields on `HourWeatherConditions` and were already arriving on the wire, but that path has not been confirmed against a real response.

## Related

- [ADR 0035: Weather Local Day Boundaries and the Absent-Precipitation Sentinel](0035-weather-local-day-boundaries.md) — the `-1` sentinel convention this extends, and the ADR whose passing mention of these two figures let them stand
- [ADR 0018: WASM Plugin Weather and Wave Providers](0018-wasm-plugin-weather-and-wave-providers.md) — the `fetch_forecast` contract being changed
- `AGENTS.md` § Fallback Policy
