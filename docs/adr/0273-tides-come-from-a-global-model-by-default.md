# ADR 0273: Tides Come From a Global Model by Default

## Status

Accepted (2026-11-09). Amends [ADR 0033](0033-remove-storm-glass-tides-plugin-only.md):
tides now have a default provider, and the position-based plugin that ADR left
open is built. Every other rule in ADR 0017 and ADR 0033 carries over.

## Context

BOM covers Australia and NOAA covers the United States. A boat in the
Mediterranean or the Caribbean got no tide at all, and the Depth & Tide tile
showed an error until the operator wrote a plugin. ADR 0033 refused a
coordinate-based default because a global model standing in for station
predictions could look authoritative and be wrong. That concern holds. It does
not outweigh a dashboard with no tide anywhere outside two countries, so long
as the model's limits are said wherever its heights appear.

The Open-Meteo Marine API serves `sea_level_height_msl`: hourly modelled sea
level on a grid of about 1/24 degree, with no key. Checked live on 2026-11-09:

- **The heights are from mean sea level, not chart datum.** Venice ran -1.02 to
  -0.08 m over a week, Cannes -0.61 to -0.39, the US Virgin Islands 0.30 to
  0.55, Sydney -0.41 to 0.93 where BOM gives about 0.3 to 1.8 above LAT.
- **They include non-tidal effects**, such as surge and air pressure.
- **They are an hourly series**, not a list of highs and lows.

Three places in the code read a tide height as a height above chart datum: the
datum depth the Anchor Watch works out for Save depth, the anchorage depth
checks, and the "-1 means not published" test on the tide payload. Feeding them
mean-sea-level heights would give a wrong depth, not an approximate one.

## Decision

### 1. A plugin, `open-meteo`, in `plugins/tides`

It lives in `docs/examples/tide-plugins/open-meteo` and ships with the other
reference plugins. It has no catalog, so `search_stations` returns an empty
list. `fetch_tide_chart` asks for eight days of hourly sea level (one back,
seven forward) and derives the highs and lows itself:

- A turn is a run of equal readings with lower neighbours on both sides (a
  high) or higher on both (a low). The service rounds to a centimetre, so a
  falling tide can hold one value for two hours, and that is a slope, not a low.
- A turn on a single hour is refined with a parabola through it and its two
  neighbours, giving a time and height between the hours.
- Highs and lows alternate. Two of a kind in a row keep the more extreme, and a
  turn that moves less than 3 cm from the one before is dropped as rounding.
- Hours the model left null are skipped. A turn is never derived across a gap.
  An empty or all-null series is an error. The plugin invents nothing.

### 2. Two optional metadata exports

Following the pattern of `place_name_at` and `marina_details`, a plugin
declares itself by exporting a function. Absent means what BOM and NOAA already
are, so neither changes.

| Export | Returns | Absent means |
| --- | --- | --- |
| `position_based` | `"true"` or `"false"` | Station-based |
| `height_datum` | `"chart_datum"` or `"mean_sea_level"` | `chart_datum` |

A `height_datum` the host does not know fails the plugin's load, because
guessing would give the operator a wrong depth. The host reads both on the
throwaway instance used for `id()` and `name()`, and the tide providers
endpoint lists them.

### 3. Position-based providers have no station

This is the design ADR 0033 recorded. The host passes `"<lat>,<lon>"` as the
station id, rounded to the nearest 0.05 degree (about 5 km), so a boat at anchor, or
making a few miles, stays on one cache entry. The host wraps such a plugin once, at
registration, in an adapter that meets the station contract: its nearest station is
the point, and its chart is stamped with the point and its zone.

- **Where the point comes from.** The boat's position, or the place named in
  the request (a planned stay), as `explicitForecastPoint` already supplies.
- **At anchor, the anchor's point.** While an anchor watch is set, the point is
  the watch's anchor position, read fresh because Adjust can move it. A boat
  swinging near a rounding line would otherwise flip the point, and with it the
  cache key, mid-stay. A watch with no usable anchor position is an error, not a
  quiet use of the boat's fix. One function resolves this, so the tile, the
  low-water check, Mate and the boat chart agree.
- **The server picks the tide.** `/api/tide-chart` with no provider and no
  station is the configured chart: the provider in force (blank means the
  default) with its saved station, or for a provider that follows a position the
  point above, through the same function the tide tile uses. With `lat` and `lon`
  it is the tide at a place, from the nearest station or the place's point. The
  screens send no provider and no station, so they never handle a station for a
  provider that follows the boat, and the chart says which provider answered
  (`provider_name`), `position_based` and `height_datum`.
