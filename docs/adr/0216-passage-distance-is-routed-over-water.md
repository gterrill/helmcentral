# ADR 0216: A passage distance is routed over water, never a straight line

## Status

Accepted (2026-10-27). Builds on [ADR 0203](0203-offline-coastline-and-anchorage-shelter.md)
(the offline coastline squares), [ADR 0091](0091-points-of-interest-as-a-plugin-kind.md)
(the host computes geometry) and [ADR 0172](0172-mate-tools-are-one-record-each.md)
(one record per Mate tool). Changes the arguments of `estimate_passage` and the
field names of `find_places`.

## Context

Mate gave passage distances as straight lines. `find_places` put a
`distance_nm` and `bearing_deg` on every candidate, measured from the boat, and
the prompt told Mate to hand that distance to `estimate_passage` for time and
fuel.

A boat at Haslewood Island, east of Whitsunday Island, asked about Cid Harbour
on the island's west side. Mate said 7.3 nm on 284°T. It said Coral Sea Marina
was 20.5 nm. Both lines cross Whitsunday Island. The real runs go round the
south through Solway Passage or round the north through Hook Passage: about 15
nm to Cid Harbour and 24 nm to the marina. Time and fuel were understated, the
bearing pointed at a hill, and the wind and tide advice followed that bearing.

The shoreline to do better is already aboard. ADR 0203 downloads it in 1°
squares for the shelter estimate.

## Decision

Distances come from a route over the offline coastline. Mate cannot pass in a
distance of its own, and the straight line is never offered as a passage
figure.

### The router

`routeOverWater` (`water_route.go`) takes two positions and the coastline
around them and returns the shortest path that stays on water, its length, and
the bearing of its first leg.

1. Coastline fragments that share an end point are joined, and the same way
   held in two squares is counted once. Reef lines are ignored: a charted reef
   is a hazard to read off the chart, not land, and a wall of reef would shut
   anchorages inside fringing reef. The shoreline is thinned to 0.01 nm.
2. OpenStreetMap winds the coastline with land on the left. That tells a point
   on land from one on water (the side of the nearest segment), and which
   corners a path can bend round: only a left turn, towards the land, is a
   headland.
3. Each headland corner becomes a node, pushed seaward by the clearance along
   the bisector of its two segments, capped at three clearances on a sharp
   corner. A node squeezed against other shore is dropped.
4. Two nodes join when the straight line between them crosses no shoreline and
   stays at least half the clearance off every segment. A start or destination
   already closer to shore than that is allowed its own distance, less ten
   percent.
5. A* searches the graph without building it. A candidate edge is queued
   untested and checked only when it reaches the front, using a grid of the
   shoreline segments near the line. Clear water is tried first and returns the
   straight line.

The clearance is 0.05 nm, about 90 m. It keeps a path off a charted edge that
may be a few tens of metres out, and it leaves the passages cruisers use open.
At 0.1 nm a 0.2 nm gap between islands closes. It is a constant, not a setting.

### Corridor and coverage

The search runs inside the box the two points span plus a margin: 3 nm first,
then 8, 20 and 50 if no path is found. A path found inside the box is the shortest there is once the margin reaches
the sideways reach of the ellipse whose foci are the two points and whose
length is the path's. With path length L and straight-line distance s, that is
sqrt(L² - s²) / 2, which is more than the (L - s) / 2 the path overshoots the
ends. When the first answer needs a wider margin to be proven, the search runs
again at that margin. Nodes outside that ellipse are left out. The shoreline of
each square is read once per call, however many margins are tried, and the
search stops when the request is cancelled. A Whitsundays run of 30 nm takes
about 0.25 s.

If the wider run cannot happen (a square beyond the first corridor is missing,
or the coast is too intricate), the call fails and names the reason. It does
not return the path already found. That path might be too long, and offering it
as the distance is the quiet substitution this record removes. A caveat in the
result would leave the operator and Mate to notice it, so the tool fails.

### Ends on the shore

A town, jetty or marina is charted a few metres behind the shoreline, so its
position reads as land. An end within 0.5 nm inland is moved to the water in
front of it, one clearance off the shore. The tool result says so and says how
far. Further inland than that is an error.

### Failing fast

Nothing masks a missing input. The router returns an error, and the tool says
"no passage distance" and why, when:

- Offline coastline is off or not available.
- A square the corridor touches is not downloaded or its download failed. The
  error names it, as "21°S 148°E (not downloaded)". An all-land square holds no
  coastline by definition and is not required.
- The start or destination is on land beyond 0.5 nm, or in an inland square.
- No water connects the two inside the widest margin.
- The run is longer than 300 nm, or the corridor holds more than 3000 corners.
  The tool asks for shorter legs. ([ADR 0223](0223-water-routing-bounds-the-queue-not-the-corners.md)
  replaces the corner limit, which the code held at 2000, with a bound on the
  search queue and one on its time.)

The prompt tells Mate to say it cannot give a passage distance and why, and
never to substitute the straight line, a guess or a figure from its own
knowledge unless the operator asks, and then to label it.

### The tools

- `estimate_passage` takes `to_lat` and `to_lon`, and optionally `from_lat` and
  `from_lon`. With no start it uses the vessel's fix. It has no distance
  argument. It routes first, so a failed route fails before any telemetry is
  read. Time and fuel use the routed distance. The result adds
  `straight_line_nm`, `course_deg` (the bearing of the longest leg, true),
  `first_leg_bearing_deg`, the waypoints in `route`, each leg's bearing and
  length in `legs`, and a `route_note` when an end was moved. A result too large
  for the chat budget loses `route` and `legs` first and says so in
  `route_omitted`, so the distance, time and fuel always survive.
- `route_distance` is the same route with no time or fuel. A planned leg months
  away has no use for the boat's fuel history, and `estimate_passage` fails
  without it.
- `find_places` keeps a figure per candidate, now named `straight_line_nm` and
  `straight_line_bearing_deg`. The place search on the Planned stay form reads
  the same fields. Mate takes the course for wind angle and tidal stream from
  the route's `course_deg`. A route whose legs differ widely is better read leg by leg.

## Consequences

- A route to a place behind an island is longer than before and is the length
  of the passage. Time, fuel and the departure window follow it.
- A run needs coastline squares around both ends. Near the edge of coverage the
  tool says which square is missing, where it used to give a number.
- The router finds the shortest path over charted water. It knows nothing of
  depth, reefs, tidal gates or restricted areas, and charted coastline is as
  good as the people who drew it. Mate still tells the operator to check the
  chart.
- Fixtures: `testdata/water_route/whitsundays.json.gz` is natural=coastline for
  20.55°S to 19.95°S and 148.55°E to 149.35°E, captured from Overpass on
  2026-10-27.

## Rejected

- **Take the straight line and add a margin.** It hides the case that matters,
  where the answer is a different route, not a longer one.
- **A visibility graph over every vertex.** Two thousand nodes in 15 nm of
  island water meant 900,000 edge tests and several seconds.
- **Ask Mate to add via points.** It guessed headlands from memory, and the
  coastline is aboard. `assess_passage_window` now takes its via points from
  the route's waypoints.
- **Fall back to the straight line when coverage is missing.** The failure is
  the same one this record fixes, made quietly.
