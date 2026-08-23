# ADR 0052: Indicator Lamp Strip

## Status
Accepted

The fourth multi-instance widget, following ADR 0031 (embed), ADR 0039 (gauge) and ADR 0049 (gauge group). Depends on ADR 0050 for the CHK rollup to mean anything.

## Context

Every one of Dirona's N2KView displays carries the same thing along the bottom: a ribbon of indicator lamps, black for not-in-use, green for running correctly, yellow or red for a problem, with a CHK lamp at the end that says whether to go looking. It is the first level of their three-level scan, and it is the same on every screen precisely so that a glance always means the same thing.

Helmcentral's pages share nothing. Each is an independent array of widgets, and there was no way to express "a dozen states, densely, at a glance" — a gauge per boolean would have consumed the page.

An `AlarmBanner` already spans every panel, but it only appears when something is already wrong. The ribbon's job is different: it shows the things that are *fine*, which is what makes the one that is not stand out.

## Decision

### 1. A widget, not a new cross-page concept

`lamps:<token>`, config on the layout item as `lamps`, `omitempty`, reject-rather-than-drop validation. The prefix means no entry in either hand-maintained widget id list, as ADR 0039 predicted.

A pinned cross-page strip was the closer match to Dirona and was not taken. It would have been the first thing in the dashboard to live outside a page, needing its own storage, its own editing surface, and a decision about how it interacts with per-page layout. Duplicate (ADR 0049) already puts the same tile on every page in three clicks, and the cost of that choice — the copies do not stay in step when one is edited — is visible and reversible. Pinning can still be added later by promoting the config out of the layout item; nothing here forecloses it.

### 2. Three lamp states, not two

`on`, `off`, and **`no data`**.

A boolean is the easiest place to lose this distinction and the worst place to lose it. A generator lamp dark because the path reports zero and a generator lamp dark because nothing has ever reported that path look identical, and mean opposite things about whether you can trust the display. The third state renders at low opacity in the border colour and reads as "no data" to a screen reader, the same commitment as the gauges' structural dash.

`invert` handles signals whose healthy state is off — a bilge float, a fault line — and inverts only the on/off pair, never `no data`. Absence is not a state to be flipped.

### 3. The CHK rollup reuses `worstAlarmState()`

The rollup is not new logic. `worstAlarmState()` already existed for the alarm banner, and `useAlarms()` already exposed it as `worst`. The CHK lamp colours from it and opens the alarms drawer on click.

**This is only truthful because of ADR 0050.** Before gauge zones became the alarm source, a CHK lamp reading "all clear" while three gauges showed red would have been actively misleading — worse than no lamp, because it would have been trusted. The two ADRs are ordered for that reason.

A strip with no lamps but CHK enabled is valid: the rollup alone is a reasonable thing to pin to a page. A strip with neither renders nothing at all, and is rejected as a mistake rather than a choice.

### 4. Density, and the refusal to stretch

Sixteen lamps maximum, labels capped at twelve characters, and the row is `overflow-x-auto`. AGENTS.md is explicit that a widget must never stretch its parent, and a ribbon is the widget most likely to try: labels are operator-supplied and a phone is narrow. It scrolls inside the tile instead.

Grid constraints are `minW: 3, minH: 2` — wide and short, unlike every other widget, because that is what a ribbon is. Default placement is the full twelve columns.

### 5. `gaugeBoundPaths()`, for the third time

Lamp values ride the existing `gauge-values` event. No new stream, no subscription protocol — the walker collects lamp paths alongside gauge and group paths, into the same dedup map, so a path bound by both a gauge and a lamp is pushed once.

This walker has now had to learn about a new widget kind three times (ADR 0039, ADR 0049, and here), and each time the failure of forgetting is the same: the widget renders permanently blank with nothing in any log to say why. It now carries a comment naming it as the single point where a bound path becomes a pushed value, and a test per widget kind.

## Consequences

- The first level of Dirona's three-level scan exists: a dense row of states, with a rollup that says whether to look further.
- Duplicating a strip across pages leaves copies that drift when one is edited. Accepted for now, and the reason a pinned strip remains the obvious follow-up if it becomes annoying in practice.
- Lamps are on/off/absent only. A three-colour lamp driven by a value's zones — Dirona's engine temperature going red, then yellow, then green as it warms — is expressible today as a `bar` or `trend` gauge with zones, but not as a lamp. Worth revisiting once there is a real case for it.
- Four widget kinds now carry per-instance config on the layout item. The pattern has held without strain across embed, gauge, group and lamps, which is a reasonable signal it is the right one.

## Verification

`go test -short ./...` and the frontend suite pass. Backend tests cover the round trip, the caps, the check-only strip, mismatched config on every other widget id, and lamp paths reaching `gaugeBoundPaths()` deduplicated. Frontend tests cover one lamp per entry, on/off/absent as three distinct rendered states, `invert`, the CHK colour tracking the worst alarm, CHK opening the drawer, and the row scrolling rather than stretching.

Not yet exercised against a live vessel. The end-to-end check still owed: lamps lighting within the one-second `gauge-values` tick rather than staying dark, which is what proves the walker end to end.
