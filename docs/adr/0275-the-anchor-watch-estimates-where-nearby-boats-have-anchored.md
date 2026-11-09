# ADR 0275: The Anchor Watch Estimates Where Nearby Boats Have Anchored

## Status

Accepted (2026-11-09). Amends the retention in
[ADR 0263](0263-helmcentral-records-ais-tracks-during-an-anchor-watch.md): points
recorded during a watch are kept for its whole length.

## Context

[ADR 0263](0263-helmcentral-records-ais-tracks-during-an-anchor-watch.md) has
Helmcentral record the AIS tracks of boats within 500 m of the anchor, and the
map draws them as trails. A trail shows where a boat has been. The skipper's
question is usually narrower: where did that boat put its anchor, and does its
swing reach mine?

A boat at anchor stays on a circle round its anchor, with the rode as the
radius. A circle fitted to its stored positions therefore estimates the anchor
(the centre) and the swing (the radius), with nothing recorded beyond what ADR
0263 already keeps.

## Decision

The server fits a circle to each nearby target's track and serves the result. The
map draws it. No alarm, no new stored data, no schema change.

### The input

Stored positions only. The fit uses no heading and no antenna dimensions, so it
needs no new column and works for every target the recorder already follows.

**Every point since the watch started.** The trails serve 24 hours, but a boat's
whole time at anchor counts towards its swing. The estimator reads all of a
target's points since the watch began, and pruning keeps them (see Retention).
This mattered on the real anchorage: 49 of one boat's points aged out of a
24 hour store between two captures, all from the previous night's swing 80 to
100 m east of our anchor, and without them its 30 m circle became a 17 m one.

**The settled run.** The fit uses the newest stretch of the track that sits
together. Walking back from the newest point, a point stays in the run while it
lies within 1.5 times the largest believable swing radius (120 m) of the median
of the points kept so far, taking x and y medians separately in local metres.
The first point that does not ends the run.

- **The approach drops out.** A boat that motored in from 300 m away leaves far
  points at the head of its track. They lie outside the run.
- **Segment flags and gaps do not matter.** A boat that went quiet for an hour
  without moving is flagged as a new segment by the recorder, but it is still one
  swing.
- **A departed boat collapses.** Its newest points lie away from the rest, so its
  run is short of 20 points and it has no estimate.
- **Why 120 m.** A boat on its anchor never strays more than its swing diameter
  from the middle of its swing.

### The fit

Points project to local metres about our anchor (equirectangular, which is exact
enough inside 500 m). The fit runs in two stages.

1. **Seed.** An algebraic least-squares circle (Kåsa). It needs no starting
   guess and fails cleanly, as a singular system, on collinear points.
2. **Outer ring.** In light air a boat sits inside its circle on slack rode, and
   only the taut stretch lies on it. A fit to every point would pull the circle
   in. So the fit repeats on the points at least 0.8 times the 90th-percentile
   distance from the current centre, for three passes.

### The gates

An estimate appears only when all of these hold, for the ring points round the
final centre. A target that fails any of them has no estimate. That is not an
error and nothing is logged.

- **At least 20 points** in the settled run, and at least 10 in the ring. A boat at
  anchor reports about every 3 minutes (a Class B boat slower), so 20 stored
  points can take about an hour.
- **Angular coverage of at least 90 degrees:** 360 minus the largest gap
  between consecutive bearings from the centre. Less leaves the centre free to
  slide along the arc.
- **A radius from 5 m to 80 m.** Under 5 m is GNSS jitter on a boat that is
  not swinging. Over 80 m is more than a boat lays out, about 5:1 scope in 12 m
  of water plus the boat's length. On a real anchorage a boat wandering about
  fitted 99 to 150 m, where the true swings were 30 to 40 m.
- **A small residual:** the RMS distance of the ring points from the circle is
  at most the larger of 5 m and 0.25 times the radius.

Every threshold is a named constant in `ais_anchor_estimate.go`, beside a
comment saying why. The gates err towards showing nothing.

### Our room and the drawn circle

The alarm radius we set is bigger than the circle our own boat traces, and the
difference is room we chose to leave. Each AIS circle is drawn at its fitted
swing times the same proportion, so the two circles read alike.

- **The ratio** is the watch radius over the radius our own swing trail fits,
  never under 1, so it never shrinks their circle, and never over 2 (a small own
  fit in light air on a big watch radius would otherwise paint every neighbour
  red). Our fit 40 m and a watch radius 50 m give 1.25, and a 20 m AIS swing is
  drawn at 25 m. The server works it out
  with the same settled run and fit as for any other boat. On the evening capture
  our own trail fits 52.7 m over 313 degrees and the 65 m watch gives 1.23.
