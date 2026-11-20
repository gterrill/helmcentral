# ADR 0296: Passage Motion Is Backfilled From InfluxDB History

## Status

Accepted (2026-11-21). Builds on
[ADR 0295](0295-passages-record-snap-and-pounding-from-a-10-hz-motion-stream.md),
which records the live figures, and keeps to the standing rule that InfluxDB
stays optional.

## Context

ADR 0295 records sway, snap and pounding as each track point is taken. Every
passage before it has none, and the comfort fit planned on top of it needs
history to learn from. The boat's InfluxDB raw bucket holds
`navigation.attitude.roll`, `.pitch` and `.heave` at about one reading a second
for as long as its retention reaches, and heading, true wind angle, speed
through the water and each engine's revolutions alongside. That is too slow for
any derivative, but it is enough for roll and pitch RMS and the heave spread.

Two review passes found bugs in how the backfill picked its points: a rule built
on "no live motion yet" and on the gap since the previous point kept mistaking a
live outage for history, and a window that stretched with that gap measured
different things for different points. The design below replaces that rule.

InfluxDB is optional, and no feature may depend on it. A passage list that fills
its figures from InfluxDB must therefore leave them empty, and say so, when
InfluxDB is not set up.

## Decision

### 1. One background run per start

When the backend starts it runs the backfill once in the background, in a
goroutine on the context that ends the live streams, so it stops at shutdown. It
never blocks start-up and never fails the app.

- Without InfluxDB settings, the run logs "motion backfill skipped" with the
  reason at each start, and ends. A secrets store that cannot be read gets its own
  message.
- A query that fails for one span (a bad response, a timeout after connecting)
  is logged and leaves its span unstamped, so the next start asks again, and the
  run carries on with the next span: one span whose query always fails must not
  block every later one. A failure is never read as "no data".
- A failure every later query would repeat ends the run at once with one log
  line: a host that cannot be reached (DNS, connection refused, no route, a
  timeout while connecting), an HTTP 401 or 403 (the token), a 404 (the org or
  bucket) or a TLS certificate error. Nothing is stamped and no finished marker is
  stored, so the next start tries again. Any other error belongs to one query and
  skips its span, as above.
- A store failure or the shutdown context also ends the run.
- When it finishes it logs one line: points with sway figures, points with
  context readings only, spans with nothing in InfluxDB, spans that failed.
  Points with only a heading, wind angle, speed or rpm are counted apart from
  points that got sway, and a span where no point got anything is counted as a
  span with nothing.

### 2. The cutoff, and what it fills

The cutoff is the earlier of two moments, worked out on each run.

- The first start of the 10 Hz recorder. `passage_motion_state` stores it as
  `capture_start`, written at start-up if absent and before the recorder starts,
  so no point the recorder writes can fall before it. It is never changed or
  moved later, so a boat with no attitude sensor has a cutoff too, and so a
  bounded job.
- The first recorded point that carries any column the live recorder introduced
  (a motion figure, `motion_rate_hz`, `heave_rate_hz`, `heading_deg`, `twa_deg`,
  `stw_kts` or `rpm_mean`, not null), ignoring points the backfill itself
  stamped. Only the live recorder writes those, so this is when live capture
  really began, whatever the clock said. A build that ran before the backfill
  existed has already captured live, and a stored start that is too late, or a
  wall clock that was wrong, is overridden by it.

A target is a track point that Helmcentral recorded itself (point source
`recorded`; GPX imports are never targets) stamped before the cutoff and not yet
asked about (see 6). Every such point is a target, whatever it holds: nothing
before the cutoff can have live motion. A point at or after the cutoff belongs to
the live recorder. A live outage after the cutoff is never
filled: it stays empty, where it can be seen, and is not passed off as history.

Once every point before the cutoff has been tried (stamped), the run stores a
`backfill_finished` marker, and later starts skip the backfill without opening
InfluxDB, with one log line saying it is complete. A span whose query failed
leaves its points unstamped, so the marker waits until a later start has tried
them. Points never appear before the cutoff afterwards, so nothing needs to be
searched for again.

The write is `column = COALESCE(column, ?)` per column, by point id, in one
transaction per span, with one statement prepared for the span. A value already
there, or one that appeared between the read and the write, is never replaced.

The ADR 0294 peaks stay null. A maximum taken from one reading a second is a
lower bound that would pass for the real thing.

### 3. Spans, queries and windows

Targets are chosen in SQL through the time index. They are grouped into spans: a
gap of over 30 minutes between targets, or a span longer than 6 hours, starts a
new one. Each span costs at most two bounded Flux queries, each over the span's
range (60 s before the first point to the last point), built by the same code as
the learned normal's minute query minus its aggregation:

- roll, pitch and heave, then, always, because InfluxDB holds the context where
  its attitude sensor was silent,
- heading, true wind angle, speed through the water and every engine's
  revolutions. The revolutions come from an anchored pattern over the span
  (`^propulsion\..*\.revolutions$`) and not from a scan of the bucket (a scan of
  the whole range timed out). The pattern is broad, and the rows are kept by the
  check the live recorder uses for an engine's revolutions path, so the two
  cannot disagree about what an engine is.

Every query goes through `fluxStringLiteral`, and the bucket is the configured
`influxdb.bucket`. The field is `value`, the one the SignalK writer stores, a
constant of the backfill and not the depth field setting.

A point's window is the 60 seconds ending at the point, whatever lies between it
and the previous point. Windows of neighbouring points may overlap. Every point
is measured over the same length of time, so one point's figures are comparable
with another's, and no previous-point lookup or window cap is needed.

