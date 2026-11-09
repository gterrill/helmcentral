# Open-Meteo tide provider plugin (the default)

Helmcentral's default tide provider. It reads the modelled sea level from the
[Open-Meteo Marine API](https://open-meteo.com/en/docs/marine-weather-api),
which is free, needs no key and covers the world's coasts, and turns it into
highs and lows. Unlike [bom](../bom) and [noaa](../noaa) it has no stations: it
follows a position.

It's the model for a position-based tide plugin. The decision record is
[ADR 0273](../../../adr/0273-tides-come-from-a-global-model-by-default.md).

## What it declares

Two optional exports tell the host what kind of plugin this is. A plugin
without them is station-based on chart datum, which is what `bom` and `noaa` are.

| Export | Returns | Meaning |
| --- | --- | --- |
| `position_based` | `true` | No station catalog. The host passes `"<lat>,<lon>"` in place of a station id, rounded to the nearest 0.05 degree (two decimals). `search_stations` returns an empty list. |
| `height_datum` | `mean_sea_level` | Heights are metres from mean sea level, not chart datum. The host never works a depth at chart datum out of them. |

## What it does

`fetch_tide_chart` asks for eight days of hourly `sea_level_height_msl` (one day
back, seven forward) at the position, in GMT, and picks the sea cell nearest it
(`cell_selection=sea`). It then finds the highs and lows, which `open-meteo.go`
does so that the logic tests on the plain Go toolchain:

- A high or low is a run of equal readings with lower (or higher) neighbours on
  both sides. The service rounds to a centimetre, so a falling tide can hold
  one value for two hours. That is a slope, not a low.
- A single hour is refined with a parabola through it and its two neighbours, so
  the time and height fall between the hours. A run of equal readings is placed
  at its middle.
- Highs and lows alternate. Two of a kind in a row keep the more extreme, and a
  turn under 3 cm from the one before is dropped as rounding.
- Hours the model left null are skipped, and no turn is derived across a gap.
  An empty or all-null series is an error, never invented data.

The heights are often negative, and they include surge and air pressure, so they
won't match a published tide table. That's why the plugin declares its datum.

## Tests

```sh
cd docs/examples/tide-plugins/open-meteo
go test ./...
```

The fixtures in `testdata/` are real responses captured from the live API on
2026-11-09: Venice (Mediterranean, negative heights all week), the US Virgin
Islands (small, mostly diurnal Caribbean tide), Sydney (semidiurnal, where BOM
gives about 0.3 to 1.8 m above LAT against this plugin's -0.41 to 0.93 m from
mean sea level), and the error body for a latitude of 999.

## Building and installing

`packaging/build-plugins.sh` builds it with the rest of the bundled plugins. To
build it alone, with Docker, from the repo root:

```sh
docker run --rm -v $(pwd):/src -w /src tinygo/tinygo:0.41.1 sh -c "
  cd docs/examples/tide-plugins/open-meteo &&
  go mod download &&
  tinygo build -o open-meteo.wasm -target wasip1 -buildmode c-shared .
"
```

Copy `open-meteo.wasm` and `open-meteo.allowed_hosts.json` into `plugins/tides/`
and restart. The allowlist names one host, `marine-api.open-meteo.com`, and a
plugin with no such file gets no network access.

Only the rounded position leaves the boat, sent to that host.
