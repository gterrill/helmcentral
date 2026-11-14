# ADR 0288: Passage Plan Progress, Arrive-By and Change Marks

## Status

Accepted (2026-11-14). Extends [ADR 0286](0286-passage-plan-verdicts-and-suggested-departures.md),
which said "no change marks yet" and left progress to this step. This is step 2b
of the passage plans. Mate's plan tools and bar crossings follow.

## Context

A plan that only shows today's verdicts leaves four questions to the operator's
memory. Has the boat already done this leg? If the run slips a day, what does the
weather say? Did a verdict change since last looked? Is the boat still at the dock
when it should have left?

The operator agreed the answers on 2026-11-14. Each is derived from what
Helmcentral already holds, the recorded passages, the assessment and the plan's
own date rule, and none rewrites the plan.

## Decision

### Arrive-by and slack

Any stop but the first may carry an arrive-by, the latest the operator wants to
be there, as `YYYY-MM-DDTHH:MM` at that stop (`plan_stops.arrive_by`, empty when
none). The plan starts where the boat already is, so the first stop is never
"reached" and a slip never moves its arrival: validation refuses an arrive-by on
it, and the editor hides the field there and sends none for a stop moved to the
front. The editor's field is a `datetime-local` input, so it speaks the same
format. A stop the operator moves keeps its arrive-by, because the arrive-by
belongs to the stop and not to its position.

Slack is the whole calendar days from the stop's arrival to the arrive-by's day.
The time of day is kept for the operator to read and does not change the count.
With several arrive-bys the tightest rules, and a stop the boat has already
reached or passed no longer binds. Slack goes out with the assessment as
`slack: {days, stop, behind_days}`, null when no stop still ahead has an
arrive-by. The page puts a line above the table: "Slack 1 day", or, when the plan
is already planned past it, "Late by 2 days" in red. With no arrive-by there is
no line.

A plan that has fallen behind is projected forward before slack is counted. When
the first leg still to do should have left on a day before today (today read in
the zone of the stop it leaves), the plan is behind by the days between. Every
leg still to do moves that many days later, and slack is the arrive-by's day less
the projected arrival's day, which can be negative. The stored dates do not move.
The line then leads with it: "Behind plan by 4 days. Late by 1 day." Slips are
measured on top of the projected plan, so none of them leaves a leg on a day that
has gone. A plan on time or ahead has a projection of zero days.

### Slip cost

