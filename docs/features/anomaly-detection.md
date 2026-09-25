# Anomaly detection

Three checks that watch for the kind of trouble a normal gauge threshold
never catches: an instrument that has quietly stopped telling the truth, a
house bank still being pushed by its alternators after it has finished
charging, and one engine of a pair pulling away from its twin at the same
rpm. Each one raises an ordinary alarm, with everything Alarms already does
(severity, dwell, acknowledge, notifications, the alarm log) behind it. See
[Alarms](alarms.md) for how the alarm system itself behaves.

See [Set up your vessel](../how-to/set-up-your-vessel.md) for picking the
engines and house bank each check below watches.

## Sensor health

Three things can go wrong with a reading that a simple high/low threshold
never notices:

- **A frozen sensor.** The instrument network keeps hearing from the sender,
  but the number itself has stopped moving, even while the engine is
  clearly working (rpm swinging, well above idle). A coolant sender stuck
  reading 74°C forever looks fine on a gauge and hides a real overheat.
- **An impossible reading.** A value outside anything a real sensor on a
  boat could ever report: a house bank at 400 volts, an engine at −40°C.
  The signature of a dead or miswired sender, not a genuine reading.
- **A source gone quiet.** Something on the instrument network that was
  reporting steadily has stopped, mid-voyage, with nothing else on the
  network affected.

The impossible-reading and gone-quiet checks need nothing set up: they
watch whatever engines and batteries the boat is already publishing. The
frozen check needs at least one engine ticked in Settings → Vessel, since it
checks a stuck reading against that engine's own rpm.

A sensor you know is dead and don't want watched (a broken exhaust
temperature sender, say) can be excluded from its own alarm card with an
**Ignore this sensor** action. See
[Set up your vessel](../how-to/set-up-your-vessel.md#excluding-a-sensor-you-know-is-dead).

## Charging into a full house bank

The house bank being driven higher by its alternators or chargers after it
has already finished charging: high state of charge, pack voltage at or
above the warning point, and real current still flowing in. This is the
exact pattern behind a battery management system tripping its protection
disconnect mid-voyage. The bank looks fully charged, the charging sources
don't know it, and the BMS eventually has to intervene on its own. A second,
tighter tier watches for the bank being driven further still, toward the
point a BMS would act.

Needs a house bank picked in Settings → Vessel → Power, with its cell count
and a battery profile linked from your equipment registry, so Helmcentral
knows what "full" and "too high" mean for your own pack chemistry.

## Engine differentials

For two or more engines running matched at the same rpm, each engine's
coolant temperature, oil pressure, boost pressure, load and transmission
readings are compared against the others. A gap that grows well past what
your own boat normally runs points at a developing fault on one side before
it becomes a breakdown. Port always running a little warmer than starboard
is normal and gets learned out; port suddenly running ten degrees warmer
than usual at the same rpm is not.

These alarms ship switched off until Helmcentral has learned your boat's own
normal gap from several weeks of history, and stay up once raised until a
steady run proves the difference has genuinely gone away, rather than
clearing the moment the numbers touch back down.

Needs at least two engines ticked in Settings → Vessel.