- **Why a ratio, not a fixed margin.** A margin in metres gave the same extra
  room to a boat on little rode as to one on a lot. A boat on less rode gets less
  room.
- **Our own trail** is read for the whole watch and thinned to one point per 30
  seconds, the recorder's own floor, so both swings cover the same span and the
  settled run stays cheap. The trail store itself holds at most its newest 10,000
  points (ADR 0199), which bounds how far back our own fit can reach.
- **Not ready, nothing drawn.** Until our own trail passes the gates, the
  endpoint returns no estimates at all, not bare fitted radii. Bare radii would
  sit beside a circle drawn with room added and look like the other boats' real
  reach. Anchors and circles appear only once both our boat and theirs have swung
  enough.
- **One source.** The wire carries `swing_radius_m` (the fit), `drawn_radius_m`
  (the fit times the ratio) and `margin_ratio` once at the top. The browser draws
  `drawn_radius_m` and adds nothing.

### Overlap

Our swing circle is the watch's own anchor position and radius, the circle the
map already draws. Overlap compares what is drawn: the two overlap when the
distance between the centres is less than our watch radius plus their drawn
radius. Touching is not overlapping.

### Retention and who is offered

- **Retention.** ADR 0263 pruned points older than 24 hours. Now a point recorded
  since the current watch started is kept for the life of the watch. Drop and raise
  still empty the table, so only a watch longer than 24 hours holds anything older,
  and with no watch the 24 hour rule holds. Points older than 24 hours are thinned
  to one per target per 5 minutes, because a boat that never leaves stores 2,880
  points a day at the 30 second floor and a week-long watch of twenty boats would
  hold over 400,000. Newer points are untouched. The estimator reads only points
  from the watch's own start. `GET /api/anchor-watch/trails/ais`
  still returns the last 24 hours, so the trails on the map are unchanged.
- **Only boats still near.** An estimate is served only for a target the recorder
  last saw inside the 500 m recording circle (its `inside` state). A boat that has
  gone has a track that stops at the circle's edge, and its last settled stretch
  can still fit a circle: one boat 1.6 km away still fitted 53 m over 138 degrees.
  The filter runs after the cache, so the cache key needs no inside set. After a
  restart `inside` is empty until the next poll tick, and no estimate shows until
  then.

### The wire

`GET /api/anchor-watch/ais-anchors` (read tier) answers:

```json
{ "margin_ratio": 1.23,
  "estimates": [ { "mmsi": "111111111", "lat": -19.9997, "lon": 149.0002,
  "swing_radius_m": 41.3, "drawn_radius_m": 50.8, "swing_arc_deg": 187, "points": 52, "overlaps_ours": true } ] }
```

With no watch the list is empty, and so it is until our own swing fits a circle.
The fit is cached until an AIS point is stored, the watch changes, or our own
trail restarts or reaches a new minute: our trail grows every few seconds, and
its newest time enters the cache key at a minute's resolution, so it costs a
refit about once a minute. A stored AIS point still invalidates the cache. A watch file that failed to load answers 500 with its
message, not an empty list. With no track store the endpoint answers 500, as the
trails endpoint does. On any failed poll the browser drops its estimates and logs
the fault, rather than keep drawing a guess it can't refresh.

The browser polls it every 30 seconds through the shared poller, since an
estimate moves slowly.

`swing_arc_deg`, `points` and `margin_ratio` are not drawn. They stay on the wire
as a diagnostic shape, so a surprising circle can be checked against how it was
fitted. The browser drops its estimates when a poll fails even though that can
flicker the circles for one poll: the Fallback Policy prefers showing nothing to
showing a guess the page can no longer refresh.

### The map

Each estimate draws a small anchor marker and a circle at the drawn radius. The
circle shares our own alarm circle's fill and stroke: a fill at 0.1 opacity and a
2 px dashed stroke at 0.8, from the same constants in the map, so the two can't
drift apart. Where ours is sky blue and follows the theme, theirs is the AIS vessel
marker's amber (amber-500) in both themes, as the AIS markers don't change with
the theme either, and both marker and circle turn the red an AIS marker takes in a
collision alarm (red-600) when `overlaps_ours` is true. The marker is a labelled
image (`role="img"`), so a screen reader announces it. As with trails, only boats
still among the map's markers are drawn. The marker sits below every other marker
and takes no taps, so it never covers a boat or swallows a tap meant for one.

## What it cannot know

- **The antenna, not the anchor, swings.** The radius includes the distance from
  the anchor to the AIS antenna, so it runs a little over the rode.
- **Wind shifts and current.** A boat that has swung through a quarter turn has
  a good estimate. One that has barely moved has none.
