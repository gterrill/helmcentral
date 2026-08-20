# ADR 0043: Service-Schedule Provider Plugins

## Status

Proposed

This records a design ahead of its implementation, so the plugin contract can be
reviewed before an implementation freezes it. It rests on a scope boundary stated in
Context: maintenance and inventory *records* stay outside Helmcentral.

## Context

A competitive evaluation of commercial "yacht operating system" products (iNav4U's
Zora, which bundles maintenance and repair management, inventory, expenses, documents
and automated logbooks) identified maintenance as Helmcentral's largest functional gap
after direct NMEA 2000 ingestion.

### Why the records themselves do not belong here

Helmcentral is a safety-adjacent binary. Startup is fail-fast by design - if the
secrets store or the plugin-override database cannot be opened, the process exits
rather than running degraded. Adding an inventory or maintenance store to the same
binary means a CRUD store's problem can refuse to boot the anchor alarm, and the only
escapes are a two-tier fatality policy inside one process or a weakened invariant.

The data lifecycles are also opposed. Telemetry history is a deliberately ephemeral
in-memory ring buffer ([ADR 0020](0020-in-memory-telemetry-history-optional-influxdb.md)),
and only `data/secrets.key` is called out as backup-critical. Maintenance logs and a
spares inventory are durable business records with real export, migration and backup
obligations, and a schema that will churn far faster than the alarm schema.

Records are therefore planned to live in a separate application. Integration needs no
new mechanism: `EmbedTile` (`frontend/src/App.tsx`) already puts any URL in the
dashboard grid, and the alarm system is a *producer* on the SignalK `notifications.*`
tree (`backend/alarm_service.go`, `backend/alarm_anchor.go`), so an external
application's "service due" reminder can enter Helmcentral's alarm list, acknowledgement
flow and transports with no integration code on either side ([ADR 0038](0038-alarms.md)).

### Why schedules are different

Manufacturer service intervals are not records. They are read-only reference data that
varies by vendor, has no local authority, and has no universal answer - Yanmar, Volvo
Penta, Cummins and Caterpillar publish genuinely different schedules for the same
nominal service. That is structurally identical to the reason tides have no default
provider (BOM and NOAA are different authorities for different water), which the
existing WASM provider system already exists to solve
([ADR 0017](0017-wasm-plugin-tide-providers.md)).

The runtime input is already free. Ingestion is a delta-stream subscription rather than
a hardcoded path list ([ADR 0037](0037-signalk-delta-stream-ingestion.md)), so
`propulsion.<id>.runTime` is already in the snapshot tree with no new ingestion work.

### Why this is not an alarm-engine change

An `alarmRule` (`backend/alarm_rules.go`) is `{Path, Op, Value, Hysteresis,
DwellSeconds}` over a live path, so `propulsion.port.runTime > 900000` is a valid rule
*today* with no code change. It is the wrong tool anyway, because the lifecycles
disagree:

- An alarm clears when the **value** travels back past its deadband
  (`backend/alarm_engine.go`). A service reminder clears when **a human logs work**,
  then re-arms one interval later.
- Runtime is monotonic, so such a rule fires once at the threshold and can never clear.

Hysteresis and dwell exist precisely to make raising and clearing a function of the
value. Bending them to accommodate an event that is not a value change would blur the
one subsystem whose semantics are the strongest thing Helmcentral can claim over a
closed competitor. Service schedules therefore get a provider category, and the alarm
engine is left alone.

## Decision

### 1. A new plugin category, `service-schedules`

It follows the established per-category shape exactly, with no new machinery:

- `backend/service_schedule_providers.go` - registry, mirroring
  `backend/tide_providers.go` and `backend/wave_providers.go`.
- `backend/wasm_service_schedule_provider.go` - WASM adapter, mirroring
  `backend/wasm_tide_provider.go`.
- `plugins/service-schedules/` - discovery directory, alongside the existing `tides`,
  `weather`, `waves` and `forecast-warnings`.

The generic loader in `backend/wasm_plugin.go` is unchanged. No new host function is
required; `ftp_fetch` remains the only custom one
([ADR 0019](0019-ftp-host-function-and-forecast-warnings-provider.md)).

### 2. Two exports, mirroring tides

Tides are the only existing category with a discovery export (`search_stations`)
alongside its fetch (`fetch_tide_chart`), because the user must first identify *which*
station applies to them. Engine selection has exactly that shape, so it takes exactly
that contract:

| Export | Input | Returns |
| --- | --- | --- |
| `search_models` | free-text query | candidate engine models, each with an opaque provider-scoped id |
| `fetch_schedule` | a `model_id` from `search_models` | the raw interval table for that model |

Plus the universal `id`, `name` and `ttl_seconds` every plugin exports.

Ids are opaque to the host and scoped to the plugin that issued them, as station ids
already are - the host never parses or constructs one.

### 3. The plugin returns intervals; the host derives everything

`fetch_schedule` returns raw manufacturer intervals - `interval_hours`,
`interval_months`, `first_at_hours`, and a description per item - and nothing else. The
host computes what is due, in what order, and how far away.

