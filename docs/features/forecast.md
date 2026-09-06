# Forecast

The forecast panel carries ten days of weather, wind, wave and tide for
wherever the boat currently is. Position comes from SignalK, so it follows you
without manual position updates.

Weather, waves and tides each come from their own provider plugin. If one is
down you get an explicit failure for that section rather than a stale
number, and the card says which provider answered, whether the data was cached
and how long ago it refreshed.

## Where the wave thresholds come from

The steepness bands, leading indicators and crossing-seas flag are taken from Steve and
Linda Dashew's *Surviving the Storm*, a heavy-weather seamanship text built
from first-hand accounts and interviews with NOAA and Bureau of Meteorology
forecasters.

Dashew Offshore and Beowulf have released it as a free PDF, along with its
companion *Mariner's Weather Handbook*:

<https://setsail.com/weather-forecasting-storm-tactics-and-successful-cruising/>

Page 231 of *Surviving the Storm* explains the 1:10 threshold used to colour the
wave graph. *Mariner's Weather Handbook* covers the 500mb material behind the
squash-zone alarm rule; the first book covers it only briefly.

These are one crew's thresholds, published in 1999. They have not been
calibrated against your boat or your coast.

## Steepness, not height

The wave graph leads with steepness because height alone does not describe sea
conditions. A 3m sea at 12 seconds is less steep than a 2m sea at 5 seconds,
which falls in the breaking band below.

Steepness is wave height divided by the distance between crests. The graph
shows it as a "1 in N" ratio beside each direction arrow, and tints the wave
line once seas get steep:

| Ratio | Reading |
| --- | --- |
| flatter than 1:25 | rolling |
| 1:25 to 1:14 | building |
| 1:14 to 1:10 | steep, line turns amber |
| 1:10 or steeper | breaking, line turns red |

The 1:10 figure comes from observations at sea. Tank tests put wave instability
nearer 1:7; this page uses the lower threshold reported by sailors.

On a day that stays in the rolling band the line keeps its ordinary colour and
the summary says nothing about steepness, reserving alerts for steeper seas.

## The largest wave you will actually meet

Under the summary the page gives a second figure, roughly 1.87 times the
significant height. Significant wave height is the average of the highest third
of the waves, not the maximum; about one wave in seven reaches that height.

Use the larger number for planning: a 3m forecast gives an estimate near 5.6m
for the largest wave over the day.

## Warning signs

The wave card lists any of these warning signs detected in the forecast. The
list is hidden when none are detected.

**Sea building fast.** Seas rising 3m or more inside three hours. This is the
threshold a NOAA forecaster used to screen a year of buoy data for rapidly
building seas, and it found ten instances on the whole US West Coast, so it is
not something you will see often.

**Height and period both up by half in an hour.** Called a certain danger
signal by a senior meteorologist at the Marine Prediction Center. Both have to
rise. Height climbing alone is an ordinary building sea.

**Period lengthening sharply.** Three seconds or more in an hour, the sort of
step from 8 seconds to 11 that often runs ahead of a wave front.

**Crossing seas.** The wind wave and the swell more than 60 degrees apart. A
single system spreads maybe 20 or 30 degrees either side of the wind, so past
60 you are looking at two. The danger is less the second system itself than
what it does to your alignment with the first, which is where knockdowns come
from. The secondary system has to be at least a third the size of the primary
before this flags, so a forecast of a 2cm swell beside a 1.4m sea does not
trigger a crossing-seas warning.

**Seas outrunning the wind.** Wave height in feet past 0.8 times the wind in
knots. Seas do not normally exceed that, so when they do the sea was not built
by the wind you can see. Something upwind made it, or a current is standing it
up.

**Cold air over warmer water.** The sea more than about 4F warmer than the
day's low. Warm water under cold air is an energy source, and the difference
drives gusty, unsettled conditions out of proportion to what the pressure chart
suggests.

## Upper air, days ahead

The strip marks a day with a small **500MB** badge when conditions high in the
atmosphere favour a low developing. Select that day and the panel below says
why.

