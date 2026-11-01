# ADR 0244: Mate assesses an anchorage

## Status

Accepted (2026-11-01). Replaces the `anchorage_shelter` tool of
[ADR 0203](0203-offline-coastline-and-anchorage-shelter.md), whose estimate it
keeps. Follows [ADR 0238](0238-mate-evaluates-a-marina.md) (an optional plugin
export read into a tool result) and [ADR 0242](0242-marina-card.md) (the card
built from the full data before the model's copy is shrunk). The draft check of
[ADR 0243](0243-an-anchorage-shelter-claim-needs-the-shoreline-check.md) now
points at the new tool. Part one of three; the follow-ups are under
Consequences.

## Context

Choosing an anchorage takes four answers, and Mate gave them in four places.
Shelter came from `anchorage_shelter`, the forecast from `get_wind_forecast`,
the tide from `get_tides`, and anything about the ground, the moorings or the
rules of the place came from the model's memory. The first three took three
calls with no shared idea of the stay, so the forecast covered "the next three
days" instead of the night the boat would be there. The fourth is where memory
does harm: a restricted area, a marine park zone, or a public mooring that
takes 12 m when the boat is 14 m.

OpenStreetMap does hold some of this. Looking at Butterfly Bay on Hook Island on
the boat's Overpass mirror (data current 2026-10-31), 23 Queensland public
moorings sit inside a nautical mile. Each is a `seamark:type=buoy_special_purpose`
with category `mooring`, and a free-text `description`:

    Pt_No 139
    Location West Butterfly Bay
    MSQ_Moorin 795
    Lat_dDeg -20.078
    Long_dDEG 148.92522
    VesselSize 20
    Mooring_cl B
    Mooring_Li 48mm square stabilized polyprop

The record says what size of vessel the mooring takes and its class. Not every
mooring has both: of the 23, 17 read size 20 class B, one reads size 10 class A,
one size 6 class T, and four carry a class with no size. The same query finds two
natural bays and two marine parks whose boundaries come within the mile. Moreton
Bay gives a charted anchorage (The Corner) and a coral seabed area. The mirror
holds no `seamark:type=restricted_area` at all, so the restricted-area path has no
real capture to test against.

## Decision

**`anchorage_details` replaces `anchorage_shelter`.** It takes `lat` and `lon`
(from `find_places`), `arrive` (an RFC 3339 time, required), and optionally
`leave` and a display `name`. There is no depth argument. The position must be
usable, `leave` must be after `arrive`, and the stay may be up to seven days: the
forecast reaches ten, and a longer stay is several questions. With no `leave` the
stay ends at the first 08:00 that is at least six hours after arrival (so
an arrival at 02:30 leaves at 08:00 the next day, and one at 01:00 at 08:00 the
same day), in the time zone at the anchorage (`zoneAt`, the lat/lon zone the forecast and passage tools use), and
the result says the default was applied so that Mate says what it assumed. The
`shelterAt` helper stays; `get_wind_forecast` still labels its rows with it.

**Four sections, one of them required.** Shelter is settled first, since it is
local, and its error is returned before any network lookup starts. The forecast,
tide and features lookups then run together; if the call is cancelled it returns
at once and the lookups finish in the background with nowhere to write. `shelter` is the Offline coastline
estimate, unchanged. If it cannot be made (the coastline is off, the position is
outside coverage, a square is still downloading), the whole call fails with that
error, as `anchorage_shelter` did: an anchorage judged without it is a guess.
`forecast`, `tide` and `features` are each a section that holds its data or an
`error` naming why not ("Forecast unavailable: weather provider ..."). A section
with an error carries nothing else, so no empty list can be read as "nothing
there", and the result's reading tells Mate the part was not checked and not to
fill it from memory. A missing plugin export is such an error. This follows the
fallback policy: the failure is shown, not covered.

**The forecast is get_wind_forecast's, sliced to the stay.** `get_wind_forecast`
has no start argument, so the tool fetches enough days to reach `leave`, then
keeps the hour nearest arrival (the earlier when two are equally near), then
every three hours after it while inside the stay, and the hour nearest leave. An
hour more than half an hour from the time it stands for is not used. A short stay
therefore still gets rows, and arrival conditions are never up to three hours
late. `get_wind_forecast` keeps its own rule, every third local hour. Row building moved out of `get_wind_forecast` into one function, and the
wave fetch into another, so the two tools cannot read an hour differently. Each
row carries its shelter grade for the wind direction (`shelterGradeAt` on the
estimate already in hand, not a second read), and each day carries its
prevailing grade. A forecast that does not cover arrival is an error section; a
forecast that ends before the stay does is cut there and says so. The card stores the
provider's fetch time and labels it "fetched": the weather plugins do not report
a model run time, and a fetch time must not be presented as one.

**The tide is read at arrival and through the stay.** `resolveTideStation`
supplies the station and its distance. The height at arrival is
`interpolateTideNow` between the extremes either side of it. If arrival falls
before the first or after the last predicted extreme, the height is absent with
a note, because `interpolateTideNow` would otherwise answer with the nearest
extreme. The section lists every high and low water from arrival to leave
inclusive, and the range is the highest minus the lowest of them. When fewer than
two fall inside the stay, the last one before arrival and the first one after
leaving are added (each marked as outside the stay) so that a range exists. The
rule is worded in `range_note` so that the model and the card say the same
thing; it names only the neighbours that exist, so it never claims an extreme
after leaving when the prediction ends first. When the prediction ends before
`leave`, a `note` (in the model's result and on the card) gives the last
predicted time. With fewer than two extremes at all there is no range.

**`anchorage_features` is a new optional plugin export.** The host probes for it
with `FunctionExists`, as it does for `marina_details`, and a plugin without it
(google-places, or an older osm-overpass) is a named features error. It makes one
Overpass request with a separate cap for each class, so a field of 131 public
moorings cannot crowd out a restricted area. Within 1852 m (one nautical mile) of
the point it returns:

- **Restricted areas:** `seamark:type=restricted_area`, with the
  `seamark:restricted_area:restriction` and `:category` tags as mapped. Also marine
  protected areas: `boundary=protected_area` or `marine_protected_area` that is
  also `maritime=yes`, with the protection title and class. Land national parks
  carry no `maritime` tag and are left out. A protected area is never reported
  as forbidding anchoring, since its zoning decides that and the map does not
  say. It has no geometry (a marine park's boundary can run hundreds of
  kilometres) and no distance (its centre says nothing about how near its
  boundary is). Only a seamark restriction whose value is `anchoring_prohibited`
  or `no_anchoring` is a prohibition; `anchoring_restricted` is not.
- **Moorings:** `seamark:type=mooring`, and `buoy_special_purpose` whose category
  is `mooring`. From the `description` the plugin reads only the `VesselSize` and
  `Mooring_cl` lines. `VesselSize` must be a positive number and is read as
  metres, which is how the Queensland dataset gives it. Neither line parsing, or
  no description at all, sets `details_untagged`; that is a fact about the data,
  not an error. The name and ref are kept and the rest of the description is not
  forwarded.
- **Anchorage areas:** `seamark:type=anchorage` or `anchorage=yes`, with
  `seamark:anchorage:category`. **Seabed areas:** `seamark:type=seabed_area`
  with `seamark:seabed_area:surface`.
- **The bay:** a `natural=bay` whose mapped outline contains the point (closed ways,
  and relations whose member ways are first joined end to end into closed rings,
  by an even-odd test that excludes the inner rings), else the nearest by name.
  The same containment gives a restricted area a distance of 0. Most bays are mapped as a single named node and so never
  "contain" anything.
- **Distance** is to the nearest edge of an element's shapes, not the nearest
  vertex, so a long edge passing close by reads as close.
- **Geometry for the card's map:** points and shapes, thinned to 300 points a shape
  and 20 shapes a feature by the plugin and checked again by the host (the marina
  rules). The host keeps them on wire types that embed the records the model sees, so
  the model sees no coordinates; a test pins that. The card carries them as `shapes`
  on each feature record, and a mooring's position as `lat` and `lon`.
- **The data date** is `osm3s.timestamp_osm_base`, passed through, checked as RFC
  3339 by the host, empty when absent and never the clock (ADR 0242).

The Overpass query does not use `is_in` or area queries, because the boat's
mirror runs without the areas database and answers them with a dispatcher error.
The cost is that a protected area or bay is found only if its boundary comes
within a nautical mile: a position deep inside a large marine park is not
flagged by this export.

**The host checks the answer strictly,** as for `marina_details`: an element with
no OSM id, an impossible position or distance, a seamark restriction with no
distance, a vessel size outside 0 to 1000 m, or an invalid shape fails the
section. Text from the map is cleaned and cut; `details_untagged` is derived from
the cleaned values and not trusted from the wire.

**What the model reads.** The sections are compact: moorings are summarised
(count, the count at each size limit, the untagged count, the ten nearest);
restricted areas, anchorage areas, seabed areas and the bay are listed. The
result is shrunk with `capToolResultJSON`: nearest moorings first, then tide
extremes beyond two, then forecast rows from the end, then the fetch table (the
arcs and the summary stay). The rules for reading it travel in the description:
an empty list means nothing is mapped, never that nothing is there; an area that
prohibits anchoring is a no-go and is said first; a protected area needs the
operator to check the zoning; a mooring's size is metres; compare it with the
boat's length; depth and holding are not part of the result. The result's
`reading` holds what depends on the call: the defaulted leave, any section that
failed, and the names of areas that prohibit anchoring.

**Prompt and draft check.** The prompt now says to call `anchorage_details` for
every anchorage under discussion instead of fetching the forecast and tide
separately, with the arrival and leave times (from the planned stay, or stated as
an assumption). The 0243 draft check accepts an `anchorage_details` call in place
of an `anchorage_shelter` call, and its correction names the new tool.

**Outside assistants.** The MCP entry is renamed to `anchorage_details`
("Anchorage details"), on by default and read-only. The MCP service clears the
card sink before every call, so an outside assistant is given no card and no
instruction about one. A token's stored tool list names tools, and a stored name
that is no longer offered is ignored, so an existing token loses
`anchorage_shelter` and gains nothing. The operator switches the new tool on per
token. The CHANGELOG says so under Breaking.

**The card.** One `anchorage` card goes in the card sink per successful call,
under the exact result text, a snapshot like the marina card. Every key is
always present (`""`, `null`, `[]`; an area with no geometry has `"shapes": []`,
is still listed and draws nothing), and the Go golden file
`testdata/mate_cards/anchorage_butterfly_bay.json` is decoded by the frontend
tests so the sides cannot drift. The decoder is strict, ignores unknown keys, and
requires 36 bearings in order. Each section has its `error` and empty lists. From
top to bottom the card shows:

- **A shelter rose:** 36 sectors of 10 degrees, each coloured by grade with spoke
  length proportional to fetch up to 15 nm, and an accessible label made from the
  server's summary. Grades use theme tokens (primary for sheltered, the gauge
  amber for partial, destructive for exposed, muted for reef) and are also
  written out as words in every badge, so colour is never the only cue. A thin
  band outside the ring carries one segment per bearing, graded the same way
  whatever the fetch, because short spokes on sheltered sides were invisible and
  the rose read as exposure only. The ring's distance labels sit along 45 degrees
  so they never meet the compass letters.
- **The forecast:** three-hourly rows in the anchorage's local time (wind and
  gust in knots, the direction, the shelter badge, the waves in the operator's
  distance units) and each day's prevailing grade.
