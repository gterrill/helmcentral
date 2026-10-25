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
- **A planned stay:** The forecast page can also show the weather at a marina
  or anchorage you have booked for later. See
  [Looking ahead to a planned stay](#looking-ahead-to-a-planned-stay).
- **Separate providers:** Weather, waves and tides each come from their own
  provider plugin. If one is down, you get an explicit failure for that section
  rather than a stale number. The card says which provider answered, whether
  the data was cached and how long ago it refreshed.

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
jet strength overhead. A day with no data shows no section, to avoid implying
an all-clear without measurements.

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

## Looking ahead to a planned stay

The week before you move to another marina, you want the weather there for the
days you will be there, not the weather where you are tied up now. Plan the stay
once, with the place and your arrival and departure dates, and the forecast page
can show that place alongside the boat's.

There is one stay at a time, kept on the boat's server, so every phone, tablet
and wall display sees the same one. It disappears the day after you leave.

The page always opens on the boat. Beside the heading sits a button with the
place and the dates, such as "Coral Coast Marina · 27-31 Oct". Choose it to
switch to the stay, and choose **Boat** to come back. The heading says which
you're looking at, and while it's the stay, the heading adds that this isn't
the boat's position. Nothing on the dashboard changes. The tiles, the clock's
sunrise and sunset, the day and night theme, the anchor rode planner and the
alarms all keep reading from the boat's own position.

Here's what the stay view shows:

- **The ten days, with your days marked.** Days you will be at the place carry a
  STAY tag. The forecast reaches about ten days ahead, so a stay further out
  gets a placeholder card for each day it doesn't reach, reading "No forecast
  yet · from 3 Nov". That date is when the forecast first covers the day.
  Nothing is guessed to fill the gap, and the last day is never stretched.
- **Times at the place.** Sunrise, sunset, the hours on every chart and the tide
  times are the place's local time. When its clock differs from the boat's, a
  note beside the buttons says so.
- **Upper air.** The tinted band on the sixteen-day trace marks your stay. Read
  it as the trend in the upper air over those days, whether heights are falling
  or recovering, and not as a forecast for each day. A stay further out than the
  trace says so.
- **Tide from the nearest station.** The tide chart uses the station closest to
  the place, names it, and says how far it is from the place. A station more
  than 20 nautical miles away is flagged, because a tide there can differ from
  the tide at the berth. Your saved tide station and automatic station setting
  are left as they are, and only this view uses the nearer one.
- **Warnings for that place.** A wind or surf warning is worded as the place's
  and names its zone, so you don't read a warning for somewhere else as your
  own.
- **Waves.** Where the wave model has no values at the spot, such as a berth up
  a river or beside a lake, the card says "No wave forecast for this spot". It
  doesn't borrow the open water's sea state.

Mate can read the stay when you talk about "the marina", and it can propose one
for you from a place name and dates. A proposal is a card, and nothing is set
until you apply it. See [Plan a stay](../how-to/plan-a-stay.md) for the steps.

Helmcentral doesn't notify you when the stay comes inside the forecast. Look at
the placeholder cards, which say when each day arrives.

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
particular has poor coverage close inshore. If you query it from a berth, you
may get zeros, because the grid cell is land. For a planned stay, the page says
there is no wave forecast for that spot.

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
