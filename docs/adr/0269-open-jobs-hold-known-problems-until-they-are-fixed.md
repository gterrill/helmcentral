# ADR 0269: Open Jobs Hold Known Problems Until They Are Fixed

## Status

Accepted (2026-11-07). Builds on [ADR 0138](0138-maintenance-rules-list-and-service-log.md)
(the rules list and the service log), [ADR 0146](0146-mate-proposes-the-operator-applies.md)
and [ADR 0158](0158-mate-changes-records-through-changesets.md)
(Mate proposes, the operator applies) and [ADR 0148](0148-maintenance-schedules-reference-the-profile.md)
(profile jobs). The watch finding, alarm and checklist entry points are a
second change that builds on this record.

## Context

Helmcentral has nowhere to put a problem the operator knows about and has not
fixed. "Shower pump leaks at the outlet" is not a maintenance rule, because a
rule repeats and is never finished. It is not a service log line, because the
log records work that is done. It is not an alarm, because nothing is raised
and nothing should wake anyone. Today such a problem lives in the operator's
head, or in a Mate conversation that scrolls away.

ADR 0138 rejected a kanban board for maintenance because recurring work has
no done column. A one-off problem does have one: it is open until it is fixed
or given up on. That is the difference this record rests on.

The word "job" is already taken. In the maintenance code a profile job
(ADR 0148) is a recurring service an equipment profile supplies, with ids that
start `job:`. The new record is called an open job in the code (`open_job`,
`openJob`, the `open_jobs` table) and Open jobs on screen, so a grep for one
never finds the other.

## Decision

### 1. A one-off record with three states

An open job has a title, an optional item, notes, optional photos, the date it
opened, an optional before-date, and a status of open, done or dropped. It
also keeps where it came from (by hand, Mate, a watch finding, an alarm or a
checklist line) and, where one exists, an in-app link back and a label for it.

- **Dates are the operator's.** The browser sends `?today=` when a job opens
  or is dropped, the way every maintenance write does (ADR 0138's
  2026-09-27 amendment). The server never reads its own clock for a date. The
  age ("12 d") is not stored: each reader works it out from the opened date
  and the date it was given.
- **The link back stays inside the app.** `source_link` must start with one
  `/`, with no scheme, no host and no `//`. A manual job has no link.
  The browser follows the link as a page of Helmcentral, so anything else is
  refused instead of cleaned up.
- **Nothing here is an alarm.** A job raises nothing, is not an alarm rule,
  and is never counted with the alarms. It is not a maintenance rule either:
  it is not in the schedule, has no status such as overdue, and is not in the
  `total` that `list_maintenance` reports.

### 2. Done writes the service log once

Done opens the service log form (date, kind, gauge hours, notes, who, cost,
currency, parts) and, in one transaction, writes one `maintenance_log_entries`
row, links it to the job and closes the job on the date of the work.

- The line is a repair unless the form says improvement or maintenance. Its
  notes read as the job's title when left blank.
- Gauge hours convert to true hours exactly as every log line's do. A job with
  no item has no meter, so hours with no item are refused.
- A refusal anywhere (a bad kind, an unknown part) writes nothing and leaves the
  job open. A second Done is a 409 and writes no second line.
- The job's photos are linked onto the log line too, so the history shows
  them where it always has.

Dropped is one confirm with an optional reason. It writes no log line, because
nothing was done. A closed job stays on record, with its outcome, and the
Open jobs group can show it again ("Show closed").

### 3. Where it lives

The group sits at the top of Inventory → Maintenance, above Overdue, and on
each item's own page. It filters with the Maintenance list's system filter, and
leaves out the jobs of stored items the way the list leaves out their rules. It
is a list, not a board: no assignment, no comments, no priority.

The REST surface is `/api/inventory/open-jobs`, with `done`, `drop` and
`photos` beneath a job. Photos follow the service log's: a table of links to
documents, JPEG or PNG only, appended in order. The shared test for whether a
photo may be deleted now also looks at job photos, so a photo that a job and a
log line both show is not deleted from under either.

### 4. Mate reads them and proposes adding one

- **Reading.** `list_maintenance`, Mate's maintenance read tool and the
  read-only MCP tool of the same name, returns an `open_jobs` list beside the
  rules, oldest first, each with `age_days` and the before-date. It follows the
  tool's item and system filters. A status filter leaves it out, because a job
  has no status of that kind. Mate's `list_records` and `get_record` read the
  type too.
- **Proposing.** `open_job` is a registered record type (ADR 0158) whose only
  action is create. Mate's proposal arrives as a card and Apply makes the job,
  dated by the operator's day. Mate sets the item, title, notes and
  before-date. It does not set the source or a link: a job it adds is marked as
  Mate's.
- **Closing and deleting stay with the operator.** Done writes the service log
  and Dropped ends an entry on the operator's own list, so both are the
  operator's tap, as deleting a rule or a log entry already is.

## Rejected

- **A rule with a one-off flag.** It would put a job with no interval into the
  schedule and its status engine, and the Maintenance list would need a second
  meaning for "overdue". The rule table's whole contract is recurrence.
- **An open status on service log lines.** It would turn the log, which is the
  history, into a to-do list, and every reader of the log (the CSV export,
  Mate's `get_maintenance_log`) would need to know to skip them.
- **A board, assignees or priorities.** The boat has one operator and a short
  list. ADR 0138's reasoning against a board still holds for everything but the
  done column, which a status gives.
- **Raising an alarm for a job.** A known problem is not an event. Alarms
  wake people.

## Consequences

- **A new table, no migration.** `open_jobs` and `open_job_photos` are created
  on first start. Nothing existing changes shape.
- **Upgrading needs nothing from the operator.** The group shows "No open
  jobs." until the first is added.
- **Deleting an item deletes its jobs,** the way it deletes its rules.
  Deleting a log line a job was closed with leaves the job, with no line to
  link to.
- **Where a job came from is kept as text and a path.** If the alarm or report
  it points at is later removed, the link goes to a page that no longer shows
  it. The label still says what it was.
- **Mate cannot edit a job after adding it.** A wrong title is the operator's
  to fix on the list.
- **Nothing leaves the boat** beyond what Mate and the outside assistants
  already read: a job's title and notes go into a chat transcript the way a
  rule's description does, so they carry no secrets by convention.

## Decisions the agreed design left open

- **Drop asks for a confirm.** The design said one tap with an optional reason.
  Drop opens a small box with the reason field and a **Drop job** button, and
  Enter in the field drops it, so the reason has somewhere to go and a stray tap
  cannot close a job with no undo.
- **Done defaults to a repair,** since a known problem fixed is a repair far
  more often than an improvement.
- **A job takes photos only while open.** Done shares them with the log line,
  and the line's own photo strip takes any later ones.
- **No reopen and no delete.** A job entered by mistake is dropped with a reason.
