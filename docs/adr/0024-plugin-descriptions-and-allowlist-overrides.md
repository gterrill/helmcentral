# ADR 0024: Plugin Descriptions And DB-Backed Allowlist Overrides

## Status
Accepted

## Context
The `/settings` page is being redesigned so each pluggable provider (tide/weather/wave/forecast-warnings) shows as an "integration card" with a name, description, and a Settings modal to configure host/secret access. Two things this needs didn't exist yet:

1. Providers had no description anywhere — `GET /api/{tide,weather,wave,forecast-warnings}-providers` returned only `{id, name}`. There was nowhere for a plugin to say what it does or what it requires (e.g. "needs a paid API key").
2. A plugin's `<name>.allowed_hosts.json`/`<name>.allowed_secrets.json` (ADR-0017, ADR-0018, ADR-0023) are plain files read once at boot, editable only by SSHing into the host. There was no way to view or change a plugin's network/secret allowlist from the Settings UI.

## Decision

### 1. Optional `description()` WASM export, mirroring `ttl_seconds()`
`newWasmPluginBase` (`backend/wasm_plugin.go`) already has a well-established pattern for optional guest exports: `ttl_seconds()` is called if present, a call failure is logged and falls back to a default, and absence is silent (the normal case for older/minimal plugins). `description()` follows the identical shape — called if `instance.FunctionExists("description")`, a call failure is logged (the export exists but is broken, worth knowing about) and falls back to `""`, and absence is silent and never logged (a plugin author simply hasn't written one yet, which is fine). Critically, unlike `id()`/`name()`, a missing or failing `description()` can **never** fail `newWasmPluginBase` itself — it is purely cosmetic metadata for the Settings UI, and this codebase's fallback policy is about failing loudly on things that are actually broken, not about turning every optional nicety into a hard requirement.

`wasmPluginBase` gained a `description string` field and a `Description() string` accessor. Because every WASM provider type (`wasmTideProvider`, `wasmWeatherProvider`, `wasmWaveProvider`, `wasmForecastWarningsProvider`) embeds `*wasmPluginBase`, this one change gave all four provider kinds `Description()` for free via Go's embedding promotion. `Description() string` was added to all four provider interfaces (`tideProvider`, `weatherProvider`, `waveProvider`, `forecastWarningsProvider`); the one native, non-WASM provider (`stormGlassTideProvider`) got a hand-written `Description()` method instead, since it has no guest contract to export it from. `Description string` was added to all four `*ProviderInfo` response structs and populated in the corresponding `*ProvidersHandler` alongside the existing `ID()`/`Name()` calls — an additive, backward-compatible field.

All seven bundled reference plugins (`docs/examples/{tide,weather,wave,forecast-warnings}-plugins/*/main.go`) got a one-line `description()` export added next to their existing `ttl_seconds()` export, and their `.wasm` binaries were rebuilt via the `plugins-builder` Docker Compose service.

A new fixture pair (`backend/testdata/wasm_plugins/src/describedvalid`, `.../describederror`) was added to exercise the "export present and successful" and "export present but fails when called" paths respectively — mirroring the existing `panicker` fixture's proof pattern for a guest-side fault on another export. The "export absent" path is deliberately tested against an **existing, unmodified** fixture (`configecho`) rather than a new one, since every fixture that existed before this change already represents "no `description()` export" — that is the fixture set's whole reason for existing, and modifying any of them would remove test coverage for the "optional export absent → graceful fallback" contract other tests (e.g. `ttl_seconds()`) already rely on.

### 2. DB-backed plugin allowlist override store, keyed by full wasm path
A new file, `backend/plugin_overrides_store.go`, adds `pluginOverridesStore` — a SQLite-backed table (`plugin_overrides(wasm_path PRIMARY KEY, allowed_hosts, allowed_secrets, updated_at)`) storing an optional override for a plugin's allowed hosts and allowed secret key names. `allowedHostsForWasmPlugin`/`allowedSecretsForWasmPlugin` (`backend/wasm_plugin.go`) now check `globalPluginOverridesStore` first and only fall back to reading the on-disk companion JSON file if no override row exists.

**The store is keyed by the plugin's full `.wasm` file path (e.g. `plugins/tides/bom.wasm`), not by `(type, id)`.** Two concrete reasons drove this, not just a preference:

- **Ordering.** `allowedHostsForWasmPlugin`/`allowedSecretsForWasmPlugin` are called while building a plugin's `extism.Manifest`, *before* the WASM module is instantiated and its `id()` export has been called. At that point in `manifestForWasmPlugin`, the plugin's self-reported id genuinely isn't known yet — only the file path is. Keying by `(type, id)` would require restructuring plugin discovery to instantiate-then-look-up-allowlist-then-build-manifest, a much bigger change than this feature warrants.
- **ID collisions across domains.** `docs/examples/tide-plugins/bom/main.go` and `docs/examples/forecast-warnings-plugins/bom/main.go` both report `id() == "bom"` — they're unrelated plugins (Australian tide data vs. Australian marine warnings) that happen to share a natural short name in their respective domains. `(type, id)` would need `type` threaded through as a new parameter into `allowedHostsForWasmPlugin`, `allowedSecretsForWasmPlugin`, `configForWasmPlugin`, and `manifestForWasmPlugin` — all of which are currently type-agnostic, shared machinery in `wasm_plugin.go` with no concept of "tide" vs. "forecast-warnings". The wasm path is already unique per plugin file and is already what these functions use for their companion-file naming convention (`strings.TrimSuffix(wasmPath, ".wasm") + ".allowed_hosts.json"`), so reusing it as the DB key required zero new plumbing.

### 3. A new sibling store, not an addition to `secrets_store.go`
`pluginOverridesStore` is a separate SQLite file (`data/plugin_overrides.sqlite`), not a table inside the existing encrypted `secrets_store.go` (`data/secrets.sqlite`, ADR-0023). Three reasons:

- **No encryption need.** Overrides are plain string arrays — hostnames and secret key *names* (e.g. `"WEATHERKIT_KEY_ID"`), never secret *values*. There is nothing here that benefits from AES-256-GCM at rest; adding it would just be encryption theater plus unnecessary coupling to `secretsStore`'s master-key resolution and integrity-check machinery.
- **Different lifecycle.** `secretsStore` rows are one-per-secret-key and read on nearly every plugin config expansion; `plugin_overrides` rows are one-per-plugin-file and read only at manifest-build time (boot) and from the new HTTP endpoints. Sharing a table would conflate two independently-evolving schemas.
- **This repo's established convention.** `nearby_contacts.go`, `tile_cache.go`, and `secrets_store.go` each already follow a one-store-one-sqlite-file pattern (`sql.Open`, `db.SetMaxOpenConns(1)`, `CREATE TABLE IF NOT EXISTS`, `cacheFilePath(envKey, fallback)` for path resolution). `plugin_overrides_store.go` follows that same idiom rather than introducing a new "shared multi-purpose store" pattern this codebase doesn't otherwise use.

### 4. New HTTP endpoints, generic across all four provider types
`backend/plugin_overrides_handlers.go` adds:
```
GET    /api/plugins/:type/:id
POST   /api/plugins/:type/:id/overrides
DELETE /api/plugins/:type/:id/overrides
```
`:type` dispatches to the matching registry (`getTideProvider`/`getWeatherProvider`/`getWaveProvider`/`getForecastWarningsProvider`); an invalid `:type` is 400, a valid `:type` with an unknown `:id` is 404. Once a provider is resolved, a small local `pluginPathProvider interface{ Path() string }` type assertion distinguishes WASM-backed providers (implement it, via `wasmPluginBase.Path()`) from the one native provider, Storm Glass (does not implement it) — this determines the response's `sandboxed` field and whether `POST`/`DELETE` are even valid for that provider (Storm Glass has no allowlist concept at all, so `POST` against it is 400). Because `Description()` (and `ID()`/`Name()`) are on every provider interface uniformly (see decision 1), no per-type switch is needed to build the bulk of the response — only the registry lookup itself is type-specific.

Secret **values** (`STORMGLASS_API_KEY`, `WEATHERKIT_*`) remain entirely out of scope for this endpoint family and continue through the existing, unmodified `GET`/`POST /api/settings/secrets` (ADR-0023). This endpoint only ever deals in secret key *names* (which names a plugin is allowed to see), never the values themselves.

### 5. No hot-reload — an explicit, operational tradeoff
Saving an override via `POST /api/plugins/:type/:id/overrides` takes effect on **the next backend restart**, not immediately. `manifestForWasmPlugin` (and therefore `allowedHostsForWasmPlugin`/`allowedSecretsForWasmPlugin`) only runs once per plugin, at boot, from each `loadWasm*Providers` call in `main()` — a plugin that's already compiled and registered keeps running with the manifest (and therefore the allowlist) it was compiled with. Building a live-reload/hot-swap mechanism (recompiling and re-registering an already-running plugin's `extism.CompiledPlugin` mid-process) was explicitly out of scope for this change.

