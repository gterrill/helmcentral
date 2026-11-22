# ADR 0299: Recorded Passages Keep The Sea State Behind Each Spell

## Status

Accepted (2026-11-22). Builds on
[ADR 0298](0298-the-logbook-says-how-a-passage-felt-in-words-learned-from-the-operators-ratings.md),
which cuts a passage into 15-minute spells and stores them with the stats,
[ADR 0296](0296-passage-motion-is-backfilled-from-influxdb-history.md), whose
one-line start-up summary this job copies,
[ADR 0278](0278-mate-reads-the-stream-from-the-open-meteo-current-model.md), which
shows the optional plugin export this one follows, and
[ADR 0232](0232-gfs-wave-land-cells-are-no-data-and-the-blend-is-opt-in.md), which
says a land-masked GFS-Wave cell is no data and not a calm sea.

## Context

The comfort model that comes later is fitted per boat, by least squares, on
recorded spells against the sea each spell had. That sea is a hindcast: what the
model says the swell and wind wave were at that hour and place. If the fit asked
Open-Meteo for it each time, a refit would depend on an archive Helmcentral does
not control, and on a model version that can change between two fits. So the sea
is fetched once and kept, and the fit reads what was kept.

This ADR builds no model and no comfort word. It stores the sea behind every
recorded spell and shows it in the logbook, so the operator can check the data
before anything is fitted to it.

## Decision

### 1. The wave plugin gains an optional export

`fetch_wave_hindcast({points, start, end})` takes a list of positions and the
first and last hour wanted for all of them (RFC3339, UTC, both inclusive). It calls
Open-Meteo's `/v1/marine` once, with the positions as comma-separated `latitude`
and `longitude` lists, `start_hour` and `end_hour` in GMT, and the same variables
and GFS-Wave pin as `fetch_waves`. A moving boat crosses a new 0.01 degree cell
about every half hour, so one position per call would be one request an hour of
passage. It answers with one entry per position, in order, in `fetch_waves` shape
(swell and wind wave height, period and direction). Each entry names the model that
answered, as an id and as a label the plugin words for the operator, and an hour
the model has no value for carries a `missing_reason`, also the plugin's wording.
The host stores the model, the label and the reasons as given, and words none of
them. The model is required, because a fit compares the hindcast's model with the
forecast's.

The plugin honours the same `use_blend_fallback` setting as the forecast, so one
setting decides both. Where it applies, only the positions over land are asked
again, together, in one more request. One call carries at most 31 days and at most
100 positions. Open-Meteo documents comma-separated coordinates without a limit;
100 keeps the request URL near 2 KB, as for `fetch_currents`. The host asks for
more positions in several calls over the same window.

The host calls it through `callOptionalExport`, as `fetch_currents` does. A plugin
without the export answers with an error that says to install the updated plugin,
by the id the host already has for it. The host checks that up front, once, before
asking for anything. It never answers a hindcast from the forecast: a forecast
hour is a prediction, and the model would then be fitted to its own predictions.

### 2. The sea is stored by hour and position, not by passage

A new table, `sea_hindcast_hours`, is created with `CREATE TABLE IF NOT EXISTS`.
No existing table changes. The key is the UTC hour (`hour_ms`) and the position
rounded to hundredths of a degree, stored as whole hundredths (`lat_c`, `lon_c`),
the precision of `passagePointKey`. A row holds the provider, the model, the fetch
time, the swell and wind-wave height, period and direction (each NULL when the
model has none), and a `missing_reason`.

Keying by hour and position means a passage that is split, merged or trimmed never
orphans a row, and two passages that sailed the same water in the same hour share
one. The job asks at the cell's centre, not at whichever spell asked first, so the
stored value depends on the key alone.

A wave system with no period is no system, as in the forecast. Open-Meteo reports
a flat component as three zeros, so a NULL swell on a row with no reason means the
model gave no swell. A reason means the model gave nothing for the hour.

### 3. Spells carry what the hindcast and the fit need

A stored spell is two parts. Its core is what a page shows: the start, the end and
the sway. Its fit features are the position nearest its middle, the mean heading (a
vector mean, so 350 and 10 degrees average to north, and a mean that cancels out
has none), roll RMS and pitch RMS apart, the peak roll acceleration, and the peak
vertical acceleration in g. The last two are present only where 10 Hz data exists,
since the points hold none below 8 Hz. Sideways g is not stored: `sidewaysGAt`
works it out from the helm height whenever it is read, as ADR 0297 does for the
passage's peak, with the same formula. Nothing reads it per spell yet.

The stats version moves from 4 to 5, so the start-up pass of ADR 0298 works every
older row out again. The features are stored with the stats and stay off the wire:
the list never carried spells, and the passage page's spells are the core, the
band and the sea, with no field copied one by one.

