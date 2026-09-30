# ADR 0146: Mate Proposes, The Operator Applies

## Status

Accepted (2026-09-30). Amends [ADR 0145](0145-mate-reads-the-maintenance-list.md)
and [ADR 0138](0138-maintenance-rules-list-and-service-log.md), whose
consequences left Mate unable to change the maintenance schedule. Builds on
[ADR 0116](0116-notes-are-documents-and-folders-are-the-manual.md) and
[ADR 0119](0119-notes-in-the-mate-panel.md) (Save as note, the precedent for
"Mate writes nothing, the operator's tap does") and on
[ADR 0120](0120-mate-on-is-the-consent.md).

## Context

ADR 0145 gave Mate the maintenance list to read. The conversation that
started it asked for more: "update the maintenance schedule for the genset".
Reading the manual and typing twelve rules by hand is the tedious part of
setting a schedule up, and it is exactly what Mate is good at.

The obstacle is a rule, not an oversight. Every Mate tool is read-only
(`runToolRound`): a round's tool calls run in parallel with no locks, and a
retried or cancelled call has no idempotency key. A `draft_note` tool was
designed once and dropped for a Save as note button (ADRs 0116 and 0119) for
that reason, and a duplicated rule is no better than a duplicated note.

Two other things in the code shape the answer.

