# ADR 0055: Host-Derived Vessel Paths

## Status
Accepted

Extends ADR 0039 (bindable gauges) and ADR 0054 (engine cluster).

## Context

The engine cluster grew a fuel-economy readout, taken from `propulsion.<id>.fuel.economy`. The unit was verified against the live vessel rather than read off a spec: at 10.21 kn with that engine burning 23.9 L/h, speed over burn gives 0.4272 nm/L and the published 792950.7 converts to 0.4281 — a 0.2% match, which settled it as metres per cubic metre.

It also settled something more important. **That figure is per engine.** On a twin it reads roughly twice the boat's actual distance per litre, because each engine's economy counts only its own burn. An operator planning range off one cluster's number is out by however many engines are running — 0.43 nm/L shown against 0.23 nm/L actual. A readout that is confidently wrong by a factor of two is worse than no readout, and putting it inside a per-engine tile makes the mistake easy: the tile says "Port", so the number looks like Port's contribution rather than a whole-vessel claim.

Nothing publishes the vessel figure. It has to be derived.

## Decision

### 1. The host derives it, which is this repo's standing rule

`docs/plugins.md` already says a plugin returns raw provider numbers and the host computes what is derived from them, and ADR 0018 made host-side derivation the rule for weather and waves. Fuel economy is the same shape: speed over ground divided by the total burn of every engine reporting one.

`fuelRatePaths` finds the burn paths by walking the snapshot for `propulsion.*.fuel.rate`, so a single, twin or triple installation needs no configuration.

### 2. A namespaced synthetic path, not a new widget

The derived value is published as `helmcentral.propulsion.fuelEconomy` and rides the existing `gauge-values` event.

This is the cheap decision and the reason it is worth making: **every widget that can bind a path can bind this one with no widget code at all.** A gauge, a cluster slot, a lamp, an alarm rule — all of it works already. A bespoke "vessel economy" tile would have been more code and would have served exactly one number.

The `helmcentral.` prefix means a derived path can never shadow one the vessel publishes, and makes it obvious in the picker that the value is computed here rather than received.

Derived paths are listed by `GET /api/signalk/paths` alongside the published ones, with their units, so the picker preselects the quantity the same way it does from SignalK's own `meta.units`. A value nobody can find is a value nobody can bind.

### 3. Absent, never zero and never infinite

The figure is reported as absent whenever it is not defined: stopped, not burning, or nothing published.

Zero would read as "this boat covers no distance per litre", which is a measurement rather than the absence of one — the same failure the structural dash exists to prevent, and the same reasoning ADR 0039 applied to a gauge reading 0 for missing data. An infinity from dividing by a zero burn is worse: it renders as a plausible-looking enormous range.

An engine that is off contributes nothing to the total rather than making the vessel's economy unknowable, so a twin running one engine still reports correctly.

### 4. `fuel.economy` is dropped from the bundled profile

Not merely omitted — removed, with a note saying why and pointing at the derived path. Leaving it available invited exactly the error this ADR exists to prevent.

The `lead` corner emphasis added for the economy sub-line was removed with it. It was three lines of config, backend validation and tests serving one caller, and that caller is gone; re-adding it when a real second case appears is cheaper than carrying it unused.

## Consequences

- The vessel's own economy is bindable anywhere a path is, and reads 0.229 nm/L against a per-engine 0.43 on the test vessel.
- A derived-path mechanism now exists with exactly one entry. That is deliberate: `derivedPathValues` is a map, so a second entry is a function and a line, but nothing was built for entries that do not exist yet.
- Derived values are computed on every stream tick rather than cached. At one tick a second over a handful of snapshot reads that is not worth a cache, and a cache would need invalidating against a snapshot that changes continuously.
- `helmcentral.*` is now a reserved namespace in the path space. Nothing enforces that against a SignalK server that decides to publish under it; the collision is unlikely enough to leave unguarded rather than to add a check that would itself need maintaining.
- Economy is instantaneous, not averaged. At displacement speeds it moves with every wave, and a trip average is the more useful number for actual planning — that wants the history store, and is a separate piece of work.

## Verification

`go test -short ./...` and 849 frontend tests pass.

The unit and the per-engine finding were both established against the live vessel before any code was written, and the derivation is tested against those same readings: 5.2525 m/s over two engines burning 6.639e-06 and 6.528e-06 m³/s gives 398924 m/m³, roughly half either engine's ~793000. Absence is tested for all four undefined cases — no speed, no burn, engines off, and stopped.
