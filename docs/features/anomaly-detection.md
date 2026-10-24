# Anomaly detection

An engine that is struggling often shows it first as rpm that stumbles for a
second or two and recovers, long before it quits. These checks watch for
that, and for other trouble a normal gauge threshold never catches: an
instrument that has quietly stopped telling the truth, a house bank still
being pushed by its alternators after it has finished charging, and one
engine of a pair pulling away from its twin at the same rpm. Each one raises
an ordinary alarm, with severity, dwell, acknowledge, notifications and the
alarm log behind it. See [Alarms](alarms.md) for how the alarm system itself
behaves.

See [Set up your vessel](../how-to/set-up-your-vessel.md) for picking the
engines and house bank each check below watches.

## Sensor health

Three things can go wrong with a reading that a simple high/low threshold
never notices.

- **A frozen sensor.** The instrument network keeps hearing from the sender,
  but the number itself has stopped moving, even while the engine is
  clearly working (rpm swinging, well above idle). A coolant sender stuck
  reading 74°C forever looks fine on a gauge and hides a real overheat.
- **An impossible reading.** A value outside anything a real sensor on a
  boat could ever report: a house bank at 400 volts, an engine at −40°C.
  That points to a dead or miswired sender, not a genuine reading.
- **A source gone quiet.** Something on the instrument network that was
  reporting steadily has stopped, mid-voyage, with nothing else on the
  network affected.

The impossible-reading and gone-quiet checks need nothing set up. They
watch whatever engines and batteries the boat is already publishing. The
frozen check needs at least one engine item assigned to a propulsion source
(Settings → SignalK), since it checks a stuck reading against that engine's
own rpm.

Each of these alarms tells you which sensor it is about, by name, and what
is wrong with it, one line per sensor. For example: "Port Engine Starter
Battery voltage 75.7 V · above the 70 V any 12, 24 or 48 V bank reaches", or
"Port engine oil pressure has not changed in 15 minutes while rpm varied", or
"PORT START BATT has stopped sending". The names are the ones you give your
batteries and engines, taken from their equipment items. Where a source has
no item, the card uses what the boat's own instruments call it.

A serial instrument that sends several kinds of data, such as a combined
AIS, GPS and instrument multiplexer, is one device on the card, named for its
connection. When all of it has gone quiet the card says what it carried:
"Vesper Cortex has stopped sending AIS, position, heading, wind, depth, boat
speed, log and weather". If only part has gone quiet, the card lists what only
that part carried, or says "some of its data" when the rest still arrives. The
card is titled for the device ("Vesper Cortex Silent", or "Vesper Cortex Partly
Silent" when some of it still sends), or "Instruments Silent" when more than one
has gone quiet. The title is set when the alarm is raised and does not change
while it is up. The card has one **Ignore** action for the whole device.

Some equipment goes quiet on purpose. A shore charger loses its power when
you disconnect shore power, and the card does not report it. When the boat's
AC input switches to battery, a device whose last report was within a minute
of the switch, before or after, is left out. This only works when something on
the boat reports the AC input source (a Victron GX does). A device that goes
quiet while you are on shore power, more than a minute from the switch, or
stays quiet after you reconnect shore power, is reported. So is the instrument
that reports the AC input, and anything behind it if it goes quiet too.

The impossible-reading check looks at batteries, not at chargers. A solar
charger reports its panel array's voltage under a battery number on some
boats, and 76 volts is a normal array and an impossible battery. Where
every device reporting a battery number is a charger, and each of those
chargers also feeds another battery number that a non-charger reports,
Helmcentral does not treat that number as a battery. A charger on its own is
still checked, and so is anything Helmcentral cannot identify.

A sensor you know is dead and don't want watched (a broken exhaust
temperature sender, say) can be excluded from its own alarm card with an
**Ignore** action named for that sensor. See
[Set up your vessel](../how-to/set-up-your-vessel.md#excluding-a-sensor-you-know-is-dead).

## Charging into a full house bank

This watches for the house bank being driven higher by its alternators or
chargers after it has already finished charging: high state of charge, pack
voltage at or above the warning point, and real current still flowing in.
That is the pattern behind a battery management system tripping its
protection disconnect mid-voyage. The bank looks fully charged, the charging sources
don't know it, and the BMS eventually has to step in. A second,
tighter tier watches for the bank being driven further still, toward the
point a BMS would act.

It needs a house bank chosen under Settings → SignalK → Roles, with its cell
count and a battery profile on the bank's equipment item, so Helmcentral
knows what "full" and "too high" mean for your own pack chemistry.

## Engine differentials

For two or more engines running matched at the same rpm, each engine's
coolant temperature, oil pressure, boost pressure, load and transmission
readings are compared against the others. A gap that grows well past what
your own boat normally runs points at a developing fault on one side before
it becomes a breakdown. Port always running a little warmer than starboard
is normal and gets learned out. Port suddenly running ten degrees warmer
than usual at the same rpm is not.

These alarms stay switched off until Helmcentral has learned your boat's own
normal gap from several weeks of history. Once raised, they stay up until a
steady run shows the difference has really gone away, rather than clearing
the moment the numbers touch back down.

It needs at least two engine items assigned to propulsion sources in
Settings → SignalK.

## Engine stumble

With the engine held at a steady throttle, rpm that dips 40 rpm or more (or
3% of the steady figure, whichever is greater) for a few seconds and then
comes back is a stumble. One now and then is nothing. A run of them, with
the throttle untouched, means an engine that is not getting what it asks for.
Helmcentral counts the stumbles on each engine over the last ten minutes and
raises a warning, titled with the engine's name (for example "Port Engine
RPM Dips"), when the count reaches three. The card gives the count and the
worst dip, for example "6 RPM dips in 10 min · up to 140 rpm below 1170 rpm".
It clears once ten minutes pass without a stumble. Helmcentral reports what
the engine did. It does not know why.

What does not count:

- A throttle change. If rpm drops and stays down, you moved the lever.
- Anything under 800 rpm, so idling and shutdown never trip it.
- A dip on both engines of a twin at about the same moment. Both propellers
  unloading on the same wave is the sea, not an engine. A dip on one engine
  alone still counts, and a single-engine boat gets no such filtering.

This needs nothing set up. Every engine the boat reports rpm for is watched
from the moment it starts, the warning is already switched on, and an engine
that first appears later gets its own warning then. An engine with an
equipment item is titled with the item's name. Otherwise the title uses the
engine's own identifier ("Port"). The alarm rule can be edited or removed in
Alarms like any other, and removing it is not undone by a restart.
