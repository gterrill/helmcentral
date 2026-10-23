# ADR 0203: Anchorage Shelter Is Estimated From an Offline Coastline

## Status

Accepted. References ADR 0091 (a provider returns raw features, the host
computes all geometry), ADR 0101 (the osm-overpass plugin is the only thing
that speaks Overpass, and its optional exports), ADR 0141 (one business
database), ADR 0172 (the Mate tool registry), ADR 0177 (feature routes), ADR
0178 (one reader for own-ship telemetry) and ADR 0184 (the transaction
convention).

This record first described a live design: the shelter tool asked Overpass
for the coastline around the anchorage at the moment Mate asked, and cached
the answer in memory. That was built and measured, found wanting (see "Why
offline"), and replaced before it shipped. The live design is gone; nothing
below describes a fallback to it.

## Context

Choosing an anchorage against a forecast turns on one question: which wind
directions is it sheltered from? Mate answered that from the model's general
knowledge of a named place, which is thin for most bays and wrong in the ones
that matter, and it had no way to look. Cruising guides and crowd-sourced
anchorage sites carry the answer for popular spots only.

OpenStreetMap already holds the coastline and reef outlines. Casting rays
over them gives a defensible first estimate for any position in the world,
with no per-anchorage data.

### Why offline

The first build fetched the shoreline live through the osm-overpass plugin.
It does not work well enough on a boat. A coastline query for one anchorage
took 30 to 50 seconds from the public Overpass server in busy hours, against
the 15 second budget a plugin call gets while Mate is answering a question,
and the boat's link is slow and sometimes absent. A tool that times out half
the time is worse than no tool: Mate has to say "I could not check" in the
middle of planning a passage. The shoreline barely changes. The right shape is
to fetch it once, ahead of need, when the boat has a link and nobody is
waiting, and keep it on board.

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

### The offline coastline

The coastline is held on board as **coverage squares**: 1 degree of latitude
by 1 degree of longitude on the integer-degree grid, named by the south-west
corner (`backend/coastline_squares.go`). Terms are in `GLOSSARY.md`.

- **Coverage** is every square any part of which lies within 200 nm of the
  boat, plus every square downloaded earlier. Squares are never dropped
  because the boat moved on; only the operator's Clear coverage removes them.
  When the boat moves, only squares newly in range are downloaded. 200 nm is
  about 40 to 60 squares, and a square of the Whitsundays is about 450 KB as
  the plugin returns it (about 150 KB stored).
- **Fixed squares** rather than a circle around the boat, so a download is
  never repeated for a boat that wobbles about an anchorage, each square can
  be dated and updated alone, and "what do we hold" is a set of integers.
- A circle across the 180th meridian or over a pole is an **error**, not a
  partial answer: squares do not wrap, and covering one side only would leave
  an anchorage with no shoreline across the line that reads as open water.
  The page and the tool both say so.

### Why Overpass, through the plugin

The alternatives were tried or costed before settling:

- **Geofabrik extracts** are per country or region. Australia is a 924 MB
  download with no state split, to serve a boat that wants 40 squares.
- **osmdata.openstreetmap.de land polygons** are the right shape for land but
  were unreachable from the boat's network, and carry no reefs, which the
  shelter estimate needs (the reef case in the algorithm above).
- **Overpass** answers exactly the square asked for, includes
  `natural=reef`, and can be asked what changed. The backend still has no
  Overpass client (ADR 0101): the osm-overpass plugin gains two optional
  exports and the Overpass server stays that plugin's one setting.

`shoreline_square({south, west, north, east})` returns `{"lines": [{"id",
"kind": "coastline"|"reef", "name", "closed", "points": [[lat, lon], ...]}]}`:
raw geometry, no distances (ADR 0091).

- One Overpass round trip, using the global `[bbox:...]` setting so the
  server starts from a spatial index (a tag-first scan of `natural=coastline`
  times out on the mirrors). Coastline is output clipped to the square
  (`out geom(bbox)`), so neighbouring squares never repeat it. Reefs are
  output **whole**, because the shelter calculation needs a closed ring to
  know whether the anchor is inside one. A reef that crosses a square edge is
  therefore stored in each square it touches, and the store dedupes reefs by
  OSM id when it assembles squares for a query.
- The plugin drops points closer than 15 m to the last one kept.
- An Overpass `remark` runtime error, a rate limit, a reef with a missing
  node, and a box over the antimeridian, a pole, or larger than 2 degrees are
  call errors. `lines: []` means open sea and is a valid, stored square.

`shoreline_changes({south, west, north, east, since})` returns `{"changed":
n}`: how many coastline and reef ways and relations in the square carry an
OSM timestamp newer than `since` (`(newer:"...")` with `out count;`). The
host gives `since` as the square's download time. It cannot see a node moved
inside an unedited way, or a deleted element; it is a hint that an update is
worth the operator's time, not a diff.

The host marks the plugin as supporting the feature only when it exports both
(`SupportsShoreline`); an older plugin makes the page report that it needs
updating, and the worker downloads nothing.

**Background calls get their own budget.** Mate's interactive plugin budget
(`WASM_PLUGIN_TIMEOUT_MS`, 15 s) is right for Mate and wrong for a square
(30 to 50 s on a good day). Raising the global would let any slow plugin stall
a question. The host instead sets an explicit per-call budget
(`callWithin`, 120 s, `coastlineBackgroundCallBudget`) on these two exports
only. The plugin asks Overpass for 100 s (60 s for the count).

### Storage

`coastline_squares` in `helmcentral.sqlite` (ADR 0141), one row per square:
the square's integer corner, status (`ready` or `failed`), download time,
edits since and when they were last checked, last error, attempt count and
next attempt time, and the geometry as one gzipped JSON blob of lines. A square
is only ever read or replaced whole. The store keeps its own handle and
mutex, like the alarm log, since these are not record tables, but writes
use the ADR 0184 shape: a `RunTx` that holds the mutex across the
transaction and commits on nil. A square that failed with nothing held is a
`failed` row with no blob so the page can show it; one that fails an update
keeps its geometry and stays `ready`.

### The worker

`backend/coastline_manager.go` runs one step at a time, with a 10 second
pause after any step that called the plugin (Overpass usage policy), and a
30 second look when idle. A step does the first of:

1. a square the operator asked to update;
2. the nearest wanted square not held, with a failed square skipped until its
   backoff has elapsed. Wanted squares are in nearest-first order;
3. the weekly edit check of a held square (oldest check first).

A failed square records its error and retries after 5 minutes, doubling to a
cap of 6 hours. It stays on the page with its error. Nothing runs, and the
plugin is never touched, while the switch is off.

A failure of the whole map server (a 429 or rate-limit page, a 5xx, a refused
connection, a timeout, or a 401, 403 or 451 turning the boat away) is not the
square's fault. 403 is the one that matters: a server that answers "only
available to white-listed usages" gives every square the same answer, so
treating it as per-square would march through about 50 squares and mark them
all failed. 401 (credentials needed) and 451 (blocked for legal reasons) are
the same kind of refusal and take the same pause, but their status text
differs: "The map server turned the boat away. Choose another Overpass server
in the plugin settings", because the fix is a different server, not the
network. 400 and other 4xx stay with
the square. It pauses the job instead:
no square is tried until the longer of the server's Retry-After and the same
5 minute doubling backoff has passed, the square is not marked failed (an
operator-requested update stays queued, unless a Clear or the switch going
off happened while the download was in flight: then the square is dropped, not
queued again; the check and the requeue happen under one lock, and the pause
is also dropped once the switch is off), and the status says the server is
refusing and when the worker will try again. The plugin carries Retry-After in
its error text; the host reads the text because it is all that crosses the
plugin boundary. Status is served from the position and switch the worker last
read, refreshed on a settings save, not fetched per request.

**Position.** The top-up pauses without a valid own-ship fix. Valid means no
critical GNSS alert and not the `-1,-1` sentinel pair or null island, not just
a coordinate in range (`hasUsableVesselPosition`, ADR 0178; the Gulf of
Guinea once collected a real forecast that way). The page says "Waiting for a
position fix". Updates and weekly checks need no position and carry on. The
tool keeps answering from the squares held.

### Edits since, and updating

Each held square is asked weekly whether its coastline or reefs have been
edited since it was downloaded, and the count is stored as its **edits since**.
Edits are **applied only when the operator taps Update**, which downloads the
squares with edits again. They are not applied silently, because an edit can
be a vandal's as easily as a surveyor's, and Mate's answer should not change
under an operator who has just read it. New coverage, on the other hand,
downloads automatically: it adds shoreline where there was none.

### Off by default

Offline coastline has its own switch, `offline_coastline.enabled`, default
off, saved through the settings Save bar. It spends the boat's data allowance
in the background, so an operator opts in. Off means no downloads and no
checks.

### Unavailable means an error

`anchorage_shelter` reads only what is held. When it cannot answer it says
which of these it is, and Mate relays it:

- the switch is off (and says where to turn it on);
- the position is outside coverage;
- the position's square has not downloaded yet, or failed (with the reason);
- a neighbouring square its rays reach is missing, named with its state.

**Shelter needs neighbouring squares.** Rays go 15 nm, so a position near a
square edge needs the adjacent squares (up to four at a corner). The tool
requires every square within 15 nm of the position, using the same square
arithmetic the worker uses, and names the ones missing. There is **no live
Overpass fallback anywhere**: a masked fallback would turn a missing square
into a confident wrong answer, or into the 30 to 50 second wait this design
exists to remove.

### Mate

`anchorage_shelter({lat, lon})` is registered after `find_places`. Its
description says it is an estimate from charted shoreline that does not
account for wind bending round headlands, gusts down valleys or swell
wrapping, so it is to be combined with the forecast wind direction. The
pilotage prompt tells Mate to call it for each candidate anchorage when
assessing against a forecast and to compare the sheltered arcs with the
forecast wind directions over the stay, saying it is an estimate. The result
carries the download date of the oldest square it used, so Mate can say how old
the shoreline is. If the tool returns an error the prompt tells Mate to say
shelter could not be checked, and why.

## Consequences

- Mate can say which wind directions an unfamiliar anchorage is sheltered
  from, with the figures behind the claim, with no internet at the time, once
  the squares are downloaded.
- Nothing is downloaded until the operator turns the switch on, and after that
  the first fill takes as long as 40 to 60 squares at one every ten seconds
  plus Overpass's own time, an hour or so on a slow link. Until it has
  filled, the tool says the area is still downloading.
- The estimate is wrong in known ways: it overshoots where swell wraps, it
  cannot see hills or valleys, and it is only as good as OSM's coastline. A
  position OSM draws as open sea is reported exposed all round.
- The same squares will serve other uses later (an offline chart land layer,
  anchor swing-room checks, distance to land), which is why the feature has its
  own settings section and its own vocabulary rather than living under Mate.
- The plugin has to be rebuilt and redeployed. The page reports an older copy
  rather than failing quietly. `shoreline_near`, from the unreleased first
  build, no longer exists.
- Areas across the 180th meridian and near the poles are not supported, and
  say so.
- The public Overpass servers are a moving target. `overpass.openstreetmap.fr`
  refused a Mac with 403 "only available to white-listed usages"; the pause
  and its visible reason are how an operator sees a server that has turned
  them away.
