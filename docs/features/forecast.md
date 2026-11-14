# Forecast

The forecast panel turns ten days of weather, wind, wave and tide into a
heavy-weather read for wherever the boat currently is. That lets you see a
building system or a dangerous sea state while there is still time to reef
early, delay a passage or pick a different anchorage.

- **Position:** It comes from the boat's own GPS feed, so the forecast follows
  you without manual position updates.
- **Three panels of equal weight:** Today covers the next 24 hours, the 10-day
  forecast gives the surface picture day by day, and upper air shows the 16-day
  pattern. Each panel header shows its span and a small meter for how far ahead
  it reaches. That way today's conditions don't get mixed up with the longer
  pattern you plan from.
- **The wind headline names a change:** Each day's wind line describes the day
  in stretches, not just its start and end. If the wind turns by 90 degrees or
  more within about three hours, the line says which way and when. A slower
  swing over the day shows as "turning X ... and settling Y" or "from X to Y",
  so check the hourly graph for when it gets there. For example, "NNW to NW 3
  to 5 kts, going light around 4 AM, then turning SSE around 5 AM and settling
  E 3 to 8 kts. Gusts to 20 kts around 5 PM."
  Hours under 2 knots count as calm, so the line says the wind goes light
  rather than quoting a direction that isn't there. When the wind comes back
  after a calm, the line says so and gives the new speeds. An odd hour or two
  between steady stretches reads as "variable" rather than as a turn, or
  "briefly variable" when the steady wind comes back. Beside a calm and under
  5 knots it reads as "light and variable". Its speed still counts, so a
  strong one-hour squall shows in the speed range. A single odd hour at the very
  start or end of the day can still show as a change. The peak gust comes with
  its hour, and a steady day stays one short sentence.
