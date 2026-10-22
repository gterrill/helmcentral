# ADR 0199: The Anchor Swing Trail Has Its Own Stored Record

## Status

Accepted

## Context

The Anchor Watch map's yellow trail was drawn from `GET /api/tracks` `self`,
which `getTracksHandler` serves from `selfTrack`: an in-memory ring buffer of
1000 points, one per 5 s poll tick (`sampleTracks`, `backend/tracks.go`). That
is about 83 minutes, it is lost on every backend restart, and it is not scoped
to the anchoring at all. On the night this was found the anchor was set at
06:59 UTC and the trail held 11:34 to 12:57, a 19 by 11 m scribble hidden under
the boat arrow. A second ring buffer, `selfTrail`, was reset on drop and
reposition and served `GET /api/anchor-watch/trails/self`, but nothing in the
frontend read that endpoint.

The always-on track point log (ADR 0176) was the obvious source, but its keep
rule (`shouldKeep`, `backend/passages_recorder.go`) stores a point only after
25 m of movement or ten minutes at rest. At anchor, swinging inside 20 m, that
is about six points an hour, far too few to draw a swing.

## Decision

1. A separate table, `anchor_trail_points (t_ms, lat, lon)`, in
   `helmcentral.sqlite` beside the track point log (`backend/anchor_trail.go`),
   with a one-row `anchor_trail_state` holding the trail's start time.
2. The 5 s poll tick offers one fix per tick (`recordAnchorTrailPoint`) only
   while an anchor watch is active and only for a real fix
   (`hasUsableVesselPosition`, so the -1,-1 no-data pair is never stored).
   A fix is stored when it is 1 m from the last stored point or 60 s after it,
   which keeps a swinging boat's trail to a few thousand points a night. One
   limit bounds the trail: the newest 10000 points are kept (trimmed every 100
   stored points) and served; there is no age limit. Each tick's check and
   insert run in one transaction: the database serialises transactions on its
   single connection, so no extra lock is used. A fix is stored only while the
   state row shows a running trail (`started_ms` not 0) and the fix was sampled
   at or after that start, so a tick that raced a raise or a reposition cannot
   restart or pollute a trail.
3. A drop, a re-POST reposition and a PATCH that changes the position restart
   the trail (rows deleted, new start time). A PATCH that changes only the
   radius does not. Raise, by the operator or by the server-side auto-raise
   (ADR 0099), both through `raiseAnchorWatch`, deletes the trail. The reset
   comes before the new watch is saved and published, so no reader sees the new
   anchor with the old trail, and a failed reset fails the drop, the move or the
   raise (500) rather than leaving the two disagreeing; the auto-raise logs it.
   A move's reset runs after the SignalK publish and before the local save. `set_at` is
   not usable as the restart marker: it is the anchoring session's identity and
   deliberately does not change on reposition (ADR 0064).
4. `GET /api/anchor-watch/trails/self?since=` returns
   `{points, trail_started_at}`: up to the newest 10000 points, oldest
   first, after `since`; empty with no watch. `trail_started_at` changes
   whenever the trail restarts. A missing store or a failed query is logged and
   answered 500; it is not an empty trail.
5. The in-memory `selfTrail` ring buffer and its recorder are removed, with no
   fallback to them. `selfTrack` stays: `/api/tracks` `self` still feeds the
   Nearby map's own-boat trail, which is not anchor-scoped.
6. The frontend reads the trail through `useAnchorTrail`
   (`frontend/src/hooks/use-anchor-trail.ts`), used by the anchor watch tile
   and drawer. It polls incrementally with `since`; when `trail_started_at`
   changes it drops its points and refetches in full. Its cap equals the
   backend's response bound.

## Consequences

- A restart mid-anchorage keeps the trail. An active watch restored from
  `anchor_watch.json` serves whatever is in the table. A watch already running
  when the table first appears gets its trail started once at startup
  (`startRestoredAnchorTrail`); nothing else starts a trail lazily.
- The map renders up to 10000 points as one GeoJSON line.
- An unavailable store fails a drop, a move or a raise loudly, as the fallback
  policy requires. A raise whose trail clear fails has already removed the
  watch; Raise is idempotent and can be retried.
- Two tick-rate recorders now exist (`selfTrack` for the Nearby trail, this
  table for anchoring). Moving the Nearby trail to a stored record is a
  separate decision.
