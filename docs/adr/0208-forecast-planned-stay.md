# ADR 0208: The Forecast Page Can Show a Planned Stay

## Status

Accepted (2026-10-24)

## Context

The forecast page answers one question: what is the weather where the boat is.
Every forecast endpoint reads the boat's position from SignalK on each request
and takes no position of its own. That is the wrong question the week before a
booking. The operator is berthed at one marina and has a place for next Tuesday
at another, five days, and wants the weather there for those days, on the page
they already read, without moving the boat's own forecast anywhere.

The pieces mostly exist. The providers already take a latitude and longitude and
cache by position. Local-day boundaries come from the longitude
(`vesselLocalLocation`, ADR 0035), so they are right for any place. Mate's
`find_places` resolves a name to a position through the configured place-names
plugin (ADR 0101), and `get_wind_forecast`, `get_tides` and
`assess_passage_window` already take an arbitrary position. What is missing is a
way to name the place and dates, a way to ask the page's endpoints about it, and
a page that says whose weather it is showing.

The dashboard shares the page's queries. `useWeatherForecast` and
`useWaveForecast` in `App.tsx` feed Current Conditions, Forecast Conditions, the
clock's sunrise and sunset, Auto theme and the Rode Planner. Pointing
them at a marina would put another place's sunset on the boat's clock.

## Decision

1. **A planned stay, not a location override.** One place with an arrival and a
   departure date; arrival defaults to today. There is one stay at a time and no
   saved list. A booking is the thing being planned, and the dates are what let
   the page say which days it covers.
2. **It is kept on the server**, in `helmcentral.sqlite` (ADR 0141), so every
   device and the wall see it. One row: name, latitude, longitude, arrive and
   depart as local `YYYY-MM-DD` at the place. It is treated as absent the day
   after departure, judged by the date at the place (its longitude's time zone,
   worked out on the server), not the browser's or the server's. The row is not swept: it is replaced or cleared by
   the operator and ignored once it has ended. `PUT` checks the dates and
   `depart >= arrive`; `GET`, `PUT` and `DELETE` are on `/api/planned-stay`.
3. **The forecast page opens on the boat.** A chip, "Coral Coast Marina ·
   27-31 Oct", switches to the stay and a Boat button returns, one tap each way.
   The header always names which place is on screen: "Forecast for Coral Coast
   Marina - not the boat's position".
4. **The place is found by name**, with the same place-names search Mate uses,
   centred on the boat, each candidate showing its distance and bearing.
   `GET /api/place-search?q=` wraps `find_places`, so the operator and Mate
   find the same places. Dropping a pin is deferred.
5. **The forecast endpoints take an optional `lat` and `lon`**: `weather-today`,
   `weather-forecast`, `wave-forecast`, `upper-air`, `forecast-warnings` and
   `tide-nearest`. Absent, they behave exactly as before. Half a pair, a value
   that is not a number or one off the globe is a 400. With a position, SignalK
   is not consulted; "now" is the server clock and the day boundaries and time
   zone come from the given longitude.
6. **The day list is the whole ten days with the stay days marked.** Stay days
   beyond the horizon are placeholder rows: "No forecast yet · from 3 Nov",
   where the date is when that day enters the ten-day window. Nothing is
   estimated for them: no climatology, no stretching the last day.
7. **The upper-air trace shades the stay window**, and the caption calls it the
   upper-air trend over those days, not a forecast for each day. A stay beyond
   the trace says so.
8. **Tide uses the station nearest the place, for that view only.** It names the
   station and its distance from the place and flags a station more than 20 NM
   away. It reads the boat's tide provider and writes nothing: `ui.tide_station_id`
   and `tide_auto_station` are untouched.
9. **Forecast warnings show the place's zone**, worded as the place's ("Wind
   warning in effect for Coral Coast Marina (QLD Gold Coast)").
10. **Waves with no values at the point say so.** A named place the model has no
    values for (the plugin returns the point's hourly series with every wave value
    null) answers 200 with no days, and the page shows "No wave forecast for this
    spot". It never moves the point to the nearest sea cell. An empty provider
    response, or one with no days from today, stays a 502, as for the boat. A provider failure is still a failure,
    shown as one.
11. **Times are the place's local time**, and a note beside the chip says so when
    its clock differs from the boat's. The tide chart reads its times in the
    place's zone too.
12. **Own-ship stays own-ship.** The stay's feeds are separate queries, keyed by
    position and held back until the stay is showing. The alarms (forecast
    warnings, ADR 0087, heavy weather, weather trend), the dashboard tiles, the
    clock's sunrise and sunset, Auto theme and the Rode Planner keep reading the
    boat. The stay never appears on the dashboard.
13. **Mate reads the saved stay and can propose one; the operator applies it.**
    `get_planned_stay` returns the stay, or none when it has ended. The stay is
    also a registered record type, `planned_stay`, with the one id `stay`, so a
    proposal goes through `propose_changes` and the Apply card (ADR 0158), the
    same shape as the vessel details (ADR 0165). The first update gives every
    field; later ones change only what they name; a delete clears it. A prompt
    line tells Mate to resolve the place with `find_places`, read the stay
    before answering about "the marina", and never say it is set until the
    operator applies the card. Applying the card refreshes any open page.
14. **No notification when the stay becomes fully forecast.** Revisit if the
    operator finds themselves checking every day.

## Rejected

- **A location override on the forecast page.** It would say where, not for
  which days, so the page could not mark the stay days or say which fall beyond
  the forecast.
- **A saved list of stays.** One booking at a time is the use. A list needs
  management screens for a feature whose point is a glance.
- **Nudging a wave point to the nearest sea cell.** A marina up a river would
  show the open water's sea state as its own. A reading from somewhere else
  under this place's name is worse than none.
- **Filling days beyond the forecast from climatology, or repeating the last
  day.** The operator would read a guess as a forecast. The placeholder row says
  when the real one arrives.
- **Repointing the dashboard queries at the stay.** They drive the clock, the
  theme and the Rode Planner, which are about where the boat is.
- **Notifying when the stay is fully forecast.** Another message to be ignored
  until it is wanted.
- **Mate's fill-form pattern for the proposal** (ADR 0165's PDF tools). That
  builds a draft document. The stay is a record, so it takes the record
  changeset the vessel details already use.

## Consequences

- A forecast request for a place is cached by position like any other, so
  opening the stay view costs one set of provider calls and repeats are served
  from cache.
- The stay row is in the record store's database file, so it is in the
  existing backup and needs no migration.
- A stay whose place or dates are edited is replaced whole; there is no history.

## Related

- [ADR 0035](0035-weather-local-day-boundaries.md): day boundaries from the longitude.
- [ADR 0101](0101-place-names-come-from-a-plugin.md): the place-names search behind `find_places`.
- [ADR 0141](0141-one-business-database.md): one SQLite file.
- [ADR 0158](0158-mate-changes-records-through-changesets.md): Mate proposes, the operator applies.
- [ADR 0165](0165-mate-fills-in-forms.md): the vessel details record this one follows.

Place search needs the boat's position: the place-names plugin searches a box
around a centre and cannot search without one. With no position the sheet is
told "Boat position unknown - place search needs it" (a 503) rather than shown
an empty list.
