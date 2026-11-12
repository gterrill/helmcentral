# ADR 0284: Lightning Probability as a Plugin Kind

## Status
Accepted. The plugin and host landed first, then the chart bands: a four-step
violet band on the Cloud, Temperature & Rain chart and the Upper Air trace,
with the Met Office credit in each chart's key. Both are drawn for the boat
only; a planned stay shows no band and the key says why.

## Context

The forecast page says nothing about lightning. The surface weather
condition can read "thunderstorms" for a day, and the Upper Air trace marks
days with support for a surface low, but neither says how likely a strike
is over the next few hours, or where in a two-week trace the risk sits.

The Met Office publishes exactly that, free and keyless. Its Blended
Probabilistic Forecast (IMPROVER, blending the Met Office and ECMWF
ensembles) puts global gridded probabilities in a public S3 bucket,
`met-office-global-gridded-probabilities` in eu-west-2, under CC BY-SA 4.0.
One file of the `lightning_flash_accumulation` family, checked on
2026-11-12, holds:

- one variable,
  `probability_of_number_of_lightning_flashes_per_unit_area_above_threshold`,
  a 960 x 1280 float32 grid of 0 to 1 in steps of 1/1024, with no gaps;
- a threshold of 0 flashes per square metre, so a cell's value is the
  chance of at least one flash in that cell over the window;
- a regular latitude-longitude grid, 0.1875 by 0.28125 degrees (about
  21 x 31 km at the equator), longitude -180 to 180, latitude south to
  north;
- the window's start and end in `time_bnds`;
- the whole grid in one chunk, gzip with shuffle, about 334 KB.

A run arrives every six hours. Each run carries 3-hour windows every three
hours out to 14.5 days (116 files) and 1-hour windows over the first days.
Open-Meteo, which feeds every other forecast panel, has no equivalent: its
`lightning_potential` covers Europe only.

## Decision

### Lightning joins the plugin system as its own kind

`lightning` becomes a plugin kind beside weather, wave and upper air, with
the same file pair (`lightning_providers.go`, `wasm_lightning_provider.go`),
a registry with no native built-in, `GET /api/lightning-providers` and a
`ui.lightning_provider` setting. The first plugin, `ukmo-lightning`, reads
the Met Office bucket and is the default, because it needs no key.

It stays out of the weather plugin for the reason ADR 0071 kept upper air
out: WeatherKit and Open-Meteo answer the surface forecast and neither
answers this, so folding lightning into `fetch_forecast` would tie it to
whichever weather provider the operator picked.

### The plugin returns windows; the host owns the schedule

The contract is `fetch_lightning`: given a position and a span of time, the
plugin returns each window it has inside that span as
`{start, end, probability}` for the grid cell holding the position, plus
the run it read them from. It does not bucket, smooth or summarise.

The host asks in spans because one file costs a 334 KB download plus
decoding a 4.9 MB grid to read a single cell, and a whole run will not fit
in one call. The host walks the trace in 24-hour spans, eight windows each
and one call each, so a slow S3 response costs one span, not the panel.
Nobody waits on these calls, so they run under a 60 second background
budget rather than the 15 second default (`defaultWasmPluginTimeoutMS`),
the way coastline downloads do. Against the live bucket a window took
about a second; the plugin refuses more than eight windows in one call. A
refresh of the whole range is about 15 calls, so `GET /api/lightning` never
waits on them: it answers from what the host holds, starts a refresh in the
background when one is due, and says `refreshing: true` while it runs.

Each call is a fresh plugin instance, so the first call of a refresh finds
the latest complete run with two or three small bucket listings: the run
folders for today and yesterday, then whether a run's last window, 348
hours out, exists yet. A run folder appears hours before its last file
does, and a run without it is not used. The host passes the run that call
returns as the optional `run` input of the other calls in the same
refresh, so the listings are paid for once and every span of a refresh
reads the same run. The far end of the trace is that run's last window, 348
hours (14.5 days) after the run, not after now. Once the first span has
returned the run, the walk stops there and asks for no span that starts at or
after it, so the last span is never one the run cannot fill.

### Near windows follow every run, far windows once a day

Only the cell at the boat is wanted, but the grid is one compressed chunk,
so a ranged read cannot reach it. Every window costs its whole file. The
full range at every run would be about 39 MB a run and 155 MB a day, too
much for a boat on its own connection. The host therefore:

- refreshes the first 48 hours every six hours, the run cadence, about
  5 MB;
- refreshes the rest of the trace once a day, about 34 MB;

which comes to roughly 60 MB a day. The host cannot see a new run without
asking the plugin, so the near refresh runs on the six-hour clock rather
than on the run's arrival; a late run is fetched again six hours on.

The host holds one cell, the Met Office grid cell the boat is in, so swinging
at anchor never triggers a fetch. It asks the plugin about the centre of that
cell, never the raw position: a position that has drifted toward the edge of
the held cell would otherwise make the plugin read the neighbouring cell and
file its windows under the wrong one. A boat at anchor can wander across a
cell edge, so the host keeps the held cell while the position is within 0.05
degrees of latitude, and the same distance in longitude (about 5.5 km), of its
edge.

