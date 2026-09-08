# ADR 0085: Suggested Ribbon Lamps

## Status
Accepted

Follows ADR 0082, which pinned the indicator ribbon as a single vessel-level
strip, and ADR 0052, which set the lamp's on/off/no-data semantics this ADR
reuses unchanged.

## Context

A fresh ribbon opens with one lamp, its path a blank text field with nothing
behind it but a datalist of paths the vessel happens to be publishing right
now. The operator has to already know, or go find, which SignalK path means
"port engine running" on this particular boat, type it in exactly, then
repeat that eleven or fifteen more times before the strip is worth looking
at. The N2KView ribbon on MV Dirona that ADR 0052 and ADR 0082 both took
their cue from carries around forty lamps, and every one of them was placed
by hand by someone who already knew the boat's wiring.

A default set has to come from the bus, not a guess. A lamp bound to a path
this vessel never publishes renders "no data" forever, at low opacity,
indistinguishable at a glance from a lamp that is merely off. A ribbon with
even one dead lamp in it stops being something the eye can trust
unconditionally, which was the entire property ADR 0082 built the ribbon to
have. Inventing a plausible-looking path such as
`propulsion.port.state` and hoping it matches would be worse than the blank
field it replaces: the blank field is honestly empty, and a guessed path that
happens to be wrong is a strip that lies quietly from the moment it is
pinned.

## Decision

### 1. A catalogue of patterns, resolved against the vessel's own paths

`suggestRibbonLamps(paths: SignalKPath[])`
(`frontend/src/lib/ribbon-defaults.ts`) walks an ordered catalogue of dotted
path patterns, each with a single `*` standing in for one instance segment,
and keeps only what the vessel's own `GET /api/signalk/paths` response
actually contains:

1. `propulsion.*.revolutions`: one lamp per engine, labelled `Port` or
   `Stbd` for those instances, `Eng <instance>` otherwise, engines sorted
   port, starboard, then the rest alphabetically.
2. `electrical.generator.*.stateNumber`: `Gen` for the first, `Gen
   <instance>` for a second and beyond.
3. `electrical.inverters.*.acState.acIn1Available`: `Shore`, one lamp even
   if more than one inverter reports it.
4. `electrical.alternator.*.chargingModeNumber`: one lamp per alternator,
   labelled `Alt <instance>`.

An entry with no match in the vessel's paths contributes nothing; there is no
step where a missing pattern falls back to a placeholder. The catalogue is
plain data, so a fifth entry is one array literal, not new control flow.

### 2. Prefill on open, never on save

The config dialog (`lamp-strip-config-dialog.tsx`) seeds a fresh ribbon's
lamp list from `suggestRibbonLamps` once `useSignalKPaths` has returned, and
leaves an existing ribbon's saved lamps untouched: the seeding effect checks
for that config before it runs at all. A **Suggest lamps** button, ribbon-only,
appends whatever the catalogue can find that is not already in the list by
path, so a lamp added since the ribbon was last edited (a second generator,
an engine added to the vessel) can be picked up without hand-typing its path.
Both paths write into the same in-memory `config` state the rest of the
dialog already edits: Save is still the only action that pins anything, and
Cancel discards a prefill exactly like it discards a hand-typed one.

### 3. What is deliberately not suggested, and why

- **No numeric inverting-mode lamp.** The inverter publishes
  `inverterModeNumber`, but its values (an enumerated charger/inverter/off
  state, not a boolean) don't reduce to a clean on-when-non-zero reading
  the way the other four entries do. A lamp built from it would either need
  a rule this dialog doesn't have a place to express or would flicker
  between "on" states that don't mean what a lamp's on/off pair is supposed
  to mean.
- **No anchor boolean.** The bus carries `navigation.anchor.position` and a
  handful of `notifications.navigation.anchor.*` string fields, nothing
  numeric that is zero when not anchored and non-zero when anchored. Anchor
  status already has its own dedicated widget and alarm; a guessed lamp here
  would either be wrong or redundant with what already exists.
- **No autopilot state.** The only autopilot path this vessel publishes is
  `steering.autopilot.target.headingTrue`, a heading, not an engaged/disengaged
  signal. There is nothing in the catalogue's shape to suggest.
- **No labelled bilge circuit.** CZone exposes bilge pumps as
  `electrical.switches.bank.<n>.<m>.state`, indistinguishable from any other
  numbered circuit on that bank without the CZone configuration naming which
  number is the bilge. Guessing one would be exactly the invented-path
  failure this ADR exists to avoid. The how-to page walks through adding it
  by hand, with `invert` when the healthy state is off.

## Consequences

- A freshly pinned ribbon is useful immediately on any vessel whose engines,
  generator, shore power or alternators are on the bus, with zero lamps
  pointed at paths that will never report.
- The catalogue only grows what the operator still has to place by hand;
  bilge floats, fault lines and anything else without a clean numeric
  zero/non-zero reading remain manual, same as before this ADR.
- `suggestRibbonLamps` is pure and untangled from the dialog, so its four
  entries and their ordering are tested directly against a real vessel's
  path list, not through rendered DOM.
- A boat with none of the four things this catalogue recognises still opens
  to the one blank lamp it always did; nothing regresses for a vessel this
  catalogue has nothing to say about.

## Verification

Test-first throughout. `frontend/src/test/ribbon-defaults.test.ts` runs
`suggestRibbonLamps` against a captured real response,
`frontend/src/test/fixtures/signalk-paths-2026-09-08.json`
(`GET /api/signalk/paths` from the live vessel, 2026-09-08), asserting the
exact six-lamp order it resolves to, an empty path list yielding nothing, a
single-engine boat yielding `Eng 0`, and a twenty-engine path list capped at
sixteen. `frontend/src/test/lamp-strip-config-dialog.test.tsx` mocks
`useSignalKPaths` to cover a fresh ribbon prefilling, an existing ribbon and
a per-page widget both leaving their lamps alone, **Suggest lamps** disabled
until paths load or once nothing new remains, and appending only the
lamps missing by path.

`npx vitest run`, `npx tsc --noEmit`, `npm run lint`: 1838 to 1851 tests
passing, no new lint warnings over the 18 pre-existing ones.

Not checked against the live vessel beyond the captured fixture; no dev-stack
or on-boat verification was done for this phase.
