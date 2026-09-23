# Open-Meteo weather provider plugin

A Helmcentral weather-provider plugin backed by Open-Meteo's free, keyless
Forecast API, covering weather worldwide. It is the default weather provider
for fresh Helmcentral installs because it needs no API key or `config.json`.
Install the `.wasm` binary and
Open-Meteo appears in the weather-provider dropdown in Settings.

Open-Meteo is a free, non-commercial weather forecast API with no signup
needed, no rate limits on public usage, and global coverage. It returns
forecasts via a JSON API in SI units (Celsius, m/s, etc.). This plugin is a
reference implementation of Helmcentral's WASM weather-provider contract.

It is not a production-hardened client: it has no pagination or retry/backoff
(see the comments at the top of `main.go`). It can be adapted to another
weather source by changing the URLs and field mappings.

Written in TinyGo. The Extism plugin contract isn't TinyGo-specific: Rust, Zig, C,
AssemblyScript, C++, and Haskell PDKs all implement the same contract
identically; see [Extism's PDK list](https://extism.org/docs/concepts/pdk)
if you'd rather use one of those.

## Contract requirements every weather plugin must honour

Two rules from
[ADR 0035](../../../adr/0035-weather-local-day-boundaries.md) prevent incorrect
day boundaries and misleading precipitation values:

1. **Use the host's `timezone` input.** `fetch_forecast` receives an IANA zone
   (e.g. `Etc/GMT-10`). If your upstream API rolls hourly data up into daily
   summaries, pass this through instead of hardcoding a zone or letting the
   API pick one. The host buckets and labels its own day series in that same
   zone; rolling up on a different boundary silently shifts every day summary
   and drops the record covering local midnight to the offset. An absent
   `timezone` is rejected, not defaulted.

2. **Report absent precipitation as `-1`, never `0`.** `0%` is a legitimate
   forecast, so it cannot double as "no data". If the upstream response omits
   a chance-of-precipitation value, emit `-1` on
   `precipitation_chance_pct` so the UI can show "unavailable". Never
   substitute a value from a different field or a different time window. A
   previous substitution caused an mm/hr rainfall rate to appear as a percentage.

## Why this plugin is two files

`open-meteo.go` holds all the parsing/filtering logic and has no dependency
on `github.com/extism/go-pdk`, so `go test ./...` (below) runs on the plain
host Go toolchain with no TinyGo or wasm target needed. `main.go` holds only
the thin `//go:wasmexport` wrapper functions and is gated `//go:build tinygo`
so it's excluded from that plain host build (TinyGo defines the `tinygo`
build tag automatically; plain `go test` doesn't). Because of this split,
always build the whole package directory (`.`), not just `main.go` by name,
as described below.

## Building it

Requires only Docker (no local TinyGo install needed), pinned to
`tinygo/tinygo:0.41.1`, the same version pinned for this repo's
WASM test fixtures (see `backend/wasm_weather_provider_test.go`'s
regeneration comment), to avoid `:latest` drift. From the repo root:

```sh
docker run --rm -v $(pwd):/src -w /src tinygo/tinygo:0.41.1 sh -c "
  cd docs/examples/weather-plugins/open-meteo &&
  go mod tidy &&
  tinygo build -o open-meteo.wasm -target wasip1 -buildmode c-shared .
"
```

Use the trailing `.` to build the whole package directory. Naming `main.go`
alone would exclude `open-meteo.go` and fail with
`undefined:` errors, since Go/TinyGo's single/multi-file build mode only
compiles the files explicitly listed.

This produces `open-meteo.wasm` in this directory. Requires network access
inside the container (`go mod tidy` fetches `github.com/extism/go-pdk`) and
Docker with the `tinygo/tinygo` image available locally.

If you'd rather install TinyGo locally instead of using Docker, the
equivalent commands are:

```sh
go mod tidy
tinygo build -o open-meteo.wasm -target wasip1 -buildmode c-shared .
```

Both the Open-Meteo and other example plugins are also built and installed
automatically as part of Docker Compose startup (see the repo's compose
files). Use the manual build instructions to build or inspect this plugin
separately.

## Installing it

Helmcentral discovers weather-provider plugins by scanning `plugins/weather/`
at startup (overridable via the `PLUGINS_WEATHER_DIR` env var). To install
this plugin manually:

1. Copy the compiled `open-meteo.wasm` into your `plugins/weather/`
   directory.
2. Create `plugins/weather/open-meteo.allowed_hosts.json` next to it,
   containing:

   ```json
   ["api.open-meteo.com"]
   ```

   This file is required for network access. A plugin with no companion
   `<name>.allowed_hosts.json` file gets no network access
   (Helmcentral's default-deny sandboxing). `api.open-meteo.com` is the only
   host this plugin talks to, so it's the only host that needs to be
   allowlisted.
3. Restart the Helmcentral container (or the dev backend). "Open-Meteo"
   should now appear in the weather-provider dropdown in Settings, with no
   frontend changes required. No `config.json` is needed since it is keyless.

`plugins/weather/` lives at the repo root and is gitignored, like `backend-data/`,
because it contains operator runtime files. A fresh Helmcentral checkout ships
with no plugins active by default, to avoid contacting external APIs before a
provider is chosen.

## Testing

`main_test.go` unit-tests the WMO weather-code mapping function
(`wmoCodeToCondition`), the local-time-to-UTC conversion function
(`parseOpenMeteoLocalTime`), and the full JSON parsing logic
(`parseOpenMeteoForecast`) against synthetic fixtures, entirely on the host
Go toolchain, with no TinyGo or WASM runtime needed:

```sh
go test ./...
```

## Open-Meteo endpoints this plugin uses

- Forecast (current + hourly + daily + 15-minute nowcast): `GET https://api.open-meteo.com/v1/forecast?latitude=<lat>&longitude=<lon>&current=temperature_2m,weather_code,wind_speed_10m,wind_gusts_10m,wind_direction_10m,is_day,precipitation_probability&hourly=temperature_2m,weather_code,wind_speed_10m,wind_gusts_10m,wind_direction_10m,precipitation_probability,precipitation,uv_index,is_day,relative_humidity_2m,visibility&daily=weather_code,temperature_2m_max,temperature_2m_min,wind_speed_10m_max,wind_gusts_10m_max,wind_direction_10m_dominant,precipitation_probability_max,sunrise,sunset&minutely_15=precipitation,precipitation_probability&forecast_minutely_15=8&wind_speed_unit=ms&timezone=<caller-timezone>&forecast_days=<days>`

See the comment block at the top of `main.go` and `open-meteo.go`'s
`parseOpenMeteoLocalTime` documentation for exact response shape details and
how they map onto Helmcentral's plugin contract (including the
UTC-offset conversion for Open-Meteo's naive local-time timestamps).

### `next_hour` (15-minute nowcast)

Open-Meteo's `minutely_15` dataset is a coarser nowcast than WeatherKit's
minute-by-minute `forecastNextHour`: four points per hour instead of sixty.
This plugin requests only `precipitation` and `precipitation_probability`
from it, bounded to `forecast_minutely_15=8` (2 hours/8 points - Open-Meteo's
undocumented default is 288 points/3 days, confirmed live, far more than a
next-hour nowcast needs), and maps each point onto the guest contract's
`next_hour` field:

- `time` is `minutely_15.time[i]` shifted back by one 15-minute step. Open-
  Meteo documents `precipitation` as a **"Preceding 15 minutes sum"** - the
  value at timestamp T covers `[T-15min, T)`, not `[T, T+15min)` - while this
  plugin's contract says a point's `time` marks the START of forward
  coverage. Shifting the timestamp back by 15 minutes is what reconciles the
  two; leaving it as Open-Meteo sends it makes rain that has already been
  falling for 10 minutes read as "expected in 5 minutes" instead.
  `precipitation_probability` has no separately documented convention at
  `minutely_15` resolution (see below), so the same shift is applied to it
  too via the shared per-point `time` - the only consistent treatment
  available.
- `precipitation` arrives as mm accumulated over the preceding 15-minute
  slot; multiplied by 4 to get this contract's `precipitation_mm_per_h`.
- `precipitation_chance_pct` comes from `precipitation_probability`
  directly, unconverted (already a 0-100 percentage, per
  `minutely_15_units` in every live response captured below).
- Both `Precipitation` and `PrecipitationProbability` decode into
  **pointer-element** slices (`[]*float64`/`[]*int`), the same reason
  `Hourly.RelativeHumidity2m`/`Visibility` do: a plain `[]float64`/`[]int`
  would silently turn a real JSON `null` into `0.0`/`0`, and neither field
  carries a documented guarantee against one. A `null`
  `precipitation_probability` falls back to this contract's "not supplied"
  `-1` sentinel, the same as an out-of-range index already did. A `null`
  `precipitation` is a different case: it means Open-Meteo has no reading
  for that point at all, which is not the same thing as a confirmed
  `0.0` (dry) - and this contract has no per-point "no data" flag for
  `precipitation_mm_per_h` the way it does for the chance field. The plugin
  therefore **omits that point from `next_hour` entirely** rather than
  emitting a fabricated dry reading; the host/frontend already treat a gap
  between `next_hour` points as "nothing known there," never as "confirmed
  dry" (`lib/nowcast.ts`'s `buildNowcastBars` only ever draws bars for the
  points it's actually given).

**Whether `minutely_15` is real 15-minute data, or interpolated from the
hourly model, depends on where the vessel is - this plugin does not assume
either way.** Open-Meteo's forecast API docs (https://open-meteo.com/en/docs,
confirmed 2026-09-23) state: *"This data is based on NOAA HRRR model for
North America and DWD ICON-D2 and Météo-France AROME model for Central
Europe. If 15-minutely data is requested for other regions data is
interpolated from 1-hourly to 15-minutely."* `precipitation_probability` is
not listed in the 15-Minutely Weather Variables table at all, in any
region - only the hourly resolution documents one, as *"Preceding hour
probability"*.

Confirmed live against `api.open-meteo.com` on 2026-09-23 at Mackay (lat
-18.65, lon 146.48 - outside both native-resolution regions):
`minutely_15.precipitation_probability` stepped smoothly between the
surrounding hourly readings (hourly 84, 82, 80 -> minutely_15 84, 83, 83,
82, 82, 81...) - exactly the shape linear interpolation produces, not
independent per-15-minute observations - and `minutely_15.precipitation`
read `0.0` at a point where the hourly figure was `0.1`. Both confirm this
window's data was backfilled from the hourly model at this position, as
Open-Meteo's own docs say it would be. Requesting an invalid `minutely_15`
variable name (`totally_fake_variable`) does still get a `400`
`"Invalid value"` response, confirming `precipitation`/
`precipitation_probability` are accepted, real API fields, not silently
ignored - just not necessarily *independently modelled* data outside the
two native-resolution regions.

`nextHourSourceForPosition` (`open-meteo.go`) reports which case applies for
a given position - `"nowcast"` inside conservative bounding boxes for the
NOAA HRRR (North America) and DWD ICON-D2/Météo-France AROME (Central
Europe) grids, `"hourly"` everywhere else - and the plugin sends it as this
response's `next_hour_source` field (backend/weather_providers.go's
`next_hour_source` contract) so the host/frontend can caption an
interpolated strip honestly instead of presenting it as a true short-range
nowcast.

`testdata/open_meteo_response_minutely15_mackay.json` (lat -18.65, lon
146.48, dry at capture time, chance readings in the 80-84% range) and
`testdata/open_meteo_response_minutely15_singapore_rain.json` (lat 1.3521,
lon 103.8198, actively raining, precipitation ramping 0.4 -> 0.6mm/15min and
chance ramping 18% -> 66% across the captured window) are both real,
unedited API responses, not synthesized fixtures - and both positions are
outside the two native-resolution regions, so both map to
`next_hour_source: "hourly"`.

`testdata/open_meteo_response_minutely15_mackay_with_nulls.json` is the
Mackay capture above with two values deliberately nulled out (one
`precipitation` entry, one `precipitation_probability` entry, at different
points) - exercising the pointer-slice null handling described above against
a genuine capture's shape rather than a hand-built one, since neither
fixture above happened to carry a real null at capture time.
