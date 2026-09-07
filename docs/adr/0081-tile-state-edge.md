# ADR 0081: The Tile Carries a State

## Status
Accepted

## Context

ADR 0080 stopped Helmcentral's dials and lamps from lighting up whether or
not anything was wrong. It left a gap the same evaluation of MV Dirona's
N2KView screen also names: the alarm signal in a tile lives inside it, in a
zone-coloured numeral or a rim on a dial, and finding it means reading every
tile in turn. N2KView's answer is the ribbon along the bottom, already built
here as ADR 0052 and about to be pinned across pages (a later phase). The
ribbon covers configured lamp paths. It does not cover a reading that only
exists inside a gauge, a gauge group, an engine cluster, the battery state of
charge, or a tank level, which is most of the dashboard.

Two of Helmcentral's existing widgets already had exactly this signal
computed and were only using it locally: `GaugeBody` already knows which zone
a reading is in (`activeZone`), the engine cluster's telltales already know
their own tier, and `TanksTile` already has a tone per tank. None of it
reached the tile's own border, so a tile with an alarm-zone reading buried
in a corner looked identical to a tile with nothing wrong, from the distance
a helm screen is read at.

## Decision

### 1. `Tile` gets a `state` prop

`state?: ZoneState | null`. When it is a named severity the operator actually
configured (`alert`, `warn`, `alarm` or `emergency`) and the tile is not
`stale`, three things happen: `severityBorderClass(state)` joins the card's
class list, `data-state={state}` is set on the card, and a small dot
(`severityFill(state)`) renders inside the same flex row as the header's
hairline rule, at its right end, before `titleExtra`. The rule and the dot
share one grid child in `CardHeader` rather than the dot being a sibling of
its own: `CardHeader` is a grid with no explicit column track for an
auto-placed item, and an early version of this change added the dot as a
bare sibling span, which put it on its own row under the title at the far
left instead of at the end of the rule. Wrapping the rule and the dot in one
`<div className="flex min-w-0 flex-1 items-center gap-2">` keeps the grid
child count exactly what it was before the dot existed.

`stale` wins outright: no border class, no dot, no `data-state`. This is the
same call ADR 0068 already made about the readings themselves. A value the
tile cannot currently vouch for should not also assert a state, alarm or
otherwise. `stale` and `state` can be passed together (a caller does not have
to gate one on the other); the suppression happens once, inside `Tile`.

`outside` is excluded the same way `normal` and `null` are, and for a
related reason: it is not a severity the operator configured, only a
reading past whatever band they did configure. A screenshot of the live
dashboard's two engine clusters, both idling, both bundled with the stock
Cummins profile that leaves warn and alarm thresholds null (ADR 0054 §5a),
showed a permanent amber edge and dot on a perfectly healthy pair of
engines: every reading above its normal band reads `outside`, all day,
because nobody has entered the manual's actual thresholds. An edge that is
amber all day on a healthy engine is precisely the noise floor ADR 0080 spent
its whole effort removing from the dial; `Tile` cannot reproduce it one layer
up. `worstZoneState` still returns `outside` and `severityBorderClass` still
has a mapping for it, for a caller that wants either; `Tile` itself simply
does not act on it.

Colour is spent the same way the rest of the board already spends it:
`normal`, `outside` and `null` all draw nothing. A healthy tile, and an
unconfigured one, both look exactly as they did before this ADR.

### 2. The ladder and its ranking live in `lib/severity.ts`

`ZoneState` is `AlarmState | 'outside'`: every existing alarm state, plus
`cluster-readings.ts`'s `outside` (a reading past every configured band, with
no band claiming it). `worstZoneState(states)` ranks by the same
`ALARM_STATES` order the alarm banner already uses, `outside` tied with
`warn` and losing that tie to a literal `warn` when both are present. That is
the same reasoning `severityTextClass` already applies at the single-reading
level, now applied across a whole tile's readings. Entries of `null` or
`undefined` are readings with nothing to report and are dropped rather than
treated as `normal`; the result is `null` only when every entry is like that.

`severityBorderClass(state)` is the fourth function on this ladder, beside
`severityFill`, `severityTextClass` and `severityClass`: `''` for `null`/
`normal`, `border-sky-500` for `alert`, `border-amber-500 dark:border-amber-400`
for `warn` and `outside`, `border-red-500 dark:border-red-400` for `alarm`,
and `border-red-700 bg-red-500/10 dark:border-red-500` for `emergency`. The
last one carries a wash as well as a border, because an emergency is the one
state SignalK will not let the operator silence or acknowledge away, and
`severityClass` already gives it the same extra weight for alarm text.

The rank lookup is computed at call time from `ALARM_STATES.indexOf(...)`
rather than a table built once when the module loads. `severity.ts` is a leaf
module most of the dashboard imports, directly or through `Tile`, and a table
built eagerly from `@/hooks/use-alarms`'s export would make every one of
those importers depend on that hook's module being fully present the instant
`severity.ts` loads, including inside a test that mocks the hook down to
only the two functions it calls. One test did exactly that
(`anchor-watch-tile.test.tsx`) and broke on the first version of this change,
which is what surfaced the coupling.

