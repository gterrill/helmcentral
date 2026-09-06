# open-meteo-upper

Helmcentral's reference **upper-air provider**: free, keyless 500mb forecasts
from Open-Meteo, out to 16 days.

## Why this is not part of the weather plugin

Surface and upper-air forecasts can come from different providers. Apple
WeatherKit provides surface forecasts but no pressure-level data, so including
upper air in `fetch_forecast` would prevent using it alongside a separate
upper-air provider.

Keeping them separate means you can run WeatherKit for weather and this for the
upper pattern.

## What it returns

Raw hourly values only, SI units, RFC3339 timestamps:

| Field | Unit | Notes |
| --- | --- | --- |
| `geopotential_height_500_m` | m | The 500mb surface. Around 5900m in the tropics, lower toward the poles. |
| `geopotential_height_1000_m` | m | Subtract from the above for thickness. |
| `wind_speed_500_ms` | m/s | Jet strength overhead. |
| `temperature_500_c` | °C | Cold aloft over warm sea drives instability. |

Day-bucketing, percentile calculation and trough detection are handled by the
host (`backend/upper_air.go`). Duplicating them in the plugin could produce
results inconsistent with the host.

## Two things about the upstream API

The following behaviours were verified against a live response.

**`wind_speed_unit=ms` applies to pressure-level winds too**, not just the
surface wind. So nothing in this plugin converts wind speed. Without that
parameter, `wind_speed_500hPa` comes back in km/h.

**Missing values are encoded as null.** Nulls occur at the end of a 16-day
run, and a model without pressure levels omits the arrays entirely. Both map
to zero here, the host contract's marker for no reading. A 0m 500mb geopotential
height is not a valid measurement, unlike a zero temperature.

No `models=` parameter is pinned. The default blend carries 500hPa across the
full 16 days (383 of 384 hourly steps non-null, the one gap being the final
hour).

## Building

Requires only Docker, pinned to `tinygo/tinygo:0.41.1` to avoid `:latest`
drift. From the repo root:

```sh
docker run --rm -v $(pwd):/src -w /src tinygo/tinygo:0.41.1 sh -c "
  cd docs/examples/upper-air-plugins/open-meteo-upper &&
  go mod tidy &&
  tinygo build -o open-meteo-upper.wasm -target wasip1 -buildmode c-shared .
"
```

Use the trailing `.` to build the whole package directory. Naming `main.go`
alone would exclude `open-meteo-upper.go` and fail with
`undefined:` errors.

## Installing

Copy both files into `plugins/upper-air/`:

```sh
cp open-meteo-upper.wasm open-meteo-upper.allowed_hosts.json ../../../../plugins/upper-air/
```

Then set `ui.upper_air_provider: open-meteo-upper` in `settings.yaml`, or leave
it unset to use the default. An unset provider with nothing installed is not
an error. Upper air is optional; without it, there is no
500mb section on the forecast page.

## Testing

`main.go` holds only the `//go:wasmexport` wrappers and is gated
`//go:build tinygo`, so the parsing logic in `open-meteo-upper.go` builds and
tests under the plain host toolchain:

```sh
go test ./...
```