Several `source` tags can exist for one path, and the source is chosen within each
point's own window, so a swap from one source to another part-way through a span
does not blank the rest of it. Heave keeps the source with the most points in the
window (ties go to the name that sorts first, so a rerun picks the same). Heading,
wind angle, speed and each revolutions path choose among the sources that have a
reading at or before the point within 10 s, as live does: the one with the most
readings in the window, then the one heard from last, so a source that was busy
and went quiet 20 s before the point does not hide a fresh one. Roll and pitch
match by the same key, the millisecond, in the choice and in the pairing. The boat's heave arrives from a different source than its roll
and pitch, so heave is chosen apart. Roll and pitch are chosen together: the
source with the most timestamps in the window that have both, so they cannot land
on different sources and pair to nothing. This differs from the learned normal,
which pins a reading to the sender of its latest minute because it scores what
the boat reports from now on. A backfill replays one window of the past, and the
source that reported most of it is the one that covers it.

### 4. The same accumulator, driven by sample time

The samples go through `motionAccumulator`, with its clock set to each sample's
own time, so the RMS, the heave spread, the measured rate and the 8 Hz floor
follow the live rules without a second implementation. One change makes that work:
the live accumulator treats a silence over 0.45 s as the end of a run, so one
reading a second is a series of one-sample runs with no measured rate and no RMS.
The longest bridged silence is one setting of each channel, fixed when the
accumulator is built: 0.45 s for the live stream, and 3 s for the backfill. The
stream then counts as continuous, its measured rate comes out near 1 Hz, and the
floor leaves every derivative null while the RMS and spread are given. The grid
fill between readings is linear, as live. Each point's window starts its channels
afresh, so its first reading begins a run: a reading from the window before is not
bridged to, and no grid point or interval stamped before the window's start feeds
its figures or its rate. A silence over 3 s restarts a run within a window too.

Roll and pitch arrive as separate measurements written from one attitude update,
so they share a timestamp (checked on the box 2026-11-21). A roll with no pitch at
its timestamp is dropped, as the live stream needs both.

What a point gets:

| Figure | Filled |
| --- | --- |
| Roll RMS, pitch RMS, heave spread | yes |
| Attitude rate and heave rate found (about 1 Hz) | yes, so the logbook can say "arrived at about 1 Hz, needs 8 Hz" |
| Roll rate, acceleration, jerk, pitch acceleration and jerk, vertical acceleration | never; null below 8 Hz |
| Heading, true wind angle, speed through the water | the latest reading at or before the point and within 10 s, read by the live recorder's own function (it never sees a reading from the future) |
| Mean rpm | see below |

### 5. Engine speed without engine state

Live, the mean skips an engine the snapshot knows is switched off and goes absent
when an engine that is not off has stopped reporting. InfluxDB holds the
revolutions but not that state. The backfill turns the latest reading at or
before the point, within 10 s, of each engine whose path appears in the span (its source chosen within the point's window, as in 3) into the same kind of reading
the live recorder gets, and runs the live functions over them (the conversions,
the freshness limit and the mean), so a change to a rule reaches both. It gives
no mean if any engine has no reading within 10 s: an engine that was off and one that died look the same here,
and a guess either way would mislead. An engine reading zero is not running. With
none running the mean is absent. An engine that logged nothing at all in the span
does not appear, and does not count. A boat that stops one engine part-way through
a passage gets no mean after that.

### 6. Attempted points

Track points carry a nullable `motion_backfill_ms`, the time the backfill last
asked InfluxDB about the point. When a span's queries succeed, every point in it
is stamped in the same transaction as its figures, whether it got any or not. A
stamped point is not a target on any later start, so a span that InfluxDB had
nothing for, such as one older than its retention, costs one query once. The
column and the `passage_motion_state` table are added when the store opens, and
the column starts null. Nothing already saved is rewritten, and no operator
action is needed.

## What it is not

- Not a source for snap, pounding or any derivative, now or later. A passage
  recorded before the live feed has none.
- Not a replacement for the live recorder. It only touches a point recorded before
  the cutoff, and only fills columns that are empty.
- Not a dependency. Nothing else reads InfluxDB for passages, and a boat without
  it runs as before.
- Not a fallback. A missing source is left null and the rate says why.

## When it can't run

- InfluxDB is not set up: one log line, nothing filled.
- InfluxDB cannot be reached, refuses the token (401, 403), has no such org or
  bucket (404) or fails TLS: the run ends with one log line, nothing is stamped,
  no finished marker is stored, and the next start asks again.
- One span's query fails: that span is logged with the error and left unstamped,
  the run goes on to the next span, and the next start asks again.
- Every point before the cutoff has been tried: the finished marker is stored and
  later starts skip the backfill.
- A retention window that has passed: the span returns nothing and is noted as
  attempted.
- Raw data drops out of `SignalK_Data` around January for October passages. A
  later change that widens the one-minute task would let a rerun find them in
  the minute bucket; this run reads the raw bucket only.

## Follow-ups

- Read the one-minute bucket when the raw data has aged out.
- Show the backfilled sway on the passage page when the logbook gains its motion
  strip.
- The finished check runs twice, in the once wrapper and again in
  `runMotionBackfill`. One would do.
- `fromSource` re-sorts rows that are already in order, for every point.
- `normalInflux.field` is read from `INFLUX_DEPTH_FIELD`, while the backfill uses
  its own `value` constant. The two should come from one place.
