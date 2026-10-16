# ADR 0181: Engines and the house bank come from Vessel settings

## Status

Accepted (2026-10-15). Follows [ADR 0180](0180-alternator-instances-are-a-vessel-setting.md),
which did the same for alternators. Supersedes section 4 of
[ADR 0077](0077-battery-power-tile-reads-the-bank-not-the-fields.md): the
`INFLUX_SOC_MEASUREMENT` pin is replaced by the house bank setting.

## Context

Alternators went blank when the GX renumbered them because the backend guessed
instance ids. Engines and the house bank had the same problem in other forms.

- Engine rpm was read from `propulsion.{0,port,main,...}` and
  `propulsion.{1,starboard,secondary,...}` by name, and ignored
  `vessel.engines`. A stale first name returned "no reading" without trying the
  others. Anchor auto-raise, the GNSS jump check, the Alternators tile and
  Mate's passage estimate all depended on that guess, and the estimate
  assumed `port` and `starboard` when it found nothing.
- The Battery & Power tile chose "the bank" by whichever battery reported state
  of charge with the highest current, then fell back to `batteries.house`,
  `batteries.service`, `chargers.276` (a Pikorua instance) and the first child
  of anything that looked like a charger or battery. It ignored
  `vessel.house_bank`, which the anomaly detector and passages already used, so
  the tile could disagree with them.
- The overnight state-of-charge history and the tile's bands were pinned to
  `electrical.batteries.0` by `INFLUX_SOC_MEASUREMENT` and a frontend constant.
  ADR 0077 section 4 pinned one path because a bank that reports under several
  instances makes a "highest current" choice drift. The pin was right, but it
  was a second place to set what Vessel settings already records.

## Decision

1. **Engines.** The vessel state reads `propulsion.<id>.revolutions` (x60) or
   `.rpm` for each `vessel.engines[].instance`, ignoring a reading older than
   the rpm age limit and carrying on to the next engine. The wire carries one
   field, `engines_running`: true when any reporting engine is turning, false
   when every reporting engine is stopped, null when no engine is configured or
   none is reporting. `engine_0_rpm` and `engine_1_rpm` are removed.
2. **Null is unknown, never "stopped" or "running".** Anchor auto-raise needs
   the engine condition true, so null never raises. The GNSS jump check reads
   null as not running, which leaves the check on. The Alternators tile is
   shown only when `engines_running` is true.
3. **Mate's passage estimate** takes its engine instances from
   `vessel.engines`. With none configured it returns an error saying engines
   are not set up, instead of assuming a port and starboard pair. The
   `instances_assumed` flag goes, because nothing is assumed any more.
4. **House bank.** State of charge, voltage, current, power and capacity on the
   Battery & Power tile all come from `vessel.house_bank.path`, a full prefix
   such as `electrical.batteries.51`. Capacity is `<path>.capacity.nominal`,
   then `vessel.house_bank.capacity_ah`. When power is not published but
   current and voltage are, power is their product from the same bank. Nothing
   falls back to another battery, a charger or a Venus aggregate. With no bank
   set, the bank figures read as unknown (-1, or null for current and power)
   and the backend logs it once, not every tick.
5. **State-of-charge history and bands.** The overnight projection reads
   `<house_bank.path>.capacity.stateOfCharge`. `INFLUX_SOC_MEASUREMENT` and the
   frontend default path are removed. With no bank set, the overnight endpoint
   answers 503 with an explanation and no `soc_path`, the tile shows no dawn
   figure. The bands read the same path from the electrical state payload
   (`soc_path`, absent with no bank), so they do not depend on the projection,
   which needs a position fix. The projection cache is keyed on the path as
   well as the position, so changing the bank never serves the old bank's result.
6. **Settings read once per call.** The electrical state and the vessel state
   each load the vessel block once per call (the settings file is cached by its
   modification time). A block that cannot be read reads as not set, affecting
   only the figures that depend on it, and is logged once per distinct error
   rather than every tick. Mate's passage estimate is the exception: it reports
   an unreadable block as such, distinct from engines not being set up.
7. **Capacity** is `<path>.capacity.nominal`, then `<path>.capacity.total` of the
   same bank, then `vessel.house_bank.capacity_ah`.

## Consequences

An upgrade leaves the Alternators tile hidden, anchor auto-raise inactive, Mate's
passage estimates unavailable and the Battery & Power bank figures blank until the
operator ticks the engines and picks the house bank, which the changelog lists as
breaking. State-of-charge band rules must be on the chosen bank's path; a rule on
another battery no longer colours the tile. The chosen bank is now the single
pinned path ADR 0077 section 4 wanted, set where the rest of the vessel is set.
The Generator, shore charger, inverter and the 24 V row still read fixed
instances; they are the next change.

## Amendment (2026-10-17)

The electrical state now carries `house_bank_configured`: true or false from the
`vessel.house_bank` setting, and null when the vessel block is unreadable. It is
independent of the SignalK feed. The Battery & Power tile uses it to say that no
bank is picked, in place of the bank's figures, instead of drawing dashes that
look like a bank that is not reporting. Solar, Loads, Shore and the readout
battery's voltage keep showing, since none of them depend on the house bank.

`soc_path` could not serve for this: it is also absent when the SignalK fetch
fails or the settings are unreadable, so its absence does not mean no bank is
picked.