- **Nothing synthetic is saved.** The auto-update of the saved station skips such
  a provider, and a settings save that sets a station while one is in force is
  refused. A station provider with none chosen answers `no_station`, and the
  forecast shows the station picker.
- **What the screens show.** The station is the point, named "Boat's position",
  or "Selected place" for a place. The host replaces whatever station the
  plugin returned, and puts the zone found at the point on it.
- **No position, no tide.** A missing, `-1,-1` or `0,0` position is an error
  naming the cause. The plugin is not asked, and no earlier position is used.
- **Settings.** `buildTideToday` does not need `tide_station_id` for such a
  provider. The Settings tide section hides the station fields and says tides
  follow the boat's position. The forecast section shows no station picker and
  never saves a station for it.

### 4. The default

A blank `ui.tide_provider` resolves to `open-meteo` in one function,
`resolveTideProviderFromUI`, used by `buildTideToday` and by Mate's tools. The
Settings card for it shows active when nothing is saved. An install that already
chose a provider keeps it. If the plugin is not installed, the error names
`plugins/tides`.

### 5. The datum travels with the tide

- **On the wire.** The tide payloads (`/api/tide-today`, `/api/tide-chart`) and
  the provider list carry `height_datum`.
- **Never a depth.** `computeDatumDepth` returns no depth, with a reason the
  operator can act on, when the height at the drop was from mean sea level. The
  Anchor Watch stores `planning_tide_datum` beside `planning_tide_height_ft`
  (absent means chart datum, so no stored watch changes). Save depth reads the
  reason and says why it cannot save.
- **Rise and fall still work.** Low-water clearance and the rode needed at high
  water use differences between heights, so the datum cancels. The rode planner
  compares a stored height and a forecast one only when both are from the same
  datum.
- **The anchorage checks skip.** Clearance and rode at the lowest and highest
  water in a stay add a tide height to a depth at chart datum. With a tide from
  mean sea level they are skipped with that reason.
- **Mate.** `get_tides` and `anchorage_details` return `height_datum` and, for
  mean sea level, a `datum_note` saying the heights are not a depth. The tool
  rules tell Mate to say so. A modelled tide names no station and no distance.
- **Screens.** Anywhere heights show, mean-sea-level ones are labelled "from
  mean sea level".

### 6. The "not published" sentinel

The tide payload marks a missing height as exactly -1 ft. A mean-sea-level
height of exactly -1 ft is possible. The host moves a real one off it by one
unit in the last place (`tideHeightFtOnTheWire`), so -1 still means none on every
datum, and the browser treats a negative height as real only when
`height_datum` says mean sea level.

## What leaves the boat

The plugin sends the rounded position to `marine-api.open-meteo.com`, and
nothing else. No key. Its allowlist file names that one host.

## When it fails

- **The service is down or answers an error.** The plugin returns the service's
  own reason (`Latitude must be in range of -90 to 90`), and the host serves a
  stale cached chart if it holds one, as for every tide plugin, marked cached.
- **No sea cell near the position** (a berth far up a river). The series is
  empty or all null, and the tide section says so. It does not borrow the
  nearest water.
- **The plugin is missing.** The tide section names `plugins/tides`.

## What it is not

It is not a tide table. The figures are a model of sea level, surge included,
on a grid that can miss a harbour's own tide. Where BOM or NOAA has a station,
that station is better, and the operator picks it in Settings. Nothing here
changes a depth at chart datum into anything else.

## Consequences

- A boat anywhere on a coast has a tide before opening Settings.
- A fresh install of the plugin bundle needs no choice for tides.
- The tide tile can show negative heights, so the tile, the rode planner and the
  Anchor Watch read "unknown" by datum rather than by sign.
- Save depth stays unavailable on mean-sea-level tides until a chart-datum
  source is chosen. It is the honest answer: a depth at chart datum needs a tide
  above chart datum.

## Related

- ADR 0017: Sandboxed WASM Plugin Tide Providers
- ADR 0033: Removing Storm Glass, Tides Become Plugin-Only (its "no default"
  stance reversed)
- ADR 0063: Planning Depth Captured at Set (the stored tide height gains a datum)
- ADR 0246: Anchor Watch Saves a Sounded Depth (the datum depth it saves)
- ADR 0272: The Low-Water Clearance Is Worked Out on the Server (rise and fall
  only, so unchanged)