- **The tide:** the station and its distance, the height at arrival, the extremes
  with those outside the stay marked, the range, and a note when the prediction
  ends before the stay does.
- **The local features:** restricted areas first (a prohibition in the alert
  palette, other seamark restrictions amber, marine parks muted with a note to
  check the zoning), then the moorings grouped by size limit and class with the
  nearest distance ("Size limit not tagged" when only a class is mapped, "Details
  not tagged" when neither), then the anchorage and seabed areas and the bay.
- **The map:** folded away under **Map** and mounted only when open and near the
  viewport. It is the marina map's base (Carto through the backend proxy, Esri
  imagery on by default with a toggle, the OpenSeaMap raster, cooperative
  gestures), with the 36 rays coloured by grade, the moorings labelled with their
  size limit, restricted areas filled, and anchorage and seabed areas outlined.
  The blocking shoreline is not drawn: the rays' length says where it is.
  The map frames the one-nautical-mile square round the anchorage plus the
  moorings; a large area is drawn but may run off the map, so one regional
  restricted area cannot zoom the anchorage out of sight. The
  map is its own lazily loaded module, and without WebGL2 a line says so.
- **A footer:** when the forecast was fetched, when the coastline was
  downloaded, and the OpenStreetMap data date, or just "OpenStreetMap" when the
  server gave none.

