# ADR 0263: Helmcentral Records AIS Tracks During An Anchor Watch

## Status

Accepted (2026-11-05). Supersedes the decision in ADR 0005 to read AIS trails
from the SignalK tracks plugin.

## Context

ADR 0005 handed AIS trail history to the `@signalk/tracks` plugin and read it
back through `GET /signalk/v1/api/tracks`. That plugin has since vanished from
the boat. Nothing said so: the endpoint answered 404, Helmcentral logged one
line and drew no trails, and the anchor map looked as though no boat had moved.

The plugin never gave Helmcentral real times either. Its tracks are bare
coordinate lists, so the adapter invented one second per point, counted back
from the moment of the request. A trail showed where a target had been, never
when.

The operator also wants less than the plugin offered. An AIS trail matters
while the boat is on the hook, for the boats near the anchor. Every vessel on
the SignalK server, at any time, was more than that use needed.

## Decision

Helmcentral records the tracks itself, in `helmcentral.sqlite` beside the swing
trail of ADR 0199. It no longer reads the tracks plugin, and it keeps no
fallback to it.

### What is recorded

- **Only during a watch.** With no watch set, nothing is recorded.
- **Only near the anchor.** A target is recorded while it is within 500 m of the
  anchor position, not of the boat. The 500 m is a constant, not a setting.
- **One point per new report, at most every 30 seconds unless it moved.** The
  poll tick stores a target's position when its report is newer than the
  target's last stored point and either at least `aisAnchorMinInterval` (30
  seconds) after it or at least `aisAnchorMinMoveM` (25 m) from it. Both are
  constants. The distance test keeps a boat crossing the anchorage at 10 kn
  from ending its trail up to 150 m behind its marker, and from losing its last
  leg before it leaves the circle. A target
  that has not reported since stores nothing, however many ticks pass. The stored
  time is the report's own time (`PositionSeen`, the time Helmcentral received
  that position delta), so the track carries real times. Nothing else smooths or
  filters the points, and no cap limits how many a crowded anchorage stores.
  Real Class A boats report every 2 to 10 seconds, and a tick every 5 seconds
  would store about 17,000 points a day for one of them, all of which the first
  load sends. A boat that reports every few minutes is not thinned. A report
  skipped for coming too soon that would have started a segment, because the
  target just re-entered the circle, hands the flag to the target's next stored
  point, so a skipped report never joins the line across the gap. The recorder
  drops that pending flag when it sees the target outside the circle again,
  because the re-entry rule flags its next point anyway.
- **By MMSI.** The key is `vesselContactKey`, the same as the sighting log. The
  name is not stored: the marker already labels the vessel from the live list. A
  target with no MMSI can't be followed from one report to the next, so it is not
  tracked and is logged once per vessel. It still shows as a marker.
- **One fetch.** The tick fetches the nearby vessels once, with
  `nearbyVesselsUnlimited`, and hands the list to both the sighting log and the
  track recorder.

### What the recorder remembers

The nearby list is not the circle. It drops a target after ten minutes without a
position, and it drops every target when the boat's own fix is lost. So "not in
the list" never means "left the circle", and the recorder keeps its own state
for the life of the process, under one mutex:

- **Inside.** Changed only on evidence: a listed target inside the circle is
  added, a listed target outside it is removed, and a target missing from the
  list changes nothing. A restart starts it empty.
- **Last report time per target.** Warmed once from the database after a start
  (`MAX(t_ms)` per MMSI). A report is stored only if it is later than this and
  later than the last reset, so a report received before a drop is not stored as
  part of the new track.
- **After a commit only.** State moves once the points are written, so a failed
  write keeps its points and segment flags for the next tick. A tick with nothing
  new opens no transaction.
- **One report per MMSI per tick.** Two SignalK contexts can carry one MMSI. The
  recorder keeps the newest of their reports, so the insert can't land out of
  order or collide with the unique key, and it counts the MMSI as inside if any
  of its contexts is.

### Gaps and age

- **Segments.** A point is flagged `new_segment` when it is written, never on
  read: a target's first point, its first point after it was seen outside the
  circle, its first point after a restart, and any point more than 15 minutes
  after the target's previous one. The browser draws no line into a flagged
  point. The 15 minutes is a constant.
- **Retention.** Points older than 24 hours are never served. The table is
  trimmed at most once a minute.

### Lifetime

The AIS tracks follow the swing trail's rule
([ADR 0262](0262-a-moved-anchor-keeps-its-swing-trail.md)). They are erased in
the same transaction that resets the swing trail, so a drop and a raise both
start from nothing, and the recorder forgets every target when it commits.
Moving the anchor mark keeps them, as does a radius-only change. They are real
positions of real boats, true wherever the mark is put. A tick that read the
watch as active can reach the database after Raise emptied it, so the write
itself checks that a trail is running and stores nothing when it is not. A watch
that is already set when this version first runs records from then, with no
backfill.

After a move the 500 m circle is measured from the new mark. A target now
outside it adds no points, and one that comes back inside starts a new segment,
as on any re-entry. Points already stored stay, and so do the lines through
them.

### The wire

The tracks have their own endpoint beside the swing trail, because they change at
their own pace and because folding them into `GET /api/tracks` made that answer
fail for the Nearby map when the track store did, and defeated its ETag.
`GET /api/tracks` carries the own vessel's track only.

