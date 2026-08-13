# ADR 0039: Bindable Gauge Widgets

## Status
Accepted

Depends on ADR 0037. Like alarms (ADR 0038), this was never blocked on its own design — it was blocked on ingestion. A widget can bind to any path only because the delta stream put every path the server publishes into a snapshot.

Extends ADR 0031, which explicitly anticipated this: *"The per-instance config field is now precedent. A second widget wanting per-instance settings extends `dashboardLayoutItem` the same way instead of inventing another mechanism."*

## Context

The dashboard had 16 widgets, each hand-wired to one data domain. An owner could not display a value the developer had not anticipated — the `embed:` iframe was the only escape hatch, and it means running Grafana to show a number.

The products Helmcentral's users come from do not work this way. N2KView offers 52 component types bindable to any of 400+ N2K data points; MConnect ships 87 screen templates plus a visual editor. The gap is not gauge *count*, it is that Helmcentral's widget set is a developer-gated list.

Engine data is the sharpest instance. `propulsion.*` was read for RPM only, rendered nowhere, and nothing else under that tree was read at all — so oil pressure, coolant temperature, fuel rate and engine hours were unreachable however well the boat published them.

## Decision

### 1. Follow the `embed:<token>` precedent exactly

Ids are `gauge:<token>`, minted by the same generator (a timestamp plus randomness, deliberately not `crypto.randomUUID()`, which is secure-context-only and undefined over plain HTTP on a boat LAN). Config rides on the layout item as a sibling of `embed`:

```go
Gauge *dashboardGaugeConfig `json:"gauge,omitempty"`
```

`omitempty` keeps existing `dashboard-pages.json` files byte-identical. Validation keeps ADR 0031's reject-rather-than-drop stance: a gauge config on a builtin id, or an embed config on a gauge, is an error rather than something silently ignored — config the renderer will never read means the caller has misunderstood the model.

Zones reuse the alarm severity vocabulary (ADR 0038) rather than inventing gauge-only colours, so a red band on a dial means the same thing as a red alarm.

### 2. Resolve the `AGENTS.md` "no new primitives" rule explicitly, not quietly

`AGENTS.md` says **"Do not introduce new primitives"** — each widget builds its own KPI stack inside `components/ui/tile.tsx`. A shared, reusable gauge component is precisely what that forbids.

The carve-out, recorded rather than assumed: **that rule governs bespoke domain tiles**, where per-tile layout is a feature and a shared abstraction would flatten deliberate differences. A user-configurable widget cannot be bespoke by definition — there is no domain to tailor it to, because the operator picks the domain at runtime. `GaugeTile` is therefore a generic renderer, and it is the only one.

Everything else in `AGENTS.md` still binds: KPI stacking, `text-gauge-primary`/`text-gauge-secondary` for hero numbers with raw palette colours reserved for alert semantics (which is exactly what zone colours are), the `text-[11px]`/`text-[10px]`/`text-[9px]` micro-type scale, no hex improvisation, and the structural dash for absent data.

### 3. Hand-rolled SVG, no charting dependency

`depth-sparkline.tsx` is 196 lines of hand-written SVG despite Recharts already being installed, and ADR 0012 makes a point of the dashboard having no chart library or layout solver. The radial arc and bar meter follow that: `useId()` for gradient ids, `hsl(var(--…))` for every colour, `aria-hidden` on decorative SVG. A gauge library would have been against the grain of a codebase with 15 runtime dependencies.

### 4. A real units module, because SignalK is SI-canonical

`lib/units.ts` knew two conversions: fahrenheit and feet. Four of the five target gauges needed conversions that did not exist — oil pressure arrives in **pascals**, temperature in **kelvin**, fuel rate in **cubic metres per second**, engine hours in **seconds**, revolutions in **hertz**. None is readable on a helm display.

`lib/quantities.ts` defines quantities with their SI unit and display options. Picking a path preselects the quantity from SignalK's own `meta.units`, so the operator never has to know that oil pressure is in pascals — the server already said so.

Absent values format to `null` and render the structural dash, never a zero. **A gauge reading 0 when it means "no data" is the dangerous failure**, and it is the one this rule exists to prevent.

### 5. The path picker allows free text, and must

`GET /api/signalk/paths` walks the snapshot and returns every node with a value leaf, with its `meta.units` and current value. On the test vessel that is 369 paths.

But the snapshot only holds paths seen **since the stream connected**. With the engines off, `propulsion.*` is entirely absent — and setting up an engine gauge is exactly what someone does with the engines off. So the picker is a datalist over a free-text field, not a closed dropdown, and says so. Restricting to known paths would have made the engine gauges — the motivating use case — impossible to configure.

### 6. The server decides what to push, because it already knows

Gauge values ride the existing telemetry stream as a `gauge-values` event. The backend owns `dashboard-pages.json`, so it can collect every bound path itself and send exactly those, deduplicated across pages. No subscription protocol, no client registration, nothing to keep in sync.

## Consequences

- Any value the SignalK server publishes can be put on the dashboard without code changes — the structural gap against N2KView's bindable component library.
- A fourth pair of hand-maintained widget id lists is now implied (`DASHBOARD_WIDGET_IDS` and `validDashboardWidgetIDs`), the wart ADR 0031 already flagged. Multi-instance ids sidestep both lists via their prefix, so this adds prefix handling rather than list entries, but the underlying duplication is unaddressed.
- `lib/units.ts` and `lib/quantities.ts` now overlap: the former holds two conversions used by tiles that predate this. Folding it in is worth doing when those tiles are next touched.
- **Bilge run-rate is not implemented.** It is not a bound path but a derived metric — cycles or minutes-run per hour, computed from edges on a boolean over a rolling window — and needs `telemetry_history.go` generalised from its two hardcoded ring buffers to a per-path store. It was scoped separately from the outset for that reason. A rising bilge run-rate feeding an alarm rule remains the single highest-value outcome still outstanding across all three plans.
- Gauges show instantaneous values only. Trend and history for an arbitrary path need the same generic history store as bilge run-rate.

## Verification

Verified against a live vessel: two gauges bound to real paths, one radial and one numeric. The dial read 28.29 V on a 20–32 scale with the arc proportionally filled; the numeric read **25.0 °C converted from the 298.15 K** SignalK published, which exercises the meta-driven quantity inference and the conversion together.

Conversions are unit-tested against real SI values — 241325 Pa as 35.0 psi, 20 L/h as its cubic-metres-per-second equivalent, 30 Hz as 1800 RPM — along with the rule that absent data formats to null while a genuine zero formats to zero.
