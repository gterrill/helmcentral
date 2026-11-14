# ADR 0285: Passage Plans Replace the Planned Stay

## Status

Accepted (2026-11-13). Supersedes [ADR 0208](0208-forecast-planned-stay.md).
This is the first of four steps: the store, the editor and the table with the
forecast columns. The verdict, suggested departures, change marks and progress
tracking, Mate's plan tools, and bar crossings follow. The verdict, ETA,
suggested departures and Current column are [ADR 0286](0286-passage-plan-verdicts-and-suggested-departures.md).

## Context

ADR 0208 let the operator book one place and its dates. The next cruise is not
one place. It is Great Sandy Straits Marina for two lay days, then Pelican Bay,
Mooloolaba, Manly and The Boat Works, with a day's sail between each pair. The
operator wants every day of that run in one table: the distance, the wind
against the course, the gusts and the waves, and the same table next week for a
different run. One stay, no legs and no list cannot hold that.

The pieces exist. The forecast endpoints take a position. The water router
(ADR 0216) gives a routed distance between two points. The place search finds
the stops.

## Decision

A **plan** is a name and an ordered list of stops. Several plans can exist, each
starting at its own first stop. A plan is not linked to a season, and nothing
chains one plan to the next.

### What a stop holds

- **Place and nights.** Name, latitude, longitude, the arrival day (`YYYY-MM-DD`
  at the place) and the nights stayed.
- **The leg leaving it.** An ordered list of via places and an optional
  departure time (`HH:MM` at the stop). The last stop has neither. The router is
  point to point, so a via is how the operator steers a leg round a bar or
  through a channel the shortest path would not use.

### One rule for dates

Only the first arrival is stored as the operator's. Every later arrival is the
stop before's arrival plus its nights, and the server recomputes it on every
write. So changing a night count or the first date moves every later date, and
the page cannot save dates that disagree. `PATCH /api/plans/{id}/stops/{index}`
takes `arrive` or `nights`. A later stop's `arrive` becomes the nights of the
stop before it, and a date before that stop's own arrival is refused. The Add
a night control is this call with `nights` plus one.

A leg is always one day: the day the boat leaves a stop is the day it arrives at
the next. A passage over midnight is not modelled yet.

### Legs

`GET /api/plans/{id}/legs` routes each leg through its via places in order and
sums the segments. A segment the router cannot route fails that leg, with the
router's own words, and returns no distance. It never falls back to a straight
line, and one failed leg does not hide the others. The leg's course is the
bearing of its longest segment, the rule `route_distance` already uses, and the
table reads the wind against it as ahead, abeam or astern. Successful segments
are cached in memory by their end points; failures are not.

### The table

The Forecast page shows the current plan: the one whose dates span today at its
first stop, otherwise the next upcoming. The server marks the status of every
plan the same way and names the current one. A plan whose last day has passed
sits under a Past heading until the operator deletes it. Nothing deletes a plan
on its own.

The table has one row per day. A passage row shows the leg, its distance, the
operator's departure time if set, and the day's wind range with its direction
against the course, the gusts and the waves. A lay day shows the overnight wind
and the wind's direction, with no shelter label. The wind and waves for a
passage are read at the middle of the routed leg, rounded to two decimals so
neighbouring rows share a request. A day past the last forecast day says "No
forecast yet" and shows nothing in its place. A row opens the existing forecast
drawer for its place, as the stay view did. On a phone each row is a card.

### Dropping the old stay

The `planned_stay` table is dropped on start. There is one operator and no
migration: a saved stay is entered again as a one-stop plan. `/api/planned-stay`
and the `planned_stay` record type are gone, so Mate can no longer propose a
stay. `get_planned_stay` stays, because the vessel status and the watch report
read it. It now returns the stop of the current plan the boat is at or sailing
from today, the first stop of an upcoming plan, or the last stop of the plan
that ended most recently (which reads as ended). Mate's plan tools replaced it
in [ADR 0289](0289-mate-reads-and-proposes-passage-plans.md).

## What it does not know yet

- **Current and lightning.** The table has no Current or Lightning column. The
  lightning feed is worked out for the boat's cell only (ADR 0284), and modelled
  current has a Mate tool but no page endpoint. Both need a place-keyed feed
  first.
- **Arrival times and verdicts.** The ETA is blank. There is no Go or No-Go
  column, no suggested departure and no mark when a forecast changes. The grid
  leaves room for the verdict.
- **Progress.** A leg is not marked done when a passage is recorded.
- **Bars.** A bar crossing is not checked.

## Rejected

- **Dates stored per stop and edited freely.** Two stops could disagree about a
  night, and the page would have to decide which one was right.
- **Overnight legs.** They need an arrival date that differs from the departure
  date, and the row model above is one row per day.
- **A straight line when routing fails.** The same refusal as ADR 0216: a
  distance the operator reads as real must have been measured over water.
- **Migrating the old stay.** One operator re-enters it in a minute.
- **A new tile on the dashboard.** The table is a page, and the kiosk does not
  show it.

## Consequences

- The Forecast page loads each row's forecast through the same place-keyed
  queries the stay view used, so a long plan makes one request per distinct
  rounded place, not per row.
- `lib/stay.ts` keeps only the date helpers the drawer, the tide section and the
  lightning bands share.
- A plan with many stops routes on every open of its legs; the in-memory cache
  makes the second open cheap.
