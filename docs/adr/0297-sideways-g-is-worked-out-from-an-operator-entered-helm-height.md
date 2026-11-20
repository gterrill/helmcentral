# ADR 0297: Sideways G Is Worked Out From An Operator-Entered Helm Height

## Status

Accepted (2026-11-21). Builds on
[ADR 0295](0295-passages-record-snap-and-pounding-from-a-10-hz-motion-stream.md),
which stores roll acceleration per track point and left sideways g to display
time.

## Context

Roll acceleration in degrees per second squared says how sharply the hull
turns. It does not say what someone at the helm feels, which depends on how far
that person sits from the axis the boat rolls about. The figure the operator
reads against the Comfortable, Lively and Shouldn't have gone ratings is the
sideways acceleration at the helm, in g.

Two things are not Helmcentral's to know. The helm height belongs to the boat,
and Pikorua's number would be wrong on every other hull. Where the roll axis
lies depends on the loading and the sea, and no instrument on board reports it.

The operator also needs to see whether the motion stream is delivering fast
enough for any of this to be recorded, before a passage and not after.

## Decision

### 1. The helm height is a vessel particular with no default

`vessel_particulars` gains a nullable `helm_height_m`, edited under Settings,
Vessel. Blank is "not set". A value must be above 0 and at most 30 m, and the
store rejects anything else with a message naming the field. The limit catches a
typing slip, such as 340 for 3.4. Mate can read and change it through the
vessel details record like every other particular.

### 2. Sideways g is computed on read and never stored

The peak is the helm height in metres times the stored roll acceleration peak
converted to radians per second squared, divided by standard gravity. The
function sits beside the passage figures, so every reader of a passage can
share it, and returns the passage's largest value and its time as `sideways_g`,
outside the stored stats. The first point is left out, as in the stored stats. A
change of height therefore changes every passage at once, with nothing to
migrate or recompute.

With no figure, `sideways_g.reason` says why: the height is blank, or the height
could not be read, or the points show which of three things happened to the
motion. A stored attitude rate under 8 Hz means the feed was too slow. Roll
peaks with no stored rate mean the passage was recorded before 10 Hz capture.
Neither means no motion was recorded at all, because there was no attitude
sensor or its feed wasn't reaching Helmcentral. The passage is still returned
when the height cannot be read, as for a Mate plan that cannot be read. That
failure is logged at most once an hour.

### 3. The waterline stands in for the roll axis

The formula puts the roll axis at the waterline. A boat that rolls about a point
higher up, as a deep-keeled yacht often does, over-reads, and one that rolls
about a lower point under-reads. The error is the same on every passage, so the
figure ranks passages on one boat, which is what the comfort fit needs. It is
not a measurement of the load on a person. The help page says so.

### 4. The motion feed shows its rate on the SignalK page

`GET /api/signalk/motion-feed` reports whether the motion stream is open and the
attitude and heave rates of the recorder's last finished window. Those are the
numbers stored on the track point and used by the 8 Hz gate, so the line shows
what was recorded, with no second measure that could disagree. The accumulator
remembers them when the recorder takes a window, and a window with no samples of
a signal leaves the earlier figure in place. The response carries each rate
rounded once to a tenth for display, the age of the window it came from, the
seconds since a sample last arrived on Helmcentral's clock, `min_hz` (the 8 Hz
floor) and a `below_min` flag for each signal, judged on the unrounded rate, so
the page holds no copy of the floor. A window taken before the current
connection opened is dropped, so an old rate never drives the shown rate or the
verdict.

Each signal also carries a plain reason, in this order: no sample since the
stream opened, silent for more than 5 s, no position recorded since the feed
(re)connected, samples arriving but no rate measurable because none continued a
run, or the recorded rate under the floor. The stream itself has a reason when it
has never connected or has dropped. A feed that was healthy and then stopped
therefore shows silence, not health. The motion client marks the accumulator
connected when its subscription is sent and disconnected when the connection
ends. Settings, SignalK shows, for each signal, the recorded rate and its age
and how long since the last sample ("attitude 10 Hz, recorded 12s ago, last
sample 0s ago"), with each reason under it.

When no sideways g can be shown, an imported passage says it was imported. For a
recorded one, "below 8 Hz" is said only when most of the stored rates after the
first point were under the floor, not any single one.

## Follow-ups

- Work out sideways g after `PassageFiguresFor` succeeds rather than before; on
  the error path the work is wasted.

## What it is not

- Not a default. A blank height records nothing and never falls back to a typical
  figure.
- Not stored. No column, cache or stats entry holds the figure.
- Not a change to what the recorder keeps or to the 8 Hz floor.

## When it can't run

- No height: `sideways_g.peak_g` is null with the reason.
- The vessel store cannot be read: the same, naming the error, and a log line.
- The motion feed endpoint cannot be read: the page says it could not be read.
