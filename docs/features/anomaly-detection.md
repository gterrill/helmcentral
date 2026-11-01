# Anomaly detection

An engine that is struggling often shows it first as rpm that stumbles for a
second or two and then recovers, long before it quits. These checks watch for
that, and for other trouble that a normal gauge threshold never catches. Here
is what they cover:

- **Instruments:** An instrument that has quietly stopped telling the truth.
- **House bank:** A house bank that its alternators are still pushing after it
  has finished charging.
- **Engine pairs:** One engine of a pair pulling away from its twin at the same
  rpm.

Each one raises an ordinary alarm, with severity, dwell, acknowledge,
notifications and the alarm log behind it. See [Alarms](alarms.md) for how the
alarm system itself behaves.

See [Set up your vessel](../how-to/set-up-your-vessel.md) for picking the
engines and house bank that each check below watches.

## Sensor health

Three things can go wrong with a reading that a simple high/low threshold
never notices:

- **A frozen sensor:** The instrument network keeps hearing from the sender,
  but the number itself has stopped moving, even while the engine is clearly
  working (rpm swinging, well above idle). A coolant sender stuck reading 74°C
  forever looks fine on a gauge and hides a real overheat.
- **An impossible reading:** The value is outside anything a real sensor on a
  boat could ever report. Examples are a house bank at 400 volts or an engine
  at −40°C. That points to a dead or miswired sender, not a genuine reading.
- **A source gone quiet:** Something on the instrument network that was
  reporting steadily has stopped, mid-voyage, with nothing else on the network
  affected.

The impossible-reading and gone-quiet checks need nothing set up. They watch
whatever engines and batteries the boat is already publishing. The frozen check
needs at least one engine item assigned to a propulsion source (Settings →
SignalK), because it checks a stuck reading against that engine's own rpm.

Each of these alarms tells you which sensor it is about, by name, and what is
wrong with it, one line per sensor. For example:

- "Port Engine Starter Battery voltage 75.7 V · above the 70 V any 12, 24 or 48
  V bank reaches"
- "Port engine oil pressure has not changed in 15 minutes while rpm varied"
- "PORT START BATT has stopped sending"

The names are the ones you give your batteries and engines, taken from their
equipment items. Where a source has no item, the card uses what the boat's own
instruments call it.

A Victron device that reaches Helmcentral through a GX shows the name you set
for it on the GX. If you haven't set one, the card says what kind of device it
is, such as "Victron charger" or "Victron solar charger". To give a device a
proper name, set one on the GX.

A serial instrument can send several kinds of data. A combined AIS, GPS and
instrument multiplexer is one example. It shows as one device on the card,
named for its connection. Here's how the card describes it:

- **All of it quiet:** The card says what the device carried: "Vesper Cortex
  has stopped sending AIS, position, heading, wind, depth, boat speed, log and
  weather".
- **Only part quiet:** The card lists what only that part carried. If the rest
  still arrives, it says "some of its data" instead.
- **The title:** The card is titled for the device ("Vesper Cortex Silent", or
  "Vesper Cortex Partly Silent" when some of it still sends). If more than one
  device has gone quiet, the title is "Instruments Silent". The title is set
  when the alarm is raised and doesn't change while it is up.
- **Ignoring it:** The card has one **Ignore** action for the whole device.

Some equipment goes quiet on purpose. A shore charger loses its power when you
disconnect shore power, and the card doesn't report it. When the boat's AC
input switches to battery, a device whose last report was within a minute of
the switch, before or after, is left out. This only works when something on the
boat reports the AC input source (a Victron GX does). Helmcentral does report
these cases:

- A device that goes quiet while you are on shore power.
- A device that goes quiet more than a minute from the switch.
- A device that stays quiet after you reconnect shore power.
- The instrument that reports the AC input, and anything behind it if it goes
  quiet too.

The impossible-reading check looks at batteries, not at chargers. On some
boats, a solar charger reports its panel array's voltage under a battery
number, and 76 volts is a normal array but an impossible battery. Sometimes
every device reporting a battery number is a charger, and each of those
chargers also feeds another battery number that a non-charger reports. In that
case, Helmcentral doesn't treat that number as a battery. A charger on its own
is still checked, and so is anything Helmcentral can't identify.

Say you know a sensor is dead and you don't want it watched, such as a broken
exhaust temperature sender. You can exclude it from its own alarm card with an
**Ignore** action named for that sensor. See
[Set up your vessel](../how-to/set-up-your-vessel.md#excluding-a-sensor-you-know-is-dead).

## Charging into a full house bank

This watches for the house bank being driven higher by its alternators or
chargers after it has already finished charging. It looks for a high state of
charge, a pack voltage at or above the warning point, and real current still
flowing in. That is the pattern behind a battery management system (BMS)
tripping its protection disconnect mid-voyage. The bank looks fully charged and
the charging sources don't know it, so the BMS eventually has to step in. A
second, tighter tier watches for the bank being driven further still, toward
the point where a BMS would act.

It needs a house bank chosen under Settings → SignalK → Roles. The bank's
equipment item also needs its cell count and a battery profile, so that
Helmcentral knows what "full" and "too high" mean for your own pack chemistry.

## Engine differentials

This check applies when two or more engines run matched at the same rpm.
Helmcentral compares each engine's coolant temperature, oil pressure, boost
pressure, load and transmission readings against the others. A gap that grows
well past what your own boat normally runs points at a developing fault on one
side, before it becomes a breakdown. Port always running a little warmer than
starboard is normal, and Helmcentral learns that out. Port suddenly running ten
degrees warmer than usual at the same rpm is not normal.

These alarms stay switched off until Helmcentral has learned your boat's own
normal gap from several weeks of history. Once raised, they stay up until a
steady run shows the difference has really gone away. They don't clear the
moment the numbers touch back down.

It needs at least two engine items assigned to propulsion sources in Settings →
SignalK.

## Engine stumble

Hold the engine at a steady throttle. If rpm dips 40 rpm or more (or 3% of the
steady figure, whichever is greater) for a few seconds and then comes back,
that's a stumble. One now and then is nothing. A run of them, with the throttle
untouched, means an engine that is not getting what it asks for. Helmcentral
counts the stumbles on each engine over the last ten minutes. When the count
reaches three, it raises a warning titled with the engine's name (for example
"Port Engine RPM Dips"). The card gives the count and the worst dip, for
example "6 RPM dips in 10 min · up to 140 rpm below 1170 rpm". The warning
clears once ten minutes pass without a stumble. Helmcentral reports what the
engine did. It doesn't know why.

What doesn't count:

- **A throttle change:** If rpm drops and stays down, you moved the lever.
- **Low rpm:** Anything under 800 rpm is ignored, so idling and shutdown never
  trip it.
- **A dip on both engines of a twin at about the same moment:** Both propellers
  unloading on the same wave is the sea, not an engine. A dip on one engine
  alone still counts, and a single-engine boat gets no such filtering.

This needs nothing set up. Every engine the boat reports rpm for is watched
from the moment it starts, and the warning is already switched on. An engine
that first appears later gets its own warning then. An engine with an equipment
item is titled with the item's name. Otherwise the title uses the engine's own
identifier ("Port"). You can edit or remove the alarm rule in Alarms like any
other, and a restart doesn't undo removing it.
