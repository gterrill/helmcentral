# ADR 0289: Mate Reads and Proposes Passage Plans

## Status

Accepted (2026-11-14). Keeps the promise in [ADR 0285](0285-passage-plans-replace-the-planned-stay.md)
that Mate's plan tools would follow the table. It builds on the verdicts of
[ADR 0286](0286-passage-plan-verdicts-and-suggested-departures.md) and the
progress and slack of [ADR 0288](0288-passage-plan-progress-arrive-by-and-change-marks.md).
This is step 3a of the passage plans, and it takes pasted text only. Reading a
photo of a plan is step 3b.

## Context

After ADR 0288 the Forecast page judged every leg of a plan, and Mate knew none
of it. `get_planned_stay` returned one stop and two dates, and the prompt told
Mate it could not create or change a plan. Two things followed. Asked "can we
leave Thursday?", Mate ran `assess_passage_window` and judged the leg a second
time, and the two answers could differ. And an operator with a day-by-day plan
in an email had to key it in stop by stop.

## What Mate reads

`get_plans` replaces `get_planned_stay`. The old tool is gone, with its rule and
its place in the MCP list. There is no alias, because there is one operator and
no installed base.

- **With no arguments** it lists every plan in brief (id, name, first and last
  date, status) and gives the current plan in full, the plan the Forecast page
  opens on.
- **With an id or a name** it gives that plan in full. A name that matches no
  plan, or two, is an error that lists the plans there are.
- **A plan in full** has its stops (position, arrival, nights, departure, the
  departure time and arrive-by the operator set, vias), the routed legs, and what
  the table shows beside each leg: verdict, limiting figure, departure and
  arrival, modelled current, Done or Skipped, the still-in-port mark, the slack,
  the official warnings on the nights, and what a slip of each day would meet.
  It carries the plan's version, for `base_version` on a proposal.
- **Times** are finished boat-time labels, "Sat 14 Nov 8.00 AM", in the zone of
  the stop each is at. Days carry a label too ("Mon 16 Nov"), so Mate never works
  out a weekday.

The tool judges nothing. It calls `planLegs`, `assessPlan` and `slipsFor`, the
functions the assessment and slips endpoints call, on one pinned clock. A figure
that cannot be had is left out with its reason, as on the page.

## One verdict

The tool's description and the prompt say the same thing: for a leg of a plan,
quote the plan's own verdict and its limiting figure, and use
`assess_passage_window` only for a date or time that is not in a plan.

The passage guard follows. A `get_plans` result with a Go, Marginal or No-Go on at
least one leg counts as the passage check, for both the limits trigger and the
relative-wind trigger. A leg that has only a note, a list with no plan in full
and a failed call do not. `get_plans` also joins the calls that bring in another
place's depths, in place of `get_planned_stay`.

## What Mate proposes

`plan` is a record type in the changeset registry (ADR 0158). It allows create
and update. A delete is refused, as it is for the other things the operator
removes with their own tap.

- **Fields.** `name`, and `stops`, a list of objects: name, lat, lon, nights,
  vias, depart_time, arrive_by and, for the card, source. Only the first stop has
  an arrival. The rest follow from it and the nights, as they do on the page.
  `first_date` and `last_date` are read-only.
- **The same rule as the page.** A create and an update call `normalisePlan` and
  `checkPlanZones`, then `createPlanTx` or `updatePlanTx`, the transaction forms
  of the store writes the page's handlers call. A stop with no lat or lon is
  refused, and so is a field that is not a stop's, so a misspelt `nights` fails
  and names itself.
- **Strict about nights and dates.** Every stop must give `nights`, 0 for a stop
  passed through the same day, because a default of 0 would silently drop a
  night Mate meant to keep. A later stop's `arrive`, or any stop's `depart`, may
  be sent back as `get_plans` shows it, but it must equal the day the first
  arrival and the nights give. A date that disagrees is refused, and the error
  names the stop, the date Mate sent and the date the nights give. The page would
  otherwise overwrite the date and the card would describe a plan the operator
  never gets.
- **An update replaces the plan whole.** The stops given are the stops kept, so a
  stop left out is removed and a field left out is cleared. The card shows that
  plainly. An update that leaves the plan as it is, is refused.
- **Freshness.** The plan's version is its `updated_at`, like the other types. A
  card drafted against one version goes stale if the plan is saved again before
  the operator taps Apply.
