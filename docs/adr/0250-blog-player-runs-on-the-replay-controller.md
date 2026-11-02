# ADR 0250: The blog player runs on the replay controller

## Status

Accepted (2026-11-02). Changes the player that [ADR 0174](0174-passage-export-is-a-self-contained-card.md)
and [ADR 0205](0205-cinematic-passage-embed.md) describe, and takes what
[ADR 0247](0247-passage-video-is-rendered-in-the-browser.md) did for the video to
the page. Photo moments are [ADR 0248](0248-photo-moments-hold-the-replay.md).

## Context

The blog card carried a replay player of its own. It had its own clock, which put
a pause in for each night at anchor, its own fly-in, its own heading smoother, its
own sign timing and its own wiring of the camera shots. The app's replay had
since grown a controller that owned all of those, plus the sun, the track coloured
by speed, photo moments, stays, passage labels and a framing for each passage of
a cruise. The video export already stepped that controller on a plain MapLibre
map. The page was the one place that still played something older, so a reader
saw a different replay from the one the operator had built.

## Decision

**The page plays the app's replay, and keeps nothing of its own.** The player
hands the card's passages, shots and signs to `planFromItems`, the planner the app
and the video use, and builds the time map with `replayConfigOf`, the same
function the video calls. `ReplayController` works out every frame, and
`ReplayPlayer` holds the place in it. The player puts the frame on a plain MapLibre
map as the video's scene does, through `replay-map-style` (the tint, the whole
track, the speed colours, the wake, the light) and a new `replay-overlays`, which
the video's scene now shares. Signs are placed by `placeSignCard`, the opening is
`replay-intro`, a cruise is framed by `replay-framing`, and the length is shared
between passages by the same minimum-share rule. The player's clock, night steps,
heading smoother, fly-in and camera wiring are deleted.

**The card carries only what the planner needs.** Each passage now has its
departure and arrival names, which the stays and passage labels read, and the
night counts and pauses are gone, because the controller works out each stay
from the passages and the boat's offset (`zone.o`, which the card has always
carried). A sign says whether its photo has a moment (`mo`).

**A sign ships one photo, at the size its moment needs.** A sign whose photo moment
is on carries its photo at 1280 pixels on the long edge (`ph`, with `mo` marking
the moment), and the page draws that one photo in the small card and in the grown
one. A sign whose moment is off carries today's 960 pixel photo. A source smaller
than the edge is not enlarged. The export menu prepares one photo per sign, at
the size its moment setting asks for, and its size readout and the over-the-limit
message count that one photo. A photo that cannot be prepared stops the export,
as before. The page asks the browser to decode the photo while the card is up,
so growing it later does not stall a frame.

The operator chose this over carrying a second, larger copy beside the card's own.
That design was first built, and it measured 251 KB for the card copy plus 426 KB
for the larger one, against 426 KB for a single larger photo. Within the 3 MB
limit that is about four photos with two copies and about seven with one.

## What stays different from the replay

- **No satellite photographs.** The blog map is always the chart, because the
  imagery comes through the boat's own proxy, which a public page cannot reach.
- **No terrain from the boat.** The page keeps its own protocol that fetches the
  public elevation tiles and flattens the sea in the reader's browser. A failure
  drops the page to flat and says so.
- **The place names are the style's own.** The page cannot use the app's label
  layers, so it raises the chart style's name layers above the time-of-day tint.
- **Pins return at the end.** The app shows no pins once a replay has finished.
  The page shows them again, so a reader can open a photo.

## Consequences

- A page, a video and the replay show the same thing, and a change to the replay
  reaches all three. The cost is size: the player grows by about 30 KB, and a photo
  with a moment ships at 1280 pixels instead of 960, against the 3 MB limit.
- The page imports lib modules that must stay free of React and the app's
  configuration. The player's tests check the built text for both.
- Pages already exported keep the older player, which is inside them. They work
  as they did, and the operator re-exports to get the new behaviour. There is no
  migration.

## Rejected

- **Keeping the page's clock and fixing it to match.** Two clocks drift, which is
  how the page fell behind.
- **Two copies of every moment photo.** The card's 960 pixel copy plus a 1280 pixel
  one for the moment fitted about four photos in 3 MB, against about seven for one
  larger photo. A moment off ships the smaller photo, so the larger size is paid
  only where the card grows. (Built first, then replaced at the operator's choice.)
- **Terrain through the boat.** A public page has no route to the boat.