- **Passage plans:** The forecast page can also lay out a run of stops day by
  day, with the weather for each. See [Passage plans](#passage-plans).
- **Separate providers:** Weather, waves and tides each come from their own
  provider plugin. If one is down, you get an explicit failure for that section
  rather than a stale number. The card says which provider answered, whether
  the data is cached and how long ago it refreshed.
- **Tides anywhere:** Until you choose a tide provider, tides come from a
  worldwide model that follows the boat's position, so there's no station to
  pick. Its heights are measured from mean sea level, not chart datum. They're
  often negative, they include surge and air pressure, and the chart and tiles
  say "from mean sea level" beside them. It's a model, not a tide table. If the
  Australian or US provider covers your waters, choose it in Settings and pick
  a station, and you get that station's published predictions on chart datum.
  Anchor Watch can't work out a depth at chart datum from the worldwide model's
  heights, so Save depth tells you why instead of saving a wrong figure. The
  tide line, the low-water clearance and the rode at high water use only how far
  the water rises and falls, so they work either way.

## Where the wave thresholds come from

The steepness bands, leading indicators and crossing-seas flag are taken from
Steve and Linda Dashew's *Surviving the Storm*. It's a heavy-weather seamanship
text built from first-hand accounts and interviews with NOAA and Bureau of
Meteorology forecasters.

Dashew Offshore and Beowulf have released it as a free PDF, along with its
companion *Mariner's Weather Handbook*:

<https://setsail.com/weather-forecasting-storm-tactics-and-successful-cruising/>

Page 231 of *Surviving the Storm* explains the 1:10 threshold used to colour the
wave graph. *Mariner's Weather Handbook* covers the 500mb material behind the
squash-zone alarm rule. The first book covers it only briefly.

These are one crew's thresholds, published in 1999. They haven't been
calibrated against your boat or your coast.

## Steepness, not height

The wave graph leads with steepness, because height alone doesn't describe sea
conditions. A 3m sea at 12 seconds is less steep than a 2m sea at 5 seconds,
which falls in the breaking band below.

Steepness is wave height divided by the distance between crests. The graph
shows it as a "1 in N" ratio beside each direction arrow, and it tints the wave
line once seas get steep:

| Ratio | Reading |
| --- | --- |
| flatter than 1:25 | rolling |
| 1:25 to 1:14 | building |
| 1:14 to 1:10 | steep, line turns amber |
| 1:10 or steeper | breaking, line turns red |

The 1:10 figure comes from observations at sea. Tank tests put wave instability
nearer 1:7. This page uses the lower threshold reported by sailors.

On a day that stays in the rolling band, the line keeps its ordinary colour and
the summary says nothing about steepness. Alerts are kept for steeper seas.

## The largest wave you will actually meet

Under the summary, the page gives a second figure, which is roughly 1.87 times
the significant height. Significant wave height is the average of the highest
third of the waves, not the maximum. About one wave in seven reaches that
height.

Use the larger number for planning. A 3m forecast gives an estimate near 5.6m
for the largest wave over the day.

## Warning signs

The wave card lists any of these warning signs detected in the forecast. The
list is hidden when none are detected. Here's what each one means:

- **Sea building fast:** Seas are rising 3m or more inside three hours. This is
  the threshold a NOAA forecaster used to screen a year of buoy data for
  rapidly building seas. It found ten instances on the whole US West Coast, so
  you won't see it often.
- **Height and period both up by half in an hour:** A senior meteorologist at
  the Marine Prediction Center called this a certain danger signal. Both have
  to rise. Height climbing alone is an ordinary building sea.
- **Period lengthening sharply:** The period has grown by three seconds or more
  in an hour. That's the sort of step from 8 seconds to 11 that often runs
  ahead of a wave front.
- **Crossing seas:** The wind wave and the swell are more than 60 degrees
  apart. A single system spreads maybe 20 or 30 degrees either side of the
  wind, so past 60 you are looking at two. The danger is less the second system
  itself than what it does to your alignment with the first, which is where
  knockdowns come from. The secondary system has to be at least a third the
  size of the primary before this flags. A forecast of a 2cm swell beside a
  1.4m sea doesn't trigger a crossing-seas warning.
- **Seas outrunning the wind:** Wave height in feet is past 0.8 times the wind
  in knots. Seas don't normally exceed that, so when they do, the sea wasn't
  built by the wind you can see. Something upwind made it, or a current is
  standing it up.
- **Cold air over warmer water:** The sea is more than about 4F warmer than the
  day's low. Warm water under cold air is an energy source, and the difference
  drives gusty, unsettled conditions out of proportion to what the pressure
  chart suggests.

## Upper air, days ahead

The strip marks a day with a small **500MB** badge when conditions high in the
atmosphere favour a low developing. Select that day and the panel below says
why.

The Dashews place particular importance on upper-air forecasting. A surface low
needs an upper trough above it to vent into and deepen. Without one, it may
remain weak or fail to form. The upper pattern can indicate days ahead whether
surface systems have support to develop, before the barometer changes.

Helmcentral watches 500mb heights across the whole forecast window. It marks a
day that sits at the low end of the window after a sustained fall. Everything
is relative to the rest of that window rather than to a fixed number. The
book's method uses chart interpretation and gives no numeric thresholds. Also,
500mb heights differ by hundreds of metres between the tropics and high
latitudes, so a fixed threshold wouldn't apply across regions.

A day with upper-air data but nothing notable still shows its height and the
strength of the jet stream overhead. A day with no data shows no section, to
avoid implying an all-clear without measurements.

### The trace

A single day's height needs context. For a reading of 5899 m, you'd want to
know whether it has fallen 40 m over two days. You'd also want to know whether
it has reached a minimum or is still falling. The forecast page ends with a
panel tracing the whole window at six-hourly resolution, which is about
the interval of the book's synoptic charts.

The trace has three elements:

- **The teal line:** This is 500mb height. A sustained fall indicates an upper
  trough moving in, and the bottom of the fall is where it sits over you.
- **The shaded strip along the bottom:** This is the lowest fifth of this
  particular window, which is the band a day has to reach before it gets
  marked. You can compare the line with this band to check which days reach the
  lowest quintile.
- **The amber area:** This is surface gust from the weather forecast, on the
  same time axis. The book describes a lag between the trough aloft venting the
  low beneath it and the surface response. A shared time axis lets you look for
  gusts building a day or two after heights fall. When they do, the surface
  forecast is consistent with the upper pattern. A fall without a surface
  response suggests the trough passed without a low beneath it to develop.

The surface forecast runs ten days and the upper-air one sixteen, so the amber
area ends at day ten. The last six days of the trace show only the upper-air
pattern, because they are beyond the surface forecast's range.

The first two days of the window can't be marked, because there isn't enough
preceding history to establish the trend. Forecast skill at 500mb also varies
across sixteen days, so treat the far end as a possible scenario. The book
advises watching the pattern over ten days to two weeks rather than relying on
a single chart. The same caution applies here.

### What it does not do

It doesn't predict a storm. The book's precursor for a bomb is two upper
troughs converging until they overlap, which it judges by eye across successive
charts, and Helmcentral doesn't assess that. It indicates days when conditions
aloft support development, as a risk modifier for your planning.

If you want to go further, the reasoning lives in *Mariner's Weather Handbook*,
free at the same link above. *Surviving the Storm* defers to it for the 500mb
material and barely covers it directly.

### Getting it

Upper air needs its own provider, separate from your weather provider and
selected independently of it, because a weather source doesn't necessarily
cover pressure-level data too. Apple WeatherKit, for example, provides surface
forecasts but no pressure-level data. That's why WeatherKit for weather and
Open-Meteo for upper air is a supported pairing rather than a contradiction.

With no upper-air provider selected, the forecast page simply has no upper-air
section. Nothing else changes.

## Lightning, days ahead

A thin band across the top of two charts shows how likely lightning is at the
boat. You can see a thundery spell coming and pick your day before it arrives.

- **Where it shows:** The band sits across the top of the Cloud, Temperature &
  Rain chart for the day you've selected, and across the top of the Upper Air
  trace. On the trace it lines up with the trough days, so you can see whether
  a falling 500mb height and a thundery spell arrive together.
- **What each block means:** Every block is a three-hour window. Its strength
  is the chance of at least one lightning flash within about 20 kilometres of
  the boat in that window. Touch or hover a chart and the tooltip adds the
  chance, such as "Lightning 40%", for the hour you're reading.
- **On the day cards too:** Each card in the day strip gets a thin violet
  line along its bottom edge, a small copy of the Cloud chart's band. You can
  see which part of which day is stormy before you open it. A quiet day has no
  line. Hours past the end of the forecast are hatched, as on the charts.
  Today's card leaves out the hours already gone, which the chart hatches.
- **In the day summary:** The summary above the Cloud, Temperature & Rain
  chart adds a sentence, such as "Lightning likely from 3 PM to 9 PM". It says
  "likely" from 60%, "possible" from 30%, and "slight chance" from 10%.
- **Four steps, not a gradient:** Under 10% draws nothing, so a quiet stretch
  is an empty slot. From 10% to 30% is a pale block, 30% to 60% is stronger, and
  60% or more is solid. The key under each chart, "Lightning chance", shows
  the three shades. Its ⓘ button says what the band measures and who the
  data comes from.
- **Hatched means no data:** The forecast reaches about 14.5 days, and the
  Upper Air trace runs a day or two longer. Hours the forecast doesn't
  cover are hatched with thin diagonal lines. That includes the hours before
  now on today's chart and the days past the end of the run. A hatched
  stretch means there's no forecast there, and an empty stretch means the
  forecast says it's quiet.
- **It's the boat's position only:** Lightning is worked out for the grid
  square the boat is in, roughly 20 by 30 kilometres. When you switch the page
  to a [passage plan](#passage-plans), the bands aren't drawn
  and the key says "Lightning is for the boat's position, not the stay."

A probability over a square that size says nothing about the next ten minutes
or the square next door. It isn't a strike detector, and it doesn't raise an
alarm.

### Where it comes from

The default source is the Met Office's global blended forecast, which is free
and needs no key. Its credit, "Lightning data © Crown copyright, Met Office,
CC BY-SA 4.0.", is behind the ⓘ button in the lightning key while that source
is in use.

It comes from its own plugin, separate from your weather and upper-air
providers. The plugin bundle includes the Met Office lightning plugin, so a
fresh install has it. Helmcentral downloads about 60 MB a day for it: the next
two days are refreshed every six hours, and the rest of the range once a day.
Only the grid square the boat is in is kept. Swinging at anchor doesn't
trigger a download, but moving into a new square does.

### When something is wrong

The band is never drawn as if the chance were zero. The hours Helmcentral has
no data for are hatched, and the key under the chart says what's wrong.

- **Unavailable:** If the Met Office can't be reached and Helmcentral holds
  nothing, the key reads "Lightning unavailable:" followed by the reason.
- **Not updated:** If the forecast has been loaded but the latest refresh
  failed, the key reads "Lightning not updated:" followed by the reason. The
  windows Helmcentral already holds stay on the chart under that line.
  Helmcentral asks again after ten minutes.
- **Stale:** If the forecast stops updating for long enough, the key shows a
  Stale badge with its age and the band turns grey.
- **No plugin:** With no lightning plugin installed, the charts show no band,
  no line and no error about it. Once the plugin has answered, a failed update
  is shown in the key. Until it has answered since you opened the page, the key
  stays empty, even if something fails first. If a later answer names no
  plugin, the band and key go away again.

## Passage plans

The week before a run, you want to see every day of it at once: where you'll be,
how far each leg is, and what the wind and sea will do. A plan is your run as a
list of stops with the nights at each, and the forecast page lays it out one row
per day.

Plans are kept on the boat's server, so every phone, tablet and wall display
sees the same ones. You can keep several. The page opens on the plan whose dates
include today, or the next one coming up, and buttons above the table switch to
another. A plan whose last day has passed moves under **Past** and stays there
until you delete it.

The page always opens on the boat. Choose a place name in a row to look at that
place's full forecast, and choose **Boat** to come back. On a passage row that
place is the middle of the leg, the same spot its wind and waves are read at,
and a leg that couldn't be routed can't be opened. The heading says which
you're looking at, and while it's a plan stop or a leg's middle the heading adds
that this isn't the boat's position. Nothing on the dashboard changes. The tiles, the clock's
sunrise and sunset, the day and night theme, the anchor rode planner and the
alarms all keep reading from the boat's own position.

Here's what the table shows:

- **One row per day.** A passage row is the day you sail from one stop to the
  next. A lay day is a day you stay put. Dates after the first follow from the
  nights, so adding a night to a stop moves every later day.
- **Distance by water.** Each leg is measured along a route that stays on the
  water, through any via places you added, in order. If a leg can't be routed,
  its row says why and shows no distance. A straight line is never drawn in its
  place. **Route** on a passage row shows the path on a small map.
- **Wind against your course.** A passage row gives the day's wind range, then
  whether it sits ahead, abeam or astern of the leg's course, the gusts and the
  wave height. They're read at the middle of the leg. A lay day shows the wind
  and the waves from 6 PM that day to 6 AM the next morning, and the wind's
  direction. If the next morning is past the forecast, only the evening is used. The last stop's final
  row is the day you leave it, and says **leave**. There's no shelter label.
- **No forecast yet.** The forecast reaches about ten days ahead, so a day
  further out says "No forecast yet" and shows nothing. Nothing is guessed to
  fill the gap.
- **Times at the place.** Dates are the place's own. When a place's clock
  differs from the boat's, a note under the table says so once you've opened it.
- **Depart and ETA.** A passage row shows when you leave and when you should
  arrive. The arrival comes from the speed your boat has actually made. That's
  the middle value of the passages Helmcentral recorded over the last twelve
  months. It leaves out any passage shorter than 3 nautical miles or 30 minutes
  under way, such as a move inside a marina. The speed comes from your recorded
  passages, not the engine log, so it's a different source from the speed Mate
  uses for its own estimate. If no recorded passage qualifies yet, the row says
  "No ETA: no recorded passages yet" and shows no arrival and no verdict.
  Helmcentral doesn't make up a speed. If you set **Leave at** on a leg, the row
  shows that time and Helmcentral never changes it.
- **A suggested departure.** If you didn't set a time, Helmcentral suggests one
  and shows it in italics, like "Suggested 6.40 AM", so you never mistake it for
  a time you chose. It looks between first light and the last time you can leave
  and still arrive in daylight, when your passage limits ask for a daylight
  arrival. Without that limit the last time is sunset. It judges every quarter
  hour in that window against your passage limits, the same way it judges a time
  you set, and then picks like this:
  - Never a No-Go: it only considers times that aren't a No-Go.
  - Go before Marginal: a Go time beats a Marginal one, even when the Marginal
    one has the fairer stream.
  - The fairest stream: among times with the same verdict, it takes the one with
    the fairest stream along the route, ranked the way Mate ranks departures.
  - Every time a No-Go: it shows the least bad one, and its verdict says No-Go.
    It never hides a bad time.

  If no time fits the daylight, the row says "No daylight window" and shows no
  time and no verdict. If the departures can't be ranked, for example because
  the current model has no figures near the shore, the row says no departure was
  suggested and why. It doesn't pick a time for you.
- **Current.** The current along your course when you leave, as "0.8 kn
  hindering" or "1.2 kn helping". It's the Open-Meteo model's total current,
  tide and ocean current together. Near the shore the model often has no
  figures, and then the cell shows a dash with the reason when you hover over
  it. A missing reading is never shown as zero. The model covers about sixteen
  days, and the figure is read for the time you leave.
- **Verdict.** Each passage row ends with Go, Marginal, No-Go or No data, and
  names what decided it, like "No-Go: gusts 32 kn ahead". Helmcentral works it
  out with the same rules as Mate's passage window check, against the limits you
  set under [Passage limits](../how-to/set-passage-limits.md). A Marginal reading
  starts at 70 percent of a no-go figure. Nothing asks Mate for it. It's judged
  hour by hour along the leg, at your departure time or the suggested one. A
  measure it couldn't check, such as waves with no wave forecast, is listed
  under the verdict as not checked. If the limits aren't set, the page says so
  once above the table.
- **Warnings on a stop.** A lay day's verdict column shows an official wind or
  sea warning that covers that night at the stop. The night you arrive at a stop
  is shown under the passage row that gets you there. A warning names its day as
  a weekday, so only nights within the next week are matched. If the warnings
  can't be read, the page says so, and an empty column then doesn't mean none are
  in force.
- **As of.** Above the table a line says when the verdicts were worked out and
  when the forecast behind them was fetched. They're worked out again each time
  the forecast refreshes, which is about every ten minutes, and whenever you
  change the plan. While the new answer is being worked out, the verdict and
  current read "…", and Helmcentral never leaves an answer from before your
  change on screen. If the check fails, the page says why.
- **Arrive by and slack.** On any stop but the first you can set an arrive-by,
  the latest date and time you want to be there. The first stop has none,
  because the plan starts where the boat already is. A line above the table
  then says "Slack 1 day", which is the whole days between when the plan has you
  arriving and that day. If the plan already has you arriving later, the line
  says "Late by 1 day" in red. With several arrive-bys, the line follows the
  tightest one that's still ahead of you. With none, there's no line.
- **A plan that's fallen behind.** If the next leg should have left on a day
  that's already gone, the page counts from today. It moves every leg you
  haven't done forward by the days you're behind, and works out the slack on
  those dates. The line says so first, for example "Behind plan by 4 days. Late
  by 1 day". The slips below are measured from the same moved plan, so none of
  them lands on a day that has passed. Your saved dates don't change.
- **What a slip would meet.** When there's slack, the page works out what
  slipping the plan would do, one day at a time, up to one day past your slack.
  Every leg you haven't done moves later by that many days, the plan is judged
  again, and the line names the worst leg. For example, "Slip 1 day: Manly →
  Boat Works Wed, Go. Slip 2 days: Mooloolaba → Manly Thu meets wind 18 kn
  ahead, No-Go (late by 1 day)." The worst is a No-Go, then No data, then
  Marginal, then Go. A leg past the forecast says "No forecast yet". Nothing is
  saved, so your plan stays as it is. The slips take a little longer than the
  table, so they turn up after it, and the page says while it's still working.
  It checks no more than ten days of slip, which is as far as the forecast
  reaches.
- **Done and Skipped.** A leg is Done when a passage Helmcentral recorded ends
  within one and a half nautical miles of the leg's destination, on or after the
  day the leg was planned. If a passage ends at a later stop, the stops it went
  by are Skipped, along with the legs into them. Done and Skipped rows are
  dimmed, have no verdict or suggested time, and are left out of the slips. Your
  plan isn't changed. If Helmcentral can't read the recorded passages, the page
  says so, and nothing is marked.
- **Still in port.** On a leg's sailing day, once you're an hour past the time
  you set to leave, and the boat is still within one and a half nautical miles of
  the stop, the row says "Planned to leave today" and names the stop you're still
  at. **Slip a
  day** adds a night at that stop, the same as **+1 night**, and moves every
  later date. If you didn't set a time, the hour counts from the first time
  Helmcentral suggested that day, even once the day's window has closed. Only when
  the window has closed and Helmcentral never suggested a time that day does the
  row say it straight away. With no usable position fix, there's
  no prompt, since an unknown position doesn't mean you're in port. Dates never
  move on their own.
- **What changed since you last looked.** When you open a plan, a leg whose
  verdict is different from the one you saw last time is marked, for example "was
  Go, now Marginal". Only the verdict counts, so a changed gust figure under the
  same verdict isn't a change. The marks compare what's on screen with what you
  were shown last time, and they hold for as long as the plan is open. Helmcentral
  then remembers what you were shown, on the boat, so the next visit compares
  against it, from any device. A
  leg you move to another day, or whose stops you rename, is a new leg and has no
  mark. Someone who can't make changes sees the marks but doesn't update what was
  last seen.

Where the table is too narrow for its columns, on a phone or a tablet held
upright, each row becomes a card.

The full forecast for a place, once you open it, works as it always has for a
place away from the boat:

- **Your days marked.** The days of the row you chose carry a STAY tag, and days
  the forecast doesn't reach are placeholder cards reading "No forecast yet ·
  from 3 Nov". That date is when the forecast first covers the day.
- **Upper air.** The tinted band on the sixteen-day trace marks those days. Read
  it as the trend in the upper air over them, and not as a forecast for each
  day.
- **No lightning bands.** Lightning is only worked out for the boat's position,
  so the charts show no band for a plan stop and the key says why. The table has
  no lightning column for the same reason.
- **Tide from the nearest station.** The tide chart uses the station closest to
  the place, names it, and says how far it is from the place. A station more
  than 20 nautical miles away is flagged, because a tide there can differ from
  the tide at the berth. Your saved tide station and automatic station setting
  are left as they are. If your tide provider is the default one, which follows
  a position rather than a station, the chart is modelled for the place itself
  and names no station.
- **Warnings for that place.** A wind or surf warning is worded as the place's
  and names its zone, so you don't read a warning for somewhere else as your
  own.
- **Waves.** Where the wave model has no values at the spot, such as a berth up
  a river or beside a lake, the card says "No wave forecast for this spot". It
  doesn't borrow the open water's sea state.

Here's where it stops. There's no Lightning column, because the lightning
forecast is only worked out for the boat's own position. A leg is one day, so a passage that runs overnight isn't judged, and its row
says so. A bar crossing isn't checked, so a leg across a bar is judged on the
wind and waves along the route alone. Wind against tide isn't checked either,
because a plan doesn't know which way the flood sets. Mate can't make or change
a plan, though it can read the stop your current plan has you at or heading for
when you talk about "the marina". See
[Make a passage plan](../how-to/make-a-passage-plan.md) for the steps.

Helmcentral doesn't notify you when a plan day comes inside the forecast. Look
at the rows, which say "No forecast yet" until it does.

## Alarm rules that use this

The forecast page shows what is coming. For what is happening now, Helmcentral
derives seven values from the boat's own instruments that
[alarm rules](alarms.md) can bind to:

- **Rate of change:** The barometer's rate of change.
- **Tendency:** The barometer's three-hour, twelve-hour and twenty-four-hour
  tendency.
- **Squash-zone signature:** It fires when the wind climbs while the barometer
  sits still.
- **Storm signatures:** Two further signatures combine the tendency with the
  barometer's own height, to flag a developing storm or severe thunderstorm.

Rule sets built on these ship with Helmcentral: two rules from *Surviving the
Storm*, disabled, and a seven-rule ladder built on the three-hour tendency
table, mostly enabled. See [Alarms](alarms.md).

## On the dashboard and the wall display

The forecast panel is the full picture. The dashboard carries condensed
versions of the same data as ordinary tiles:

- **Current Conditions:** It shows true wind speed, with an arrow for its
  direction, and outside temperature against today's forecast range. It also
  shows whether rain is likely in the next hour, when it starts and how heavy.
  Apple WeatherKit gives that minute by minute, and Open-Meteo gives it in
  coarser fifteen-minute steps over North America and central Europe.
  Elsewhere, Open-Meteo's near-term strip is built from the hourly outlook
  rather than a true short-range reading. It's labelled "hourly forecast" so
  that it doesn't promise accuracy it can't back up. Where neither provider has
  any near-term coverage at all, the strip is dropped in favour of the plain
  hourly outlook.
- **Forecast Conditions:** It lists the next five days with condition, high and
  low. Under them it draws the same wind, gust and wave-height series this page
  does, steepness-coloured the same way, each day lined up under its own card.
- **Clock:** It shows sunrise, sunset and moon phase alongside the time.

None of the three replaces this page. None shows the warning signs, the
largest-wave figure or the upper-air outlook. They are for a glance that
doesn't want the detail, particularly on a [wall
display](../how-to/set-up-a-wall-display.md), which has no room for this whole
page at once. See [The dashboard](dashboard.md#instrument-tiles).

## Where it stops

The forecast is only as good as the models behind it, and the wave model in
particular has poor coverage close inshore. The default wave model, NOAA's
GFS-Wave, works on a grid of cells about 25 kilometres wide, and a cell that
touches land has no figures. Here is what you see when that happens:

- **At the boat:** The Wave card, and the Forecast Conditions tile, say in quiet
  grey text "The wave model has no data at this position". They don't draw a
  flat line, because that would look like a calm sea that nobody forecast.
  There's no Retry button, because trying again won't change the answer. The
  sea temperature still shows when the weather source has it.
- **For a plan stop:** The page says there is no wave forecast for that spot.
- **With the blend switched on:** Helmcentral asks Open-Meteo's own blend of
  models for the spot instead. The card's footer then says "fallback model:
  best_match", which is Open-Meteo's name for that blend. Steps are under
  [Use the blend where GFS-Wave has no data](#use-the-blend-where-gfs-wave-has-no-data).

An hour with no wave period counts as no data. Hours like that show as gaps in
the graph and are left out of the summary.

### Use the blend where GFS-Wave has no data

The blend is Open-Meteo's own mix of wave models, on a finer grid, so it often
has figures for a spot that GFS-Wave leaves blank. It can read lower than
GFS-Wave, so Helmcentral leaves it off until you ask for it. To turn it on:

1. Open **Settings** and find the **Open-Meteo Marine** wave provider.
2. Tap the gear icon on its card.
3. Switch on **Use Open-Meteo's blend where GFS-Wave has no data**, then tap
   **Save**.

The change applies from the next forecast fetch. The blend is used only for a
spot where GFS-Wave has no figures at all. Everywhere else you still get
GFS-Wave.

None of the indicators here are predictions of severe weather. They are the
warning signs a seamanship text says to watch for, evaluated automatically so
that you don't have to read every hour of a ten-day forecast yourself. Official
warnings for your area still come from your meteorological service. When one is
in force for your zone, it raises an alarm (see [Alarms](alarms.md)) and is
named under the summary on this page, with a link to the bulletin.

The book notes that a low probability of severe weather may not justify an
official warning. The absence of a warning therefore doesn't rule out risk on
your passage. These indicators support, but do not replace, your own
assessment.
