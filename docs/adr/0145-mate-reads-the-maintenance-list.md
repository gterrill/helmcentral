# ADR 0145: Mate Reads The Maintenance List

## Status

Accepted (2026-09-30). Amends [ADR 0138](0138-maintenance-rules-list-and-service-log.md),
whose consequences named "no Mate tool" as out of scope for that cycle.
Builds on [ADR 0131](0131-mate-diagnoses-missing-and-stale-telemetry.md)
(Mate's read-only diagnostic tools), [ADR 0106](0106-documents-in-the-binary.md)'s
search and citation links, and the "today" amendment to ADR 0138 (the server
never works out the operator's date for itself).

## Context

A conversation asked Mate to update the maintenance schedule for the
genset. Mate correctly said it had no maintenance tools: ADR 0138 built the
rules, the status engine and the service log, and left Mate out. The data
exists, it is the most useful thing on the boat to ask a question about
("what's overdue", "is the schedule for the main engine complete"), and Mate
could see none of it.

Three constraints from the code shape the answer.

- Every Mate tool is read-only (the rule in `runToolRound`). Tools run in
  parallel with no locks and a retried or cancelled call has no idempotency
  key. This cycle adds reads only; letting Mate propose changes is a later
  cycle and keeps that rule.
- A rule's status depends on today's date, and ADR 0138's amendment says the
  server never uses its own clock for that: every rule view needs `?today=`
  from the browser. Mate's tools need the same date, and a chat message is
  the only request that reaches them.
- A rule's hours axis depends on a live meter reading that is sometimes not
  there. The status engine already reports that as "hours unknown". A tool
  that turned it into 0 would make a genset with an unreadable meter look
  freshly serviced.

## Decision

### 1. The message carries today

`POST /api/assistant/conversations/:id/messages` takes a required `today`
(YYYY-MM-DD, the browser's own local date, `todayISO()`). A missing or
malformed value is a 400 naming the field; there is no fallback to the
server's clock. The date rides on the run into the tool dependencies. The
message POST is the only request that starts a run, so no other endpoint
needed it. Every frontend send path (panel, sheet, voice, Hey Mate) goes
through one hook, which now always sends it.

### 2. Three read tools

- **`find_equipment`** matches registry items by name, manufacturer, model or
  alias, optionally within a system. Each hit carries the id, whether the
  hour meter is bound, whether it is reading, the reading, the rule count, and
  the linked profile's service block (the manufacturer's recommended
  intervals), so Mate can compare the rules against the book. A profile that
  is named but no longer installed is reported as missing.
- **`list_maintenance`** returns rules filtered by item, status, system or
  calendar-only, most urgent first. Every row's status, remaining hours and
  remaining days come from `resolveMaintenanceRuleView` against the message's
  date. Nothing in the tool works out a status. The next due date is today
  plus the view's remaining days, and the next due meter reading is the
  current meter reading plus the view's remaining hours.
- **`get_maintenance_log`** returns log entries newest first, filtered by
  item, rule and a since date: date, hours, kind, description, who, cost,
  currency and parts.

Results go through `capToolResultJSON`. A capped list drops its last rows
(the least urgent for rules, the oldest for the log), says `truncated` and
still reports the full total.

### 3. Unknown stays unknown

A reading that is not there is omitted from the JSON, never written as 0:
`current_meter_reading` and `remaining_hours` are absent, `hours_unknown` is true,
and the prompt tells Mate to say the hours are unknown. A tool called with no
`today`, or with no store behind it, returns an error that surfaces to Mate
and the operator, the same as `search_documents` does for a missing library.
A missing procedure note or profile is an error or a `profile_missing` flag,
not an empty string.

### 4. Prompt rules and citations

The stable prompt says: any due, overdue or schedule question goes through
these tools, never memory; a schedule review compares the rules with the
profile's service block and the manuals (`search_documents`) and names the
gaps; unknown hours are reported as unknown; and Mate cannot change the
schedule yet.

Rule and log rows are cited as a link to Inventory, Maintenance
(`/inventory/maintenance`); the section has no per-rule URL. An item links
to its editor page (`/inventory/equipment/<id>`).

### 5. Help pages are not documents

`read_help` returns a page id such as `features/maintenance`, not a document
id, so a citation built from it came out as `/documents?document=?` (seen in
the conversation above). Two changes: the prompt says a help page has no link
and is named in plain words, and the markdown renderer treats a
`/documents?document=` link whose id is not a real document id as plain text,
with no lookup, rather than an icon that can only show "not found".

## Consequences

- Mate can answer what is due and how the schedule compares with the
  manufacturer's, from the same status the Maintenance list shows.
- The three tools only read, so the `runToolRound` rule stands and its
  comment now names them.
- `resolveMaintenanceRuleView` and `equipmentHourReading` read the global
  store, so the tools' store dependency and that global must be the same
  store. They are in production; a test that injects a separate store must set
  the global too.
- The tools name the scale of every hours figure. `current_meter_reading`
  and `next_due_meter_reading` are what the operator's gauge shows, and the
  prompt tells Mate to quote them when saying when a job falls due.
  `last_done_engine_hours` and the log's `engine_hours` are cumulative
  across meter replacements (reading plus the reset offset) and equal the
  meter only when no replacement is recorded. `remaining_hours` is the same on
  either scale.
- A rule can be flagged hours unknown and still read as fine on its calendar
  interval. The tool reports both facts; the prompt asks Mate to say so.
- Every send now needs `today`. An older browser tab that loaded the app
  before an upgrade will get a 400 naming the field until it is reloaded.
- Mate still cannot change a rule or the log. That is the next cycle:
  a tool that builds a validated proposal the operator applies with a tap.
  *Amended 2026-09-30: [ADR 0146](0146-mate-proposes-the-operator-applies.md)
  adds `propose_maintenance_changes` and the Apply and Dismiss card. Mate
  still writes nothing; the operator's tap does. The prompt's "cannot change
  the schedule yet" is replaced by the propose-and-apply rules.*

## Alternatives considered

**Let the server use its own date for Mate.** Rejected for the reason the
maintenance endpoints reject it: a boat east of UTC would get yesterday's
status for the first hours of every day, and Mate would disagree with the
list on screen.

**Compute status inside the tool.** Rejected. There is one status engine and
one place that composes it with the live hours. A second computation would
drift, and Mate's answer and the list would disagree without either being
obviously wrong.

**One combined `get_maintenance` tool.** Rejected. Finding an item, listing
its rules and reading its history are separate questions with separate
filters, and a combined result would spend the tool-result budget on history
when the question was only what is due.

**A dedicated help-page citation link.** Rejected for now. The help sheet has
no URL for a page, so there is nothing to link to. Naming the page in plain
words is accurate; a link waits until the sheet can open a page by address.
