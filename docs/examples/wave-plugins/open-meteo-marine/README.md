# Open-Meteo Marine wave provider plugin

A Helmcentral wave-provider plugin backed by Open-Meteo's free, keyless Marine
API. It is the default wave provider for fresh Helmcentral installs because
it needs no API key or config.json.

The plugin fetches hourly wave forecasts (wave height, period, direction, wind
swell, and swell components), and optionally sea surface temperature, converted
to SI units and RFC3339 timestamps per the plugin contract. A separate,
optional `fetch_currents` export gives the hourly modelled ocean current.

Written in TinyGo. The Extism plugin contract isn't TinyGo-specific: Rust, Zig, C,
AssemblyScript, C++, and Haskell PDKs all implement the same contract
identically; see [Extism's PDK list](https://extism.org/docs/concepts/pdk) if
you'd rather use one of those.

## Request design

The plugin makes two separate HTTP calls to the Marine API for `fetch_waves`, and a third only when the host calls `fetch_currents`:

1. **Wave data (REQUIRED)**: Fetches hourly wave heights, periods, directions,
   wind-wave heights, and swell-wave heights. This call is pinned to NOAA's
   GFS-Wave (WaveWatch III) model via `models=ncep_gfswave025` so swell height,
   period and direction line up with other WaveWatch III-based swell forecasts
   for a given coastline. Open-Meteo's default "best_match" model blend runs
   noticeably lower/different here. GFS-Wave's 0.25 degree grid is land-masked,
   and over a masked cell Open-Meteo returns every wave field as `0`, not
   null. See "Land cells and the blend fallback" below.

2. **Sea surface temperature (OPTIONAL)**: Fetches current sea surface
   temperature. This call does not pin a `models=` param (unlike the wave-data
   fetch), because the wave-specific NOAA GFS-Wave model does not carry
   sea surface temperature at all (returns null/"undefined" units); only
   Open-Meteo's default model blend does. If this fetch fails or the value is
   absent/null (confirmed live: inland coordinates return 200 with
   sea_surface_temperature:null), the fetch_waves call still succeeds.
   sea_surface_temperature_c is omitted from the output JSON rather than
   replaced with zero.

3. **Ocean currents (OPTIONAL, `fetch_currents` only)**: Fetches
   `ocean_current_velocity` and `ocean_current_direction` hourly for every
   point of a route in one request, and nothing else, also without a `models=`
   param. GFS-Wave carries no ocean currents
   (every value comes back null), so these cannot ride on the wave request; only
   Open-Meteo's default blend answers. This request is made only when the host
   calls the separate `fetch_currents` export, so a wave fetch costs no extra
   request. See "Ocean currents" below.

## Ocean currents

`fetch_currents({points: [{lat, lon}, ...], days})` returns
`{"points":[{"hourly":[{time, current_velocity_ms?, current_set_deg?}]}, ...]}`,
one entry per point in the same order. A call carries at most 100 points
(`maxCurrentsPoints`, which keeps the URL near 2 KB); the host errors above that
and never splits a route. The plugin makes ONE request with comma-separated
latitude and longitude lists, in `timezone=GMT` so the times are UTC and a
daylight-saving change cannot make one offset wrong, with `past_days=1` so the
series starts at 00:00 UTC yesterday and a window that starts earlier today is
bracketed. Open-Meteo answers several
locations with a JSON array of per-location objects and one location with a
single object; the plugin reads both. A response whose `utc_offset_seconds` is
missing or not zero is an error.

Open-Meteo gives velocity in km/h and the direction the current flows
**toward**, in degrees; the plugin converts the velocity to metres per second and
refuses a response whose velocity unit is not `km/h`, because it would convert it
wrongly. This is the total current the model gives (tide plus ocean currents),
not a tide alone. The test fixture, `testdata/currents_four_points.json`, is a
real response, including a location whose hours are all null.

Every hour is listed. Open-Meteo returns JSON `null` in both arrays over land and
near shore. Only that means "no current this hour": the hour carries neither key,
which the host reads as "no current", never as slack. A reported `0` is a real
figure and is kept.

Anything else that is not a clean answer is an error returned to the host, which
reports it: a missing or mistyped array, arrays of unequal length, an unparseable
time, a missing or non-`km/h` velocity unit, a null in only one of the pair, a
negative velocity or a direction outside 0 to 360. So are a failed request and a
non-2xx status. Nothing is dropped or swallowed. A host or plugin
built before currents existed lacks the export, and the host says to install the
updated plugin. The model's grid is about 9 km, so it does not resolve river
entrances, narrow straits or shoals, and rates there are likely understated.

## Land cells and the blend fallback

At -20.2666, 148.7099 (Whitsundays) the nearest GFS-Wave cell is land, and
Open-Meteo returns height, period, direction and every component as `0.0`.
Read as figures, that is a flat calm from due north. A wave with no period is
not a wave, so the host (`backend/wasm_wave_provider.go`) treats an hour whose
combined `wave_period_s` is `0` as absent, and a wind-wave or swell component
with period `0` as no train. The plugin applies the same rule to the
combined figures, so it leaves them out of such an hour.

The plugin also has an operator setting, declared in
`open-meteo-marine.config_fields.json`:

| Key                  | Type    | Default | Effect |
| -------------------- | ------- | ------- | ------ |
| `use_blend_fallback` | boolean | off     | When every hour of the GFS-Wave series has a missing or zero period, fetch the same point again without `models=` (Open-Meteo's default blend, finer grid) and use that series. |

The fallback runs only on a series with no waves at all, never per hour. The
output then carries `"model": "best_match"` and `"fallback_used": true`
(otherwise `"model": "ncep_gfswave025"`), and the host logs the fallback (the plugin does not log it, so it appears once) and
shows the model in the Wave card's footer. If the blend fetch fails, `fetch_waves`
fails; it does not fall back to the land cell. Both requests go to
`marine-api.open-meteo.com`, so the allowed hosts file is unchanged.

Install `open-meteo-marine.config_fields.json` next to the `.wasm` file so
Settings can show the switch.

## Why this plugin is two files

`open-meteo-marine.go` holds all the parsing/HTTP logic and has no dependency
on `github.com/extism/go-pdk`, so `go test ./...` (below) runs on the plain
host Go toolchain with no TinyGo or wasm target needed. `main.go` holds only
the thin `//go:wasmexport` wrapper functions and is gated `//go:build tinygo`
so it's excluded from that plain host build (TinyGo defines the `tinygo` build
tag automatically; plain `go test` doesn't). Because of this split, always
build the whole package directory (`.`), not just `main.go` by name, as described
below.

## Building it

Requires only Docker (no local TinyGo install needed), pinned to
`tinygo/tinygo:0.41.1`, the same version pinned for this repo's
WASM test fixtures (see `backend/wasm_tide_provider_test.go`'s regeneration
comment), to avoid `:latest` drift. From the repo root:

```sh
docker run --rm -v $(pwd):/src -w /src tinygo/tinygo:0.41.1 sh -c "
  cd docs/examples/wave-plugins/open-meteo-marine &&
  go mod tidy &&
  tinygo build -o open-meteo-marine.wasm -target wasip1 -buildmode c-shared .
"
```

Use the trailing `.` to build the whole package directory. Naming `main.go`
alone would exclude `open-meteo-marine.go` and fail with
`undefined:` errors, since Go/TinyGo's single/multi-file build mode only
compiles the files explicitly listed.

This produces `open-meteo-marine.wasm` in this directory. Requires network
access inside the container (`go mod tidy` fetches `github.com/extism/go-pdk`)
and Docker with the `tinygo/tinygo` image available locally.

If you'd rather install TinyGo locally instead of using Docker, the equivalent
commands are:

```sh
go mod tidy
tinygo build -o open-meteo-marine.wasm -target wasip1 -buildmode c-shared .
```

Both the Open-Meteo and other wave plugins are also built and installed
automatically as part of Docker Compose startup (see the repo's compose files).
Use the manual build instructions to build or inspect this plugin separately.

## Installing it

Helmcentral discovers wave-provider plugins by scanning `plugins/waves/` at
startup (overridable via the `PLUGINS_WAVES_DIR` env var). To install this
plugin manually:

1. Copy the compiled `open-meteo-marine.wasm` into your `plugins/waves/`
   directory.
2. Create `plugins/waves/open-meteo-marine.allowed_hosts.json` next to it,
   containing:

   ```json
   ["marine-api.open-meteo.com"]
   ```

   This file is required for network access. A plugin with no companion
   `<name>.allowed_hosts.json` file gets no network access
   (Helmcentral's default-deny sandboxing). `marine-api.open-meteo.com` is the
   only host this plugin talks to.
3. Restart the Helmcentral container (or the dev backend). "Open-Meteo Marine"
   should now appear in the wave-provider dropdown in Settings, with no
   frontend changes required.

`plugins/waves/` lives at the repo root and is gitignored, like `backend-data/`,
because it contains operator runtime files. A fresh Helmcentral checkout ships
with no plugins active by default, to avoid contacting external APIs before a
provider is chosen.

## Testing

`main_test.go` unit-tests the parsing logic directly, entirely on the host Go
toolchain, with no TinyGo or WASM runtime needed:

```sh
cd docs/examples/wave-plugins/open-meteo-marine && go mod tidy && go vet ./... && go test ./...
```

## Endpoints this plugin uses

1. Wave data: `GET https://marine-api.open-meteo.com/v1/marine?latitude=<lat>&longitude=<lon>&hourly=wave_height,wave_direction,wave_period,wind_wave_height,wind_wave_direction,wind_wave_period,swell_wave_height,swell_wave_direction,swell_wave_period&timezone=auto&forecast_days=<days>&models=ncep_gfswave025`

   With the blend fallback on and a land cell, the same request is repeated
   without `&models=ncep_gfswave025`.

   Response includes `utc_offset_seconds` (how many seconds local time is ahead
   of UTC), hourly time arrays in naive local-time format
   ("2026-07-19T00:00"), and parallel arrays of wave measurements.

2. Sea temperature: `GET https://marine-api.open-meteo.com/v1/marine?latitude=<lat>&longitude=<lon>&current=sea_surface_temperature`

   Response includes current sea surface temperature (can be null for inland
   coordinates).

3. Ocean currents (`fetch_currents` only): `GET https://marine-api.open-meteo.com/v1/marine?latitude=<lat1>,<lat2>,...&longitude=<lon1>,<lon2>,...&hourly=ocean_current_velocity,ocean_current_direction&timezone=GMT&past_days=1&forecast_days=<days>`

   One request for every point. No `models=` param. Velocity is in km/h, direction in degrees toward which the
   current flows, and both can be null.

See Open-Meteo's [Marine Weather API
docs](https://open-meteo.com/en/docs/marine-weather-api) for full endpoint
details and coverage.

## Per-component direction and period

The query asks for `wind_wave_direction`, `wind_wave_period`,
`swell_wave_direction` and `swell_wave_period` alongside the combined figures,
because combined figures can hide crossing seas by merging two wave trains.

**Open-Meteo reports an absent component as three zeros, not null.** An hour
with no swell comes back with swell height 0, swell
period 0 and swell direction 0, and that last one is indistinguishable from a
genuine northerly swell if you read it on its own.

Verified against a live response at a vessel position:

```
time                  Hs     T   wwH   wwD   wwT   swH   swD   swT
2026-09-03T00:00     0.6   7.4   0.6    79   7.4   0.0     0   0.0
2026-09-03T18:00     0.5   7.7  0.26   109  2.35  0.44    65   7.7
```

The first hour is a single wind sea with no swell. The second is a
two-system sea. The host treats a **zero period** as the marker for "this
component is absent", so pass the provider's zeros through unchanged rather
than substituting anything. A plugin whose model does not carry these fields
at all should omit them; they arrive as zeros and the host reads that as no
per-component data rather than as waves out of the north.

## Missing combined figures are left out

`wave_height_m`, `wave_period_s` and `wave_direction_deg` are optional in the
plugin contract. When Open-Meteo returns null for one of them (the model has no
coverage for that hour or point), or something that is not a number, the plugin
**omits that key** from the hour. The host reads a missing key as "no value",
so an hour with no height is not checked as a flat calm, and an hour with no
direction is not read as a sea from due north.

A real zero is kept. Open-Meteo reports a flat sea as height 0, and that is a
figure. Only null and non-numeric entries are dropped.

The per-component fields above keep their zero encoding, because that is how
Open-Meteo itself marks an absent component.

A combined period of `0` is the exception to "a real zero is kept": a wave with
no period is not a wave, so the plugin leaves the whole hour's combined figures
out, and the host applies the same rule to any plugin. See "Land cells and
the blend fallback".
