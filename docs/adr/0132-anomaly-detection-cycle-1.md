# ADR 0132: Anomaly Detection, Cycle 1

## Status

Accepted (2026-09-25). Complete and tested end to end: sensor health
(frozen/impossible/silent-source), full-bank charging (including its
dV/dt-driven level 2, a trailing 60s voltage history the detector goroutine
holds), and engine differentials, each a derived-path producer alerted
through ordinary seeded `alarmRule`s; the `vessel:` settings block; a new
`battery` equipment-profile kind; the candidates endpoint; the ignore-sensor
list; and the Settings -> Vessel UI (`boat-ui-section.tsx`'s Engines/Power
fieldsets) and the alarm card's own "Ignore this sensor" action, both built
in a second pass on top of the already-tested backend API. Not yet run: the
2026-09-21 dead-ship replay against real InfluxDB (no token in the build
worktree) and the on-boat verification pass the plan's own checklist calls
for. See this ADR's Consequences for the complete list.

## Context

Pikorua went dead in the water docking on 2026-09-21: the house bank sat at
100% SoC for four days on shore power, both alternators then drove it to
29.0-29.1 V anyway, the BMS's high-cell-voltage protection opened the
battery's remote-disconnect switch, and the inverter/charger dropped out
with it. Nothing in Helmcentral was watching for a bank being charged past
where it needed to be.

An earlier proposal built streaming CUSUM/Welford change-point detectors
with their own YAML config and a SignalK publisher. It didn't fit this
codebase: there is no event bus here, and the alarm engine
([ADR 0038](0038-signalk-notification-lifecycle-alarms.md)) already handles
dwell, hysteresis, acknowledge, notification transports and the alarm log.
Half its proposed detectors can't run on Pikorua anyway - no fuel vacuum
sender, no bilge instrumentation, no per-cell data, and the exhaust
temperature senders are frozen (a real, physical sensor problem this cycle's
own frozen-sensor check would now catch). That critique stood, so this cycle
rebuilds on this codebase's existing pattern instead: a detector computes a
number, publishes it as a `helmcentral.*` derived path
([ADR 0055](0055-host-derived-vessel-paths.md)), and an ordinary seeded
`alarmRule` watches it, the same shape every other alarm source in this
codebase already uses.

Three detectors, chosen because Pikorua's own instrumentation supports them
and each maps to a real failure mode: a stuck, out-of-range or newly-quiet
sensor; the 2026-09-21 pattern of charging into an already-full bank; and a
twin (or N-way) engine's coolant/oil pressure/boost/load pulling away from
its peers at matched rpm.

The standing rule for this whole cycle: nothing Pikorua-specific ships in
code, `settings.example.yaml`, or a seeded rule. A fresh install starts with
every detector unset, and the operator sets their own vessel up from what it
actually publishes, through the UI (or, until that UI exists, the API this
ADR ships).

## Decision

### Sensor health: range, frozen, silent source

`anomaly_sensor_health.go`'s `physicalLimits` table is a SI-unit sanity
range per engine/battery quantity (`classifyRange`) - not an alarm
threshold, a "this cannot be a real reading" floor/ceiling (a 0 K coolant
reading, a 400 V house bank). It is scoped to `propulsion.<id>.*` and
`electrical.batteries.<id>.*` leaves only, inferred from whatever the boat
happens to publish (`discoveredEngineBatteryPaths`), so the impossible-
reading and silent-source checks need no vessel setup at all - every
install gets them.

`frozenVerdict` fires when an engine-correlated path (coolant, oil
pressure, boost, load, transmission) kept updating, its raw value never
changed, and every configured engine held steady well above idle with a
real rpm swing over the same 15-minute window - a value that should have
moved but didn't, correlated against proof the engine was actually doing
something during that window. The window itself is a tumbling accumulator
(`frozenTrackerState`) owned by the single detector goroutine, not the
concurrently-read derived-paths computation (`computeDerivedPathsFromTree`'s
own doc comment explains why that function cannot hold state across ticks).
Frozen needs at least one configured engine, since its gate reads every
configured engine's rpm.