- **Re-anchoring close by.** The settled run forgives anything within 120 m of
  its median, so a boat that re-sets its anchor within that distance keeps both
  swings in one run. It can show a blended circle, or none, until its new swing
  outweighs the old. A boat that moves well away gets a fresh estimate. The
  algorithm is left as it is.
- **A stray approach point can bend the circle.** One far point left in the run
  pulls the seed circle towards it. On a boat that had sat in a 12 by 30 m patch
  for five hours, an approach point 90 m out gave a 66 m circle through it and the
  cluster. The arc gate caught that one. With a little more swing it would not
  have.
- **A boat circling or manoeuvring** on a long arc can pass the gates. This is
  accepted: it still draws as a clearly marked estimate and raises nothing.
- **A small swing gets a small circle.** A boat that has hardly swung fits its
  small cluster, not its real reach.
- **A disc of slack-rode points with no taut ring** fits to the disc's edge,
  not to the true swing.

## What fails, and how it shows

- **No store.** The endpoint answers 500 during a watch and logs the fault.
- **No watch, or our own swing not fitted yet.** An empty list.
- **A failed poll.** The map drops the estimates, logs the fault and tries again.
- **A watch file that won't load.** The endpoint answers 500 with the load error.

## Tried against the real captures, and dropped

Each was tried on three captures from the anchorage at Great Keppel: a Keppel
capture of every boat near the anchor, one boat that had anchored hours earlier
and barely swung, and an evening capture with our own swing trail.

- **A speed trim of the approach.** Drop leading points faster than 1.5 knots.
  One slow pair ended the leading run, so most approaches from 300 to 400 m stayed
  in and those boats got no estimate.
- **Segment-based selection.** Use the points since the recorder's last
  `new_segment` flag. A reporting gap of over 15 minutes starts a segment even when
  the boat never moved: one anchored boat with 454 points kept 12.
- **A cutoff that follows the boat's own spread.** Trim the run's oldest points
  beyond 4 times its median spread. It fixed the 90 m approach point but trimmed
  real swing from boats that lay one way for hours and swung briefly: one boat
  went from 29.2 m over 263 degrees to 17.0 m over 122, and our own fit from
  52.6 m to about 42 m, which moved our ratio.
- **A consensus fit.** Score 500 circles through three sampled points by their
  inliers, with and without bearing coverage in the score, and refit the best.
  It favoured small circles round the dense patch where a boat lay longest, and
  gave wanderers circles of 17 to 57 m centred 300 m or more from our anchor.
- **A minimum arc of 150 degrees.** Once the run was trimmed, a boat that had sat
  in a 12 by 30 m patch fitted a 17 m circle over 112 degrees and another fitted
  20 m over 94, while two boats that had swung properly covered 210 and 252. 150
  would have shown nothing for the first two. The operator kept 90 so that circles
  appear sooner, accepting that a boat with a small swing gets a small circle.
- **A fixed margin in metres** instead of the ratio, for the reason above.
- **A heading line.** A Class A boat reports heading and dimensions; the anchor
  lies up-wind of the bow on a line through the heading. That needs heading and
  dimension A stored per point, a schema change, and it covers Class A only.
- **An alarm on overlap.** Agreed to be map-only first, to see how often the
  estimate is right on a real anchorage.
- **A fit in the browser.** Go is the single source, and the same estimate
  should serve any later alarm or Mate answer.

## What the captures give

| Capture | Boat | Fit |
|---|---|---|
| Keppel | the first anchored boat | 36.3 m over 210 degrees |
| Keppel | Wild Child, with last night's swing | 30.5 m over 263 degrees |
| Evening | our own trail | 52.7 m over 313 degrees, ratio 1.23 |
| Evening | Wild Child, last night's swing aged out | 17.0 m over 122 degrees |
| Evening | a boat that has left | 53.4 m over 138 degrees, hidden by the inside filter |
| Unswung | a boat anchored hours before, barely swung | none |

Keeping points for the whole watch is what returns Wild Child to about 30 m live.

## Follow-ups

- The heading-line method for Class A boats, which needs heading and dimension A
  recorded with each point.
- An overlap alarm, once the estimates have proved themselves on the water.
- A fit that ignores stray points without trimming real swing.

## Consequences

- A boat at anchor shows its estimated anchor and swing once it has about 20
  stored points, which can take an hour, with no new data kept, and only once our
  own boat has swung enough to give a ratio.
- A long watch keeps its AIS points, so the table grows with the watch until the
  next drop or raise.
- The help page for the Anchor Watch says an estimate appears only after both
  boats have swung enough, and only for boats still near the anchor.
- Every threshold above can be tuned in one file.
