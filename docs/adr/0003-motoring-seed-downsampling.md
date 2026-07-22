# ADR 0003: Downsample Influx Seed For Motoring History

## Status
Superseded by ADR-0020

The Influx startup seed this ADR describes has been removed entirely (see
ADR-0020). The motoring trail now starts empty and fills purely from live
sampling; this document is retained for historical context only.

## Context
On startup the server seeds the motoring ring buffer from InfluxDB so `/api/tracks/motoring` is useful before any live server-side sampling has occurred.

Raw `navigation.position` data from Influx can be extremely dense and can include multiple SignalK sources. Loading raw points into a 1000-point ring buffer caused the buffer to fill with the final cluster near anchor drop instead of representing the full approach.

## Decision
Downsample the startup seed from Influx before inserting into the motoring ring buffer.

- Query a bounded historical window.
- Use `aggregateWindow(..., fn: last)` before pivoting position fields.
- Seed the motoring ring buffer with the downsampled result.

## Consequences
Positive:
- The seeded motoring trail represents the full spatial span of the approach.
- Ring-buffer capacity is used for coverage instead of redundant high-frequency points.
- Startup behavior aligns better with what users expect to see on the map.

Negative:
- Seed data is lower fidelity than raw Influx history.
- Downsampling interval is a tuning decision that may need adjustment for different operating profiles.

## Notes
This applies only to startup seeding of the motoring history. Ongoing live trail capture remains governed by the server polling cadence.