`silentSources` reads `signalKSnapshot`'s new `sourceSeen` map (written in
`applyDelta`, one entry per `$source` per context) and flags a source that
established a real publishing cadence (5 minutes, 30 updates) and then went
quiet for over 120 seconds - suppressed entirely while the snapshot's own
last message is itself more than 10 seconds old, so a dead SignalK
connection reads as the connection outage it is, not as every source going
silent at once.

`inputValidity` is the single lookup every other detector trusts before
publishing anything from a given path: present, not stale, and in range.

### Full-bank charging

`anomaly_battery.go`'s `fullBankChargingLevel` is the rule the 2026-09-21
incident is named after: SoC at/above the battery profile's `full_soc`,
pack voltage at/above `charge_warn` x cell count, and charge current at or
above 2% of bank capacity (C/50, so the threshold scales with bank size,
never a fixed amp figure) is level 1; adding overvoltage
(`charge_high` x cells) or a rising pack voltage (+5 mV/min over 60s) is
level 2. It reads the bank's own shunt current, not a configured list of
alternators/chargers - any charge source the boat happens to publish only
enriches the evidence text ("port alternator 21 A"), discovered rather than
configured. `HighVoltage` (and therefore level 2) is optional, matching a
profile whose high-voltage slot is not yet filled; `WarnVoltage`
(`FullSOC`/`CapacityAh` too) is not - any of those absent means "house bank
not configured," not "threshold zero."

The rising-voltage half of level 2's OR condition needed its own state:
`voltageHistoryTracker` holds the house bank's trailing 60 seconds of
voltage samples (goroutine-owned, the same reason `frozenTrackerState` and
`conditionTracker` are), pruned each tick down to the window but keeping
one anchor sample at or before the cutoff so the window's effective span
stays close to 60s rather than shrinking every tick as samples inside it
age past the cutoff too. `computeAnomalyBattery` feeds it the bank's own
voltage and reads back volts per minute for `DVdtPerMinute`; a first
reading, or a gap while the bank was unconfigured, reports 0 (unknown),
which only ever fails to promote to level 2, never blocks level 1.

### Sensor-health evidence names the offending identifiers

The three sensor-health counts are alarm-worthy on their own, but a bare
"2 out-of-range readings" gives an operator nothing to act on. Each tick
now also collects which paths (impossible-reading, frozen) or `$source` ids
(silent-source) actually tripped it and joins them into that path's
`Evidence`, comma-separated - the same field the battery and engine
detectors already populate, read the same way by the alarm card and by the
"Ignore this sensor" action below, which parses that list back out.

### Ignore this sensor, in the UI

`alarm-display.ts`'s `ignorableSensorIdentifiers(alarm)` recognises the
three sensor-health count paths and splits their evidence into individual
identifiers; `alarms-drawer.tsx` renders one small action per identifier,
calling the already-tested `POST /api/alarms/ignored-sensors`. Both the
card action and the alarm's own "Settings -> Alarms" review list
(`ignored-sensors-list.tsx`) share one hook, `useIgnoredSensors`, so an
identifier ignored from a card and one reviewed or removed from Settings
are reading and writing the same state, not two copies of it.

### Settings -> Vessel: Engines and Power

`boat-ui-section.tsx` gained the Engines and Power fieldsets on top of
`useVesselSettings` (wrapping the already-tested candidates/GET/POST
endpoints): a row per discovered-or-configured engine instance with its
live rpm/coolant, a linked-item-plus-profile picker
(`equipment-profile-linker.tsx`, shared by both fieldsets, since "pick an
existing registry item or create one, then assign its profile" is the same
interaction for an engine and for the house bank), and a per-detector
ready/not-ready line read straight from the candidates response. Saving is
its own button, posting directly to `/api/vessel` - a separate save path
from the page's pinned "Save Settings" button, the same split the Widgets
provider cards already use, since vessel settings are not part of that
button's own patch.