The map module and the marina map each build their own layers on the same base.
Pulling the base into one component would have changed the marina map's tests
and layer order for no change in behaviour, so it is a later cleanup.

## Consequences

- An installed osm-overpass plugin must be replaced for the features section.
  Until it is, the section says the plugin does not provide them and the rest
  works.
- The restricted-area path has fixtures only for what the mirror holds: marine
  protected areas, mooring buoys, an anchorage node, a seabed node and bays,
  captured from the exact queries the plugin sends. The seamark restricted area
  and the containing bay are tested on small hand-written element sets, because
  the mirror has none to capture.
- A marine park is listed whenever its boundary is within a nautical mile. The
  Great Barrier Reef Marine Park appears at Butterfly Bay, where its zoning is
  what decides whether anchoring is allowed, and the card says to check it.
- The mooring size limit is assumed to be in metres. If a source mapped it in
  another unit, the card would be wrong by that factor.
- The forecast time on the card is when the provider's answer was fetched, not
  when the model ran.
- The tool gives no depth and no holding, and says so. The follow-ups, each a
  separate change:
  - **Depth, swing and clearance.** Done in
    [ADR 0245](0245-anchorage-depth-and-swing.md): a saved place's depth or the
    operator's stated figure, the swing circle and the water under the keel at
    low water, and a depth section on the card. Holding is still not given.
  - **Anchor watch.** Save the anchor's depth when the watch starts, with
    the transducer depth (`transducer_depth_m`) so that a depth below the
    transducer is turned into a depth below the surface.
