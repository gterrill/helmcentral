# ADR 0173: Helmcentral records its own track and passage readings

## Status

Accepted (2026-10-14). Mate's `list_passages` ([ADR 0166](0166-mate-reads-logged-telemetry-for-history-questions.md))
keeps reading InfluxDB until it is moved onto recorded passages in a later cycle.

## Context

The Passages page is a ship's logbook: each passage's track, departure and
arrival, fuel burned, wind, and at Cruise level generator hours, house battery
and water. Three sources already existed and none of them fits.

- **InfluxDB** holds everything the SignalK plugin logs, and Mate's
  `list_passages` already finds passages in it from speed alone. But Influx is
  optional ([ADR 0020](0020-in-memory-telemetry-history-optional-influxdb.md)),
  Helmcentral never writes to it, retention is the operator's bucket setting,
  and position needs a pivot query nothing in the code runs any more. A logbook
  that vanishes when Influx is off, or when the bucket ages out, is not a log.
- **The in-memory trails** ([ADR 0001](0001-server-owned-trail-sampling.md),
  [ADR 0002](0002-separate-motoring-and-anchor-trails.md)) hold about 83 minutes
  and are lost on restart.
- **signalk-daily-gpx-plugin**, which the boat runs today, writes position-only
  GPX files. It has no readings, no fix filtering beyond null island, ends a
  voyage after five quiet samples with no grace period, and its file route
  likely allows path traversal.

## Decision

1. **Helmcentral records a track point log in its own SQLite store**, append
   only, kept indefinitely, always on. It is the source of truth for passages.
   Influx is not required for any part of the Passages page.
2. **Keep rule.** A point is kept when the boat has moved 25 m and at least 5 s
   have passed; at rest one point every 10 minutes regardless. A change in
   generator state always forces a point, so generator hours are summed from
   running intervals (the GX `runtime` resets every run). The same will apply to
   the watermaker circuit once CZone circuit data reaches SignalK.
3. **Each point carries a fixed set of readings**: SOG, COG, true wind speed,
   gust, house SoC, total fuel rate, generator state, fresh water level (all
   freshWater tanks summed). A reading older than 10 s is stored as absent,
   never carried forward.
4. **Fix filtering.** The -1/-1 sentinel, `gnss_critical_alert`, out-of-range
   coordinates and any jump implying more than 40 kn from the last kept point are
   rejected. A gap of more than 10 minutes with no fix starts a new segment.
5. **Passages are detected from the log and then stored as records**, so the
   operator's edits (split, merge, trim, hide, names) survive. A passage starts
   at 3 kn held for 2 minutes, backdated to the last at-rest point; it ends after
   60 minutes below 1 kn or when the anchor is dropped, whichever is first. Stops
   under 60 minutes stay inside the passage (filling 5500 L of fuel takes a
   while). Runs under 15 minutes or 0.5 NM are dropped. The detector never
   recreates a passage the operator hid or edited.
6. **GPX import** feeds the same detector, with speed derived from successive
   fixes. Imported passages have no readings and show `--` for them.
   Imported tracks never use the ten-minute segment gap. The daily-gpx plugin
   simplifies its tracks and writes nothing while the boat is at rest, so a long
   gap between two fixes is either a straight leg (a few points many minutes
   apart) or a stop, not missing data. The speed a gap implies decides which: at
   one knot or more the boat travelled through it and the track is continuous;
   under one knot it was at rest, and the sixty-minute stop rule decides whether
   the passage ends. A passage departs at the fix that ends a long rest, not at
   the fix that began it.
7. **Simplification happens only on output** (display and export), in a metric
   projection. The log keeps full resolution.

## Considered options

- **Query Influx on demand.** Day-one history for free, but makes the logbook
  depend on an optional service the operator configures retention for. Rejected.
- **Keep using the GPX plugin.** One more SignalK plugin to depend on, position
  only, and the segmentation flaws above. Its good ideas (on-disk buffer, speed
  and distance gate, simplify at output) are kept.
- **Enrich passages from Influx when present.** Would bring the dependency back
  for Cruise metrics. Recording the readings on every point covers them instead.
  Influx enrichment may come later for imported passages only.

## Consequences

Passages exist only from the day the recorder ships, plus whatever is imported
as GPX. Once it is running, the daily-gpx plugin can be removed from the boat.
`list_passages` and the Passages page use different detection rules until Mate
is moved onto the recorded passages.
