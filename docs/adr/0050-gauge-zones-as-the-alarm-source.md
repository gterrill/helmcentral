# ADR 0050: Gauge Zones as the Alarm Source

## Status
Accepted

Extends ADR 0038 (Alarms) and ADR 0039 (Bindable Gauge Widgets), which were built independently and never joined.

## Context

Helmcentral had two threshold systems that did not know about each other.

`GaugeZone` (ADR 0039) coloured a band of a gauge's range using the alarm severity vocabulary, with the intent that "a red band on a dial means the same thing as a red alarm". However, a red band did not raise, log or notify an alarm. Alarm rules (ADR 0038) carried separate thresholds and paths without colouring the dashboard.

Zones also had no editing UI. They were in the type, validated by the backend and rendered by every display kind, but setting one required hand-editing `dashboard-pages.json` and restarting.

The reference point is N2KView, where the three-level scan — glance at the colour, read the gauge, check the history — works because the colour *is* the alarm. An operator who drags an engine-temperature band into the red has said what they mean. Making them then go to a separate screen and re-enter the same number against the same path, in different units, is asking them to say it twice and keep the two in step forever.

## Decision

### 1. Derive rules at evaluation time, do not materialise them

`zoneDerivedAlarmRules()` walks `dashboardPagesState` on every evaluation tick and returns one `alarmRule` per alarm-severity zone. Nothing is written to `alarm-rules.json`.

This follows `gaugeBoundPaths()` exactly, and for the same reason: the backend already owns the page config, so a derived view of it has nothing to keep in sync. Materialising would have meant an upsert on every dashboard save, orphan cleanup when a gauge is deleted, and a migration for zones that already exist. Deriving has none of that.

Ids are `zone:<widgetID>:<gaugeIndex>:<zoneIndex>` — stable across restarts, which matters more than it looks: an id that changed between ticks would make the engine treat one steady alarm as an endless series of new ones.

`alarmEngine.evaluate` takes `rules []alarmRule` and needed no change at all. `evaluateAlarmsOnce` concatenates the two sources.

### 2. A zone is a direction and a threshold, not a free band

An `alarmRule` carries one operator and one value. A zone is a range. A band anchored to the bottom of the scale maps cleanly (`below` its upper edge); one anchored to the top maps cleanly (`above` its lower edge); **a band floating in the middle of the range has no single-threshold equivalent.**

Rather than accept such a band and quietly never alarm on it, the editor is built so it cannot produce one. Zones are edited as a direction (Below / Above) plus a threshold, and the stored `{from, to}` is derived from the gauge's own min/max. The stored shape is unchanged, so the renderer is untouched.

`validateGaugeConfig` still rejects a mid-range band, because the stored shape can also arrive from a hand-edited file, and a silent non-alarm is the failure this whole ADR exists to remove.

### 3. The backend needed unit conversion, and did not have any

Zones are authored in **display units** — oil pressure in psi, temperature in °C — because that is what the operator sets them against on the gauge scale. `alarmReader` reads **SI** from the SignalK snapshot: pascals, kelvin. Comparing 15 against 103421 produces an alarm that never fires and says nothing about why.

`lib/quantities.ts` is frontend-only, so `backend/quantities.go` now mirrors it in the toSI direction. Both directions are defined per unit and round-trip-tested against each other, and the table is asserted against the same values as `quantities.test.ts` — 35.0 psi ⇄ 241325 Pa, 1800 rpm ⇄ 30 Hz, 25 °C ⇄ 298.15 K — so the two cannot drift apart without a test failing.

An unknown quantity or unit is an **error**, never a pass-through. A pass-through here is exactly the silent psi-against-pascals comparison being guarded against.

Hysteresis is a *span*, not a point, so it is converted as the difference between two converted values rather than one converted value. On an offset scale like temperature, 2 °C of deadband is 2 K — converting it as a point would have made it 275.15 K, and the alarm would never have cleared.

### 4. Defaults that a zone has no opinion about

A zone says nothing about dwell or hysteresis, and both default to zero in the struct. With both at zero, a value hovering at a threshold can repeatedly raise and clear an alarm, a problem identified in ADR 0038.

Derived rules get a 10-second dwell and a deadband of 2% of the gauge's own range, so it scales with the scale.

### 5. Derived rules are visible but not editable

They appear in the alarms drawer alongside stored rules, flagged `derived: true`, rendered read-only with "From a gauge zone" and "Edit on the gauge". `PUT` and `DELETE` on a `zone:` id return 400 with that instruction rather than 404.

Displaying derived rules lets operators identify the source of an alarm. Edit controls are omitted because those requests would return 400, following `czone-switches-tile.tsx`'s treatment of role-gated circuits.

## Consequences

- Setting a red band on a gauge now raises an alarm through the full ADR 0038 pipeline: notification transports, acknowledge, silence, history.
- Zones are editable at all for the first time. ADR 0039's zone support went from unreachable to the primary way thresholds get set.
- The CHK rollup in ADR 0052 is truthful because of this. Without it, a lamp claiming "all clear" while three gauges showed red would have been actively misleading.
- Two unit tables now exist, in two languages. They are pinned to each other by shared test values, which is weaker than sharing code and stronger than nothing. Generating one from the other was considered and rejected as more machinery than 25 unit definitions justify.
- An operator cannot express "alarm when this value is between X and Y". No marine instrument alarm the authors could find works that way; unsupported bands are rejected rather than accepted without an alarm.
- Zone thresholds do not carry per-zone dwell or hysteresis. If one turns out to need them, they belong on the zone rather than as more defaults here.

## Verification

`go test -short ./...` and the frontend suite pass. `quantities_test.go` covers every unit in both directions plus the round trip; `gauge_zone_alarms_test.go` covers lower and upper bands, the psi→pascal conversion of the threshold, group members, normal bands producing nothing, id stability across calls, and the mid-range rejection.

Not yet exercised against a live vessel. The end-to-end check still owed: set a red band below 15 psi on a real oil-pressure gauge, confirm the gauge colours **and** the alarm banner raises from the same threshold.
