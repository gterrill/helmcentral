# ADR 0148: Maintenance Schedules Reference The Equipment Profile

## Status

Accepted (2026-10-01). Reverses the alternative ADR 0138 rejected as "Rules
live only on the profile, instance overrides live on the item", and removes
ADR 0138 §7's copy. Amends [ADR 0145](0145-mate-reads-the-maintenance-list.md)
(`list_maintenance` gains provenance) and
[ADR 0146](0146-mate-proposes-the-operator-applies.md) (the copy operation
goes; `update_rule` on a profile job becomes an override). Supersedes
[ADR 0053](0053-engine-profiles.md) and
[ADR 0102](0102-equipment-profiles-and-the-duplicate-path.md) on where
profiles are stored. ADR 0149, to follow, does the same for alarm thresholds.

## Context

"Use profile schedule" copied a profile's service jobs into an item's
maintenance rules once, at the operator's click. After that the copy and the
profile went their own ways. A corrected oil-change interval on the QSB
profile reached no engine that had already been set up, and nothing said so.
The operator's words: every item using a profile should reference it.

ADR 0138 rejected exactly this, on the ground that a profile has nowhere to
hold one engine's baseline (last done, acknowledgement, due-soon settings).
That is true, and it is an argument for keeping per-item state on the item,
not for copying the schedule itself. The two can be separated.

Two other facts shaped the answer.

- The bundled QSB 6.7 and Onan profiles ship every service interval empty.
  Cummins publishes them in QuickServe, behind a login with no export, so
  there is nothing to cite. The operator fills them in. Without per-item
  overrides, one engine that genuinely differs (an impeller in silty water)
  would force a second copy of the whole profile.
- Profiles lived as JSON files under `plugins/engine-profiles`, which the boat
  mounts read-only. Saving a profile failed on the boat. They reached the boat
  by hand copy, and on dev a UI save rewrote files tracked in git. Once
  maintenance rows depend on a profile live, deleting it or dropping a job
  from it has to be checked against those rows, and a check against a file
  and a write to SQLite cannot share a transaction.

## Decision

### 1. Profiles are operator data in helmcentral.sqlite

An `equipment_profiles` table (id, kind, validated JSON body, `based_on_*`,
timestamps) replaces the directory. The profile store shares the document
store's connection, so profile writes and maintenance writes run in one
transaction. Every row is re-validated on load; an invalid row is reported
as a problem and its items' jobs do not render (§4).

The three shipped profiles move into the binary as a catalogue
(`backend/profile_catalogue/`, source `helmcentral`). "Add from catalogue"
copies one in as the operator's own, recording `based_on_{source, id,
sha256}`. The catalogue keeps ADR 0053's citation rule and its "no bundled
alarm thresholds" test. Operator profiles may be uncited. The `source` field
leaves room for a moderated community library of profiles later; nothing
here builds it.

Profiles are not plugins. They run no code and fetch nothing, and they are
edited, which no plugin is.

### 2. A profile job is a live reference with lazy per-item state

An item's schedule is its profile's service jobs, resolved on every read,
plus the rules the operator added for that item. A profile job has the
deterministic id `job:<equipment_id>:<service_id>`. Its state row in
`maintenance_rules` is created on the first write (acknowledge, last done,
complete, procedure note, override). An untouched job has no row.

The id leaves out the profile id, so switching an item between two profile
variants that share a service id keeps its baseline.

### 3. Overrides are per item, marked and resettable

An item may override a job's description, hours interval, months interval,
or mark it not applicable. `overridden_fields` names which; interval columns
on a job row hold a value only when overridden. Every override shows as one
and resets to the profile. Due-soon settings and the fixed due date are
ordinary per-item state, not overrides.

`PUT /rules/:id/overrides` and `DELETE /rules/:id/overrides/:field` are the
only way to change a job. `PUT` and `DELETE` on a live `job:` id return 400.

### 4. One resolver, no snapshots

`buildMaintenanceSchedule` is the single source of effective values: list,
single read, write responses, completion (hours check, fixed-date advance),
procedure-note title, CSV export and Mate. Nothing reads a job row's raw
interval columns.

If an item's profile is missing or invalid, its jobs are not rendered from
any stored copy. The list carries a `schedule_errors` entry naming the item
and the problem, writes to its jobs return 409, and its hand rules still
list. A description snapshot is kept on the row only to label history.

### 5. Jobs that leave

A job removed from the profile, or left behind when an item changes profile,
leaves the schedule. Its row and log entries stay and show on that item's
page under "No longer in the profile", where they can be deleted. A preview
endpoint lists what an item's profile change keeps, drops and adds, so the
operator confirms it first.

A profile save that drops a job some item holds state for returns 409 with
the list, unless confirmed. Deleting a profile any item uses returns 409.

### 6. Conversion is a command the operator runs

`helmcentral convert-profile-rules` (dry run by default, `--apply`), after
the `migrate-db` precedent (ADR 0141), imports the old profile directory and
converts copied rules in one transaction: differing values become overrides,
rows are re-keyed to `job:` ids with their log entries and saved Mate
proposals following, and a rule whose job left the profile becomes a hand
rule. An item whose profile does not resolve aborts the run unless
`--detach-unresolved`; duplicate rules for one job always abort. The
effective schedule before and after must match or the run rolls back with
the difference. The server refuses to start while unconverted rows or an
unimported profile directory remain.

### 7. first_at_hours and supersedes

Shown as the profile states them. Their semantics are deferred.

## Consequences

- A profile edit reaches every item using it at the next read. That is the
  point, and it is fleet-wide and immediate; the 409 guards are what stand
  between a mistyped save and lost history.
- Rule ids change once, at conversion. Anything holding an old UUID (an open
  tab, an unsaved proposal outside the database) goes stale.
- The CSV export gains a `schedule` column (profile or item).
- Mate no longer proposes copying a schedule; there is nothing to copy.
- The frontend shows provenance, overrides and removed jobs; "Use profile
  schedule" is gone.

## Alternatives considered

**Keep copying, add a "profile changed, re-copy?" prompt.** Every item then
carries a diff to review after every profile fix, and the operator was
explicit that the profile should be the reference.

**Profiles as JSON files on the data volume.** Keeps the readable file but
the guards in §5 would race the save. The Profiles page editor and download
keep the readable form.

**Make the plugins mount writable.** Fixes the boat's failed saves and
nothing else.

**No per-item overrides.** Rejected for the reason in the context: one odd
engine would fork the whole profile.