- The maintenance write handlers held their own validation and the
  gauge-to-true hours conversion (ADR 0138's 2026-09-27 amendment) inside
  echo handlers. A second writer could only copy them.
- `maintenance_rules.updated_at` exists, so a proposal can notice that the
  rule it was written against has since changed.

## Decision

### 1. Mate proposes; the operator's Apply tap writes

`propose_maintenance_changes` takes a list of operations, validates them,
and returns them as a proposal. It writes nothing, so the read-only rule
stands and the `runToolRound` comment now says why this tool is not an
exception. The runner collects proposals from the tool results, the message
handler saves them with the assistant message in that message's own
transaction, and the operator's tap on the card is what changes the
schedule. A run that fails or is cancelled saves no message and therefore no
proposal. A retried or duplicated tool call costs at worst an extra card.

### 2. The operations

| Operation | Does |
| --- | --- |
| `create_rule` | A new rule, for an item or calendar-only, optionally with what was last done |
| `update_rule` | Description, intervals, due-soon windows; only the fields given change, `clear` removes an interval |
| `set_last_done` | The baseline: a date and/or a meter reading |
| `complete_rule` | Log the job as done (date, meter reading, description, who, cost) and reset the baseline |
| `acknowledge` | Acknowledge a rule with a reason |
| `copy_profile_schedule` | Copy the profile's services the item has no rule for |

Out of scope: deleting rules or log entries, editing log entries, photos,
parts, meter replacements and procedure notes. They either destroy something
or need something physical from the operator.

Every hours figure in a proposal is a meter reading, what the gauge shows,
named `meter_reading` and `last_done_meter_reading`. The handlers' own
convention (ADR 0138's amendment) is unchanged: the commands convert to true
engine hours, with the offset in force on the date given, when the proposal
is applied, so an applied proposal and the same edit typed into the form
store the same number.

### 3. One set of commands

Each maintenance write handler's body moved into a context-free command in
`maintenance_commands.go`: validation, gauge conversion and the store call,
taking an open transaction, the store's clock reading and the operator's
`today`. The store's create, update, acknowledge, set-last-done, copy and
complete methods gained transaction-taking variants; the public methods and
the handlers call them through `RunMaintenanceTx`. The handlers are now bind,
call, respond, and Apply runs the very same commands, so the two cannot
disagree about what is valid. The existing handler tests passed unchanged.

### 4. Propose dry-runs, snapshots and summarises

For each operation the tool resolves ids and checks its fields with the
commands' own validators. It then dry-runs the whole list: one transaction on
the document store, every operation in order through the same commands Apply
uses, then always a rollback. Validating operations one at a time against the
current schedule would miss what an earlier operation in the same proposal
changes (an update that adds an hours interval makes a later completion need
a meter reading); the dry run cannot. Any failure fails the whole call with an
error naming the operation and the field (`ops[2] (update_rule):
interval_hours: ...`), so Mate corrects it rather than showing the operator a
card that cannot apply. An unknown field, or one that does not belong to the
operation, is refused rather than ignored.

Nothing is committed, so the tool still writes nothing and the `runToolRound`
comment says so. The transaction holds the document store's mutex, so a
round's concurrent tool calls queue behind it rather than deadlock; its only
other wait is the database file's write lock, which an Apply holds briefly and
never while waiting on that mutex.

The tool records each target rule's `updated_at` as a snapshot, and writes one
line per operation in the operator's words ("Generator · Oil and filter: every
250 h or 12 mo, last done 9 Jan 2025 at 239 h (meter)"). The line comes from
what the dry run produced: a completion states the fixed due date it moves the
rule to, and a profile copy names the rules it would create, and its service
ids are stored as the copy's snapshot. The line is written once, on the
server; the card and Mate's history both use it.

### 5. Storage

`assistant_message_proposals` (id, message id, position, operations JSON,
status pending, applied, dismissed or stale, stale reason, resolved time,
result JSON) lives in
the assistant half of the one business database (ADR 0141), created by
`createAssistantSchema`, with a real foreign key to `messages` and cascading
delete. `AppendMessage` inserts a message's proposals in its own transaction.
`ListMessages` returns each message's proposals with their current status, so
a reloaded conversation shows applied and dismissed cards as they were left.

### 6. Apply

`POST /api/assistant/proposals/:id/apply?today=` and `POST
.../dismiss`, both write tier.

- One transaction runs every operation and marks the proposal applied, or
  nothing is written. The transaction is opened on the assistant store's
  connection but writes the maintenance tables, which share the one database
  file; that is what lets the operations and the applied mark commit together.
  The commands read only through the transaction.
- Before the first write, every rule an operation targets is compared with
  the snapshot. A different `updated_at`, or a deleted rule, is a 409 ("a
  rule changed since Mate proposed this, so nothing was applied") and nothing
  is written. A profile copy has no rule to snapshot, so it is checked by
  outcome: if it would now create a different set of rules than the card
  promised (the operator copied by hand in the meantime), that is the same
  409, rather than an apply that creates nothing and reports success. `updated_at` has one-second resolution, so an edit in the same
  second as the proposal is not detected; the proposal is read minutes
  before a tap, so this is accepted.
- Applying an applied proposal returns the stored result and writes
  nothing, so a double tap or a retry is safe. The second request waits on
  the first's transaction and then reads the applied status.
- A dismissed proposal cannot be applied (409); an applied one cannot be
  dismissed (409). Dismissing twice is a no-op.
- `today` is required, as on every maintenance write: an hours-only baseline
  is converted at the operator's date.
- A failing operation midway rolls everything back. The schedule no longer
  matches the proposal, so this is treated like the stale check: the answer
  is a 409 whose message says which change failed and why.
- Stale is stored. After the rollback, a separate transaction sets the
  proposal's status to `stale` and saves the reason (`stale_reason`), for the
  stale check, an operation the commands now refuse, a missing target, or a
  changed copy. A database failure is not one of these and leaves the proposal
  pending. A stale proposal can be neither applied nor dismissed (409, with
  its stored reason).

### 7. Mate sees what became of its proposals

`assistantHistoryMessages` appends each proposal's summary lines and its
status to the assistant turn, worded so that Mate knows on the next turn
whether the changes are in the schedule, were dismissed or went stale (and
should not be proposed again unasked) or are still waiting. A stale proposal is reported as such, with its reason. The prompt replaces cycle 1's
"cannot change yet": propose only what the operator asked for or agreed to,
read the rules first, put all changes in one call, say to tap Apply and never
claim it is done, and give hours as meter readings.

### 8. The card

Under the assistant message in `assistant-thread.tsx`: one line per change,
Apply and Dismiss, and, once applied, each line linking to Inventory,
Maintenance. A 409 shows the server's reason and "Ask Mate to redo it". A
viewer below write tier sees the card with no buttons. A stale card shows the
stored reason and "Ask Mate to redo it" with no buttons. The card renders from
the stored status, so it is correct after a reload.

## Consequences

- Mate can set up a schedule from a manual or from what the operator says,
  and the operator checks one card instead of typing a dozen rules.
- The read-only tool rule holds. Nothing Mate calls writes; the write is an
  authenticated, write-tier endpoint the operator's tap reaches.
- Handler and Apply validation cannot drift. The shared table-driven tests
  run the same cases through the HTTP handler and through the tool.
- All-or-nothing Apply. One wrong line means dismissing and asking again, or
  editing the rule in Maintenance afterwards. Per-line tick boxes are the
  obvious follow-up if that chafes.
- A stale proposal is stored as stale with its reason, so the card, a reload
  and Mate's next turn all say so, and Apply is never offered again for
  something that can only fail the same way. Mate is told it went stale and
  why, and is asked not to re-propose unless the operator asks, and then to
  read the rules again first.
- The proposal's operations JSON carries the ids Mate used. A rule deleted
  between proposing and applying counts as stale, not as a separate error.
- Two operations that target the same rule are checked against the same
  snapshot up front, then run in order; the second sees the first's writes.
  A `copy_profile_schedule` cannot be followed by an operation on the rules
  it creates in the same proposal, since those ids do not exist yet;
  `create_rule` carries its own last-done fields for that reason.
- ADR 0145's consequence that Mate cannot change the schedule, and ADR
  0138's that Mate has no tool, are both superseded for proposals.

## Alternatives considered

**Give Mate a write tool.** Rejected for the reason ADR 0116 rejected
`draft_note`: parallel tool calls with no locks and no idempotency key.
Locking and keying it would work, but it also puts a language model's
unreviewed output straight into the service schedule of a boat.

**Have the frontend replay the operations through the existing endpoints.**
Rejected. Several requests are not one transaction, so a failure midway
leaves half a schedule; and the stale check and the idempotent re-apply need
the server to own the proposal.

**Per-line tick boxes.** Deferred. They add a partial-apply state to a
transaction that is otherwise all or nothing, and a card of a handful of
lines is quicker to dismiss and redo than to tick through.

**A separate table in the documents half of the database.** Rejected. The
proposal belongs to a message and must be saved in that message's
transaction; keeping it beside `messages` makes that one transaction rather
than two connections agreeing.

**Store the proposal only in the conversation text.** Rejected. A card needs
a status that survives a reload, and Mate's next turn needs to know it.
