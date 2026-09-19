# Product

<!-- impeccable:product-schema 1 -->

## Platform

web

## Users

Anyone running a SignalK server on their own boat, from weekend sailors to
full-time cruisers. Technical depth varies widely, so the install and first run
have to work for someone who will never read the source. The author is a
full-time live-aboard and its first user, but it is intended for other boat
owners too.

Users need to see the state of the boat at a glance,
and be told reliably when something is wrong. Much of the time the person
responsible is asleep, off the boat, or busy with something else.

## Product Purpose

A dashboard and alarm system for SignalK. It puts anchor watch, tides, weather,
routes, tanks and electrical monitoring on one screen at the helm, and sends
notifications off the boat when a rule fires.

Failures must be visible to the operator. Stale readings must be distinguishable
from live data, and failed notification deliveries must be reported.

## Positioning

Helmcentral runs on the boat as a single Go binary with the React UI compiled
in. It requires no database server, cloud account or subscription.

It runs separately from SignalK. SignalK provides the NMEA 2000 translation
layer. Helmcentral uses a single read-only delta subscription into an isolated
snapshot, role
resolution downstream of SignalK that fails closed, and third-party provider
code confined to WASM with host allowlists and hard timeouts. SignalK's own
plugin model runs third-party code in-process; running Helmcentral separately
isolates its alarm processing from those plugins.

It also works both directions on the notification bus. Alarms raised by a
Victron GX, an N2K device or another plugin appear in its list with no
per-source integration, and its own rules reach the MFD by the same route.

## Operating Context

Design for two viewing conditions:

- **A helm touchscreen in daylight.** Direct sun and glare, read at arm's
  length, by someone doing something else at the same time.
- **A laptop at the nav station.** Longer sessions with a pointer and keyboard:
  planning routes, arranging layouts, configuring alarms and settings.

Phones receive off-boat alarm notifications. Phone use was considered but is
not a primary design target.

The vessel itself sets the rest of the context: intermittent power, unreliable
internet, and no guarantee anyone is watching the screen. Browser support floor
is Baseline 2024 (Chrome/Edge 111+, Firefox 111+, Safari 16.4+, which means
iPadOS 16.4+ on a helm tablet); the shipped CSS is not downlevelled past that.
First run searches the network for a SignalK server and offers what it finds, so
there is nothing to edit by hand to get started.

## Capabilities and Constraints

Shipped:

- Dashboard of 17 built-in widgets, gauge widgets bound to any path the server
  publishes, and embed tiles for any URL. Named pages the operator arranges and
  switches between, persisted server-side. A page carries a skin (`default` or
  `instrument`, ADR 0060). A gauge's coloured band set into a severity is the
  source of an alarm rule.
- Anchor watch with trail sampling and drag detection on the server, so closing
  the browser does not stop detection, and a lost GNSS fix does not raise a drag
  alarm. The rode
  planner does pay-out and swing radius against tide-corrected depth.
- Alarm rules on any SignalK path, using SignalK's five severities in both
  directions, with mandatory dwell and hysteresis on every rule. Five transports
  (ntfy, SMTP, webhook, web push, SignalK), none needing a paid subscription.
  Failed deliveries are queued and retried with backoff from 30 seconds to 30
  minutes for up to 24 hours, every attempt logged.
- A watchdog on the delta stream raises an alarm if the SignalK connection is
  lost. An external service can monitor a periodic off-boat heartbeat.
- Route planning that pushes an active route to SignalK for the autopilot,
  autopilot control on SignalK's v2 API, ARPA radar targets, embedded weather
  radar.
- Tides, weather, waves and forecast warnings as sandboxed WASM plugins loaded
  from a directory (Extism/wazero, no filesystem or process access, per-plugin
  host allowlist). Adding a region means dropping in a `.wasm` file. Weather and
  waves default to keyless Open-Meteo. Tides have no default, because tide data
  comes from regional station networks that the operator must choose between.