This is a real UX tradeoff, not a hidden detail: an operator who saves a new allowed host from the Settings UI will not see it take effect until they restart the container. The Settings UI is expected to surface this plainly (e.g. "Restart Helmcentral for this change to take effect") rather than implying it's live. This mirrors ADR-0023's existing precedent of the secrets store also only being read into plugin config at boot, and was accepted for the same reason: implementing safe hot-reload of a compiled WASM module's sandbox boundary is a meaningfully larger and riskier change than a documented restart requirement.

## Consequences
Positive:
- The Settings UI can show what every installed provider does and what it needs, in plain language, without any of that information having existed in the API before.
- Operators can review and adjust a plugin's network/secret allowlist from the UI instead of SSHing in to hand-edit JSON files — while the underlying files remain the source of truth for anyone who still prefers that workflow (an override simply takes priority over them; deleting the override reverts to file-based behavior).
- The wasm-path keying means this feature required no changes to plugin discovery ordering, no new `type` parameter threaded through shared host code, and no risk from the two same-named "bom" plugins colliding.
- Zero changes to the encrypted secrets store or its AES-GCM machinery — this feature only ever touches secret *names*, not values.

Tradeoffs:
- No hot-reload: every override save requires an operator-initiated restart to take effect. This is a genuine UX rough edge the frontend must communicate clearly, not paper over.
- `description()` is unauthenticated, unvalidated free-text supplied by the plugin author and rendered directly in the Settings UI — same trust model as `name()` already has today (a plugin author who controls a `.wasm` file already controls what string appears in the provider dropdown). No new trust boundary was introduced.
- Two on-disk representations of the same allowlist can now diverge (the companion JSON file vs. the DB override) for a given plugin. The DB always wins once an override row exists, and `DELETE` cleanly reverts to the file — but an operator debugging "why isn't my edited `.allowed_hosts.json` taking effect" needs to know this endpoint exists and check for a saved override.

## Related
- ADR 0017: Sandboxed WASM Plugin Tide Providers (the `allowed_hosts.json` companion-file mechanism this ADR's DB override takes priority over)
- ADR 0018: WASM Plugin Weather And Wave Providers (the shared `wasm_plugin.go` host layer this ADR extends)
- ADR 0023: Encrypted Secrets Store (the `allowed_secrets.json` companion-file mechanism this ADR's DB override takes priority over; the one-store-one-file convention this ADR's new sibling store follows)
- `backend/wasm_plugin.go` (`description()` export handling, `allowedHostsForWasmPlugin`/`allowedSecretsForWasmPlugin`'s override-first lookup)
- `backend/plugin_overrides_store.go` (`pluginOverridesStore`)
- `backend/plugin_overrides_handlers.go` (`GET`/`POST`/`DELETE /api/plugins/:type/:id(/overrides)`)
