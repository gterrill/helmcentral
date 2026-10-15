# ADR 0182: Electrical instances and solar names are Vessel settings

## Status

Accepted (2026-10-15). Follows [ADR 0180](0180-alternator-instances-are-a-vessel-setting.md)
and [ADR 0181](0181-engines-and-house-bank-come-from-vessel-settings.md), which
did the same for alternators, engines and the house bank. Replaces the
`INFLUX_SHORE_MEASUREMENT` and `INFLUX_GENERATOR_MEASUREMENT` pins described in
[ADR 0077](0077-battery-power-tile-reads-the-bank-not-the-fields.md) and the
overnight model's exclusion checks.

## Context

The generator, shore charger, inverter, extra voltage row and solar labels were
the last places that guessed an instance number.

- The generator state, its start and stop writes, its history path and the
  overnight exclusion all used `electrical.generator.0`. The write path was
  built from a literal, so a renumbered generator would have sent start and stop
  to an instance that no longer existed.
- The generator's real power was read from `electrical.ac.1`, and the shore
  charger from `electrical.chargers.0`, whatever those turned out to be.
- The inverter output tried `inverters.0`, then `alternators.0.ac.output.power`
  (not an inverter at all), then the first inverter child that had a value. The
  live boat has inverters 5 and 276, so the tile read whichever sorted first.
- The "24V" row read `dc.24v` and `batteries.starter`, neither of which the boat
  publishes, and then silently fell back to the house bank's voltage under a
  24V label.
- Solar controllers were labelled Port, Starboard and Salon by their position in
  the list. The boat has four controllers, and a list position says nothing about
  which array a controller is.
- The two overnight exclusions were set by two environment variables whose
  defaults named instance 0.

## Decision

1. **Settings.** `vessel.electrical` holds five bare instance ids as strings
   (empty is not set): `generator_instance`, `generator_ac_instance`,
   `shore_charger_instance`, `inverter_instance` and `voltage_readout_battery`.
   `vessel.solar` is a list of `{instance, name}` with the same shape and
   normaliser as `vessel.batteries`. Ids are checked with the alternator id
   pattern and trimmed on save. No vessel values ship as defaults.
2. **Generator.** The vessel state reads `electrical.generator.<id>`. Start and
   stop build their path from the setting and check the id again server side
   before it goes into a URL. With none set they answer 409 with a plain message
   that the generator is not set up in Vessel settings, and write nothing. The
   electrical state carries `generator_configured`, taken from the settings alone
   (so a SignalK drop never reads as not set up, and null when the vessel block is
   unreadable), so the tile can say so instead of showing an empty generator with
   a Start button that cannot work. The tile treats null as unknown and shows
   neither the message nor the buttons.
3. **Real power, shore charger, inverter.** Generator real power is read from
   `electrical.ac.<generator_ac_instance>` (the same three leaf variants as
   before). The shore charger figures come from `electrical.chargers.<id>`. The
   inverter AC output comes from `electrical.inverters.<id>` only, in its three
   leaf variants. Unset reads as unknown. AC loads still fall back to the
   inverter output when the boat publishes no AC loads total, so with no inverter
   chosen that fallback is unknown too.
4. **Voltage readout.** The wire fields `voltage_readout_v` and
   `voltage_readout_label` replace `dc_24v_voltage_v`. The voltage is the chosen
   battery's own, and the label is the operator's name for it from
   `vessel.batteries`, else the name the bus publishes for that battery, else
   "Battery <id>". Unset, or a chosen battery that is not reporting, hides the
   row. Nothing stands in for it. DC loads (`venus.dcPower`, `dc.12v`,
   `loads.12v`) are system level rather than per instance and are unchanged.
5. **Solar labels.** A controller is labelled with its operator name, else
   "Array <id>". The position-based Port, Starboard and Salon labels are gone.
6. **Overnight model.** The generator exclusion reads
   `electrical.generator.<id>.stateNumber` and the shore exclusion reads
   `electrical.chargers.<id>.acin.1.current`. Both environment variables are
   removed. An exclusion with no instance chosen is skipped: its Influx query is
   not made. The overnight result carries `shore_checked` and `generator_checked`
   so a zero in `nights_excluded_shore` or `nights_excluded_generator` beside a
   false flag means "not checked", not "none found". The projection cache key is
   the state-of-charge, shore and generator paths together, so choosing another
   instance never serves the old result.
7. **Candidates.** `GET /api/vessel/candidates` adds `generators` (state), `ac`
   (real power), `chargers` (output and AC input current), `inverters` (AC output)
   and `solar` (panel power), each sorted numerically. An instance the boat has
   published is always listed, since the operator may have chosen it, but a
   reading is attached only when it arrived within five minutes. A stale value is
   not shown as live.
8. **Logging.** The electrical state logs once which of the five electrical
   choices are unset, and again only if that set changes. An unreadable vessel
   block is logged once per distinct error, as for alternators.

## Consequences

An upgrade leaves the Generator tile saying it is not set up (and without Start
and Stop), the Shore line, the Loads inverter figure, generator power and the
voltage row empty, and solar controllers named "Array <id>", until the operator
chooses them in Settings, Vessel. The overnight projection keeps working but does
not screen nights for shore power or generator running until those two are chosen;
the result says so. This is listed as breaking in the changelog.

Equipment hour-meter paths (`electrical.generator.0.runtime` and similar) are the
operator's own text on an inventory item and are not touched here.