### 4. One hourly sweep fills the hours

A spell's hour is its middle rounded to the nearest hour, not truncated. Open-Meteo's
values are instantaneous at the hour, so a spell whose middle is at 10:47 is the
11:00 value. The age cutoff below applies to that rounded hour.

`runSeaHindcast` is one pass. It finds every spell hour at least 12 hours old that
has no row. The wait is there because the model's values for an hour keep changing
for about half a day while forecast-lead runs are replaced, and a stored row is
never refetched. A passage's sea therefore shows about 12 hours after the hour it
covers. The first pass covers every recorded passage back to 2026-10-11. It plans each passage on its
own: the passage's wanted hours, cut into windows of up to seven days, and one
request for each window, covering the cells that passage needs over that
passage's own hour range (an hour two passages share goes to the first). One
window across every passage would let a refusal for the oldest hours settle newer
hours that are still in the archive, and would ask every cell for every hour of
the window. It matches each hour to the series of its own cell and stores it at
the same 0.01 degree key as before. Cells beyond one call's 100 go in further
calls over the same window. Planning the windows, fetching one, and reporting are
separate steps, and the counts come from what each hour came to, not from
adjusting totals afterwards.

A sweep paces itself. It asks about at most 300 positions (`seaHindcastLocationBudget`;
each position in a request counts), oldest passage first, then stops and logs that
it reached its budget. The first sweep after an upgrade has weeks of passages to
fill, and asking for all of it at once would use up Open-Meteo's per-minute limit
and rate-limit the forecast from the same address. A large backfill therefore
spreads over several hourly sweeps, each asking only for what is still missing.
The sweep also stops at the first request Open-Meteo rate limits, logs it, and
leaves the next sweep to resume.

It reads each passage's spells through the same function a page uses, which works
the figures out when they are missing or old, so it never depends on the start-up
pass having run first and never skips a passage for being stale. A passage whose
figures cannot be read is logged, counted in the summary line as left out, and the
other passages carry on. A spell with no recorded position (stats carried from
version 4 to 5 with no track points left to work them out again) is never asked
about: its sea says so, and its position is explicit in the type (an absent
position, not 0, 0).

