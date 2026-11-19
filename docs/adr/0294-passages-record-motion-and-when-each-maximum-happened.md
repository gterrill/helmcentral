# ADR 0294: Passages Record Motion And When Each Maximum Happened

## Status

Accepted (2026-11-20). Extends [ADR 0176](0176-helmcentral-records-its-own-track.md)
(what a track point holds) and the per-passage figures it fed.

## Context

The operator rates each passage Comfortable, Lively or Shouldn't have gone.
The page gave nothing to rate it by: the strongest gust was a number with no
moment attached, and the boat's roll, pitch and heave were not recorded at
all, though the YachtDevices gateway publishes them. The operator had to scrub
the replay hunting for the rough patch.

A track point is taken when the boat has moved 25 m, or every ten minutes at
rest. The attitude sensor sends many times a second. Storing the value on the
tick that happened to be kept would record a calm instant between two rolls
and miss the roll itself.

## Decision

### 1. Peaks between points, not samples

SignalK publishes `navigation.attitude` (an object with roll, pitch and yaw in
radians) and `navigation.attitude.heave` (metres). Every five-second poll tick
reads roll and pitch in degrees and heave in metres, each only when it arrived
in the last ten seconds, the same rule every other reading follows.

The recorder keeps the largest magnitude of each since the last stored point,
as it already does for gusts, and writes the three peaks on the next point it
keeps. Then it resets them. A tick whose position fix is rejected still counts
towards the peak, because the boat moved whatever the GPS said.

The point gains three nullable columns: `roll_peak_deg`, `pitch_peak_deg` and
`heave_peak_m`. A peak is a magnitude, so it has no sign: the page says how
far the boat rolled, not to which side. With no reading since the last point
the column is null, never zero.

### 2. The time of each maximum

The passage figures gain the maximum roll, pitch and heave, and the time of
each of those and of the maximum speed and gust, as Unix seconds. The time is
that of the stored point carrying the maximum. A peak is the largest since the
point before it, so the real moment lies in the interval just before. Under
way the points come every few seconds. At rest the gap can reach ten minutes,
which is the coarsest the time can be.

A figure with no reading is null, and so is its time.

### 3. Old data

The columns are added to an existing `track_points` table when the store
opens, and every old row keeps null. Stored passage figures carry a version.
Figures stored before this change lack the times, so they are worked out again
from the points the next time the passage is read, once.

Passages recorded before this release have no motion. The page says "Motion
not recorded for this passage" and does not estimate it. Imported GPX points
never have it.

### 4. The page moves the replay

Each maximum on the passage page is a button that pauses the replay and moves
it to that time, through one function the replay panel exposes by a ref, the
way it already exposes its cursor. The page itself works out nothing.

## What it is not

- Not a sea-state measure. Roll and pitch come from the gateway's own sensor
  and its mounting offset. A boat at rest with a wake passing records a roll.
- Not a time series. The peaks are one number per stored point, so a later
  chart can show motion across a passage but not a waveform.
- Not backfilled. A passage with no recorded motion stays without.

## When it can't run

If the gateway is silent, the readings go stale after ten seconds and the
peaks stay null, the same as a silent anemometer leaves gusts null. If SignalK
publishes the heave without the attitude object, or the reverse, the figures
that arrived are stored and the others stay null.
