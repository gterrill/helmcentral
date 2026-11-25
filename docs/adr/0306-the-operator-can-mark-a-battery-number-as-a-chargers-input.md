# ADR 0306: The Operator Can Mark A Battery Number As A Charger's Input

## Status

Accepted (2026-11-25). Adds to the automatic charger-input test that
[ADR 0164](0164-sensor-health-cards-name-the-sensor.md) describes; the test
itself lives in `sensorNamer.isChargerInstance` and is unchanged.

## Context

A solar charger on the NMEA 2000 bus often reports its panel array's voltage
under a battery number. 80 volts is a normal array and an impossible battery, so
the impossible-reading check raised "Impossible sensor reading: BlueSolar
Charger MPPT 100/50 re voltage 80.7 V" about twenty times a day on the boat.

The automatic test skips such a number only when two things hold. Every device
that publishes it is a known charger, and each of those chargers also publishes
another battery number that a non-charger reports, the bank it really feeds. The
second condition is deliberate: a charger on its own, or beside other chargers
only, has no bank to point at, and its number could be a real battery. The
boat's MPPT publishes one number and nothing else, so the test fails and the
number is range-checked as a battery.

Loosening the test would stop the alarm and also stop checking a real battery
that happens to be reported by a charger alone. The operator can tell which case
it is. Helmcentral can't.

## Decision

**The operator marks the number.** In Settings → SignalK → Discovery, a battery
row that the server says can be marked (`can_mark`) offers **Mark as charger input**. Any marked row
offers **Unmark charger input**.

**The mark is stored as given and judged every time it is read.** No write path
(an item taking the number, a battery monitor starting to report) has to clear
it, so none can leave it hiding a real battery. The mark applies only when both
hold:

- No equipment item owns the number.
- The number has publishers and none of them is a known non-charger.

The operator vouched for the number, so only evidence lifts the mark. A publisher
the sources tree doesn't know yet, after a restart say, leaves it in effect. The
automatic test is unchanged: it still needs every publisher known to be a charger,
and each charger to feed another bank.

The row reports the outcome as `charger_input_mark`:

- `none`: not marked.
- `applies`: marked, and in effect.
- `automatic`: marked, but the automatic test already recognises the number, so
  the mark adds nothing. The row says only "Charger input, not a battery" and
  keeps Unmark. The PUT handler refuses a new mark here (409), exactly where
  `can_mark` is false.
- `item`: marked, but an item owns the number.
- `not_chargers`: marked, but a known non-charger also reports it.
- `no_publishers`: marked, but nothing has reported it since Helmcentral started.

`applies` and `automatic` act like the automatic test: the impossible-reading check skips
the number, and its row has `charger_input` true, so the Roles picker leaves it
out (`offerableSources`). In every other state the stored mark stays, the row says
why it is not in effect, and the number is range-checked as a battery.
Helmcentral does not mask that quietly. If the cause goes away, the mark takes
effect again. The row also carries `can_mark`, true for an unmarked number the
automatic test doesn't recognise and where a mark would apply. It comes from the
same function (`sensorNamer.chargerInput`) as the PUT handler's check and the
detector's skip, so the page, the server and the check can't disagree.

**The two stay distinguishable.** A number the automatic test recognises is
`charger_input` with `charger_input_mark` `none`, and has no button, since there
is nothing of the operator's to take back.

**The mark has its own small table.** `charger_input_marks (instance, marked_at)`
sits in `helmcentral.sqlite` beside the other operator stores, held in memory
because the anomaly tick reads it every pass. It isn't one of the ignore lists:
the mark says what a number is, and doesn't switch a check off. The table is empty
on a new install and holds no vessel's values by default. A failed save leaves the marks as they were. The handler logs the database
error and tells the operator "Couldn't save the charger-input mark. Try again; if
it keeps failing, see Settings → Logs." A marked
number is always listed on the Discovery page, even quiet or past the record's
look-back, so it can be unmarked.

**One endpoint sets and clears it.** `PUT /api/vessel/charger-inputs/:instance`
with `{"marked": true|false}` answers `{"instance", "marked"}`.

- The instance must be letters, digits, `_` or `-`; anything else is a 400.
- A body that isn't exactly `{"marked": <bool>}` is a 400.
- Marking is a 409 unless the one rule says the mark would apply: the number is
  owned by an item (the message names it), nothing has reported it since
  Helmcentral started, or a known non-charger also reports it.
- Clearing is always allowed, so a mark on a number that has since gone can still
  be removed.

Assigning an item to a marked number is allowed. It simply makes the mark inert.

## Alternatives considered

- **Relax the automatic test to accept a lone charger.** It would hide a real
  battery that only a charger reports. The agreed design stays as it is.
- **Ignore the sensor from the alarm card.** The ignore list is keyed by path and
  switches off every check for that reading, including the frozen and
  gone-quiet checks, and it leaves the number in the Roles picker. A mark says
  what the number is, and the effects follow from that.
- **A setting in `settings.yaml`.** The operator already works on the Discovery
  page, and the other per-sensor choices live in `helmcentral.sqlite`.
- **Clear the mark when an item or a monitor appears.** It needs every write path
  to remember, and one that forgets leaves a real battery unchecked.

## Consequences

- A boat whose lone charger raises impossible readings has a one-tap fix, and no
  other boat sees a change.
- Marking a number that only chargers publish stops its range check, and the row
  says "Charger input, not a battery". A mark can't silence a number a battery
  monitor also reports.
- A mark survives restarts and is not part of the settings the Save bar writes.
