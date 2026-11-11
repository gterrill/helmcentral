# ADR 0277: A Current Tile Reports the Water Against the Bow

## Status

Accepted (2026-11-11). Adds one built-in tile to the catalogue of
[ADR 0107](0107-new-page-flow.md). Reads the set and drift the Wind tile already
shows, and changes neither the Wind tile nor how they are computed.

## Context

The Wind tile shows set and drift as a compass bearing and a speed. That answers
"where is the water going" and leaves the helm to work out the rest: whether it
helps or holds the boat back, and from which side. On passage the question is
quicker than that. A skipper wants to read "2.1 kt, S35" beside the wind, the
same way the wind reads, and to know at once whether the water is on their side.

The frontend already carries everything needed: `currentSetDeg` (true, where the
water flows to), `currentDriftKts`, `currentDriftImpactKts` (along the course,
negative against the boat), `headingTrue` and `navigationState`.

## Decision

Add a built-in tile, id `water-current`, titled **Current**, in the Navigation
group of the tile picker. (`current-conditions` is the weather tile and keeps its
name.) Set, drift and impact are already in the vessel state. Two small server
changes follow: the list of valid tile ids gains the new id, and the vessel state
gains `current_last_update_age_s`, the age of `environment.current`, so the tile can
go stale like the Wind tile does. A frontend test holds the id lists equal.

### What it shows

- **Speed.** `currentDriftKts` to 0.1 kt.
- **From.** The angle the water comes from, off the bow, in the wind's
  convention: `normalize(set + 180 - heading)`, rounded to a whole degree.
  0 to 180 reads `S<n>`, over 180 reads `P<360-n>`. Exactly ahead reads `0`
  and exactly astern reads `180`, since "S0" and "S180" name no side. A word such
  as "Astern" does not fit two columns at the reading's size floor.
- **Helping or Hindering.** `currentDriftImpactKts` to 0.1 kt, as a small word
  over a reading in the same size as the other two, so the number never truncates:
  "Helping" over "+0.4 kt"
  (teal, `text-gauge-secondary`) or "Hindering" over "−1.8 kt" (amber,
  `text-amber-600`, matching the Wind tile's SET and DRIFT cards), with a true
  minus sign. An impact that rounds to 0.0 reads "No effect" in the muted colour.
  "Across the course" was rejected: a weak head current rounds to zero too, and
  that wording contradicted an ahead angle.
- **Still water.** Drift that rounds to 0.0 kt keeps the speed, drops the angle
  to `—` (still water has no direction) and reads "No current".

The comes-from convention matches the wind, so two tiles on one page do not
disagree about which way an angle points. It is also the sailor's usage for
anything that arrives, and the Helping or Hindering line settles any doubt about
what the angle means for the boat.

### When it will not read

The tile fails visibly and never guesses. The reason sits under the readings.

- **Stale feed.** Past the usual threshold the tile takes the amber stale outline
  and badge, as the Wind tile does, and the values stay on screen. The age is the
  older of the `setTrue` and `drift` leaves, so a live `setMagnetic` or
  `driftImpact` cannot hide a frozen set or drift. A tile left blank for any
  reason below shows no stale marker, since nothing on it can have frozen.
- **No navigation state.** With no state at all, the source is missing and the
  tile says "No navigation state" instead of "Shows under way".
- **Not under way.** Only `motoring` and `sailing` read, after the same state
  normalisation wall displays use, so "under way using engine" counts. At anchor or on a
  mooring, and with no navigation state at all, every value is `—` and the tile
  says "Shows under way". Set and drift at anchor are noise from a stationary
  log, and a confident "Hindering" there would mislead.
- **No set or drift.** Every value is `—` with "No current reading".
- **No heading.** Speed and Helping or Hindering still read, because neither
  needs the bow. From is `—` with "No heading".

### What it is not

The current is derived: speed through the water (log) and compass heading against
GPS over the ground. A log that reads high shows as a foul current that is not
there. The tile cannot tell the two apart, so the help page says to check the log
calibration if it always says Hindering.

It has no arrow in this version. The numbers carry the reading, and an arrow on a
tile this small would compete with them.

## Consequences

- No default page layout gains the tile. Settings stay vessel-agnostic, and the
  operator adds it from the picker.
- The tile reads the vessel state store directly, like Wind, so it needs no new
  stream or poll.
- The tile is two columns wide, like a Gauge tile, so it can sit under one. Speed,
  From and the effect figure are three stacked rows, each a small label over a
  reading in the Gauge tile's own fluid size (`lib/fluid-readout.ts`). A reason
  replaces the effect word, or the From label for "No heading", so it needs no
  line of its own. Three readings at the 36px floor need five rows.
- A later arrow, or a trend over the last hour, builds on the same three fields.
