# open-meteo-upper

Helmcentral's reference **upper-air provider**: free, keyless 500mb forecasts
from Open-Meteo, out to 16 days.

## Why this is not part of the weather plugin

The two data sources rarely come from the same place. Apple WeatherKit makes an
excellent surface forecast and carries no pressure levels at all, so folding
upper air into `fetch_forecast` would force a boat to choose between a good
surface forecast and any upper-air data whatsoever.

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

It does **not** bucket by day, work out percentiles or decide what counts as a
trough. The host does all of that (`backend/upper_air.go`), so duplicating any
of it here would risk drifting from the host's behaviour.

## Two things about the upstream API

Both verified against a live response rather than assumed, and both matter to
anyone writing another upper-air plugin.

**`wind_speed_unit=ms` applies to pressure-level winds too**, not just the
surface wind. So nothing in this plugin converts wind speed. Without that
parameter, `wind_speed_500hPa` comes back in km/h.

**Absence is encoded as null, and nulls arrive.** The tail of a 16-day run
carries them, and a model without pressure levels omits the arrays entirely.
Both land on zero here, which is the host contract's marker for "no reading":
a 0m 500mb geopotential height is not a measurement of anything, so the zero is
unambiguous in a way it would not be for a temperature.

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

Note the trailing `.` (build the whole package directory), not `main.go` —
naming `main.go` alone would exclude `open-meteo-upper.go` and fail with
`undefined:` errors.

## Installing

Copy both files into `plugins/upper-air/`:

```sh
cp open-meteo-upper.wasm open-meteo-upper.allowed_hosts.json ../../../../plugins/upper-air/
```

Then set `ui.upper_air_provider: open-meteo-upper` in `settings.yaml`, or leave
it unset — this is the default, and an unset provider with nothing installed is
not an error. Upper air is optional, and a boat without it simply has no
500mb section on the forecast page.

## Testing

`main.go` holds only the `//go:wasmexport` wrappers and is gated
`//go:build tinygo`, so the parsing logic in `open-meteo-upper.go` builds and
tests under the plain host toolchain:

```sh
go test ./...
```
