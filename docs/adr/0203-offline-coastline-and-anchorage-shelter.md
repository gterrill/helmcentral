# ADR 0201: Anchorage Shelter Is Estimated From Charted Shoreline

## Status

Accepted. References ADR 0091 (a provider returns raw features, the host
computes all geometry), ADR 0101 (the osm-overpass plugin is the only thing
that speaks Overpass, and its optional exports), ADR 0172 (the Mate tool
registry).

## Context

Choosing an anchorage against a forecast turns on one question: which wind
directions is it sheltered from? Mate answered that from the model's general
knowledge of a named place, which is thin for most bays and wrong in the ones
that matter, and it had no way to look. Cruising guides and crowd-sourced
anchorage sites carry the answer for popular spots only.

OpenStreetMap already holds the coastline and reef outlines. Casting rays
over them gives a defensible first estimate for any position in the world,
with no per-anchorage data.

### Why not noforeignland

Noforeignland publishes per-anchorage shelter sectors drawn by the crowd, and
it was the obvious source. It is not usable from the boat: the site sits
behind a Cloudflare challenge that a server-side client cannot pass, the API
payload is encrypted, and its robots content signals do not grant `ai-input`,
so feeding it to Mate is not permitted whatever the technical route. Its data
is used here only as a hand check on the two validation cases below.

## Decision

### Algorithm

All of it is pure geometry in `backend/shelter.go`, tested against two raw
Overpass captures in `backend/testdata/shelter/` (gzipped, cropped to 16 nm).

1. **Projection.** Equirectangular around the anchor, in metres
   (`x = dLon * cos(lat0) * R`, `y = dLat * R`, R = 6371 km).
2. **Obstacles.** Every segment of `natural=coastline` ways and
   `natural=reef` ways, plus the outer members of `natural=reef` relations.
3. **Fetch.** From the anchor a ray is cast every 10 degrees. The distance to
   the first segment it crosses (ignoring hits within 1 m) is the fetch: how
   far wind from that bearing has run over water. The longest ray is 15 nm;
   a ray that finds nothing is reported open (15 nm, flagged `open`). Bearings
   are where the wind comes FROM.
4. **Window rule.** A bearing is sheltered only if the fetch on every ray
   within 30 degrees either side (seven rays) is at most 1.5 nm. Without the
   window, bearings running along a shore read as sheltered: at Stonehaven
   (Hook Island) bearing 000 hits land at 1.0 nm, but 330 is open water. The
   window is what makes the north end of the arc stop where the land stops.
5. **Grade.**
   - `sheltered`: window maximum at most 1.5 nm.
   - `partial`: not sheltered, but the bearing's own fetch is at most 5 nm.
     This is the fringe of a shelter (the bearings within a window width of
     open water) and an island a few miles off, such as Hayman Island at
     2.2 to 2.6 nm from Stonehaven.
   - `exposed`: everything else, including all open rays.
   - `reef`: see below.
6. **Reef case.** If the anchor is inside a closed reef ring, the distance to
   the reef edge is not shore, it is reef width. Containment is a
   ray-crossing parity test on closed `natural=reef` rings only (the smallest
   one containing the anchor wins). Coastline ways are open fragments, so
   parity on them is meaningless, and a closed coastline ring (an island) is
   land, never a reef. Bearings whose first obstruction is that ring's edge
   are graded `reef`, with the reef width upwind as the fetch. Reef shelter is
   never better than partial and depends on the tide: the reef breaks the sea
   near low water and covers as the tide rises. The summary says so.
7. **Arcs.** Consecutive bearings with the same grade fold into compass arcs,
   cyclic through north, reported as "sheltered from NNE through S
   (030°–190°)". The endpoints are bearings, each of which stands for the
   10 degrees centred on it. The tool returns the arcs, the per-bearing table
   (bearing, fetch, grade, what blocks it, with an island or reef name when
   OSM tags one) and a caveat.