This is the existing rule applied unchanged: the host owns all derived data, so a
plugin only ever returns raw provider numbers (`docs/plugins.md`). A plugin that
accepted current engine hours and returned "due now" would own derived data and break
its own category's precedent, and would additionally have to be re-invoked on every
runtime change rather than cached.

Warnings are the standing exception to host-side derivation, for a reason that does not
apply here: BOM's and NWS's zone taxonomies are incompatible namespaces with nothing
universal to factor out. "Hours since a number" is universal.

### 4. Offline by default

Under the existing sandbox, a plugin with no `<name>.allowed_hosts.json` sidecar gets
no network at all. Service schedules are static reference data, so a schedule plugin is
expected to ship **no sidecar** and run fully offline - correct for a boat at sea, and
a first for the plugin system, where every other category exists to reach a remote API.

`ttl_seconds` should be long (weeks). Published intervals effectively never change
within a release of a plugin, and re-deriving them is cheap and local anyway.

Nothing prevents a network-capable schedule plugin; a vendor with a real API can ship a
sidecar and the existing allowlist applies unchanged. It is simply not the expected
case.

### 5. No default provider

Tides have no default because tide data is tied to physical station networks rather
than a global model, so there is nothing keyless and worldwide to hardcode. Service
schedules have no default for the same reason at one remove: there is no universal
engine.

The behaviour follows the tide precedent rather than inventing one. Until a provider is
selected, the endpoint returns an error naming what is missing rather than guessing or
falling back, consistent with the repository's fallback policy.

### 6. Settings shape

Two keys on the UI settings payload (`backend/signalk.go`), mirroring the existing
`tide_provider` / `tide_station_id` / `tide_station_name` triple:

- `service_schedule_provider` - the selected plugin's `id`.
- `engine_model_id` and `engine_model_name` - the selection made via `search_models`,
  stored as submitted so an unrecognised value round-trips rather than being silently
  dropped.

## Alternatives considered and rejected

**A YAML or JSON interval table instead of a WASM plugin.** This is the closest call in
this ADR and deserves to be recorded as such: schedules are static, and a data file
would carry no sandbox, no toolchain and no build step. It was rejected on two grounds.
Real schedules need computation, not lookup - "every 250 hours or 12 months, whichever
comes first", intervals conditional on engine variant or duty rating, and items that
supersede one another - and expressing that in a data format means inventing an
expression language. A plugin also inherits discovery, per-plugin overrides
(`backend/plugin_overrides_store.go`), the Settings dropdown and the versioned release
bundle for free, where a bespoke file format inherits none of them. This is nonetheless
the weakest-fitting category so far, and if a schedule plugin in practice turns out to
be a table with no logic in it, that is evidence to revisit this decision, not to
defend it.

**A "maintenance provider" that returns due items directly.** Rejected under decision 3
- it moves derivation into the guest and inverts the category's contract.

**Storing "last serviced at hours" in Helmcentral.** It would make the feature
self-sufficient immediately, and it is rejected anyway: a per-item completion record is
the thin end of maintenance records, and it contradicts the boundary the whole design
rests on. The first field would be `last_serviced_hours`; the tenth would be a parts
list and an attachment.

**Extending the alarm engine with a resettable recurring rule type.** Rejected under
Context - a different state machine wearing the same struct.

## Consequences

- Helmcentral can surface a manufacturer's schedule and current accumulated runtime,
  but **not** hours-since-last-service, because it deliberately stores no completion
  records. The category is genuinely useful only once a records application owns
  completions and feeds them back. This is a real limitation of the shipped feature,
  not a phase of it, and should be stated plainly in user-facing documentation rather
  than implied away.
- A category whose plugins are expected to have no `allowed_hosts.json` makes the
  sidecar's absence load-bearing for the first time. The absence already means "no
  network"; nothing changes mechanically, but a missing sidecar stops being a smell.
- Reference plugins for this category embed vendor-published intervals, which raises a
  content question the other categories do not have (the others call an API at runtime
  and redistribute nothing). Which schedules can be bundled, and under what terms, is
  left open here and must be settled before any reference plugin ships.
- Schedules bundled into a plugin go stale silently when a manufacturer revises them.
  A long `ttl_seconds` is right for caching but does nothing about staleness across
  plugin versions; there is no freshness signal in the contract, and adding one is
  deferred until there is evidence it matters.
- The generic loader, the sandbox and the release bundle absorb a fifth category with
  no changes, which is the outcome [ADR 0017](0017-wasm-plugin-tide-providers.md)
  predicted for a category that fits. A category that had needed loader changes would
  have been evidence it did not belong.

## Related

- [ADR 0017](0017-wasm-plugin-tide-providers.md) - the sandbox, the discovery-plus-fetch
  export shape, and why tides have no default.
- [ADR 0018](0018-wasm-plugin-weather-and-wave-providers.md) - host-side derivation.
- [ADR 0019](0019-ftp-host-function-and-forecast-warnings-provider.md) - the one custom
  host function, and the one category exempted from host-side derivation.
- [ADR 0037](0037-signalk-delta-stream-ingestion.md) - why `propulsion.*.runTime` needs
  no new ingestion.
- [ADR 0038](0038-alarms.md) - the alarm lifecycle this category deliberately does not
  join, and the `notifications.*` producer path an external records application can use.
