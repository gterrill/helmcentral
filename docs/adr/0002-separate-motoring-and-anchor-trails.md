# ADR 0002: Separate Motoring And Anchor Trails

## Status
Accepted

## Context
The pre-anchor motoring approach and the post-anchor watch trail are visually and operationally different.

- The motoring track is used when repositioning the anchor to see where the vessel came from before anchoring.
- The post-anchor trail shows movement after anchor watch becomes active.

Earlier implementations mixed these concerns together, which made the code brittle and made endpoint semantics unclear.

## Decision
Keep motoring and anchor-watch trails separate.

- Ongoing anchor-watch/self/AIS trails are exposed through `GET /api/tracks?since=<timestamp>`.
- The motoring approach trail is exposed separately through `GET /api/tracks/motoring`.
- The anchor watch map fetches the motoring approach only when entering anchor reposition mode.
- The map renders motoring and post-anchor trails with different visual styles.

## Consequences
Positive:
- Clear endpoint responsibilities.
- Simpler mental model for future changes.
- Reposition-only behavior stays isolated from ongoing anchor-watch trail updates.

Negative:
- There are multiple trail endpoints instead of a single aggregate endpoint.
- The frontend needs explicit logic for when motoring history should be fetched.

## Notes
This separation is intentional even if both features use similar underlying data structures such as ring buffers.
