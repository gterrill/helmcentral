# ADR 0295: Passages Record Snap And Pounding From A 10 Hz Motion Stream

## Status

Accepted (2026-11-20). Builds on [ADR 0294](0294-passages-record-motion-and-when-each-maximum-happened.md)
(peak roll, pitch and heave per track point) and extends
[ADR 0176](0176-helmcentral-records-its-own-track.md) (what a track point holds).

## Context

ADR 0294 gave each track point the largest roll, pitch and heave since the
previous point. A peak says how far the boat moved. It does not say how
violently, and the operator's ratings (Comfortable, Lively, Shouldn't have
gone) follow the violence: a boat that rolls 8 degrees over six seconds is a
different passage from one that rolls 8 degrees in two, and a hull that drops
off a short steep sea is a third. The spike on the nineteen passages with
motion data showed felt motion tracking the ratings well, and showed the peak
alone cannot separate them.

What separates them is how fast the motion changes: roll rate, acceleration
and jerk for snap, and pitch jerk and vertical acceleration for pounding. Those
are derivatives, and a derivative needs samples close together.

The attitude sensor (a Furuno SCX-20, through the YachtDevices gateway)
publishes `navigation.attitude` and `navigation.attitude.heave` at 10 Hz,
measured against the boat: 10 Hz for attitude and 10 Hz for heave. The main
SignalK stream subscribes to every path with `fixed` policy and a 1000 ms
period, on purpose (see the comments in `signalk_stream.go`): nothing it
feeds reads faster, and each frame costs a websocket read and an unmarshal.
The shared snapshot therefore sees one attitude a second. A cubic filter across
one second of that has one sample.

Arrival times are no better. Frames reach the backend 13 to 274 ms apart for a
sensor that sent them 100 ms apart, so a filter that assumed even arrival would
read network jitter as motion.

## Decision

### 1. A separate stream, not a faster main one

A second websocket carries the motion alone. It sends `subscribe=none` in the
URL, then subscribes in context `vessels.self` to `navigation.attitude` and
`navigation.attitude.heave` with policy `instant` and no period. It feeds a
motion accumulator and never the shared snapshot.

The main stream is unchanged: still `vessels.*`, path `*`, `fixed` at 1000 ms.
Raising its rate to serve one consumer would put every path at 10 Hz for
every other. The motion client is the main client with a different
subscription and a different consumer, so the URL building, token handling,
read limit, reconnect and backoff are one piece of code. It starts where the
main stream starts and stops with the same context.

### 2. Source timestamps, an even grid, and a Savitzky-Golay filter

The accumulator stamps every sample with the update's own SignalK `timestamp`,
never its arrival time. It lays each signal on an even 10 Hz grid by linear
interpolation between neighbouring samples. A gap over 0.45 s breaks the signal:
nothing is interpolated across it, and the filter starts again from the next
sample. The worst spacing measured between the sensor's update timestamps was
274 ms, so 450 ms clears it with room, and a stretch slower than about 2 Hz
breaks continuity instead of being interpolated into something that looks like
10 Hz. A stretch between 2 and 8 Hz stays continuous and is caught by the 8 Hz
floor below.

The sensor's timestamps do this one job and no other. The window's start and its
15-minute cap below run on Helmcentral's own clock, the time each sample is
received, so no sensor clock can hold a window open, close it early or reset it.
A backward clock step, a future-dated sample or a source switch restarts only
the filter of the signal it happened to. Attitude and heave each have their own
filter and their own clock, so neither one's restart touches the other's state
or the window.

A source faster than 10 Hz is resampled to 10 Hz with no anti-alias filter, so
motion above 5 Hz can fold into the figures. The SCX-20 sends 10 Hz and a hull's
motion sits well below 5 Hz, so this is accepted now. Revisit it if a faster
sensor is fitted.

The stream guards its inputs, and logs each problem once per episode (at most
hourly while it lasts, again after five quiet minutes or a reconnect):

