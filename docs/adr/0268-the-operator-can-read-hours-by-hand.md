# ADR 0268: The Operator Can Read Hours By Hand

## Status

Accepted (2026-11-07). Extends [ADR 0138](0138-maintenance-rules-list-and-service-log.md)
and its meter-reset amendment. Nothing it decided about live hour meters,
true hours or the status engine changes.

## Context

An hours-based rule counts only for an item with a SignalK hour meter. Gear
without one, such as a watermaker with a mechanical gauge, showed "Hours
unknown" for ever, and its filter change could never come due by hours. The
operator could already type an hours figure on a Complete form, a standalone
log entry or Set last done, but nothing read those figures back: they became
a rule's baseline and went no further.

## Decision

An item with no hour meter takes its current hours from the latest-dated gauge
figure the operator has typed for it. The figure always shows as read by hand.

### Where a figure comes from

- **Log a reading**: a date, a gauge figure and an optional note, kept as one
  row in `hand_hour_readings`. Each reading stays, so the item has a history
  and a typo can be deleted.
- **A Complete form or a standalone service log entry**: the hours on the
  entry, as the item's service log already stores them.
- **Set last done**: the hours typed there also keep a reading, dated the
  last-done date, or today for an hours-only entry. A date after today keeps
  none.

The item's current hours are the figure with the latest date. On the same date
the larger figure wins, since hours only run forward. Nothing is averaged,
extrapolated or carried forward by engine use.

### Gauge and true hours

The operator types the gauge. A reading stores both the gauge figure and the
true hours, converted with `offsetInForceAt` on the reading's own date, the
same conversion every typed hours figure already takes. The gauge shown for
the current hours is the true hours less the offset in force today, so a
meter replacement recorded after a reading does not rewrite what was read.

### Where it shows

- The Maintenance list, the Complete form and the item page say "Read by hand,
  3 weeks ago" where a live reading says "as of 6h ago". The Complete form
  pre-fills from it, labelled the same way.
- Mate's `find_equipment` and `list_maintenance` return
  `hours_read_by_hand` and `hours_read_on` beside the figure, and their
  descriptions tell Mate to say it was read by hand and when. Mate does not
  propose readings.

### What it refuses

- **An item with an hour meter takes no hand reading.** The API answers 409
  with "this item has an hour meter", and the UI offers no action. If a meter
  goes quiet, the rule reads "Hours unknown" as before, whatever was typed. A
  hand figure standing in for a dead meter is the masking fallback the
  project's policy rules out, and the operator would not see that the meter
  had stopped.
- **An item with no meter and no figure** reads "Hours unknown", as today.
- **A date after today, a negative gauge, or a missing gauge.** The reading
  is refused and names the field.

If a meter is later bound to an item, its old readings stay on file and are
never read. The item takes the live figure.

## Rejected

- **Using the hand figure when a meter is silent.** Rejected for the reason
  above.
- **A staleness alarm for old readings.** Out of scope. The age sits beside the
  figure, and the operator judges it.
- **Reading hours off a rule's `last_done_hours`.** That field moves with the
  rule's date and can pair an old figure with a new date, so it is not a
  dated reading. Set last done writes a reading of its own instead.

## Consequences

- Hours on an item with no meter are as good as the operator's last
  reading. A watermaker run hard since the last reading shows fewer hours
  than it has.
- Reading the hours costs two small queries per item with no meter, in
  place of none.
- Hand readings are saved data in a new table. A database without it gets it
  on the next start, and no stored shape changes.