Leaving the cell by more than that margin drops everything held: the
windows, the errors and the near clock. The new cell's near spans are fetched
at once, about 5 MB, and its far spans at the next daily refresh, because the
far clock belongs to the schedule rather than to a cell. Returning to a cell
fetches its near spans again. One refresh runs at a time, and one ten minute
wait after a failure covers whatever cell the boat is in. A refresh still
running for the cell just left is let go, and its results are discarded
rather than merged into the new cell.

The host keeps the cell in `data/lightning_<provider>_state.json`, under
`data/` because that is the directory a container recreate keeps. It is a
cache of data the host can fetch again, so an unreadable file, or one in an
older shape, is logged and dropped. The host writes the file after releasing
its lock, so a slow disk never holds up a request.

Holding several cells would save the 5 MB when the boat returns, and an
earlier design did so. Per-cell refresh flags, retry clocks and errors, with
a shared far clock and eviction, produced bugs in two review passes, for a
saving a single boat on a single passage seldom uses.

### Shown on both forecast charts

- **Cloud, Temperature & Rain**: a thin band across the top of the chart,
  one block per 3-hour window of the selected day, shaded by probability.
- **Upper Air**: the same band across the top of the 16-day trace, so
  lightning risk reads against the trough days.

Both use 3-hour windows. The 1-hour files are not fetched: they would
triple the download for the first days and the charts gain little from
them.

### What it does not do

- It does not show live strikes. A probability over a cell of about
  20 x 30 km and a 3-hour window says nothing about the cell next door or
  the next ten minutes.
- It does not reach past the run's last window. The Upper Air trace runs to
  16 days and the lightning band stops at 14.5; the remainder reads as no
  data, never as zero.
- It raises no alarm.

## When it can't run

- **The bucket is unreachable or a run is late**: the host keeps the
  windows it already holds while they are still in the future, each
  carrying the run it came from, and the response carries the error as
  `refresh_error`. Past windows are dropped. With nothing held and no
  refresh running, the response is a 502; while a refresh runs it is a 200
  with no windows, `refreshing: true` and the last error. A range stops after
  three spans fail in a row, and no new refresh starts for ten minutes, even
  if the boat has moved to another cell.
- **Part of the far range fails**: the failed spans stay gaps until the next
  daily refresh, which is not brought forward. `refresh_error` keeps
  reporting them until a later far refresh completes or the boat leaves the
  cell; a near refresh does not clear it.
- **The boat leaves the cell**: the old windows are gone at once and the new
  cell shows nothing until its near spans arrive, with `refreshing: true`.
  Returning to the old cell fetches its 48 hours again, about 5 MB.
- **A window file is missing from a run**: that block is empty, not
  interpolated from its neighbours. Any other bad answer from the bucket,
  or a file the reader was not written for, fails the span.
- **No position, or the -1,-1 or 0,0 placeholder**: no fetch, and a 502,
  the same gate `waveForecast` uses.
- **No lightning plugin installed and none chosen**: an empty list, and
  neither band is drawn. A chosen provider that is not installed is a 502.

Lightning has no provider card in Settings, as upper air has none: the
provider is chosen with `ui.lightning_provider`.

## What leaves the boat

Anonymous GETs to `met-office-global-gridded-probabilities.s3.eu-west-2.amazonaws.com`:
bucket listings to find the latest run, and whole-grid file downloads. The
boat's position never leaves it; the plugin picks the cell from the
downloaded grid.

## Consequences

- The Met Office credit, "Lightning probability: © Crown copyright, Met
  Office, CC BY-SA 4.0", goes with the forecast credits.
- `ukmo-lightning` carries its own HDF5 reader, written for this file's
  layout alone: superblock version 2, a version 2 object header, and the
  grid in one chunk under gzip and shuffle. It finds the grid by its
  `long_name` attribute and stops with an error on anything it was not
  written for, so a change at the Met Office surfaces as a failed fetch,
  never as wrong numbers.
- The same bucket holds hail, freezing rain, visibility and cloud-base
  height in the same layout. A later kind could reuse the reader.

### Rejected: a general NetCDF library

`github.com/batchatco/go-native-netcdf`, a pure-Go NetCDF4/HDF5 reader,
reads the file and builds for `wasip1` under the standard Go compiler. The
plugins are built with TinyGo, and there it panics at run time:
`reflect.SliceOf` is unimplemented. Decoding in the backend with the
library would work, but it would make lightning the one plugin kind whose
default provider lives outside a `.wasm` file. Forking the library to avoid
`SliceOf` would leave us maintaining a fork against whatever TinyGo gap
came next.

## Related

- ADR 0018: weather and wave providers, the template for this kind's shape.
- ADR 0071: upper air as its own provider, and the 16-day trace the band
  sits on.
- ADR 0091: the last new plugin kind, and the `pluginProviderRegistries` table
  this one extends.
