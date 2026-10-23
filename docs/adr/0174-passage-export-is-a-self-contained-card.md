# ADR 0174: The blog export is a self-contained card that fetches public tiles

## Status

Accepted (2026-10-14). Amended by [ADR 0205](0205-cinematic-passage-embed.md): the
player is now built from the app's own code and draws terrain, the boat model and
camera shots.

## Context

The operator wants passages and Cruises on the boat's blog (Ghost) as an
animated map readers can replay, replacing a Saillogger embed. Helmcentral runs
on the boat behind Tailscale, so a blog reader's browser cannot reach it, and
moving the data to a hosted service is the cloud dependency the feature exists
to leave.

## Decision

1. **The export is one HTML snippet** pasted into a Ghost HTML card. It loads
   MapLibre from a public CDN and carries the simplified track, readings for the
   replay display and the stats strip inline. Nothing in it points back at the
   boat.
2. **Tiles come from public servers in the reader's browser**: Carto for the
   base and OpenSeaMap seamarks (on in cycle 1), with attribution. The boat's
   tile proxy and offline caches are not used.
3. **The replay waits for a tap**, showing the whole track first. It opens with
   a zoom from globe height so readers who do not know Australia see where the
   boat is, then the inset map appears if it was on at export. Play time is a
   fixed length chosen at export; each night at anchor collapses to a short
   captioned pause.
4. **Times are boat-local and labelled** (for example "06:40 AEST"); units are
   the operator's.
5. **Only finished passages are exported**, never live position. No trimming
   of anchorages: the boat broadcasts AIS, so the track adds no material
   exposure.
6. **Static images and the later video export render the same player**, so
   all three look alike.

## Considered options

- **Publish to a hosted bucket the operator runs.** A live URL, but another
  account and service to keep. Rejected.
- **Link to the boat over a public tunnel.** Exposes the boat. Rejected.

## Consequences

Each export is a snapshot: editing a passage afterwards means exporting again.
The card depends on a public CDN and Carto's free basemap terms, both of which
suit a personal blog.