`GET /api/anchor-watch/trails/ais?since=<cursor>` answers:

```json
{ "trail_started_at": "2026-11-05T09:00:00.123Z", "cursor": 412,
  "tracks": [ { "mmsi": "111111111",
    "points": [ { "lat": -19.998, "lon": 149.0, "t_ms": 1793882567809, "new_segment": true } ] } ] }
```

- **Cursor.** Each stored point has an increasing `seq`. `t_ms` is each target's
  own receive time, and does not rise across targets, so it can't serve. A call
  with `since=<cursor>` returns only the points stored after it.
- **trail_started_at.** The same value and meaning as on the swing trail. A
  client that sees it change drops what it holds and asks again from 0.
- **No watch.** The answer has no tracks and reads no points.
- **server_time_ms.** The server's own clock in Unix ms, on this answer and on
  `GET /api/anchor-watch/trails/self`. The map ages both trails and applies the
  24 hour cut against it, not against the browser's clock, since a display
  without internet can be hours out while the box takes its time from GPS. A
  small module of its own keeps the offset (`lib/trail-clock.ts`). It is not the
  clock of [ADR 0218](0218-the-stream-replays-the-current-payload-to-a-late-listener.md)
  (`lib/server-clock.ts`), which holds the GNSS fix time. That can lag the box's
  clock by seconds and ticks every second, so feeding both into one clock would
  make them overwrite each other. The trail clock tells the map only when the
  offset moves by a second or more. The offset ignores the request's transit
  time and is zero before the first answer.

### The feed behind the markers

The map's markers come from the nearby-vessels feed, which kept only the 10
vessels nearest the boat. In a crowded anchorage a recorded boat 300 m from the
anchor could rank 11th by distance from the boat, so it had no marker and,
because the map draws a track only for a listed target, no trail. It would also
blink in and out as it crossed rank 10. During a watch the feed now carries the
10 nearest plus every vessel within 500 m of the anchor, still in range order.
With no watch it is unchanged. The REST answer and the SSE `nearby-vessels`
event share one builder, which fetches the whole list once and selects from it,
and contact-history enrichment runs on the selection. The Nearby tile reads the
same feed, so it can list more boats during a watch. That is accepted.

### The map

The map keys tracks by MMSI. It draws a track only while its target is among the
markers, so a track outlives its circle but not its target.

Brightness follows age, not distance along the line. The old `line-gradient`
over `line-progress` faded with distance travelled. The map now splits each
trail into five age bands (`lib/trail-age-bands.ts`), colours each band, and
draws by `line-sort-key` on the band so the newest sits on top.

- **AIS:** The bands span each track's own length, from its oldest point to now
  and no more than 24 hours, from nearly clear amber to solid amber. A fixed 24
  hours left a track only hours old in the top two bands, 1.0 and 0.78 opacity,
  which look the same on the dark chart, so it showed no fade. The span is at
  least an hour: a boat seen for minutes would otherwise fade its first minutes
  to clear, and its bands would jump a large share of the track at each step
  of the clock below. They are worked
  out against a clock that moves in five minute steps, so the map redraws them
  once at each mark, with one timeout to the next, and not when each point
  crosses a band edge.
- **Own swing trail:** The same bands span the whole stay, from dim yellow to
  bright yellow, solid at the old end. This also corrects the old gradient, which
  ran light yellow to darker amber and so read newest as dimmest.

## What fails, and how it shows

- **No store.** Without the SQLite store, recording logs the fault and stores
  nothing, and the AIS endpoint answers 500 during a watch.
- **The nearby fetch fails.** The tick skips both the sighting log and the
  recorder. The log says so once when the fetch starts failing and once when it
  recovers, not every tick. The recorder's state is untouched, so the tracks
  carry on from where they were.
- **A write fails.** The tick logs the error and the next tick tries again. The
  failure is not masked.
- **No tracks plugin.** Nothing reads it, so its absence is no longer a fault.
- **Down time.** Nothing records while Helmcentral is stopped. The first point
  after a restart starts a new segment, so the map shows the gap.

## Rejected

- **Keep the plugin as a fallback.** It hides exactly the failure that led here.
- **Record every target, always.** It stores tracks nobody looks at and would
  need a pruning policy for a whole season's traffic.
- **Record per poll tick.** A target repeats the same position until it
  reports again, so this stores the poll rate, not the report rate.
- **Store every report.** A 2 second reporter then stores 43,000 points a day.
- **Send every track whole on each poll.** A crowded anchorage over a day is
  about a megabyte, every five seconds.
- **A gradient over line progress.** It dims with distance, not time.
- **A timer per point at each band edge.** With twenty targets that wakes the map
  tens of thousands of times a day. The five minute clock wakes it 288.

## Consequences

- AIS tracks have real times and survive a Helmcentral restart.
- Operators can drop the tracks plugin, and `SIGNALK_TRACKS_PATH` is gone.
- `/api/anchor-watch/trails/ais/:id` is removed, and `/api/anchor-watch/trails/ais`
  now answers in the shape above. Nothing in the app used the old ones.
- Boats that anchor outside the circle are visible as markers only. Widening
  the circle means changing the constant.