One loop owns the passes, `runSeaSweeps`, run by the start-up goroutine
(`startPassageFigures`). After the recompute pass of ADR 0298 it runs one pass, then
one every hour, until the shutdown context (the streams' context) ends. Each pass
takes the time afresh and looks at every passage, hidden ones included. So a
passage detected, split, merged, trimmed or imported since the last pass is picked
up by the next, and nothing else asks for a fill. There are no triggers on passage
close or on the edit and import handlers, no lock and no run-again flag: one loop
cannot overlap itself, and an hourly scan of the passages is cheap. The interval and
the clock are parameters, so a test runs the loop with a short interval and a
chosen clock.

Each pass has a second step after the hindcast, which ADR 0300 describes:
`runSeaFetch` works out what the offline coastline says of each spell that now has
a sea state (how far the wind ran over water, and whether the swell's quarter is
closed) and stores it beside the hindcast. Running it in the same pass means an
hour fetched now has its shore worked out now. It reads the same passages through
the same function, builds at most 2000 geometries a pass and honours ctx like the
hindcast, and a pass that finds nothing to do logs nothing. It takes the coastline
as a parameter, as the hindcast takes the wave plugin, so a test stands in for it.

A pass honours ctx between requests. It logs one line, in the shape of ADR 0296's,
only when it did something or failed: how many hours it filled, how many had no
value (with the first few reasons), and how many failed (with the first few
errors). A pass with nothing to fetch logs nothing, so a quiet hour leaves no line.

### 5. When it can't fetch, it stores nothing

- **No wave plugin, or a plugin without the export:** nothing is stored. The log
  says so, with the install hint for the missing export, and the next hourly pass
  tries again.
- **A transient failure:** a network error, a timeout, a 401, 403, 404 or 414
  (login, firewall, quota, URL length), a 5xx or a rate limit stores nothing for
  those hours. They count as failed and the next hourly pass asks again. An answer
  with the wrong number of positions is the same, but the provider refuses it at
  its boundary (`mapWasmWaveHindcastOutput`), so the job does not check the count
  a second time.
- **A rate limit:** the plugin words a 429 as "rate limited", and the provider
  turns that into one error the sweep recognises. It is transient, so nothing is
  stored, and the sweep stops there, logs it, and leaves the rest for the next
  sweep.
- **A definite refusal:** only an HTTP 400 whose reason says the requested time or
  date is out of range (a date outside the archive) will give the same answer every
  time, so asking again each hour would never settle. A 400 about anything else is
  an error like the rest. The plugin reports a refusal as `{"refused": "<sentence>"}`
  with no points, and the host stores every hour of that request with the
  sentence ("Open-Meteo wouldn't give the sea for this hour: ..."). It is not
  asked again. Because each passage is its own request, a refusal settles that
  passage's hours alone.
- **A refused blend request:** with `use_blend_fallback` on, the plugin asks again
  for the land points alone. When Open-Meteo refuses that second request, the
  GFS-Wave answers already in hand for the open-water points stand. Only the land
  points carry the sentence, as the `missing_reason` of each of their hours, so
  one refused request never discards good answers. The contract needed no new
  field: `missing_reason` is already per hour.
- **An answer that leaves out an hour that was asked for:** the plugin answered
  the position and gave no figure for that hour, which is its definite word. The
  hour is stored with "The wave plugin gave no figure for this hour." and is not
  asked again. It is never read as an empty sea or filled from a neighbour.
- **The model has no value for the cell:** a row is stored with the plugin's
  reason, for example "GFS-Wave has no value at this cell.", and it is not asked
  again. The same goes for a combined sea the model did not split into swell and
  wind wave, which the plugin words as its own reason. Both are settled answers.

InfluxDB is not involved. Nothing here is a default or a guess: the job never fills
an hour from the forecast, from a neighbouring cell, or from a model other than
the one the plugin names.

### 6. What the logbook shows

`GET /api/passages/:id` gains, on each of `felt.spells`, a `sea`: the swell and the
wind wave with height in metres, period in seconds, the direction they come from
in degrees true and in words relative to the mean heading, or the reason there is
none. The words are built on `relativeAngleLabel`, the five bands Mate uses, so
the two describe one sea alike: "ahead" (the head band, which spans both sides),
"on the port bow", "on the starboard beam", "on the port quarter", and "astern"
(the following band). Bow, beam and quarter take the side the sea comes from. A
sea state with neither swell nor wind wave is a flat sea, and the page says so
without a field of its own. A spell whose hour has not been fetched yet says so,
and so does one with no recorded position. A failure to read the table is logged
and every spell carries it as the reason. Every reason ends in a full stop,
whoever worded it: the plugin's reasons carry one, and the host adds one to a
stored reason that lacks it. When the sweep could not ask at all (no wave plugin, or one without
the export), it keeps that outcome in memory, and every spell with no stored hour
and the passage line say "The sea state can't be fetched: ..." with the install
hint naming the plugin, until a sweep that can ask clears it. The passage-wide line
has its own wording ("The sea state for this passage hasn't been fetched yet."),
never a spell's "this hour", and never a spell's raw reason: when every spell was
refused it says "Open-Meteo wouldn't give the sea for this passage's hours.", and
when the spells otherwise have one reason of the plugin's wording, or differ, it
says each spell says why. The
page reads the passage's rows in one query over its hour range, and each spell
finds its own by key. The page's `felt.sea` sums the passage up: the largest swell
and the largest wind wave, each with its time. When only some spells have a sea,
the peaks cover only those, so the summary says how many have none (`partial`,
"no sea for 3 of 12 spells"). When the spells with a sea are all flat and the rest
have none, the summary says that in its reason and never reports "no swell or wind
wave" for spells that were not looked at.

With no heading recorded, the direction is the true bearing and says that no
heading was recorded. Heights show in metres, as the forecast does, since the app
has no height-unit setting.

The replay strip's label for a spell gains the sea ("Lively spell, 1.9 degrees
sway, from 10.15 AM. Sea: swell 0.8 m 9 s on the port beam, wind wave 0.6 m 4 s
on the port bow" for a wind wave 60 degrees off the bow). The Felt row gains one line on the sea over the passage, or the
reason when no spell has one.

## What it is not

- Not a model and not a comfort word. Nothing is fitted to the rows yet.
- Not a sea state from a sensor. It is the model's figure for the cell, which in
  sheltered water is often several times what the boat measured as heave. The fetch
  cap for sheltered water is a separate step, and lands before any fit so that
  inflated sheltered rows do not train the slopes.
- Not a fallback. A plugin that cannot give a hindcast gives none.

## When it can't run

- The wave plugin is missing or older than this release: every spell with no sea
  and the passage line say "The sea state can't be fetched" and name the plugin to
  install, and the log says the same.
- Open-Meteo is down: the same text shows, the log gives the errors, and the next
  hourly pass fills the hours.

## Follow-ups

- The fetch cap on wind-wave height for sheltered water, shown in the logbook.
- The model itself, the leg chip and the departure ranking.
- A recorded row keeps the model that answered, so a fit can use only the spells
  whose model matches the forecast's.