"Apply gauge zones" reuses `EngineProfileDialog` outright rather than a
second copy of it, with two additions: `initialProfileId`/`initialInstance`
props that seed the dialog once, marking the instance as already touched so
the dialog's own live-path auto-seed effect never second-guesses it, and a
filter dropping `kind: 'battery'` profiles from every gauge-tile picker
(`EngineProfileDialog` and `EngineClusterConfigDialog` both list from
`useEquipmentProfiles`/`useEngineProfiles`, which now also return battery
profiles) - a battery profile carries no `gauges` at all, so without the
filter, picking one crashed the first render that tried `profile.gauges.map`.
Settings has no notion of "the active dashboard page" the way the dashboard
itself does, so applying zones from a Vessel row lands the new tile on the
first dashboard page; moving it afterward is a normal dashboard edit, not a
missing feature.

### Engine differentials

`anomaly_engines.go`'s twin gate
(`twinGateHolds`) requires every configured engine at or above 900 rpm and
within 50 rpm of the group, every coolant at or above 65 degC, both held
continuously (60s rpm, 300s coolant - the caller's own windowed
`conditionTracker`, the same tick-owned-state split frozen uses), and every
engine running at least 10 minutes. Once the gate holds, each engine's
residual (`residualQuantity`) is its own reading minus the median of its
peers, minus a learned offset for the current 200 rpm bucket - absent
entirely without a learned bucket, which is itself absent until 30
qualifying minutes have accumulated in it. `learnTwinBaseline` aggregates
already-fetched, minute-aligned `[]telemetryPoint` series (the real type
`queryInfluxPathRange` returns) into those buckets, requiring a 5-minute
unbroken qualifying run before any minute counts, so a boat's genuine
23.9L/h-at-2400rpm cruise teaches the boat's own bias rather than a
transient. `data/anomaly-twin-baseline.json` persists the result;
`startTwinBaselineRefresher` relearns it every 24h from 14 days of 1-minute
Influx history, fetched in 7-day chunks against `queryInfluxPathRange`'s own
6-second query budget. A relearn failure (no InfluxDB configured, a query
error, too little data) is logged and keeps whatever baseline was already
loaded - never zeroed, which would silently mute every residual.

For exactly two engines the residuals are exact mirror images (A-B and
B-A), so seeding binds one alert pair per quantity to the first configured
engine only; for three or more, each engine is compared against the median
of the rest and is independently informative, so every engine gets its own
pair (`enginesToSeedResidualRulesFor`).

### `deltaK` / `deltaPa`: difference units, not absolute ones

`alarm_units.go`'s existing `"K"` entry converts absolutely (subtracts
273.15), correct for a coolant *reading* and wrong for a coolant
*residual* - a genuine 4 K gap between two engines would print as -269 degC.
`deltaK` (scale only, °C label) and `deltaPa` (scale only, kPa label - the
unit an operator reads an oil/boost pressure gap in, not `Pa`'s own mb,
which is for barometric pressure) are new SI-ish unit strings carried by
every residual path, added to both the backend
(`alarm_units.go`/`quantities.go`) and frontend
(`alarm-display.ts`/`quantities.ts`) tables together, so an alarm card and
an ntfy push read the same sentence. `engineLoad`'s residual reuses the
existing `ratio` unit unchanged - a ratio's conversion is already scale-only,
so it was never broken.

### `vessel:` settings and the `1440` leak