- **Where a position came from.** `source` is "saved place" or "place names". It
  rides on the proposal for the card and is not stored, because the plan has no
  use for it once the operator has seen it. A new `Show` hook on the record type
  lets the card's rows carry it.

### What the card says

- **A new plan** is one row per stop: the day, the nights, the place, the source
  and the position. There is no map.
- **An update** is one sentence worked out on the server from the plan as it
  stood and as the dry run leaves it, so the same change always reads the same:
  "Manly: 1 night → 2 nights. Later stops move a day: The Boat Works arrives Wed
  18 Nov." It names the stops that came or went, what changed at each stop that
  stayed, and how the dates after the change moved. It says nothing of a stop
  that is as it was.
- **After Apply** the card offers **Open plan**. It links to `/forecast?plan=<id>`,
  and the Forecast page opens on that plan. The address carries a plan id and
  nothing else. An id that is not a whole number above zero, or a plan since
  deleted, opens the current plan.
- **Open plan is followed once.** The page selects the plan when it opens, or
  when the link is followed while the page is already open, and then the app
  drops `?plan` from the address by replacing the history entry. Following the
  link first closes any open plan editor and any delete prompt, because Save in
  an editor left open would write one plan's stops over another's. Back and
  forward never select a plan, since no entry keeps the `?plan`.

## What the plan read carries

`get_plans` carries the wind changes ADR 0287 put on the table: each leg's
changes with their boat-clock labels and the reason when they were not checked,
and the changes for each lay-day night. Mate quotes them as given and does not
read the forecast again to find them. A result too big to send drops the nights'
changes before the warning text, and the note says so.

## Places

Mate looks up every stop with `find_places`, saved places first, then the
place-names provider, the same path as the place search on the page. A stop with
no match or a tie goes back to the operator before anything is drafted. Mate
never guesses a position. The backend backs that up: lat and lon must be given,
not defaulted to 0, 0.

## Reading a day-by-day plan

The prompt tells Mate how to read pasted text.

- A lay day row is a night at that stop, not a stop.
- The year comes from today's date.
- A via is kept only when it matches a place. Otherwise it is left out and the
  reply says so.
- A bar crossing stays in the reply as a note, because a plan does not model it
  yet.
- The name is "To <last stop>" unless the operator gives one.
- A first date already past is kept as given, and Mate says the plan started in
  the past.

## Only when asked

Mate proposes a plan change only when the operator asks to change or create a
plan. A No-Go leg is answered, not re-planned. The prompt says so, and nothing in
the tool offers a plan unprompted.

## Outside assistants and reviews

`get_plans` is read-only and on by default for outside assistants and for alarm
and watch reviews. `propose_changes` stays out of reach of both, so no outside
assistant can make or change a plan.

A token made before this change keeps the list it was given, and that list names
`get_planned_stay`, which is gone. The operator switches **Passage plans** on for
such a token in **Settings → Outside assistants**. The release notes say so.

## What leaves the boat

An outside assistant a token has switched on to read plans sees the stops'
names and positions, the dates and the verdicts. They leave the boat only to the
assistant the operator connected, as the rest of the tool list does.

## What changed beside it

- **The vessel status.** Its `planned_stay` field is `plan_next_stop`, and the
  words around it say "the plan's next stop". The watch report reads the same
  field. The logic is unchanged: the stop the boat spends tonight at, the first
  stop of an upcoming plan, or the last stop of the plan that ended most recently,
  which reads as ended.
- **Old cards.** A stored `planned_stay` proposal already goes stale with a
  reason, because the type is gone. Nothing else needed a change.

## Rejected

- **Keeping `get_planned_stay` beside `get_plans`.** Two tools for one fact is how
  a model quotes the older one.
- **A verdict of Mate's own.** The tool would have to match the page to the
  hour, and it would drift.
- **Stop-level edit operations.** "Add a night at Manly" is a smaller card, but
  it needs its own freshness rule per stop and a second path to the plan. One
  whole-plan replace shares every rule with the page, and the card's sentence
  keeps it readable.
- **Storing the source of a position.** It describes how Mate found the stop, not
  the plan.
- **Deleting a plan by changeset.** A delete cannot be seen on a card the way a
  stop can, and a plan's slips, marks and kept suggestions go with it.

## What it does not do yet

- **Photos.** Reading a plan from a picture is step 3b.
- **Bars.** A bar crossing is a note in the reply and is not in the plan.
- **Vias it cannot place.** Mate leaves them out and says so. It does not invent
  a position.