- A sample stamped more than 5 s ahead of receipt is dropped. Kept, it would
  hold its channel until the clock caught up.
- A sensor clock that steps back by more than 2 s restarts that channel's
  filter from the new sample. A smaller step back is a repeat and is dropped.
- Each signal, attitude and heave, is locked to the first `$source` seen. It
  moves to another only after the first has been silent for 5 s, restarts its
  filter on the move and logs it. Two sources on one path would interleave two
  independent clocks.

Savitzky-Golay filters give the derivatives, with coefficients worked out by
least squares at start-up:

- Cubic over 11 samples (1.0 s) for the first and second derivative.
- Quartic over 15 samples (1.5 s) for the third.

A shorter window passes the noise; a longer one flattens the roll it is meant
to measure. The derivative at each point is centred, so it lags the newest
sample by half the window.

The filter is a low-pass, so it reads low and never high. Against a sine of
3 s period, a lively roll, it passes about 91% of the true acceleration and
88% of the true jerk. A 2 s period loses more. The figures are for comparing
passages on one boat with one filter, and the later comfort fit learns its
bands from them, so the bias is fixed, not corrected.

### 3. What a window yields

When the recorder keeps a track point it takes the accumulator's figures for
the window since the previous kept point, then starts a new window. The filter
keeps its sample history across the boundary, so derivatives run on without a
1.5 s gap at the start of each window.

A window covers at most 15 minutes. The recorder keeps no point while the
position fix is rejected (the -1/-1 sentinel at the yard can last days), and an
unbounded window would grow without limit. A window opens when the previous
one is taken. When a sample arrives more than 15 minutes of Helmcentral's time
after that, the window's figures are discarded and a new one starts, with the
filter history kept. A point's figures therefore describe
at most the last 15 minutes before it.

| Figure | Column |
| --- | --- |
| Roll RMS about the window mean, degrees | `roll_rms_deg` |
| Roll rate, peak and RMS, degrees per second | `roll_rate_peak_dps`, `roll_rate_rms_dps` |
| Roll acceleration peak, degrees per second squared | `roll_accel_peak_dps2` |
| Roll jerk, 95th percentile of magnitude, degrees per second cubed | `roll_jerk_p95_dps3` |
| Pitch RMS, degrees | `pitch_rms_deg` |
| Pitch acceleration peak and jerk p95 | `pitch_accel_peak_dps2`, `pitch_jerk_p95_dps3` |
| Heave standard deviation, metres | `heave_sd_m` |
| Vertical acceleration p95 and peak, metres per second squared | `vert_accel_p95_ms2`, `vert_accel_peak_ms2` |
| Attitude rate actually received, hertz | `motion_rate_hz` |
| Heave rate actually received, hertz | `heave_rate_hz` |

The same row also gains four readings from the shared snapshot, with the
freshness rule every other reading follows (older than ten seconds is
absent): `heading_deg` (true, 0 to 360), `twa_deg` (true wind angle through
the water, signed, in -180 to 180), `stw_kts` and `rpm_mean`. The mean is
taken across every `propulsion.<id>.revolutions` the boat publishes whose
engine is running, whatever the ids. An engine that is off or turning at zero
is left out. A path that is not off and has not been updated for ten seconds is
an engine that has stopped reporting, however long ago, and it leaves the mean
and the fuel total absent: a dead engine feed must show, not be skipped. With
none running the mean is absent and never an estimate. A switched-off engine is
kept, as a known zero. These feed the fits of speed and comfort in a later
change.

All of it is stored as nullable columns on `track_points`, added when the
store opens. Every older row keeps null.

### 4. The 8 Hz floor

The accumulator measures the rate it receives, for attitude and for heave
separately. A rate is the number of intervals between consecutive samples of an
unbroken run, over the time those intervals cover. A dropout is not averaged
in, so a long gap neither nulls clean stretches around it nor lets a slow
stretch pass, and a 3 Hz stretch contributes no intervals at all.

