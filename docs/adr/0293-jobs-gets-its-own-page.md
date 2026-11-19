# ADR 0293: Jobs Gets Its Own Page

## Status

Accepted (2026-11-19). Amends [ADR 0269](0269-open-jobs-hold-known-problems-until-they-are-fixed.md)
§3 (where open jobs live) and renames the section [ADR 0138](0138-maintenance-rules-list-and-service-log.md)
built, as [ADR 0123](0123-inventory-begins-with-the-equipment-registry.md)'s
2026-11-19 amendment left it in the Inventory sub-menu.

## Context

ADR 0269 put the open jobs at the top of Inventory → Maintenance. A new
operator never finds them there. Nothing in the sidebar says "what is broken
on the boat", and a section called Maintenance suggests a schedule, so the
person who has just noticed a leaking pump looks somewhere else, or nowhere.

The question the operator asks is "what needs doing?". Whether the answer is
a rule that has come due or a problem somebody wrote down is the code's
distinction, and the screen was making them carry it.

## Decision

### 1. A top-level Jobs page

`/jobs` is a panel of its own, labelled Jobs, in the sidebar after Documents
and before Inventory. It has one view and nothing to remember, like Alarms.
It holds two lists:

- **Open jobs,** the group ADR 0269 built, unchanged: Done, Drop, Show closed,
  the age and the before-date. The page's own **Add job** button starts a new
  one, so the group draws no second button of its own on this page. On an
  item's page the group still draws its own.
- **Services due,** the rules whose status is overdue or due soon, overdue
  first. An acknowledged rule stays in the list with its Ack mark, below the
  unacknowledged ones. The rows are the Service schedule's rows, one
  component with one set of dialogs behind **Complete**, **Ack**, **Set last
  done** and **Log a reading**, so a rule reads and behaves the same in both
  places. The equipment labels keep the same-name disambiguation, worked out
  over every rule, not only the ones shown.

A link, Service schedule, goes to the full list. The system filter stays on
the schedule: the Jobs list is short enough that it would only hide work.

With nothing open and nothing due the page says so, with **Add job** under it.
A read that failed is never taken for an empty list.

### 2. The count on the sidebar row

The Jobs row shows the open jobs plus the services that are overdue or due
soon, and nothing at all at zero.

- **Acknowledged services are left out.** Ack means the operator has decided it
  can wait, so counting it would put a number on the row that the operator has
  already answered. The page still lists them.
- **The server counts.** `GET /api/inventory/jobs/count` answers
  `{"count": N, "incomplete": bool}`. It runs the same reads as the two lists
  (the open jobs, and the rules on the boat's day through the function behind
  the rules list), so there is one implementation of what is due. It leaves out
  stored items, as the lists do, and acknowledged rules. No tab downloads the
  lists to count them. An earlier version summed the two lists in the browser
  and twice reported a wrong number when one read was stale or failed.
- **The row stays quiet.** The hook reads only that endpoint, through a
  selector that returns one small value, so the shell, which re-renders on every
  telemetry delta, never re-renders the badge. It re-reads every five minutes
  while the tab is visible, when the window regains focus, after any open-job or
  service write, and when the boat's day arrives or turns. The server works out
  the boat's day, as it does for the lists; a `today` from the client is ignored.
- **Incomplete says so.** `incomplete` is true when any item's schedule could
  not be read, meaning its profile is missing or invalid, so its services were
  never judged. The row then shows an amber "!" in place of the number, with the
  label "Jobs list incomplete: an item's schedule couldn't be read". A number
  would be a floor presented as the answer. The Jobs page's alert names the item.
- **A failure says nothing.** If the count has not answered or could not be read,
  the badge is hidden, and the server answers an error, never a 0. A user who
  cannot read maintenance sees no count, and a read-only or kiosk user who can
  read it sees the same one as anybody.

### 3. Maintenance is called Service schedule

On every screen the operator reads, the section is Service schedule: the
Inventory sub-menu, the collapsed menu, the page heading, the breadcrumb and
the help link labels. The section id stays `maintenance` and the address stays
`/inventory/maintenance`, so saved links, tags and Mate's earlier replies
still open it. The Open jobs group leaves that page.

### 4. Links into jobs

Wherever Helmcentral sends the operator to an open job, it now goes to `/jobs`:
the card Mate shows after adding one, the link on each open job in
`list_maintenance`, and the way Mate's prompt tells it to cite one. Mate
still links a rule or a log entry to `/inventory/maintenance`. A job's own
`source_link` is untouched: it points back at where the job came from, not at
the list.

## Rejected

- **A Jobs tab inside Maintenance.** It would be one more place to look inside
  the place nobody looked.
- **Putting every rule on the page.** Most rules are fine most of the time, and
  a list of fifty rows with two that matter is the Service schedule again.
- **Counting acknowledged services.** It would leave a number on the row that
  Ack was meant to clear.
- **Renaming the id and the address with the label.** It breaks every saved
  link for a name change nobody reads in the address bar.
- **Summing the two lists in the browser.** It put both lists in every tab and
  gave a wrong number whenever one read lagged or failed.
- **A second Add job button in the group.** Two buttons that do one thing, a
  few centimetres apart.

## Consequences

- **Nothing is rewritten or moved in storage.** No operator action on upgrade.
- **The badge costs one small read every five minutes,** answered by the
  server. The count and the lists agree because they share their code.
- **A rule's row exists once in the frontend,** in `maintenance-rules-table.tsx`.
  A change to it shows on both pages.
- **The Maintenance item-page block keeps its name.** It holds that item's rules
  and service history, which is what it is.