The Dashews place particular importance on upper-air forecasting. A surface low
needs an upper trough above it to vent into and deepen; without one, it may
remain weak or fail to form. The upper pattern can indicate days ahead whether
surface systems have support to develop, before the barometer changes.

Helmcentral watches 500mb heights across the whole forecast window and marks a
day that sits at the low end of it after a sustained fall. Everything is
relative to the rest of that window rather than to a fixed number. The book's
method uses chart interpretation and gives no numeric thresholds. Also, 500mb
heights differ by hundreds of metres between the tropics and high latitudes,
so a fixed threshold would not apply across regions.

A day with upper-air data but nothing notable still shows its height and the
jet strength overhead. A day with no data shows no section, to avoid implying
an all-clear without measurements.

### The trace

A single day's height needs context. For a reading of 5899 m, consider whether
it has fallen 40 m over two days and whether it has reached a minimum or is
still falling. The forecast page ends with a panel tracing the whole window
at six-hourly resolution, approximately the interval of the book's synoptic
charts.

The page has three panels with equal prominence. Today is
the next 24 hours, the 10-day forecast is the surface picture day by day, and
upper air is the 16-day pattern. Each panel header carries its span and a small
meter showing how far ahead it reaches. This helps distinguish today's
conditions from the longer-term pattern used for planning.

The trace has three elements.

The teal line is 500mb height. A sustained fall indicates
an upper trough moving in, and the bottom of the fall is where it sits over you.

The shaded strip along the bottom is the lowest fifth of this particular
window, which is the band a day has to reach before it gets marked. You can
compare the line with this band to check which days reach the lowest quintile.

The amber area is surface gust from the weather forecast, on the same time
axis. The book describes a lag between the trough aloft venting the low beneath
it and the surface response. A shared time axis lets you look for gusts building
a day or two after heights fall. When they do, the surface forecast is
consistent with the upper pattern. A fall without a surface response suggests
the trough passed without a low beneath it to develop.

The surface forecast runs ten days and the upper-air one sixteen, so the amber
area ends at day ten. The last six days of the trace show only the upper-air
pattern because they are beyond the surface forecast's range.

The first two days of the window cannot be marked because there is not enough
preceding history to establish the trend. Forecast skill at 500mb also varies
across sixteen days; treat the far end as a possible scenario. The book advises
watching the pattern over ten days to two weeks rather than relying on a single
chart. The same caution applies here.

### What it does not do

It does not predict a storm. The book's precursor for a bomb is two
upper troughs converging until they overlap, which it judges by eye across
successive charts; Helmcentral does not assess that. It indicates days when
conditions aloft support development, as a risk modifier for your planning.

If you want to go further, the reasoning lives in *Mariner's Weather Handbook*,
free at the same link above. *Surviving the Storm* defers to it for the 500mb
material and barely covers it directly.

### Getting it

Upper air needs its own provider plugin in `plugins/upper-air/`, separate from
your weather provider. Apple WeatherKit provides surface forecasts but no
pressure-level data. Separate providers let you use WeatherKit for weather
and Open-Meteo for upper air.

With no plugin installed, the forecast page simply has no upper-air section.
Nothing else changes.

## Alarm rules that use this

The forecast page shows what is coming. For what is happening now, Helmcentral
derives three values from the boat's own instruments that
[alarm rules](alarms.md) can bind to: the barometer's rate of change, its
three-hour tendency, and a squash-zone signature that fires when the wind
climbs while the barometer sits still.

A set of rules using the book's thresholds ships with Helmcentral, disabled.
See [Alarms](alarms.md).

## Where it stops

The forecast is only as good as the models behind it, and the wave model in
particular has poor coverage close inshore. Query it from a berth and you may
get zeros, because the grid cell is land.

None of the indicators here are predictions of severe weather. They are the
warning signs a seamanship text says to watch for, evaluated automatically so
you do not have to read every hour of a ten-day forecast yourself. Official
warnings for your area still come from your meteorological service and appear
in their own banner.

The book notes that a low probability of severe weather may not justify an
official warning. The absence of a warning therefore does not rule out risk
on your passage. These indicators support, but do not replace, your own
assessment.
