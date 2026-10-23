# ADR 0206: Camera shots direct the passage replay

## Status

Accepted (2026-10-23). Third of four steps toward a cinematic passage replay,
after [ADR 0200](0200-passage-replay-in-3d.md) (3D) and
[ADR 0202](0202-passage-replay-boat-model.md) (the boat model).

## Context

A 3D replay with a boat in it invites a camera that does more than sit over the
whole track. An operator filming a passage for a blog wants to open on the
overview, chase the boat through a pass, circle her at the anchorage and slow
down for the interesting stretch. Whatever they set must survive editing the
passages themselves: the passage finder splits, the operator merges and trims
(ADR 0176).

## Decision

1. **Shots are stored on the server, keyed by real time.** A shot is a row in
   `replay_camera_shots` in the passage store: `t_ms` (Unix milliseconds), `mode`
   (overview, follow, orbit), `zoom`, `pitch`, `bearing_offset_deg` (follow),
   `orbit_deg_per_s` (orbit, negative for the other way), `pace` (1, 0.5 or
   0.25), and created and updated times. It carries no passage id. Splitting or
   merging a passage leaves every shot where it was, since the instant is
   unchanged. A trim that cuts a shot out hides it (the replay only plays shots
   that fall inside a replayed passage) and undoing the trim brings it back. A
   replay of several passages picks up every shot inside any of them. With no
   shots the replay is exactly what it was: the fly-in and the overview.
2. **Validation is on the server.** Mode is one of three, zoom 0 to 22, pitch 0
   to 70, pace one of 1, 0.5, 0.25, bearing offset -180 to 180, orbit rate -90 to
   90. A new shot's time must fall inside some passage's span, hidden or not, at
   creation. Routes sit in the passages route table: `GET
   /api/passages/camera-shots?from=&to=` is read tier, create, patch and delete
   are write tier (ADR 0177). The store follows the passage store's own
   transaction habit (`db.Begin`, rollback on error); the shared `RunTx` helper
   of ADR 0184 belongs to the record store, which the passage store is not part
   of.
3. **A shot lasts until the next one.** Before the first shot the whole-track
   framing holds. **Overview** frames the whole replayed track with the
   pitch-aware fit of ADR 0200. **Follow** is a chase camera centred on the boat,
   bearing = the boat's heading (the smoothed course of ADR 0202) plus the
   offset (0 from astern, 90 abeam), at the shot's zoom and pitch. **Orbit**
   circles the moving boat at the shot's zoom and pitch, starting from the
   heading at the shot.
4. **Orbit speed is per second of watching.** The bearing advances by
   `orbit_deg_per_s` for each second the operator watches the shot, not each
   second of passage time, so choosing a faster or slower replay does not change
   how fast the camera spins.
5. **One piecewise clock.** The replay clock, the scrubber and the boat stay in
   timeline seconds. A pure module maps watching time to timeline time and back
   through a piecewise rate: the chosen replay length sets the base rate, and
   each shot's pace multiplies it from the shot's start. A quarter-pace shot
   makes the replay longer to watch. The same mapping measures orbit time, so a
   seek puts the camera in exactly the state continuous play would have reached.
   A 60 s length joins Slow, Normal and Fast.
6. **A director that keeps the shot's own time.** Each shot activation records
   how long it has been watched, the camera it eases from and the heading it
   started on. While playing, the watched time grows by the real time between
   frames, so changing the replay speed or editing a pace (even an earlier one)
   leaves the Orbit bearing continuous and never restarts an ease. A seek or
   scrub sets the watched time once from the replay's clock mapping, so the
   camera lands where continuous play would have. The ease (ease in and out,
   1.5 s of watching) starts from the map's actual camera, read once when the
   shot activates or control is handed back, so after the operator has moved the
   map the next shot eases from where they left it with no snap. Scrubbing
   places the camera directly, with no ease. The shot's start state is read in
   the passage the shot's time falls in (not whichever passage the clock is in
   at a boundary), and a passage with no position there gives no camera update
   rather than an error. The map applies the result with `jumpTo` each time the
   replay clock moves, only when it differs from the camera already there;
   editing a shot while paused does not move the view. The clock's piecewise
   table is built once per set of shots and length and searched by bisection.
7. **The operator can take the camera.** A drag past about 4 px, a second
   finger, the wheel, or a MapLibre `movestart` made by a person (it carries an
   `originalEvent`) stops the replay placing the camera until the next shot
   starts, or Play or a scrub hands control back. A tap or click does not. The
   pointer is watched directly as well as `movestart`, because the camera's own
   jumps each frame can swallow a drag's own `movestart`.
8. **Flat map.** With 3D off or the terrain failed (the map reports which, and
   the editor's hint follows the same answer), shots play as zoom and centre
   only: pitch 0, bearing 0.
9. **Reduced motion.** There is no continuous camera motion. At each shot start
   the camera cuts to the shot's framing (Follow: boat centred at that moment
   with the shot's zoom, pitch and bearing; Orbit: a fixed bearing, the heading at
   the shot). In Follow and Orbit the camera cuts again when the boat reaches
   outside the central 60% of the view, so she cannot leave the picture. Terrain,
   model and track are unchanged.
10. **Sky.** While the terrain is up the tilted map has a sky: a sky colour, a
    horizon colour and a horizon fog blend, with day and night variants, so the
    horizon fades into the sky rather than standing blank white. It is removed
    when the map is flat and re-applied after a style change.
11. **Editor.** A Camera button on the replay bar opens a compact editor. "Add
    shot here" creates a Follow shot at the replay's current instant with the map's
    present zoom and pitch. Each shot is a mark on the scrubber; clicking one
    seeks to it and opens its fields: mode, zoom, pitch, angle (Follow), orbit
    rate (Orbit), pace, "Use current view", Delete. The overview map in the corner
    is untouched.

## Rejected

- **Storing shots per passage.** A split or merge would have to move, copy or
  drop them, and a trim would delete them. Time is the one thing those edits do
  not change.
- **A keyframe editor for every camera parameter.** Powerful, but a timeline
  of curves is far heavier than a skipper filming a passage will use. Three
  modes with a few numbers cover the shots people want.
- **Orbit rate in passage time.** The camera would spin faster when the replay is
  played faster, and a slowed shot would barely turn.
- **Deriving orbit time and the ease from the replay clock.** Changing the
  speed or an earlier pace would jump the bearing and restart the blend.

## Consequences

- Shots made for a passage stay with its time. If the passage is later
  re-imported with different times they show where the clock now says.
- A shot time is validated only when it is created or moved; a later trim may
  leave a shot outside every passage, where it is kept but not played.
- The pace stretches only the replay. Exports and the embedded player (ADR 0199)
  do not use shots yet.