- `anchorage_shelter` is gone from the registry, the MCP list and the prompt. The
  `shelter_summary` and labels that `get_wind_forecast` adds are unchanged.

## Rejected

- **Keeping `anchorage_shelter` beside the new tool.** Two ways to ask the same
  thing, and Mate would pick the one without the forecast.
- **Failing the call when the forecast, tide or features fail.** The shelter is
  useful alone, and a section that says why it is missing is more honest than no
  answer. The shelter is the one section nothing else can stand in for.
- **Reading the height at arrival off the nearest extreme.** That is a guess.
- **A depth argument now.** Depth needs a saved place and the swing and
  clearance check to mean anything; a number with no check would invite a
  confident wrong answer.
- **Using `is_in` for protected areas and bays.** The mirror cannot answer it.
- **Calling a marine park a no-anchoring zone.** Zones differ inside one park,
  and the tags do not say.
- **Drawing the blocking shoreline on the map.** It is thousands of segments, and
  the rays already show where they stop.

## Amendment: one card per position per reply

Mate sometimes calls the tool with a guessed arrival, then again with the right
one, and both cards showed, the first contradicting the answer. The run now
applies the rule where it adds a tool call's cards to the reply: an anchorage
card within 10 m of one the reply already holds replaces it. The run takes cards
in the model's call order, within a round and across rounds, so the later call
in the model's list wins whatever order the calls finished in. The card carries
its position beside the payload, so nothing re-parses the JSON. Cards are saved
with the reply and never streamed per round, so nothing needs withdrawing on
screen. Other card kinds and different positions are untouched.
The `protect_class` code list on a marine park (such as `1;2;4;6`) is no longer
passed on as its category: the plugin stopped sending it, the host drops it from
any plugin version's answer, and the card ignores it on stored cards.
