# ADR 0138: Maintenance Rules, A List, And A Service Log

## Status

Accepted (2026-09-27). Builds on [ADR 0123](0123-inventory-begins-with-the-equipment-registry.md)
(the equipment registry), whose own consequences section named exactly this
gap: "Service intervals remain parsed, served and unconsumed. The instance
record they were waiting for now exists, which is what makes the
maintenance cycle buildable." Reuses [ADR 0131](0131-mate-diagnoses-missing-and-stale-telemetry.md)'s
live-path-reading machinery for hours, [ADR 0127](0127-bins-open-from-a-tag-items-carry-photos.md)'s
shared upload intake for log-entry photos, and [ADR 0116](0116-notes-are-documents-and-folders-are-the-manual.md)'s
note store for procedure notes.

## Context

An equipment profile ([ADR 0053](0053-engine-profiles.md), extended by
[ADR 0102](0102-equipment-profiles-and-the-duplicate-path.md)) has always
been able to say "oil and filter every 250 hours" - the `service` block is
validated and served over the API. Nothing has ever read it. Every engine
aboard eventually needs its own service history: not what the manufacturer
recommends in the abstract, but what was actually done to *this* one, when,
at what hours, at what cost, and what's due next. The equipment registry
(ADR 0123) is what makes that a well-posed question at all - "20 hours
since the last oil change" now has an instance to hang the hours on.

Two things outside the registry's own reach also needed a place: gear that
never gets a profile still needs a schedule (anode checks, seacock
services), and several real things aboard a boat expire on the calendar
with no piece of equipment behind them at all - flares, an EPIRB battery,
liferaft service, insurance, registration. All of it is fundamentally the
same question - "when is X next due, and what happens when it's overdue" -
and the operator asked for one feature that answers it, not three.

## Decision

### 1. One table, rules with or without an item

A `maintenance_rules` row is a description plus an interval (hours, months,
or both - "whichever comes first") or a fixed calendar date, and a
baseline (last done, by date and/or hours). `equipment_id` is nullable:
most rules belong to an item, and a calendar-only rule (§7) simply has
none. Modelling calendar-only rules as a separate "certificates" feature
was considered and rejected - the due/overdue/due-soon computation, the
due-soon window, acknowledgement and the service log are identical in
every respect that matters, and a second feature would mean a second copy
of all four, disagreeing eventually.

A rule needs at least one of an hours interval, a months interval or a
fixed date - except a rule copied from a profile's own service entry with
both intervals empty (a *slot*: the job is named, its frequency isn't
known yet). That rule is real and shown, not hidden, as its own status:
**interval not set**.

### 2. Status is computed, never stored

`computeMaintenanceRuleStatus` (`backend/maintenance_status.go`) is a pure
function: a rule's own interval/baseline fields and the item's current
hours in, one of six states out - **overdue**, **due soon**, **ok**,
**never recorded**, **interval not set**, **hours unknown**. Nothing about
a rule's status is ever written to the database; every response recomputes
it against the live hour reading and the wall clock, so the list can never
show a status that was true five minutes ago and isn't any more.

"Whichever comes first" is the more urgent of the hours-axis and the
calendar-axis result, each evaluated independently and only when its own
interval is actually configured. A rule that has genuinely never been done
(no baseline recorded at all) reports **never recorded** ahead of either
axis - baseline-zero is not the same claim as "this is overdue," and
conflating them would make onboarding a boat's existing gear look like a
crisis.

The due-soon window (default 50 hours / 1 month, per-rule overridable) is
two named constants (`maintenanceDefaultDueSoonHours/Months`), not a value
seeded anywhere with a Pikorua-specific number - AGENTS.md's vessel-agnostic
rule applies to defaults as much as to hard-coded thresholds.

### 3. Hours are read live, the same way Mate already does

An item's current true hours come from its own `hour_meter_path` via the
identical snapshot-reading path ADR 0131's diagnostic tools use
(`snapshotAlarmReader`/`pathAge`) - not a second way of asking SignalK "what
does this say and how old is that." SignalK publishes runtime in seconds;
`currentEquipmentHours` (`backend/maintenance_hours.go`) converts to hours
and reports **unknown** (never a guess) when the path is unbound, has never
reported, or is older than the same staleness threshold
(`derivedInputMaxAge`) every stale badge in this codebase already agrees on.
A rule whose only configured interval is hours, on an item whose reading is
unknown, shows **hours unknown** as its own status; a rule with both an
hours and a months interval still resolves from the calendar axis, with
`hours_unknown` carried as a separate flag so the operator sees both facts
rather than one masking the other.

