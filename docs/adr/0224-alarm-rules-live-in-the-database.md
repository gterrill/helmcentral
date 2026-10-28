# ADR 0224: Alarm rules live in the database

## Status

Accepted (2026-10-28). Builds on
[ADR 0141](0141-one-sqlite-file-for-helmcentral.md) (one SQLite file) and
[ADR 0184](0184-one-transaction-convention-for-the-record-store.md) (one
transaction convention). It is the move
[ADR 0158](0158-mate-changes-records-through-changesets.md) section 9 waits for
before alarm rules can become a changeset type. The changeset type is the next
change and is not in this one.

## Context

Operator alarm rules lived in `data/alarm-rules.json`: a `rules` array and a
`seeded_sets` list of markers, rewritten whole on every change. That cost three
things.

- A changeset is one SQLite transaction (ADR 0158 section 9), so rules could not
  be part of one. Mate could change a maintenance rule and an equipment item
  together, and could not touch an alarm.
- A write was the whole file. Four seeders, the REST handlers, the house bank
  sync and the vessel conversion each held `alarmRulesMu` across a full
  rewrite, and a seeder that failed part way left some of its rules saved and
  its marker unrecorded.
- Three of the four seeded sets (heavy weather, forecast warnings, Law of
  Storms) gave their rules random ids. On disk a seeded rule was the same as one
  the operator wrote, so nothing could ever offer to reset a rule to its default.

## Decision

**Two tables in the record store**, created by `createDocumentsSchema` with the
other record tables (`alarm_rules_store.go`).

- `alarm_rules`: one row per stored rule. The threshold fields are columns
  (`enabled`, `path`, `label`, `path_role`, `op`, `value`, `hysteresis`,
  `dwell_seconds`, `stale_after_seconds`, `alarm_level_value` nullable,
  `capacity_path`, `state`, `escalate_after_seconds`). The two lists, `methods`
  and `notify`, are JSON text. `seeded_by` is the marker of the built-in set
  that made the rule, NULL for a rule the operator made. `created_at` and
  `updated_at` are Unix nanoseconds, so a rule keeps the precision the file gave
  it. `updated_at` is the rule's version when it becomes a changeset type.
- `alarm_rule_seeded_sets`: `marker` and `seeded_at`. A marker is recorded once
  and never removed, so a set the operator deleted on purpose does not come back.

Derived rules (`zone:`, `tank:`) are still never stored.

**Store functions take a transaction.** `insertAlarmRuleTx`, `updateAlarmRuleTx`,
`deleteAlarmRuleTx` and `recordAlarmSeededSetTx` run on a `*sql.Tx`, and the
readers (`alarmRuleByIDQ`, `listAlarmRulesQ`, `alarmSeededSetsQ`) take a
`sqlQueryer`. The changeset type calls them from inside `RunTx`. Nothing here
decides what a rule means: validation stays in `validateAlarmRuleFor`, and the
caller runs it before the transaction, because it reads the vessel settings.

**The engine still reads an in-memory map.** The alarm engine reads the rules
every second and must not touch SQLite to do it. `documentStore.writeAlarmRules`
runs a write in one transaction and, once it has committed and still holding the
store lock, reloads the map from the database. Two writers therefore cannot
leave the map older than the database, and a failed commit leaves the map alone.
A failed reload after a commit is returned as `errAlarmRulesNotReloaded`, which
says the change was saved and should not be repeated, and the handlers answer
500 with that text. A changeset
that writes through `RunTx` calls `refreshAlarmRules` after the commit, as the
equipment type's `AfterCommit` does for anomaly rules.

**Seeding is atomic.** `seedAlarmRuleSet` adds every rule of a set, stamped with
its marker in `seeded_by`, and records the marker in the same transaction. A
failure leaves nothing. The anomaly set keeps its fixed ids. One of those that
already exists with the same path and state is left alone, which covers a boat
whose earlier build created the rule without recording a marker. The old
partial-retry code in `seedAnomalySet` is gone, since a partial set can no longer
be saved.

**The HTTP API does not change.** `seeded_by` is not in the JSON
(`json:"-"`). An update keeps the stored `seeded_by`, `id` and `created_at`
whatever the payload says, as it did for the id and creation time.

