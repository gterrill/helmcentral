# ADR 0222: Mate gives a vessel status snapshot

## Status

Accepted (2026-10-28). Builds on
[ADR 0172](0172-mate-tool-registry.md) (the tool registry),
[ADR 0178](0178-one-reader-for-own-ship-telemetry.md) (the own-ship reader),
[ADR 0187](0187-equipment-items-own-their-signalk-source.md) (items own their
source) and [ADR 0214](0214-tank-levels-drive-the-tiles-and-the-alarms.md) (tank
levels). Reads the barometer figures of
[ADR 0095](0095-law-of-storms-barometer-rules.md).

## Context

Asked for "a quick status update", a competing logger answers with figures under
four headings (power, inside, outside, tanks) and closes with three small things
to note. Mate had no status tool and its prompt had no status rule. It called
`check_signalk_paths`, `get_active_alarms` and `get_planned_stay` together and
summarised freshness, because that is what those return. The answer had position
and alarms and no battery, tank, depth, solar or pressure figure.

The same competitor raised two things nobody asked about: a house bank costing
about 9 percent a night, and one sensor reading lower than its siblings. Mate
never speaks first. Helmcentral's anomaly rules cover impossible, frozen and
silent sensors, but nothing covers an overnight draw above the usual one, and
an alarm would be the wrong weight for it.

## Decision

**One tool, `get_vessel_status`, builds the snapshot in Go.** Mate copies the
figures. It does not choose paths. Every field is read the way the tile that
shows it reads it.

- **Power.** The house bank is the role's item (`vesselView.houseBankPath`): state
  of charge (a 0 to 1 ratio is scaled to percent as the Battery and Power tile
  does), voltage and signed current. A charge outside 0 to 100 percent is
  "impossible reading", not clamped. Solar is the Victron total when the boat
  publishes one, else the sum of the controllers. DC load is the first of the
  Venus, `dc.12v` and `loads.12v` power paths that the boat publishes, and it
  does not fall through to the next if that one is stale. A negative solar or DC
  load is "reading is negative", not clamped.
- **Tanks.** One row per equipment item whose source is a tank, with the level
  (`tankLevelPercent`, the one place that decides which scale a number is on),
  the warn and alarm levels the item resolves to (`resolveTankLevels`), whether
  alarms are off, which end is the bad one, and the state of its `tank:` alarm.
- **Surroundings.** Depth and true wind come from the vessel state, so they keep
  its own recency gates. The gust is the strongest true wind in the Wind tile's
  10 minute buffer. Pressure is `environment.outside.pressure`. Its three hour
  change is the derived `helmcentral.environment.pressureChange3h`, the figure the
  Law of Storms alarms judge. It is given only with a fresh pressure behind it and
  about 3 hours of record (2.5 hours, the span `tendencyOverWindow` accepts);
  until then it reads "pressure history still filling (N min of 3 h)". The
  derived path itself exists after 30 minutes, which is right for the alarms and
  too short to call a 3 hour change. `barometerHistory` now records only a
  pressure under 5 minutes old, the gate the true wind buffers beside it
  already had, so a silent barometer no longer repeats its last value.
- **Boat.** Each configured engine is running, off or unknown. Speed is read from
  `.revolutions` only, as the Engine Cluster does. An engine whose
  readings have stopped is off only when the engine state says so (ADR 0163).
  Silence alone is unknown. The anchor watch gives down or not, its radius and
  when it was set, and how far the boat is from it, or `distance_missing` when there is no fresh position.
- **Alarms.** Active alarms by the presenter's label, message and state, and the
  titles of ignored network alarms (ADR 0207).
- **Sensor checks.** The anomaly detector's frozen, impossible and silent counts
  with the named sensors, and nothing when its last tick is over 5 seconds old.
- **Planned stay.** The stay with its straight line distance and true bearing from
  the boat, as a fact in the result, so a stay on the wrong side of the coast does
  not rely on Mate noticing.

**A reading that cannot be given is absent, with a reason.** The shape is a
`missing` string in place of `value` and `display`. Ages are checked against
2 minutes for live readings (the age the tank alarms already call stale) and 5
minutes for the barometer and the position. A reading older than that, or with no
timestamp, is "no fresh reading", and the old value is not in the result. A
solar total with one silent controller is withheld, because the sum of the rest
would be a guess. A roles or telemetry dependency that is not wired, or SignalK
having delivered nothing, fails the call. Nothing is estimated and nothing falls
back (AGENTS.md).

**Values are SI plus the display figure in the operator's units**, through the
same `chartDisplayFor` the charts use. Pressure is given in hectopascals on both
systems, because the barometer alarms are written in them.

**Observations are deterministic and are never alarms.** They sit in the same
result under `observations`, and `observations_unchecked` says which could not be
looked at and why, so an empty list is not read as an all-clear for them.

The one observation is the overnight house draw. `computeOvernight` now also
reports the last completed night's own rate and the usable nights before it,
which the dawn projection's median already scanned, so the comparison does not
include the night being judged. It is raised only when all three hold:

- at least **3** usable earlier nights (the projection will take 2, but two nights
  can both be unusual),
- last night drained at least **1.5 times** the median of those nights, and
- by at least **0.25 percentage points of charge an hour** more than that
  median (about 2 points over an eight hour night), so a tiny draw does not
  produce a big ratio.

Nights with shore power, generator running, a data gap or a net rise are
excluded by the existing scan, so a charged night is never "last night". With
fewer than 3 earlier usable nights the check is listed in
`observations_unchecked` ("only N earlier usable nights, need 3"). The
summary states the two rates, the ratio and the number of nights, and no cause.
"Last completed night" is exact: a night still in progress is not scored.

**Left out.** A sensor reading outside its siblings' range and a source that has
been silent since install were not built. The detector's sibling logic covers
engines only, and "never reported" needs the source signature history joined to
the silent-source list, which is more than a snapshot field. Both stay
candidates for a later cycle. The routine run (item 6 of the plan) is a
separate cycle and will read this result.

**Prompt.** "Status", "how's the boat", "quick update" and similar call the tool.
The answer gives figures under Power, Tanks, Outside and Boat, then at most three
things worth noting, drawn from the observations, the sensor counts and the
missing readings. An observation is put as a question, with its figures, and
never with a guessed cause. A chart is offered, not drawn unasked.

## Consequences

- One tool call replaces a guess across four. The result is capped by
  `capToolResultJSON` and is well under the limit with ten tanks and ten alarms.
- A boat with no InfluxDB, or no house bank role, still gets a status. The
  observation shows as unchecked with the reason.
- The gust window and the pressure change are in memory and restart empty. The
  result says so in the field's note or reason.
- The snapshot's tests use hand-built telemetry (`fakeTelemetry`) whose figures
  are taken from the live self tree captured in
  `testdata/anomaly/signalk-self-snapshot-2026-09-25.json`. They are not a replay
  of the capture itself, which carries no timestamps on its leaves.