An item with no `hour_meter_path` at all has no live reading full stop -
the operator records hours by hand, at completion time, same as a boat with
no engine-hours meter has always done. That case and "path bound but
stale/missing" both read as `has_hour_meter_path: false` and `hours_unknown:
true`/`false` respectively, and the frontend words them differently rather
than treating "not bound" and "gone quiet" as the same sentence.

**Meter replacement.** `hour_meter_resets` keeps one row per replacement
(the old reading, the new meter's own reading, the date), and only the MOST
RECENT row's `old_reading - new_reading` is added to the live meter's raw
value going forward - not a running sum over every replacement ever
recorded. The operator's own stated `old_reading` at each replacement
already IS the cumulative true figure up to that point (it's what the
superseded meter or gauge was showing), so the arithmetic is self-correcting
across any number of replacements without this code ever summing history
itself.

### 4. A rule's own baseline, and three ways to set it

`last_done_at`/`last_done_hours` are a rule's own columns, set three
different ways for three different reasons:

- **Completing** the rule (§6) - the ordinary path, always with a log entry.
- **Set last done** - onboarding: recording what was already true before the
  rule existed in this app, explicitly *without* writing a log entry, so a
  boat's whole existing service history is never faked as "done through
  Helmcentral today."
- **Copying** a profile schedule entry (§7) starts a rule with no baseline
  at all - **never recorded** until one of the above sets it.

An ordinary rule edit (description, interval, due-soon override) never
touches the baseline, the acknowledgement, or the procedure note link -
each has its own endpoint, the same reason equipment's own PUT excludes
photos and documents (ADR 0123/0127): a save on one part of the record must
never have a side effect on another part the operator wasn't looking at.

### 5. Acknowledging is a display concern, not a status

A rule can be acknowledged with a short reason. It stays overdue or due
soon and still counts toward everything that reads status - acknowledging
is not a seventh state, it is a flag (`ack_at`/`ack_reason`) the list uses
to sort an acknowledged rule after its unacknowledged neighbours within the
same status group, and to show a small badge. `computeMaintenanceRuleStatus`
takes no acknowledgement input at all, by design: nothing about *whether* a
rule is due can depend on whether someone has already said "I know."
Completing a rule clears its acknowledgement unconditionally - the thing it
was acknowledging is now done.

### 6. Completing writes history; the item's own page manages the rest

Completing a rule (`CompleteMaintenanceRule`, one transaction) writes a
`maintenance_log_entries` row with `kind` fixed to `'maintenance'` - never
taken from the request, because completing a rule *is* the maintenance
kind - resets the rule's baseline to that same date/hours, and clears any
acknowledgement. A log entry can also stand alone, against an item with no
rule at all (a repair, an improvement), with its own kind chosen by the
operator.

Editing or deleting a log entry never retroactively recomputes its rule's
baseline, even when it was the rule's most recent entry - the baseline is
only ever set by completing a rule or by Set-last-done. Chasing "is this
still the most recent entry" on every edit was judged not worth the
complexity for what is, in practice, a rare correction to a typo or a cost
figure.

### 7. Calendar-only rules keep the same table, the same list, a different heading

A certificate/expiry rule is `equipment_id = NULL`, `interval_months` and/or
a `fixed_due_date`, everything else identical. The list groups it under
"Certificates and expiries" instead of an item name - a display decision,
not a schema one. A system filter excludes these rules, the same way it
would exclude gear the operator isn't asking about: there is no system a
liferaft service belongs to.

### 8. A procedure note is a link, not a body

A rule may link one document as its procedure - reusing the notes feature
(ADR 0116) rather than inventing a second place to write steps. "Create
procedure note" (`createMaintenanceProcedureNote`) writes a minimal
Procedure-type note titled from the rule's own description and links it in
one action; this duplicates the small essential slice of
`createNoteHandler`'s own logic (id/hash/lock/insert) rather than
refactoring that handler to share it - the same call ADR 0127 made for its
first photo-upload cut ("a second write-tier file intake... judged lower
risk than reshaping it to serve both"), and the same kind of call: if this
drifts the way that one did, a shared intake is the natural next step, not
a foreclosed option.

"Opens it" is the existing citation-link mechanism (`documentViewerHref`,
`lib/document-citation.ts`) Mate's own chat already uses for exactly this
job - no new cross-panel navigation was built. If the linked note happens
to be a checklist, the checklist-run UI it already carries appears the
moment the note opens, the same as it does anywhere else a checklist note
is opened; nothing here duplicates that UI. On completion, the dialog
offers "Open procedure note to update" as a link - it never opens the
editor automatically, because a rule can be completed with nothing about
the procedure needing to change.

### 9. A plain list, not kanban

The Maintenance section is a new entry in the Inventory panel's own
`SectionNav`, beside Equipment rather than nested under it - it lists rules
across every item plus every calendar-only one, not one item's own view.
Grouped by status in a fixed order (Overdue, Due soon, Never recorded,
Hours unknown, Interval not set, OK), sorted within a group by
acknowledgement, filterable by system. See §10 for why kanban was rejected.

The equipment editor's own Maintenance block (an item's page) is
deliberately narrower: it lists that item's rules and recent log entries,
with add/edit for both, "Use profile schedule," and the meter-reset
control - all of which are genuinely about *this item*. Completing and
acknowledging a rule are not duplicated there; they live only on the main
list, which is where an operator actually works through what's due across
the boat, not item by item.

### 10. Cascades: a rule and its log follow the item; links follow the document convention

`maintenance_rules.equipment_id` and `maintenance_log_entries.equipment_id`
are both `ON DELETE CASCADE` - deleting an item takes its rules and its
whole service log with them. This is a deliberate departure from
`equipment_documents`' own unlink-only convention (ADR 0127's 2026-09-25
amendment: never destroy a document as a side effect of removing it from an
item), because a rule or a log entry is not a shared, independently
meaningful resource the way a linked document is - nothing else can ever
reference the same rule or log row, and a service log entry for an engine
that no longer exists in the registry has no meaning to preserve.
`maintenance_log_entries.rule_id` is `ON DELETE SET NULL` instead: deleting
a *rule* must never erase the history of it having been done, so a
completed entry survives as a standalone log row.

Log-entry photos (`maintenance_log_photos`) and parts-used
(`maintenance_log_parts`) both `CASCADE` on every foreign key they carry -
ordinary link tables, the same reasoning `equipment_documents` itself
already gives for why a link is metadata about a relationship, not a thing
either side must protect the other's existence for. A photo shared (by
sha256 dedupe) between an item's own photo strip and a log entry is
protected from deletion by either side alone -
`DocumentDeletableAsOrphanPhoto`/`MaintenanceLogPhotoDeletableAsOrphan` each
check both tables before ever deleting the underlying document.

### 11. CSV export, one endpoint, no new format

`GET /api/inventory/maintenance/log/export.csv[?equipment=]` streams
`date, item, rule, kind, hours, description, who, cost, currency, parts` -
every entry, or one item's own. No new export mechanism: this is the same
route-per-format pattern the rest of the API already uses, just answering
`text/csv` instead of JSON.

## Consequences

- ADR 0123's own open item - "service intervals remain parsed, served and
  unconsumed" - is closed. Copying a profile's `service` block into rules is
  one action, and a fresh operator who has never touched Maintenance still
  sees every equipment profile's own numbers the moment they choose one.
- The status engine's own test suite is the actual specification: every
  combination in this ADR's §2/§3 is a table-tested case in
  `maintenance_status_test.go`/`maintenance_hours_test.go`, written before
  the functions they test.
- No Mate tool, no alarm/notification when a rule becomes due, no stock
  decrement for parts used, no interval by engine starts or distance run,
  and no per-rule reminder schedule - all explicitly out of scope for this
  cycle. Mate cannot yet see a rule's own due status the way it can already
  see live telemetry; that is the natural next integration once this data
  exists to read.
- A photo linked to a log entry has no cover/reorder concept, unlike an
  equipment item's own photo strip - a log entry has no single "hero" image,
  so `AddMaintenanceLogPhoto` only ever appends, with no
  `SetMaintenanceLogPhotoOrder` endpoint at all.
- The equipment editor's Maintenance block does not duplicate Complete/
  Acknowledge (§9) - an operator standing at an item's own page who wants to
  mark a rule done is one click into the main Maintenance list rather than
  a second, parallel set of controls maintained in two places.

## Alternatives considered

**Kanban, a column per status.** The operator's own first instinct, and
rejected on inspection: recurring maintenance doesn't have a "done" column
to sit in - a completed rule doesn't leave the board, it goes back to
whatever its next-due status computes to, possibly OK, possibly already
due soon again if the interval is short. A board whose cards move
themselves out from under a glance is worse than a sortable list that says
the same thing in words.

**Rules live only on the profile, instance overrides live on the item.** A
profile is deliberately shared, unchanged reference data across every boat
that uses it (ADR 0123 §6) - it has no baseline, because a baseline is a
fact about one specific engine's history, not about the make and model. A
rule needs somewhere to hold `last_done_at`/`last_done_hours` and its own
due-soon override, which a profile can never carry without ceasing to be
shareable. Copying (§7) is the seam: the profile's own numbers pass through
once, at the operator's own action, and never again.

**A separate certificates/expiries feature.** Considered because a flare's
expiry really does feel unlike an engine's oil change to an operator typing
it in. Rejected because it is identical in every way the code has to
reason about - due, overdue, due soon, acknowledge, complete-with-a-log-
entry - and a second feature would be a second copy of `computeMaintenanceRuleStatus`
with no behavioural difference to justify the duplication. §7's `equipment_id
IS NULL` plus a different list heading gets the same operator-facing
distinction for the cost of one nullable column.
