# ADR 0225: Mate proposes alarm rules

## Status

Accepted (2026-10-28). Builds on
[ADR 0224](0224-alarm-rules-live-in-the-database.md), which moved the rules into
the record store, and on [ADR 0158](0158-mate-changes-records-through-changesets.md)
(changesets) and [ADR 0172](0172-mate-tools-are-one-record-each.md) (the tool
registry). It is cycle 4b of the agent-first plan, covering its items 1, 4
and 9.

## Context

Mate could audit the alarms and then had to say "you set the threshold yourself
in Settings". The research notes show the cost: an answer that ends in a table
of five candidate rules and no way to make one. With the rules in SQLite, an
alarm is a record like a maintenance rule, so a changeset can write it and the
operator's existing Apply tap is the consent.

Three things about alarm rules do not fit the existing types:

- Rules are stored in SI. A battery's state of charge is the ratio 0.5, a
  temperature is kelvin, a speed is metres per second. A model that writes "50"
  into a ratio makes an alarm that never fires, and the operator reads "0.5" on
  a card as a half percent.
- Some rules the Alarms drawer lists are not stored: tank levels and gauge
  zones are derived (`tank:`, `zone:` ids).
- Some stored rules came with Helmcentral (`seeded_by` is set).

## Decision

**A record type, `alarm_rule`**, registered in `record_type_alarm_rule.go` with
create, update and delete. Its commands take the changeset's transaction and use
`insertAlarmRuleTx`, `updateAlarmRuleTx` and `deleteAlarmRuleTx`. Validation is
`validateAlarmRule` and `validateAlarmRuleFor`, the validators the REST handlers
call, so a rule is checked in one place whoever makes it. A validator message is
reported against the field it names so Mate can correct that field. The version
is `updated_at` to the nanosecond, so a rule edited in the drawer between
proposal and Apply makes the proposal stale.

**Values are in the operator's unit.** `value` and `clear_value` are given and
shown in the unit `operatorUnitTable` (alarm_units.go) names for the path's SI
unit, the table the alarm cards already use. Every entry is affine, so the type
derives the inverse from two points of the forward conversion rather than keeping
a second table. A path with no declared unit takes the number as it is, and the
snapshot's `unit` field is empty. The rule's stored deadband is
`|toSI(clear_value) - value|`, taken on the SI side, so an offset unit such as
degrees Celsius converts correctly. A clear point on the wrong side of the
threshold is refused. Without `clear_value` on an update the existing deadband
stays. The snapshot shows `clear_value` as the absolute point the alarm clears
at, as `alarmClearValueFor` computes it.

**The card shows units.** A new optional hook on `recordType`, `Display`,
rewrites the Before and After values the card shows. The alarm type renders
`50 %` and `30 s`. It runs after the record's own fields are used, so freshness
and the no-change check still compare raw values.

**Derived rules are refused, with a reason.** `Get` answers a `tank:` or `zone:`
id with the sentence the Alarms drawer's REST handler already gives: tank
changes go to `tank_warn_percent`, `tank_alarm_percent` or `tank_alarms_off` on
the equipment record (writable since ADR 0224's release), and a zone
is edited on the gauge. A create on such a path is not special: the path is an
ordinary SignalK path.

**A rule's path is fixed after it exists.** Changing the path changes which unit
`value` is in, and an update that moves the path and the number together is
easy to get wrong. The type refuses it and says to add a new alarm. Delete plus
create fit in one proposal.

**Built-in rules can be edited, switched off and deleted.** The drawer lets the
operator delete any stored rule, and the seeded-set marker is never removed, so
a deleted built-in does not come back (ADR 0224). A changeset does not widen or
narrow that. The proposal says "(built-in rule)" in its line so the operator
knows what they are changing. The snapshot's `built_in` and the tool's `origin`
carry it.

**A new alarm is audible and visible** (`visual` and `sound`), the drawer's
default, and enabled. `label`, `path`, `op` and `state` are required, plus
`value` and `dwell_seconds` for a threshold and `stale_after_seconds` for a stale
rule, because a default Mate did not choose is a value the operator did not see.
Escalation, notification transports, and `path_role` are not writable. They are
operator choices made in the drawer.

**After the commit the running rules reload.** The type's `AfterCommit` calls
`refreshAlarmRules` once the changeset's transaction has committed and the store
lock is released, as ADR 0224 prescribed. A failed reload is logged: the rule is
saved and the engine catches up on the next write or restart.

**One read tool, `list_alarm_rules`**, in the registry. It returns every stored
rule with its version, origin (own or built-in) and values in display units, then
the derived tank and zone rules as `read_only` with `change_it` saying where the
change goes. The prompt requires calling it before proposing, so Mate edits the
rule already watching a path instead of adding a second.

**The prompt.** The old line forbidding changes to settings stays for what it
means. Alarm rules are records, so they are named among the types Mate may
propose, with the rule that it checks `list_alarm_rules`, finds the path with the
path tools, gives levels in the operator's units, always states a clear point and
a dwell, titles the alarm for the 02:00 reader (ADR 0157), and does not claim the
alarm is set until Apply. Monitoring suggestions (item 4) only happen when the
operator asks what to monitor: at most three, from what the boat publishes and
what has no rule yet, each a proposal.

## Consequences

- The operator can ask for an alarm in a sentence and tap once. The research
  notes' stale-source and missing-sensor warnings stay Mate's job to say before
  proposing.
- A model that gets a unit wrong shows it on the card ("falls below 0.5 %"), and
  the operator is the check. The tool result and the card both name the unit.
  The type cannot know a unit the boat never declared, and then the number is
  taken as the rule's own.
- Not done: setting escalation or notification transports, rules that follow
  the house bank by role, editing the tank and zone alarms, and a "reset to
  default" for built-in rules. Each needs its own operator-facing decision.