The thresholds (1.5 nm, 5 nm, 30 degrees, 15 nm) were tuned on the two cases
below and are constants at the top of `shelter.go`.

### Validation

Stonehaven, Hook Island (-20.0976, 148.9025). The estimate gives sheltered
030°–190°, partial 200°–220° and 340°–020° (Hayman Island to the NNW),
exposed 230°–330°. Noforeignland's crowd data says about 020°–155°. The
estimate overshoots the south end. The likely cause is that the charted coast to the
south is close, so the rays are short, while real swell wraps round the point
and the crowd marks it open from that side. That is a known limitation of a
pure line-of-sight fetch, which cannot see diffraction or swell bending, and
the tool's caveat says so. The test pins what the
algorithm gives, not the crowd figure.

Fairey Reef (-19.5434, 148.9394). The anchor lies inside OSM reef way
300537195. The nearest edge is 0.37 nm to the west (the narrowest reef
upwind) and the reef is widest, 1.0 to 1.7 nm, to the east, south east and
south. All 36 bearings are `reef`.

### Data comes through the plugin

The backend still has no Overpass client (ADR 0101). The osm-overpass plugin
gains an optional export, `shoreline_near({lat, lon, radius_m})`, returning
`{"lines": [{"id", "kind": "coastline"|"reef", "name", "closed", "points":
[[lat, lon], ...]}]}`: raw geometry, no distances (ADR 0091).

- One Overpass round trip, using the global `[bbox:...]` setting so the
  server starts from a spatial index (a tag-first scan of `natural=coastline`
  times out on the mirrors). Coastline is output clipped to the box with
  `out geom(bbox)`, reefs whole, so a reef ring stays closed.
- The boat link is slow (about 5.5 s for a trivial query against a 15 s plugin
  budget), so the plugin also drops coastline outside the box and points
  closer than 15 m to the last one kept. Stonehaven comes to about 340 KB.
- A box that would cross the antimeridian or a pole is an error, not an
  unbounded scan. Any upstream failure, rate limit or Overpass runtime-error
  `remark` is a call error. A reef ring with a missing node is an error.
- `backend/wasm_poi_provider.go` validates the output strictly (kind, point
  count and range, a ring marked closed really closing) and the plugin flags
  the export as supported only when it is present. A place-names plugin
  without it makes the tool return a clear error naming `shoreline_near`;
  there is no fallback to Mate's general knowledge or to another source.

### Caching

Raw lines are cached in memory per 0.02 degree cell for seven days (16 cells,
oldest evicted); shoreline barely changes and the fetch is slow. The plugin is
asked for 15 nm plus 1.5 nm so every ray from anywhere in the cell is covered,
and the estimate is computed from the true position over the cached lines.
Errors are never cached.

### Mate

`anchorage_shelter({lat, lon})` is registered after `find_places`. Its
description says it is an estimate from charted shoreline that does not
account for wind bending round headlands, gusts down valleys or swell
wrapping, so it is to be combined with the forecast wind direction. The
pilotage prompt tells Mate to call it for each candidate anchorage when
assessing against a forecast and to compare the sheltered arcs with the
forecast wind directions over the stay, saying it is an estimate.

## Consequences

- Mate can say which wind directions an unfamiliar anchorage is sheltered
  from, with the figures behind the claim in the result.
- The estimate is wrong in known ways: it overshoots where swell wraps, it
  cannot see hills or valleys, and it is only as good as OSM's coastline. A
  position OSM draws as open sea is reported exposed all round.
- The plugin has to be rebuilt and redeployed for the tool to work. Until it
  is, the tool reports the missing export and the rest of Mate is unaffected.
- The public Overpass servers are a moving target. When this was built,
  `overpass.openstreetmap.fr` refused a Mac with 403 "only available to
  white-listed usages" and `overpass-api.de` answered but took 30 to 50 s for
  a coastline query during busy periods; the 12 s server timeout then
  surfaces as an explicit error rather than a slow tool.
