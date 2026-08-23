# ADR 0049: Grouped Gauge Tiles and Tile Duplication

## Status
Accepted

Extends ADR 0039 (Bindable Gauge Widgets), which extends ADR 0031 (Generic Embed Widget). This is the third widget to use the `<prefix>:<token>` per-instance model, and the first to hold more than one bound path.

## Context

ADR 0039 made any published SignalK path displayable. What it produced was one number per tile.

A twin-engine boat needs more than that. "Port" is not five separate readings that happen to share a prefix — it is one instrument cluster: RPM, oil pressure, exhaust temperature, coolant, hours. Scattered across a 12-column grid as five independent tiles, the grouping the operator has in their head has to be reconstructed by eye every time they look at the dashboard, and it comes apart the moment anything else is dragged.

Maretron's N2KView is the reference point here, and it is the product Helmcentral's users are coming from. Two things it takes for granted are missing here. First, a screen is composed of *components arranged into meaningful clusters*, not a flat pile of gauges. Second, **duplication is the normal way to build the second of anything** — the second engine, the second battery bank, the second tank. You build one, copy it, and repoint it. Nothing in Helmcentral could be copied at all: building "Starboard" after "Port" meant placing five more widgets and typing five more paths, with the near-certainty of a typo somewhere in `propulsion.starboard.exhaustTemperature`.

The gap was never the gauge. It was that a gauge is the wrong unit of composition, and that there was no way to say "another one of those, but over there".

## Decision

### 1. A third multi-instance widget, following the precedent exactly

`gauge-group:<token>` ids, minted by the same generator as the other two (a timestamp plus randomness, deliberately not `crypto.randomUUID()`, which is secure-context-only and undefined over plain HTTP on a boat LAN). Config rides on the layout item as a sibling of `embed` and `gauge`:

```go
GaugeGroup *dashboardGaugeGroupConfig `json:"gaugeGroup,omitempty"`
```

`omitempty` keeps existing `dashboard-pages.json` files byte-identical. Validation keeps the reject-rather-than-drop stance both predecessors take: a `gaugeGroup` on a builtin, gauge or embed id is an error, as is a `gauge` or `embed` config on a group. Config the renderer will never read means the caller has misunderstood the model.

The prefix is not a prefix of `gauge:` and `gauge:` is not a prefix of it, so no id check had to be reordered. As ADR 0039 predicted, a prefixed widget needs no entry in either of the two hand-maintained id lists.

### 2. Members are `GaugeWidgetConfig` verbatim, not a parallel type

A group holds `[]dashboardGaugeConfig` — the same struct a standalone gauge uses, unchanged. This is the decision the whole ADR rests on. It means the four display kinds, `lib/quantities.ts`, the zone vocabulary and the backend's per-gauge validation are *shared*, not forked, and a group can never accept a gauge a standalone widget would have rejected.

The per-gauge rules were extracted out of `validateGaugeWidget` into `validateGaugeConfig`, which both callers now use. Without that extraction the two would have drifted the first time a rule changed.

### 3. `gaugeBoundPaths` was the real risk, and it is silent

The backend decides what to push by walking the page config it already owns (ADR 0039 §6). That walker read `widget.Gauge` and nothing else. A group whose members were not collected would render the structural dash forever — with no error, no log line, and nothing in the browser to distinguish it from a sensor that is genuinely off. The widget would look like it worked.

The walker now covers both, feeding one dedup map so a path bound by a standalone gauge *and* a group member is pushed once. A test exists specifically for that case, because it is the failure that would not have announced itself.

### 4. Duplicate applies to every multi-instance tile, not just groups

The affordance is a Copy button in the layout-edit overlay, alongside the existing remove and drag handles, shown for `gauge-group:`, `gauge:` and `embed:` and withheld from builtins — which are one-per-page and have nothing to duplicate. One `duplicateWidget` function serves all three: fresh token, deep copy, same geometry at the bottom of the page.

The copy is **deep**. A shallow one would leave both tiles sharing the same `gauges` array, and retargeting the copy would silently rewrite the original — the exact failure the feature exists to prevent, delivered by the feature itself.

Duplicating persists immediately and *then* opens the copy's config dialog. Unlike a fresh draft, a duplicate is already valid, and the copy exists to be retargeted, so retargeting is the next step rather than something to remember.

### 5. Find/replace over paths, previewed before it is applied

The group dialog carries a two-field replace row. Type `port` → `starboard`, and every member path is rewritten in one pass.

It previews rather than acting: the dialog shows "2 of 3 paths will change" with each rewritten path listed, and Apply is disabled when nothing matches. A mistyped search term producing silence is the ordinary case, and finding that out before pressing the button rather than after is most of the value.

**Paths only.** Labels and the title are left alone. "Port RPM" → "Starboard RPM" is a two-word edit an operator will make anyway; a wrong bulk label rewrite is silent and hard to spot later. That asymmetry — a wrong path shows a dash, a wrong label shows a confident lie — is the reason for the split.

### 6. `GaugeBody` extends ADR 0039's carve-out, deliberately

`AGENTS.md` forbids new shared primitives, and ADR 0039 §2 carved that open for user-configurable widgets while stating `GaugeTile` "is the only one". Rendering N gauges inside one tile means that is no longer true.

The body of a gauge — the four display kinds and the readout — is now `GaugeBody`, shared between the standalone tile and each member of a group, with a `density` prop dropping the nested border and stepping the readout down a tier when a group tile already supplies the surrounding chrome. The same reasoning applies as before: a user-configurable widget cannot be bespoke, because the operator picks the domain at runtime. The carve-out now reads: **generic renderers exist for operator-configured widgets, and only for those.** Bespoke domain tiles still build their own KPI stacks.

The per-gauge *form* was extracted the same way, into `GaugeFields`, shared by the single-gauge dialog and every member row.

## Consequences

- An instrument cluster is now a thing the dashboard can express, and the second engine is a copy rather than a retype — the two things N2KView assumes and Helmcentral could not do.
- Duplication arriving for embeds and standalone gauges at the same time is free: one function, one button, three widget kinds.
- Groups show instantaneous values only, like every gauge since ADR 0039. Trend and history for an arbitrary path still need the generic per-path history store that bilge run-rate also waits on.
- Zone editing still has no UI — zones are in the type, validated, and rendered, but only reachable by editing `dashboard-pages.json` by hand. Grouping made this more visible, not worse: a five-gauge engine cluster is exactly where red bands earn their keep.
- Gauges cannot be dragged between groups, and a group cannot be split back into standalone tiles. Both are re-entering paths in a dialog, which is tolerable at the sizes involved.
- `lib/units.ts` and `lib/quantities.ts` still overlap, unchanged from ADR 0039's note.

## Verification

Backend and frontend suites pass (`go test -short ./...`; 714 frontend tests across 96 files). The `GaugeBody` extraction was made behind a `gauge-tile.test.tsx` written first and passing both before and after — closing a coverage gap ADR 0039 left, since `gauge-tile.tsx` had no test at all.

Not yet exercised against a live vessel. The end-to-end check still owed: a "Port" group of three gauges reading converted values from the stream within the one-second `gauge-values` tick, duplicated to "Starboard", retargeted with a single `port` → `starboard` replace, and both surviving a reload with independent values. The `gaugeBoundPaths` walk is what that run actually proves — the unit test covers the collection, not the delivery.