Below 8 Hz a signal keeps its rate and its RMS (heave spread for heave) and
nulls its derived figures. Attitude's rate gates the roll and pitch rates,
accelerations and jerks. Heave's own rate gates the vertical acceleration, so a
10 Hz attitude cannot release a figure taken from 5 Hz heave. A signal with no
continuous interval in the window has no figure of its own, rate included;
heave and attitude do not depend on each other.

The window rate is an average, so it cannot see a slow stretch inside a fast
window: a minute at 3 Hz, bridged by interpolation, inside nine minutes at
10 Hz. Each derivative therefore carries its own check. It enters the window's
peaks and percentiles only if the raw samples inside its filter span (11 grid
steps for rate and acceleration, 15 for jerk) number at least 0.8 per step, over
the whole span and over each half of it. This applies to heave's vertical
acceleration too. The window-level rate figures stay as they are.

The recorder also watches the stream. Per signal it holds the time, on
Helmcentral's clock, of the last accepted sample, or of startup when there has
been none. When that is 60 s or more ago while the main snapshot has a fresh
reading of the same signal, it logs
"motion stream delivering no attitude while SignalK has it", once per episode,
cleared when samples return. Heave has the same check. The measure runs across
windows because under way a window is only seconds long, and nothing is logged
in the first 60 s after startup.

A jerk worked out from 4 Hz samples would be a number about the interpolation,
not the boat. Helmcentral does not configure the device to raise its rate. It
records the rate it measured, and a later page can say "attitude arrived at
4 Hz, needs 8 Hz".

### 5. The noise floor, and why jerk is usable

The accelerations and jerks are only worth keeping if the sensor's own noise
sits well below the motion. At rest at the yard, 60 s, boat still:

| Figure | Noise floor | Expected on a lively passage |
| --- | --- | --- |
| Roll rate | 0.05 °/s | not estimated |
| Roll acceleration | 0.12 °/s² | 10 to 30 °/s² |
| Roll jerk, p95 | 0.4 °/s³ | 20 to 65 °/s³ |

That is fifty to a hundred times below the signal. The yard measurement was
taken with the boat still and not under way. Whether the floor holds under way,
with the engines running, is not yet known, and is the first thing to check on
the first passage with 10 Hz capture.

### 6. Pounding, not slamming

Vertical acceleration comes from the second derivative of the sensor's
heave. That heave is filtered by the device, so a sharp impact is smoothed
before Helmcentral sees it, and the peaks under-read. The figure is labelled
pounding, a measure of how hard the hull works in the sea, and not slam, which
would claim an impact load it cannot see. A true impact peak needs a raw IMU
sampling at 50 to 100 Hz, which is a later hardware change.

## What it is not

- Not sideways g. Sideways acceleration at the helm is helm height times roll
  acceleration, and the helm height is the operator's entry. It is worked out
  at display time from the stored roll acceleration, and no height ships as a
  default.
- Not a comfort verdict. These are the measurements a verdict will be fitted
  to.
- Not backfilled. A passage recorded before this change has the ADR 0294
  peaks at most, and the RMS-only history in InfluxDB is a separate change.
- Not a change to the main stream's rate or to what the shared snapshot sees.

## When it can't run

- If the motion stream is down, the accumulator gets no samples and every
  figure stays null. The reconnect and backoff are the main stream's, and each
  attempt is logged under its own name, `signalk motion stream`.
- If the gateway is silent, the same: nothing arrives, nothing is recorded.
- If an update has no parseable timestamp, or an attitude lacks roll or pitch,
  the update is skipped and the reason logged once per episode. Nothing is
  substituted.
- If the device sends under 8 Hz, the derived figures are null and the rate
  says why.
- If the track point write fails, the window's figures are lost with it, where
  the peaks of ADR 0294 survive to the next point.

## Follow-ups

- Linear interpolation across normal timestamp jitter (gaps up to about
  274 ms) can put small kinks into the grid, and the acceleration and jerk
  filters respond to them, so window peaks may be slightly inflated. Consider
  fitting least squares on the irregular samples instead of interpolating.