### 3. Five tiles pass a state, each from what it already computes

- **Gauge tile.** The same `activeZone(converted, config.zones)` `GaugeBody`
  already runs, computed once more at the `GaugeTile` level so the border can
  see it without `GaugeBody` reaching back out through its own wrapper.
- **Gauge group.** `worstZoneState` over `reading(gauge, values).zone` for
  every member. `reading()` comes from `cluster-readings.ts`, the same helper
  the engine cluster already uses, rather than a second zone computation.
- **Engine cluster.** `worstZoneState` over the ring, the centre, every
  corner row, and the telltales. The telltales do not share the gauge zone
  vocabulary directly: a telltale is a warning light with three tiers, not
  five alarm states. So `telltaleZoneState` translates the same `zone`/
  `value` pair the telltale's own colour logic (`telltaleClass`) already
  computes: past an alarm zone maps to `alarm`, `outside` a normal band or a
  literal `warn`/`alert` zone maps to `warn`, no band or inside the normal
  one maps to `normal`, and no reading at all maps to `null`. The two
  functions read the same inputs so a telltale's colour and its contribution
  to the tile's edge can never disagree about which tier it is in. This is
  the widget that actually surfaced Decision 1's `outside` exclusion: the
  stock Cummins profile's engine reads `outside` on its oil pressure and its
  temperature telltales at idle, every idle, because ADR 0054 §5a shipped it
  with no warn or alarm thresholds. Before the exclusion, both clusters on a
  healthy running boat carried a standing amber edge for exactly that reason.
- **Battery & Power.** `socSeverity(batterySocPercent, socBands)` already
  returns `'alarm' | 'warn' | null`; `null` (in band, or no rule configured)
  maps to `'normal'` for the tile, which is inert either way since `Tile`
  treats `null` and `'normal'` identically.
- **Tanks.** `worstZoneState` over each visible tank's existing `TankTone`.
  `critical` maps to `alarm`, `warn` maps to `warn`, `normal` maps to `normal`.

No other tile was touched. A tile with no zones, bands or tones configured
anywhere on it renders exactly as it did before this ADR, because
`worstZoneState` returns `null` when every input does.

## Consequences

- An engine cluster with one telltale in alarm, or a tank tile with one tank
  critical, now shows on its own border and a header dot without the operator
  reading every reading inside it first.
- The five wired tiles each recompute a zone or tone they already compute
  elsewhere in the same render, once more, rather than threading the value
  out of the child that first calculated it. This is consistent with the
  pattern `GaugeGroupTile` and the engine cluster's corner cards already used
  before this ADR: a second cheap computation of the same pure function, not
  a new prop-drilling path.
- The ribbon (ADR 0052, promoted to a pinned strip in a later phase) still
  covers the boolean/lamp case this does not: a path with no gauge, group,
  cluster, battery or tank tile around it still has no border to show.
- `severity.ts` now imports `ALARM_STATES` from `@/hooks/use-alarms` as a
  value, not only `AlarmState` as a type. The rank-at-call-time design in
  Decision 2 exists specifically so that import does not force every module
  that touches `Tile` to also load a working `use-alarms` at the moment
  `severity.ts` does.
- A gauge, group, cluster or other tile whose only band configured is a
  `normal` one (an advisory healthy range with no warn or alarm threshold,
  ADR 0053's healthy-zone case) never lights its own edge, no matter how far
  out of that band the reading sits, until an operator fills in an actual
  warn or alarm threshold. This is deliberate, not a gap: it is the same
  choice the telltales already made (ADR 0054 §5a) about not asserting an
  alarm a configuration has no threshold to detect, now made once at the
  `Tile` level instead of per widget.

## Verification

`npx vitest run`, `npx tsc --noEmit` and `npm run lint` in `frontend/`, all
clean. Coverage: `severity.test.ts` (`worstZoneState` ranking, the
warn/outside tie, null/empty handling, `severityBorderClass` per state),
`tile-state.test.tsx` (border class, `data-state` and the dot per named
severity; `normal`, `outside` and no state all render neither; a stale tile
suppresses all three even with an alarm state passed in), and a
state-specific test added to each of `gauge-tile.test.tsx`,
`gauge-group-tile.test.tsx`, `engine-cluster-tile.test.tsx`,
`battery-power-tile.test.tsx` and `tanks-tile.test.tsx`, including the
engine cluster's own out-of-band case.

Also checked against the live dev dashboard (screenshot only, no writes) on
the Cluster preview page, which carries the two idling engines this ADR's
`outside` exclusion was written for: the header dot sits at the end of the
hairline rule rather than wrapped onto its own row, and both engine clusters
show a plain, unlit edge instead of the standing amber border the first
version of this change produced.
