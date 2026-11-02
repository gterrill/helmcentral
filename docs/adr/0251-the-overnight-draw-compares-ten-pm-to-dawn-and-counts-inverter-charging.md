# ADR 0251: The overnight draw compares 10 PM to dawn and counts inverter charging

## Status

Accepted (2026-11-03). Amends [ADR 0222](0222-mate-gives-a-vessel-status-snapshot.md)
(the overnight observation) and the generator screen described in
[ADR 0182](0182-electrical-instances-and-solar-names-are-vessel-settings.md).

## Context

On 3 Nov the morning report said last night drained the house bank at 2.52
percent an hour, 1.6 times a usual 1.61. The usual figure was flattered. On 24
to 27 Oct the bank charged at +130 to +150 A in the evening, inside the night
window, and the scan did not drop those nights.

Two checks should have caught them and neither did.

- **The generator screen read the wrong signal.** `electrical.generator.0.stateNumber`
  stayed 0 the whole time the generator ran. On 25 Oct
  `electrical.inverters.276.acin.power` read about 8000 W until 19:30 boat time
  and then 0, which is the generator feeding the Multi. A screen on
  `stateNumber > 0` can never fire on this boat.
- **The net-rise backstop saw nothing.** State of charge still fell across the
  night, because the charging stopped by 19:30 and the bank spent the rest of it
  discharging.

The steady late-night current was about -28 A on 23 to 25 Oct, -38 A on 26 Oct,
-35 A on 27 Oct, -33 A on 1 Nov and -26 A on 2 Nov. Last night was not unusual late at night. It had a
heavy evening with no charging, set against baselines that included charged
evenings. Measured from 22:00 to dawn, 2 Nov drained about 1.9 percent an hour
against about 2.1 to 2.6 on 25, 26 and 27 Oct and 1 Nov, so the finding would not
have fired. The 23 and 24 Oct readings were not checked.

## Decision

**Charging is the inverter's AC-in power, from any source.** A sample above
`inverterChargingPresentWatts` (100 W) counts as charging. An idle inverter
reads exactly 0 W here, and the generator run was about 8000 W, so 100 W sits
well clear of both. The inverter is the one the roles name. No setting is added.
This replaces the generator `stateNumber` screen, and the generator role no
longer feeds the overnight model.

**The shore charger screen stays.** The Skylla chargers feed DC, so their AC-in
current is not the inverter's AC-in. The Multi is 60 Hz and the 50 Hz shore feed
goes only to the Skyllas. Dropping the shore screen would put marina nights
back in the baseline.

**Charging that cannot be checked is said, not assumed away.** With no inverter
role, or an inverter with no history, the result reports `charging_checked:
false`, the finding says "Charging was not checked", and nights stay in. There
is no fallback signal and no guess. The Battery & Power tile's Dawn hover text
says "charging not checked" too. With some inverter history, each night is
judged on its own: a night with no inverter sample inside its window is left out
as "its charging could not be checked", because a clean slope proves nothing
about charging when nothing was logged. The inverter was seen logging 0 W while
idle on 24 to 27 Oct, and the history query keeps every bucket holding a sample,
so an idle night counts as checked. If the logger ever stops writing unchanged
values, idle nights will show as unchecked and the fix is in the logger.

**The rule compares 10 PM to dawn, boat time.** For the `overnight_draw` finding
only, last night and every comparison night are measured from 22:00 boat time to
the existing dawn end of the night window (15 minutes before sunrise). A night
with shore power or charging inside that window is left out. A night with
charging only in the early evening stays in, measured from 22:00. The 80 percent
sample-completeness test is relative to the shorter window. A night that opens
after 22:00 keeps its own start, and so does one that opens after midnight (a
high-latitude summer), which belongs to the evening before. The rule stands on
its own nights: it does not wait for the tile's whole-night scan to find
history, and a boat that runs the generator most evenings still gets a finding. The narrow window is a new function,
`ruleNightWindow`, and `recentNightWindows` keeps its meaning. Boat time is the
zone `computeOvernight` already receives.

**The Battery & Power tile's Dawn figure keeps the whole night.** It answers
"will the bank hold until morning", and tonight's evening loads are part of that.
It uses the same charging screen, so a night with charging anywhere in it is
excluded. The estimate can become more pessimistic. That is intended, and no
fallback is added.

**The finding shows its working and offers no cause.** It says what it compared,
and says when the fall came if one two-hour stretch holds more than half of the
window's fall. It counts the nights it left out for charging and for shore
power, each on its own. The stretches
are two hours from the window's start, and a last stretch under an hour joins the one
before it.

**Mate is given the nights.** The observation lists each earlier night's
boat-local date, its rate if it was compared, why it was left out (shore power,
charging, charging that could not be checked, a gap in the data, the charge
rising) and where the boat was. The
place comes from the Passages logbook: the arrival place of the last passage
before the night, if no passage was under way (finished or still open) and no
hidden passage moved the boat since. Otherwise it is "not known". A logbook
that is absent gives "not known"; one that cannot be read is logged and listed
in `observations_unchecked`, and the finding stays. InfluxDB is not involved, so the place
does not depend on it. The same list reaches the watch review in the power
section's figures. Both prompts tell Mate to check an explanation the skipper
offers against these nights, say plainly when it does not explain the figures,
and never say a charger or shore power affected the baseline unless the list or
the data shows it.

**The thresholds do not move.** The 1.5 ratio, the 0.25 point minimum and the
three-night minimum stay. Mate cannot remove a finding the rules reached, and a
real rise keeps reporting every morning.

## Alternatives rejected

- **A: drop whole nights with charging anywhere in them.** It is simple, and the
  tile does it. For the rule it throws away nights whose late hours are clean, so
  on this boat, where the generator runs most evenings, almost no nights would
  remain to compare.
- **B: start each night from when charging stops.** It uses more of every night,
  but the start differs night to night, so rates cover different hours with
  different loads. It also needs a reliable end-of-charge time, and the cooking
  and lighting tail of the evening moves the baseline around. A fixed 10 PM start compares like with like.
- **Read the generator role's `stateNumber` better.** The value does not change
  while it runs, so there is nothing to read.

## What it cannot know

- A charge below 100 W, or one that left no sample in a 15-minute mean.
- A source that charges the bank without passing through the inverter, such as
  a generator feeding standalone chargers. The operator confirmed this boat's
  generator feeds the Multi, and chose to replace the generator check rather
  than keep both. The net-rise check still backstops a night that rose.
- The zone a past night was spent in. Every night uses the boat's zone today, so
  after crossing a zone an older night opens at the wrong local hour until it
  ages out of the comparison, a week or two later. Finding each night's zone
  would take a position for every night and a dependency on position history,
  for a case that matters only just after a crossing. The whole-night scan has
  the same limit.
- Where the boat was on a night the logbook does not cover, or when a passage
  was never recorded. The place reads "not known".

## Consequences

- The overnight result renames `generator_checked` to `charging_checked` and
  `nights_excluded_generator` to `nights_excluded_charging`, and adds
  `nights_excluded_charging_unchecked`. The frontend reads the new names. There
  is no compatibility alias.
- `get_vessel_status` observations carry the window, the nights and the places.
  They grow with the number of nights scanned, up to 30.
- A boat with no inverter role keeps working and the finding says it did not
  check charging.