A new `vessel:` block in `settings.yaml`
(`vessel_settings.go`, load/save/validate, independent of the large
`settingsPayload` struct the same way `loadSettingString`/`loadSettingFloat`
already are) holds `engines: [{instance, name, equipment_id}]` and
`house_bank: {path, equipment_id, capacity_ah, cells, warn_voltage?,
high_voltage?}`. Both link to an inventory registry item's `profile_id`
rather than duplicating a profile - the same registry item edited from
Settings -> Vessel or from Inventory, never two copies that drift
([ADR 0102](0102-equipment-profiles-and-the-duplicate-path.md)'s "one home
for the profile" carried forward).

Fixing this cycle's own instance of the leak it was built to prevent:
`main.go`'s `defaultHouseBatteryCapacityAh = 1440` was Pikorua's real bank
size baked into shipped code, the fallback of last resort for the overnight
electrical estimate. It is gone outright - capacity now comes only from
`vessel.house_bank.capacity_ah`, and unset reports "not set", the same `-1`
sentinel every neighbouring field in `electricalStateData` already uses for
unknown, not a guessed number.

### Battery equipment-profile kind

A fourth `engineProfile.Kind` (`engine_profiles.go`), alongside engine/
alternator/generator: `chemistry` plus three thresholds (`full_soc`,
`charge_warn`, `charge_high`), each a nilable `{value, source, note}` slot
exactly like an engine zone's nil threshold
([ADR 0102](0102-equipment-profiles-and-the-duplicate-path.md)). A filled
value with no `source` citation is rejected outright - this number feeds
the alarm about the failure mode that started this cycle, so an uncited
guess is a safety defect, not a style nit. `batteryPackThresholds`
multiplies the per-cell voltages by the house bank's own cell count; the
vessel settings' own `warn_voltage`/`high_voltage` override the computed
pack figure when the operator sets one. No bundled battery profile ships in
this cycle - citing a real datasheet's numbers needs the operator's own
battery model in hand, so every install starts with a from-scratch or
operator-authored profile rather than a fabricated citation.

### One goroutine, one slot, ordinary seeded rules

`anomaly_detector.go`'s `startAnomalyDetector` runs all three checks once a
second, using vessel settings, the linked battery profile and the cached
Influx-learned baseline, and writes one `anomalyReading` (values, evidence
sentences, per-input validity, one shared timestamp) into `globalAnomalySlot`
- the same mutex-guarded-slot-plus-background-goroutine shape
`forecast_warnings_fetcher.go`'s `globalForecastWarningsSlot` already
established, needed here for the same reason: the rolling windows (frozen,
twin gate steadiness) have to live somewhere a concurrently-read function
cannot hold them. `addAnomalyValues` (`derived_paths.go`) folds the slot into
the ordinary derived-values map every `computeDerivedPaths` tick already
builds, absent entirely once the slot is more than 5 seconds stale. The
per-engine residual paths are the one genuinely dynamic derived-path set in
this codebase - their count depends on `vessel.engines` - so they are not in
`derivedPathIDs`' static list; `unitForAlarmPath` and the path picker
(`signalk_paths.go`) both fall back to a pattern match against the known
quantity suffixes instead.

`alarm_seed_anomaly.go` seeds `anomaly-v1` as several independent markers,
not one: impossible-reading and silent-source need no setup and always
ship enabled; frozen needs one engine; full-bank charging needs a complete
house bank (ships its warn tier enabled - it is the 2026-09-21 rule itself
- and its near-trip tier disabled until tuned); each engine's residual pair
needs that engine configured and ships disabled, per the plan's own
operator decision that a twin alarm should stay up until a steady run
proves it clear, not auto-clear on the first crossing. `seedAnomalyRules`
is safe to call repeatedly - at startup, and (once the frontend calls it)
on every vessel-settings save - so an engine added six months later gets
its own rules exactly then, with every marker it already earned left alone.

### Evidence: frozen at raise, not live

`alarmStatus` gains an `Evidence` field alongside the existing collision-only
`Encounter`. Unlike `Encounter`, which stays live because it exists to guide
what happens *next*, `Evidence` is captured once, from
`evidenceFor(rule.Path, now)`, in the same `advanceAlarmRule` transition
that raises the alarm and builds its message - an anomaly reading is a
snapshot of what tripped the alarm, not a running commentary. It is reset
on clear, so a later, unrelated occurrence of the same rule never inherits
stale evidence. `alarmMessageFor` appends it as its own clause, so ntfy,
email and the SignalK bus carry the same "why" the alarm card does; the
frontend's `ActiveAlarm.evidence` renders it as a muted secondary line right
under the condition sentence, the same treatment `encounter` already gets.

### Ignore this sensor

`data/alarm-rules.json` gains an `ignored_sensors` list alongside its
existing seed-marker list
(`alarm_ignored_sensors.go`), holding either a SignalK path (excluded from
the frozen check) or a `$source` id (excluded from the silent-source check)
- one list serving both checks, since a path string and a `$source` string
never collide. `POST`/`DELETE /api/alarms/ignored-sensors` exist and are
tested; the alarm card's own button that calls them is not yet built (see
Consequences).

