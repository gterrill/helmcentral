# ADR 0248: Photo moments hold the replay

## Status

Accepted (2026-11-02). Builds on [ADR 0204](0204-passage-replay-signs.md), which
put signs on the replay, and [ADR 0206](0206-passage-replay-camera-shots.md),
which put camera shots there and gave the replay its paced clock. The replay
runs in the shared controller of [ADR 0200](0200-passage-replay-in-3d.md)'s map,
which the coming video export steps frame by frame.

## Context

A sign's photo sits in a card the size of a postage stamp, on a replay that
moves. The card shows for a few seconds beside the boat, and there is no time to
look at the photo. The photographs are the part of a passage a reader most wants
to see, and the replay hurries past each one.

The operator wanted the replay to stop for them: hold still where the boat meets
a sign, let the photo grow out of its card to fill most of the map, and carry on.
Three facts about the replay shaped how.

- Signs and camera shots belong to a moment in time, not to a passage. Splitting,
  merging or trimming a passage leaves them where they are (ADR 0204). Anything
  that belongs to a sign has to belong to the sign.
- The operator picks the replay's length, 8, 15, 30 or 60 seconds, and means it.
  A shot's pace already stretches the watching (ADR 0206), so the replay has two
  clocks: the timeline, which the slider and the boat run on, and the watching,
  which the operator sits through.
- The video export draws the replay frame by frame on a canvas, so nothing about
  a moment can be read from the page. The page's own numbers have to be the
  numbers the export uses.

## Decision

**A sign carries one switch, a tri-state.** `replay_signs.photo_moment` is a
nullable column, added in place with `ensureColumn`. Null is the default, and the
default is on for a sign with a photo. Only an explicit false turns the moment
off, so a photo added later gets its moment without a second step. An explicit
true is the same as null and is stored only because the operator pressed the
switch. The API takes `true`, `false` or `null` on create and on patch, where
patch distinguishes a field left out (untouched) from `null` (back to the
default), the way `document_id` does; a small generic helper, `decodeNullable`,
now decodes all three of those fields. The editor shows a **Photo moment** switch
for a sign whose photo is available, on until it is turned off. Whether a sign
has a moment is one rule, `photoMomentOn`: the photo is there and the choice is
not false. `momentsOf` lists the signs that qualify, leaving out an untitled join
and any sign whose photo failed to load.

**A moment is a hold in the watching, and it adds to the chosen length.** The
length the operator picked is the sailing. Each moment inserts a hold of 2.3
seconds at the sign, so a replay with three moments takes about seven seconds
longer than the number on the length list. Taking the holds out of the chosen
length was the other option, below.

**The replay keeps a time map, `lib/replay-time`.** It builds from the timeline's
length, the chosen length, the shots' paces and the moments, and answers four
questions, all pure:

- `at(wall)`: the timeline position after `wall` seconds of watching, and the
  moment if the watching is inside a hold, with the seconds into it.
- `wallAtClock(clock)`: where a seek to a timeline position lands in the
  watching.
- `holdEnd(wall)`: where the hold the watching is in ends.
- `sailingWallAt(clock)`: the sailing alone, without holds, which the camera
  uses so an orbit does not turn during a hold.

The player holds one number, the watching time, and derives everything else from
it. The controller's frame input is that number and nothing more, so a frame
cannot disagree with itself about whether it is in a hold.

**The slider runs over the sailing, and a seek never lands mid-hold.** A hold has
no length on the slider. A seek to any timeline position lands clear of every
hold, after those of any sign standing exactly there, so seeking to the end is
the end. The one exception is an explicit jump to a sign (its mark on the slider,
or a nudge that moves it), which lands at the start of its moment so the
operator sees what they asked for. When the timing changes under a replay (a
shot's pace edited, a sign gaining or losing a moment), the place is kept: the
same timeline position, the same second of the same moment if it is still there,
and past any hold the replay has already seen at that position. A replay at the
very start stays before a moment standing at the start.

**The moment's geometry is a pure function.** `momentView` turns the seconds into
a moment into a phase (grow, hold or shrink), how far it has grown, and the zoom
inside the photo. The grow is 0.4 seconds, the hold 1.5 seconds with a slow zoom
to 1.06 times, and the shrink 0.4 seconds, in one place. `momentRect` turns that
into the photo's rectangle, interpolating from the card's photo to a rectangle
filling 70 percent of the map in the photo's own 4 by 3 shape. The card's photo
rectangle is worked out by `signCardPhotoRect` from the sign's projected point,
`placeSignCard` and the card's size, and `SignCard` is built from the same
constants. The page draws a DOM overlay from the rectangle, and the video export
will draw the same numbers on a canvas. Reduced motion has no growing, shrinking
or zoom: the photo shows full size for the whole hold and then goes.

**A moment gives way to the viewer.** Pausing pauses the moment, and Play carries
on from the same second. A drag, a tap or the wheel on the map, while the replay
is playing, ends the moment and the replay carries on from the sign; a tap on a
sign's card is a marker, not the map, and does not. The camera holds still for
the hold: its shot's motion stops and the ease into a shot does not run.

**A photo that will not load gets no moment.** The panel loads each moment's photo
ahead of time. One that fails is left out of the moments, with a warning in the
console, so the replay does not stop for 2.3 seconds over a broken picture. The
sign's card still says "Photo not available", so the failure stays visible. The
sign's card also stays up for the whole moment, taking the place of the card that
appeared first if two others are showing.

## Consequences

- The slider's length is not the watching's length. A replay with moments is
  longer than the number on the length list, and the help page says so.
- Everything a moment needs is in the time map, `momentView` and `momentRect`, so
  the export needs no new idea, only a canvas to draw on and the card's photo
  rectangle, which comes from the sign and its projected point.
- A seek is not a jump. The distinction costs one option on `wallAtClock` and a
  test for each side of it.
- The photo is loaded twice on the page, once to check it and once to show it.
  The browser caches the second.
- The HTML export player has its own timing and does not use the time map yet.
  Moving it onto the map, and carrying the choice in the exported card, is later
  work.

## Rejected

- **A per-passage setting.** Signs and shots are keyed by time and survive a
  split, a merge and a trim. A setting on the passage would have to be copied or
  merged through each, and a combined replay of several passages would have no
  place to hold it. The switch lives on the sign.
- **Holds taken out of the chosen length.** Shortening the sailing to make room
  would change the pace of a replay the operator tuned, and would make the 8
  second replay a 5 second one with a few photos. Adding the hold keeps the
  number the operator picked honest: it is how long the boat sails.
- **Reading the card's rectangle from the page.** Growing the photo from the
  card's measured box needs the page, a forced layout every frame, and a fallback
  for a card that is off the map. The export has no page. The card's size and
  placement are known from constants and `placeSignCard`, so the rectangle is a
  function of the sign's point.
- **Always on, with no switch.** A sign meant as a small note, or a photo that is
  a diagram, should not stop the replay. The default is on because the
  operator who adds a photo almost always wants it seen.