Constraints that future work must preserve:

- No fallback that masks an upstream failure. Delta ingestion is one WebSocket
  subscription with no REST fallback and no toggle. Startup is fail-fast: if the
  secrets store or a plugin override database will not open, the process exits
  rather than running degraded.
- Secrets are never set in files or environment variables. They are pasted into
  Settings in the running app and encrypted at rest with AES-256-GCM.
- No user database of its own. `auth.mode: signalk` forwards credentials to the
  SignalK server and maps its `readonly`/`readwrite`/`admin` levels, failing
  closed on anything unrecognised. It defaults to `none` so upgrading an
  existing install cannot lock anyone out of a running boat.
- Storage is SQLite for secrets, alarm history and tile caches, JSON files for
  routes and dashboard pages, and an in-memory ring buffer for telemetry, with
  InfluxDB optional for longer retention.
- Out of scope: hazard avoidance, weather routing, live navigation,
  and anything requiring a chart licence.

Planned but not yet built:

- Inventory tracking. Storage zones and numbered bins matching the boat, items
  searchable by name or part number, and a stocktake done by walking the boat
  with an RFID reader. Designed and written up in
  `docs/features/inventory-tracking.md`.
- Maintenance and service history, tied to live engine hours so a service
  falling due raises an ordinary alarm and logging the work clears it.
- Document storage. Confirmed in scope, no written spec yet.

These live inside Helmcentral. There is no separate companion app.

Terminology in use: paths and deltas (from SignalK), widgets and tiles, pages
and skins, rules, dwell, hysteresis, transports, severities, plugins, providers.

## Brand Commitments

- The name is Helmcentral. MIT licensed, developed in the open.
- Prose reads as a veteran engineer who lives aboard: plain, specific, willing
  to say what a thing does not do. No marketing voice, no slogan bullets, no
  em-dashes.
- The stack is already committed: React and TypeScript on Vite, Tailwind, Base
  UI, Lucide icons, Geist Sans and Geist Mono, MapLibre for charts and Recharts
  for plots.
- `AGENTS.md` carries binding UI rules and outranks taste: the 60-30-10 split
  for telemetry colour, `text-gauge-*` tokens for instrument readouts with raw
  palette colours reserved for alert semantics, the standard KPI stack (muted
  uppercase label over a large bold tabular readout), and the ban on
  low-opacity text at or below 11px.

## Evidence on Hand

- A running install on the author's own vessel, and a deployment box on the boat
  network. Real data, not fixtures.
- Screenshots of the shipped dashboard under `docs/images/`.
- An operator documentation tree (`docs/features/`, `docs/how-to/`,
  `docs/reference/`) and architecture decision records under `docs/adr/`
  recording decisions and their rationale.

There are no customers, testimonials, case studies, press, benchmarks, pricing
or licensing terms. Nothing in that list may be invented.

## Product Principles

1. **Visible failures.** Do not add fallbacks that hide upstream failures.
  A missing feed must raise an alarm so the operator can distinguish a quiet
  system from a failed one.
2. **Local operation.** Helmcentral runs aboard in one binary. Do not introduce
  required external services whose availability, pricing or internet access
  could prevent it from operating.
3. **Legibility.** The screen is read at arm's length, in sun, by someone with
  other things to do. Prioritize legibility in those conditions.
4. **No-cost setup.** A fresh install works without API keys,
   subscriptions or chart licences. Where that is impossible, say what is
  missing.
5. **Command access controls.** Guard commands that can engage an autopilot or
  switch a breaker, and sandbox third-party provider code.

## Accessibility & Inclusion

The product-specific requirement is daylight legibility on a helm screen:
contrast and type size have to hold in direct sun, which is why `AGENTS.md` bans
stacked low-opacity modifiers on small text and sets a floor for de-emphasis.
Touch is a first-class input on the helm surface, alongside pointer and keyboard
at the nav station. No formal conformance standard has been adopted.
