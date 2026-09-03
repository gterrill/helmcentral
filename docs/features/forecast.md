# Forecast

The forecast panel carries ten days of weather, wind, wave and tide for
wherever the boat currently is. Position comes from SignalK, so it follows you
without being told anything.

Weather, waves and tides each come from their own provider plugin. If one is
down you get an explicit failure for that section rather than a quietly stale
number, and the card says which provider answered, whether the data was cached
and how long ago it refreshed.

## Where the wave thresholds come from

Most of what the wave section says is not house opinion. The steepness bands,
the leading indicators and the crossing-seas flag are taken from Steve and
Linda Dashew's *Surviving the Storm*, a heavy-weather seamanship text built
from first-hand accounts and interviews with NOAA and Bureau of Meteorology
forecasters.

Dashew Offshore and Beowulf have released it as a free PDF, along with its
companion *Mariner's Weather Handbook*:

<https://setsail.com/weather-forecasting-storm-tactics-and-successful-cruising/>

Both are worth having aboard. If you want to know why 1:10 is the line at which
this page starts colouring the wave graph, the argument is on page 231 of the
first, and it is a better read than any summary here. The second is where the
500mb material lives, which is the upper-air reasoning behind the squash-zone
alarm rule and is barely touched in the first.

Two things to keep in mind about these numbers. They are one very experienced
crew's thresholds, published in 1999. And they have not been calibrated against
your boat or your coast.

## Steepness, not height

The wave graph leads with steepness because height on its own does not tell you
what you need. A 3m sea at 12 seconds is a comfortable day. A 2m sea at 5
seconds is breaking. Reported as heights, those look like the same information.

Steepness is wave height divided by the distance between crests. The graph
shows it as a "1 in N" ratio beside each direction arrow, and tints the wave
line once seas get steep:

| Ratio | Reading |
| --- | --- |
| flatter than 1:25 | rolling, nothing to think about |
| 1:25 to 1:14 | building |
| 1:14 to 1:10 | steep, line turns amber |
| 1:10 or steeper | breaking, line turns red |

The 1:10 figure is the one observation supports. Tank tests put wave
instability nearer 1:7; sailors report it happening sooner, and this page uses
the sailors' number.

On a day that stays in the rolling band the line keeps its ordinary colour and
the summary says nothing about steepness. That is deliberate. Most days are
benign, and a page that shouts on all of them stops being read.

## The largest wave you will actually meet

Under the summary the page gives a second figure, roughly 1.87 times the
significant height. Significant wave height is the average of the highest third
of the waves, so it is not the biggest thing out there, and about one wave in
seven reaches even that.

Plan on the larger number. A 3m forecast will hand you something near 5.6m
before the day is out.

## Warning signs

When the forecast trips one of these, the wave card grows a short list naming
it in words. Nothing appears when nothing is tripped.

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
before this flags, because forecast models will report a 2cm swell beside a
1.4m sea and that is arithmetic rather than weather.

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

This is the part of forecasting the Dashews rate above all others. The reason
is mechanical: a surface low needs an upper trough above it to vent into. With
one, it deepens. Without one it stays anemic or never forms. So the upper
pattern tells you days ahead whether the surface systems in your forecast have
the support to turn nasty, and it is often the only warning you get before the
barometer moves.

Helmcentral watches 500mb heights across the whole forecast window and marks a
day that sits at the low end of it after a sustained fall. Everything is
relative to the rest of that window rather than to a fixed number, for two
reasons. The book gives no numbers, because its method is reading charts. And
500mb heights differ by hundreds of metres between the tropics and the high
latitudes, so any fixed threshold would be wrong as soon as you sailed
anywhere.

A day with upper-air data but nothing notable still shows its height and the
jet strength overhead. A day with no data shows nothing at all, rather than an
empty section that would read as an all-clear nobody measured.

Two limits worth knowing. The first two days of the window can never be marked,
because there is no history behind them to say whether heights are arriving or
leaving. And forecast skill at 500mb is not uniform across sixteen days: the far
end is a scenario, not a forecast. The book's own instruction is to watch the
rhythm over ten days to two weeks rather than to trust any single chart, which
is the right way to read this too.

### What it does not do

It does not tell you a storm is coming. The book's real bomb precursor is two
upper troughs converging until they overlap, which it judges by eye across
successive charts, and Helmcentral does not attempt that call. What you get is
"conditions aloft support development on this day", which is a risk modifier for
your own planning rather than a prediction.

If you want to go further, the reasoning lives in *Mariner's Weather Handbook*,
free at the same link above. *Surviving the Storm* defers to it for the 500mb
material and barely covers it directly.

### Getting it

Upper air needs its own provider plugin in `plugins/upper-air/`, separate from
your weather provider. That separation is deliberate: Apple WeatherKit makes an
excellent surface forecast and carries no pressure-level data whatsoever, so
tying the two together would force a choice between them. Run WeatherKit for
weather and Open-Meteo for upper air if you like.

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

The book makes a point worth repeating: a forecaster sitting on a low
probability of severe weather will not issue a warning for it, because the odds
do not justify one. That is a reasonable thing for a forecaster to do and a
poor thing for you to rely on. Working out the risk for your own passage is
your job, and these indicators are an aid to it rather than a substitute.