With slack of S days, the plan prices slips of 1 to S+1 days. A slip of N days
moves every leg still to do N days later, by adding N nights at the stop the first
of them leaves, so the plan's own date rule shifts every later date. The shifted
plan is assessed in memory with the same `assessPlan` as the table, and nothing
is saved. The worst leg is named for each N, ranked No-Go, then No data (and "No
forecast yet", which is the same kind of unknown), then Marginal, then Go, the
earliest leg among equals. The slip carries the leg, its shifted day, its verdict,
the limiting factor and, when the slip puts the plan past its arrive-by, how many
days late. The page words it: "Slip 2 days: Mooloolaba → Manly Thu meets wind 18
kn ahead, No-Go (late by 1 day)."

- **Its own endpoint.** `GET /api/plans/{id}/slips` (read tier). Each slip is a
  whole assessment, so the table must not wait for them. The page asks when the
  has slack to spend (an arrive-by ahead, and not yet late), and again on the
  forecast interval, on an edit, and when the passage limits are saved.
- **Reuses the point cache.** A slipped plan reads the same forecast points, so
  the cache of ADR 0286 answers most of the reads.
- **Reads less.** A slip needs verdicts only, so `assessPlan` is asked for
  `VerdictsOnly` with the progress and slack the request already read. It skips
  the warnings, the still-in-port check (which would write the day's first
  suggestion) and the modelled current of legs the operator set a time for. A leg
  with no set time still ranks its departures, since that ranking picks the time
  the verdict is judged at. A plan that is already late, has nothing left to do or
  has unreadable progress is answered before any leg is routed.
- **Bounded.** The forecast points of the legs still to do are read once before
  the slips fan out, three slips are assessed at once, each reading its legs three
  at a time, and at most ten slips are priced (`planSlipMaxDays`, the forecast's
  reach). A longer slack says "Slips are checked as far as the forecast reaches."
- **No wind-change detector.** The slips rest on the verdicts alone.
- **Fails loudly.** Limits not set, or recorded passages that cannot be read
  (without them done legs would be priced), give one `problem` and no slips.
  A plan already late has nothing to price.

### Progress from the recorded passages

A leg is Done when a recorded passage ends within 1.5 nm of the leg's destination
any time after the plan's first day, and after the passage that reached the
previous stop. The passages are taken in time order, and each counts for the
first stop after the last one reached that it ends within 1.5 nm of. The passage's
end is its last track point inside the passage, and the first day starts at
midnight in the zone of the first stop. A boat that leaves a day early, or goes
straight to a later stop, is therefore marked. A stop that no passage ended at,
with a later stop that one did, is Skipped, and so are the legs into it and out
of it that no passage ended at: the boat went by. Stop 0 is never skipped.

- **Recorded passages only.** The operator marks nothing by hand, and the plan is
  never rewritten. Hidden passages are not read, as everywhere else.
- **Done and Skipped legs** carry `progress` in the assessment and nothing else:
  no verdict, no note, no departure, no suggestion, no current, and they are left
  out of the slips. Progress is read first and a leg that has it is not assessed. The table shows them dimmed and says Done or Skipped, and the lay
  days of a skipped stop say Skipped.
- **When the record cannot be read** (no passage store, a read error), the
  assessment says so once in `progress_error` and marks no leg. It does not treat
  the failure as "nothing sailed". A passage with no track point near its end
  cannot be placed and is left out, with a log line.
- **Imported passages** are not told apart from recorded ones. The first-day rule
  keeps an import from before the plan from marking a leg.

### Still in port

On a leg's departure day, the assessment flags `still_in_port` when it is an hour
past the leg's departure and the boat's own fix is within 1.5 nm of the stop
the leg leaves. The row then reads "Planned to leave today — still at Manly", with
**Slip a day**, which is the existing +1 night on that stop. Dates never move on
their own.

- **The departure** is the operator's time when set. A suggested departure is
  always for a time from now on, so by the time the boat is late the suggestion
  has moved. The box therefore keeps the first suggestion the assessment made for
  the leg that day (`plan_first_suggestions`, in the documents database, by plan,
  leg key and the stop's local date) and counts the hour from it. A restart keeps
  it. A request writes only when the leg has no row for the day, and then prunes
  the days older than two, at most once for each day. A plan's rows go with the
  plan, and the slips never write it. If the row cannot be kept, there is no
  prompt, and the log says why. A day whose window has closed with the boat still
  at the stop counts the hour from the day's first suggestion when one was kept,
  read without writing; with none kept, the closed window alone counts as past
  the hour.
- **The position** is the boat's trusted fix (`boatOwnPosition`). A critical GNSS
  alert and the -1, -1 pair are refused as everywhere else. With no position there
  is no prompt: an unknown fix is not "in port". It is read only when some leg
  leaves today and is past its hour.

### Change marks

Each plan keeps, per leg, the verdict level the operator last looked at
(`plan_verdicts_seen`, in the documents database on the box, so every device
agrees). Only the level counts: Go, Marginal, No-Go or No data. A moved limiting
factor under the same verdict is no change.

- **Leg identity** is the from-stop name, the to-stop name and which trip between
  that pair it is, counting from 0 (`key` on each assessed leg). Each name is
  escaped, so a name holding the separator cannot make two legs share a key. The
  date is not part of it: a leg slipped to another day keeps its baseline. The
  server owns the key, so the page never builds one. An edit that renames a stop
  gives the leg a new key and no mark, which is right: it is not the leg that was
  looked at. A key the plan no
  longer has is gone when the next snapshot is saved, and a plan's snapshot
  goes with the plan. A current leg that has no verdict in a save keeps the one it
  had, so a leg that briefly says nothing is still compared with its last verdict
  when it speaks again.
- **The visit.** The page reads `GET /api/plans/{id}/seen` once when the plan
  opens and never again that visit, and marks each leg whose level differs from it
  ("was Go, now Marginal"), in the colour of what it landed on: Go emerald, a fall
  to No-Go red, a fall to Marginal amber, anything else neutral. The first time the plan has a verdict on the page it
  saves what is on screen as the snapshot, `PUT /api/plans/{id}/seen`
  (write tier), in one transaction and only if it differs from the saved one. It
  saves once per visit. A viewer who cannot write sees the marks and saves nothing.
- **When it fails.** A snapshot that cannot be read is said once above the table
  and the page does not overwrite it.

## What it does not know

- **Where a passage started.** Skipped is inferred from which stops passages
  ended at. A passage from the first stop straight to the third marks the second
  Skipped, and a boat that went to the third by an unlogged route reads the same.
- **A leg slipped by hand.** The operator adding nights changes the dates. The
  progress rule does not read the leg's date, so it is unaffected, and the leg
  keeps its change-mark baseline. Nothing remembers the old plan.
- **A prompt for a suggested leg the box never saw suggested.** With no first
  suggestion kept there is no reference, so the prompt waits for one.

## Rejected

- **Slack in hours.** The arrive-by's time of day depends on an ETA that the
  plan's day-level date rule does not carry, and "a day" is how the operator
  slips.
- **Pricing slips inside the assessment.** It would make every table load wait
  for up to eleven more assessments.
- **A mark for any change of figure.** Limiting factors move with every forecast
  refresh. Only a change of level is news.
- **Keying marks by leg position.** Inserting a stop would hand every later leg
  the previous leg's verdict.
- **Writing the snapshot on every refresh.** The marks would vanish at the next
  refresh, and the next visit would compare against the last refresh, not the last
  visit.
- **Reading the leg's suggested departure fresh for the late check.** It moves
  forward with the clock, so the boat would never be an hour past it.
- **Marking legs done by hand.** The recorded passages already say.

## Consequences

- The assessment gains `key`, `progress` and `still_in_port` on each leg, and
  `slack` and `progress_error` on the plan. It reads the passage record once per
  request, and the boat's position only on a day a leg is late.
- A plan with an arrive-by costs up to eleven more assessments on the server per
  refresh, behind their own endpoint, three at a time.
- `plan_stops` gains a column and the documents database two tables. Both are
  created when absent, so nothing needs migrating.
