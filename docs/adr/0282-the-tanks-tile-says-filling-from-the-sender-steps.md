# ADR 0282: The Tanks Tile Says Filling From the Sender's Steps

## Status

Accepted (2026-11-12). Adds two fields to the tanks payload of
[ADR 0214](0214-tank-levels-drive-the-tiles-and-the-alarms.md) and one word to
the Tanks tile. It changes no tank alarm.

## Context

Filling the fresh water tank is a job the skipper stands over, and the tile
gives no sign that it is happening. The percent creeps up and the word beside it
says OK, as it did a minute before. The skipper wants to see that the tank is
filling and roughly how long until it is full, without doing the sum.

The sender is quantised. The Pikorua freshwater sender reports in steps of two to
three percent and holds each value a minute or two, even with the hose running.
A raw two-second capture of a fill shows a rising step every two and a half
minutes at about one percent a minute, and a 23-minute pause in the middle shows
a flat line. A slope over a short window reads that staircase as noise, flat then
a jump then flat.

## Decision

The backend keeps a short buffer of each fresh water and fuel tank's level
changes and, every time the payload asks, works out from that buffer alone
whether the tank is filling. The tile shows the time to full in place of the
state word (OK, LOW or CRIT) while it sees a fill, and **FILLING** until it has
one.

Nothing persists between calls. An earlier version latched a filling flag, held
it after the last rise, stretched the hold for slow fillers and reset runs on
every fall. Two review passes kept finding bugs in that machinery, so it was
removed. The detector now holds the buffer, the time each tank was last read and
the tank's learned step size, and nothing else.

### What counts as a rise

The sender's step size is learned per tank as the median of its last twenty
nonzero changes, with a floor of half a percent, so no tank's step is written
into the code. The noise floor is the larger of half a percent and 0.4 of that
step.

Each rule looks at the part of its window since the window's lowest level (the
latest, if the level touched it twice). A rise is a level at least the noise
floor above the level of the previous rise, or above that starting level for the
first. A level that dips and comes back to where it was therefore adds no rise,
and neither does a sender that flips between two values at a step boundary. A
sender creeping up in sub-floor increments does earn a rise each time the
creep adds up to the floor. Rises less than 30 seconds after the first rise of
their group merge into one, because the sender sometimes reports one real step
as several values a moment apart. The 30 seconds run from the first rise, so a
dithering sender cannot chain a whole fill into one rise.

### The rule

Both rules read the buffer at the moment of the call. A level is at its high
when it equals the window's maximum and no fall of the noise floor or more has
come after that maximum, so a step down ends a fill at once. Net gain is the
current level less the level the climb starts from, never less than 2.0
percentage points however fine the sender, so a noisy 0.1% sender does not
read as a fill.

- **Fast (hose or nozzle):** in the last 7 minutes, at least two merged rises, a
  net gain of at least two learned steps (and at least 2.0 points), and the level
  at its high. Seven minutes, not six, keeps two rises in view for a hose whose
  steps come three and a half minutes apart, right up to the next step. A hose
  that stops still clears about 7 minutes after its last rise.
- **Slow (watermaker):** in the last 30 minutes, at least three merged rises, a
  net gain of at least three learned steps (and at least 2.0 points), and the
  level at its high. The last rise must be within 15 minutes of now and within
  twice the median gap between the rises, whichever is shorter, and the gap
  before the last rise is held to the same bound. A hose paused for a few
  minutes is therefore not held for long (a two-minute step holds about four
  minutes), and a single rise after a long pause does not revive an old fill.
- **Filling** is fast or slow, and speed over ground is known, fresh and under
  1 kn, and the tank's own level reading is fresh.
- **Unknown or stale speed:** the tank is never reported as filling. A missing
  speed is no evidence the boat is stopped, and this follows the fail-fast rule
  rather than a guess. The sampler reads the speed's own age and treats one older
  than 60 seconds as unknown.
- **A dead sender:** a level reading older than 60 seconds is not recorded, and
  a tank not read for 60 seconds is not filling.
- **Kinds:** water and fuel, chosen by one allowlist next to the tank kind
  classifier. Waste and live wells never show filling.

### The time to full

The line is the Theil-Sen fit (the median of the slopes between pairs of rising
points, with the median of the intercepts) through the merged rises of the
current climb, each at the time of the rise and the level after it. The climb
runs back at most 30 minutes, to the window's lowest level or the last step
down of the noise floor or more, and starts again after any gap between rises
longer than 15 minutes. It is wider than the window that qualified, because a
hose with a step every 2.5 minutes puts only two or three rises in 7 minutes. Pairs
closer than a minute are left out. The line is fixed in time, so the
answer counts down between steps.

`seconds_to_full` is the time from now until that line reaches 100%, rounded to
whole minutes (a multiple of 60) so the payload does not change on every
5-second poll between steps. It is null
when the climb holds fewer than three rises, when the rate is unsteady (the
upper quartile of the pair slopes more than three times the lower), or when the
tank is under 60 seconds from full.

### Where it runs

The server's 5-second poll tick reads the tank levels and the speed and feeds an
in-memory tracker, one buffer per tank id, kept 35 minutes. Nothing goes to
InfluxDB, and a restart starts the tracker empty. The tanks payload reads the
tracker's answer, so every connected screen agrees.

### The tile

Each tank in `tanks-state` gains `filling` (boolean) and `seconds_to_full`
(number or null). While `filling` is true the word reads `12 MIN`, or
plain `FILLING` when `seconds_to_full` is null, in the neutral colour of a normal
tank. The minutes round to the nearest under 30, to the nearest 5 up to two
hours, and then read as hours and minutes, `3 H 20 MIN`. A tank in warning or
critical still gets its amber or red from the alarm levels on the bar, label and
percent. Only the word and its colour change.

## Rejected

- **A Mahalanobis score as in [ADR 0276](0276-each-engine-learns-its-own-normal.md).**
  That scores an unusual combination of several readings against a history
  learned offline. A tank has one reading, and nothing to correlate it with.
- **A short-window slope.** The staircase makes it flicker between zero and a
  large number.
- **A fallback speed.** Treating unknown speed as stopped would show FILLING on a
  tank filling by a fault or sloshing under way.
- **InfluxDB.** The decision is live and a few minutes deep. InfluxDB is optional
  on this boat.

## Limits

- A fill must last long enough to show two steps, about three to five minutes
  on a hose, before the word appears.
- A hose fill clears about 7 minutes after it stops. A watermaker's can linger
  up to 15 minutes after it stops, which the operator accepted. A watermaker
  whose steps come more than about 10 minutes apart can blink off between steps,
  because three rises no longer fit in 30 minutes.
- A hose with steps more than 3.5 minutes apart has no word between its second
  and third rises when the gain is still under three steps.
- A step down mid-fill ends the word at once. Climbing back to the old high is
  not a rise, so it returns only with new highs above it.
- A sender creeping up by about 0.1% a minute is read as a slow fill once it
  has gained 2 points; below that rate it is noise.
- A fill that stopped for more than 15 minutes and resumed shows plain FILLING
  until three rises have come since the resume, because the old rises would
  give an unsteady rate.
- A restart loses the history, so a fill under way then waits for two new steps.
- Sloshing at a dock can fake a rise. The stopped-only rule and the new-high
  rule cover most of it, not all.
- The tile only. Mate, the MCP server and the watch report do not read it.

## Follow-ups

- Litres to full in a popover on the tile.
- A "falling too fast" leak check on the same level history.