**A missing store is a fault.** If `globalDocumentStore` is nil, every rule
function returns an error naming the record store. Nothing falls back to the file.

## The import

`importLegacyAlarmRules` runs once at boot, after `moveLegacyIgnoredSensors`
(which empties the `ignored_sensors` field of the same file, ADR 0207) and before
`loadAlarmRules`. It follows that move's shape.

1. Read the file. A file that is absent is a fresh install or an install that has
   already been imported, and nothing happens.
2. Parse and check it. Invalid JSON, a rule with no or a repeated id, no label,
   an unknown operator or state, or a leftover `ignored_sensors` list stops the
   boot with the file untouched. The old loader skipped a rule with no id
   silently. The import refuses it, because the file is the only copy.
3. If `alarm-rules.json.imported` already exists, stop. Overwriting an earlier
   copy would destroy it.
4. In one transaction insert every rule that is not already in the database and
   every marker, then record `legacy-import:alarm-rules.json` in
   `alarm_rule_seeded_sets`. That table already holds the "offered once, never
   again" markers and goes with every backup, so the import marker needs no
   table of its own. If the marker is already there, the file is not read into
   the database again and only the rename is left. Without it, a file put back
   after a lost rename would bring back rules deleted since.
5. Rename the file to `alarm-rules.json.imported` and sync the folder.

The insert commits before the rename. A crash between them leaves the file in
place, and the next boot sees the marker and only renames, so edits and deletions
made in between are not lost. This differs from the ignored-sensor move in the last
step: that move rewrites the file without the field, and this one renames it,
so the operator keeps the whole original as a backup. The one operator has one
box and no installed base, so the importer, and the call in `main.go`, can be
deleted in a later release.

`ALARM_RULES_FILE` is now only the import source. It is not read after the
rename.

### How `seeded_by` is inferred for imported rules

The file never recorded which rules a seeder made, so the importer works it out
and leaves NULL wherever it cannot be sure.

- **Anomaly rules** have fixed ids, which settle it: the base ids to
  `anomaly-v1`, `helmcentral:anomaly-engine-stumble-<instance>` to
  `anomaly-v1:stumble:<instance>`, the frozen id to `anomaly-v1:frozen`, the two full bank ids to
  `anomaly-v1:battery`, and `helmcentral:anomaly-<quantity>-<instance>-high|low`
  to `anomaly-v1:engine:<instance>`.
- **Heavy weather, forecast warnings and Law of Storms** rules got random ids.
  They are matched on label, path and operator against the seeders' current
  definitions, and only for a set whose marker the file records. A definition
  that two of those sets share is left unclaimed, and so is a definition that
  two rules in the file match (an operator's copy beside the original).
  Value, dwell and state are not compared, since the operator may have tuned
  them.

What stays NULL, and is therefore treated as the operator's own: a seeded rule
the operator renamed or re-pointed, a rule from a seeder whose definitions have
since changed (an older label or path), and any duplicate. The cost of a wrong
NULL is small, because `seeded_by` is not read by anything yet. A wrong claim
would one day offer to reset a rule the operator wrote, so the inference errs
towards NULL.

## Consequences

- Rules are backed up with `helmcentral.sqlite` (the live `.backup` route in the
  configuration reference). A backup of `alarm-rules.json` alone no longer
  covers them. The renamed `alarm-rules.json.imported` is the pre-upgrade copy.
- The next change registers `alarm_rule` as a changeset type over these functions.
  `updated_at` gives it a version, and `refreshAlarmRules` is its after-commit
  hook. Mate-made rules will go through the same `validateAlarmRuleFor`.
- A rule write is now a SQLite transaction, serialised with every other record
  write on the store mutex, instead of a file rewrite under `alarmRulesMu`. Rule
  edits are rare and small, so there is no measurable queueing.
- The same change makes a tank's `tank_warn_percent`, `tank_alarm_percent` and
  `tank_alarms_off` writable on the equipment record type, so a changeset can set
  them. They went through `validateTankLevels` on the item page already, and a
  changeset's update runs the same `updateEquipmentTx` and so the same check,
  which the tests cover.