## Consequences

- An operator gets three working detectors and an ordinary alarm-rule
  interface to tune them, with nothing Pikorua-specific anywhere in shipped
  code or config - `settings.example.yaml` and a from-scratch
  `backend/settings.yaml` both load with every detector reporting "not set
  up" and zero anomaly rules seeded.
- Re-entering house bank capacity under Settings -> Vessel -> Power is a
  one-time, deliberate breaking change on upgrade (see CHANGELOG) - the
  price of removing a hardcoded fleet-of-one number from shipped code.
- Setting a vessel up is a Settings -> Vessel workflow end to end: tick
  engines, link or create an inventory item and its profile per row, apply
  gauge zones with the instance already known, pick the house bank from its
  live readings, link its profile, set cells/capacity/overrides. A dead
  sensor is excluded from its own alarm card's "Ignore this sensor" action,
  reviewed and reversed from Settings -> Alarms -> Ignored sensors.
  `docs/how-to/set-up-your-vessel.md` walks the whole thing.
- `EngineProfileDialog`/`EngineClusterConfigDialog` gained an
  `initialInstance`/`initialProfileId` seed path and now filter
  `kind: 'battery'` profiles out of their own pickers - a battery profile
  carries no gauges, and would otherwise crash the first time an operator
  applied one through either dialog by mistake.
- The 2026-09-21 regression itself - replaying the real incident against
  InfluxDB and confirming the warn level fires ahead of the BMS trip - has
  not been run. This build worktree has no InfluxDB token, and reconstructed
  or estimated telemetry was deliberately not used to fake the test (see
  `anomaly_battery_test.go`'s removed regression case). It is the first
  thing to run once real Influx access and the on-boat setup below are
  available.
- Also pending, all on-boat and undone in this cycle: the dev-stack fresh-
  install check, watching the silent-source alert raise and clear against a
  real pulled source, Pikorua's own vessel setup through the UI (ticking
  port/starboard, resolving `batteries.0` vs `239`/`512` by comparing live
  current, excluding the dead exhaust senders), and `/code-review`/
  `/security-review` on the committed diff.

## Related

- [ADR 0055](0055-host-derived-vessel-paths.md) for the derived-path pattern
  every one of this cycle's outputs follows.
- [ADR 0050](0050-gauge-zones-as-the-alarm-source.md) for the
  zone-threshold-to-SI-alarm conversion `batteryPackThresholds` extends into
  a new profile kind.
- [ADR 0102](0102-equipment-profiles-and-the-duplicate-path.md) for the
  nil-threshold-is-a-slot idiom the battery profile's `full_soc`/
  `charge_warn`/`charge_high` reuse, and the "one home for the profile"
  registry-link pattern `vessel.engines`/`vessel.house_bank` both follow.
- [ADR 0038](0038-signalk-notification-lifecycle-alarms.md) for the alarm
  engine's dwell/hysteresis/acknowledge state machine every seeded
  `anomaly-v1` rule runs on unmodified.
- [ADR 0083](0083-ages-ride-the-gauge-values-stream.md) for the staleness
  contract `anomalySlotMaxAge` and `inputValidity` both follow.
