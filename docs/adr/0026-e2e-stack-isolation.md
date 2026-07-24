# ADR 0026: Isolated E2E Stack and a Single Runtime State Root

## Status
Accepted

## Context

Browser-driven verification of frontend work — Playwright scripts run ad hoc to confirm a UI change behaves, not a committed suite — has been pointed at the dev stack on `localhost:5173`. That stack is not a test fixture. It is the developer's actual running dashboard: `docker-compose.dev.yml`'s `backend-dev` bind-mounts the real `./settings.yaml` and, because the backend resolves runtime state relative to its working directory, writes `routes.json`, `dashboard-pages.json`, `secrets.sqlite`, `plugin_overrides.sqlite` and the caches straight back into `./backend/data` and `./backend/cache` in the working tree.

This produced a real outage. While verifying the ADR 0025 settings page's unsaved-changes indicator, a script needed to make a field dirty:

```js
await page.locator('#signalk-address').fill('192.168.50.243');
await page.getByText('Routes', { exact: true }).first().click();
await page.getByRole('button', { name: 'Save and Continue' }).click();
```

`192.168.50.243` was an arbitrary throwaway value chosen to differ from the current one. "Save and Continue" did exactly what it says: `POST /api/settings` persisted it to the real `settings.yaml`, repointing the dashboard at an address with no host on it. Vessel state served `source: "signalk-unreachable"` until someone noticed and investigated.

Three properties turned a harmless test keystroke into an outage:

1. **`POST /api/settings` does not validate the SignalK address.** Its sibling `POST /api/settings/signalk` probes the server and returns 502 rather than persist something unreachable; the bulk endpoint writes `settings["signalk"]` straight from the payload. The sectioned settings page's Save goes through the bulk path — and "Save and Continue", which fires when navigating away from a half-finished edit, makes persisting an in-progress value the *default* outcome rather than a deliberate one.
2. **`settings.yaml` is gitignored**, so there was no history to restore from. The original value was only recovered because the `influxdb` stanza still pointed at the same host.
3. **Nothing distinguished "look at the app" from "drive the app".** Both used the same URL, so the choice was never presented.

An obvious narrow fix — snapshot `settings.yaml` before a run and restore after — was rejected: it is racy against the running backend, it restores only the one file out of a dozen state paths, and it leaves the hazard in place for anyone who forgets the wrapper.

## Decision

1. **A single runtime state root, `HELMCENTRAL_STATE_DIR`.** Every state path in the backend already funnels through one helper, `cacheFilePath(envKey, fallback)` (`backend/weather_tide.go`). It now resolves in this precedence: an explicit per-file env override (`ROUTES_FILE`, `SECRETS_DB_PATH`, …) wins outright; otherwise a relative fallback is rooted at `HELMCENTRAL_STATE_DIR` when set; otherwise the bare fallback, unchanged. Absolute fallbacks are never re-rooted.

   The alternative was enumerating a dozen per-file env vars in the compose config. Rejected because it is not merely verbose but *silently* incomplete: a state path added later would default back into the working tree with nothing to signal it. One root means new state paths are isolated by construction.

2. **An `e2e` compose profile serving the same UI from a throwaway substrate.** `backend-e2e` (`:8090`) and `frontend-e2e` (`:5174`) build from the same sources with hot-reload intact, but set `HELMCENTRAL_STATE_DIR=/state` and `SETTINGS_FILE=/state/settings.yaml` against a container-local volume, and — critically — **do not mount `./settings.yaml` at all**. Settings are seeded from a committed `e2e/settings.seed.yaml` on every container start, so a restart returns the stack to a known state. `frontend-e2e` gets its own `node_modules` volume; sharing one with `frontend-dev` would race two concurrent `npm install`s against the same volume.

   `frontend-e2e` must also set **`VITE_API_BASE_URL=http://localhost:8090`**, and this is load-bearing rather than defensive. Several hooks do not use the Vite proxy at all — `use-settings-form.ts` (the sole caller of `POST /api/settings`), `signalk-connection-section.tsx`, `use-secrets-status.ts` and `use-vessel-identity.ts` each build an absolute URL defaulting to `` `${window.location.protocol}//${window.location.hostname}:8080` ``. Redirecting only the proxy therefore isolates the read path while leaving the *write* path pointed at the dev backend: an E2E run on `:5174` would still have written to the live `settings.yaml`, reproducing the exact incident. This was caught during verification — the E2E dashboard rendered the real vessel's name and model instead of the seed's — not by reasoning about the config.

   The seed points SignalK at `127.0.0.1:9` deliberately. E2E here verifies layout, forms and flows; an unreachable source renders the documented `—` placeholders rather than hanging on a live fetch, and no real vessel configuration is committed to the repo.

3. **The choice is made explicit at the point of use.** The `run-dashboard` skill — the thing an agent actually reads before opening a browser — now leads with a table keyed on what the script *does*: read-only looking goes to `:5173`, anything that clicks Save/Connect/Apply goes to `make e2e-up` on `:5174`, and the tie-break for "unsure" is the E2E stack.

4. **`make e2e-*` targets never use `docker compose down`.** Compose's `down` is project-wide and ignores `--profile` for teardown, so `--profile e2e down` would stop the shared, long-running dev stack, and `down -v` would additionally wipe `frontend_node_modules`. The targets use `rm -sf` on the two named services, and `e2e-reset` drops only the `e2e_state` volume by name.

## Consequences

Positive:
- The incident is not reproducible. Replaying the original script — same `#signalk-address` fill, same navigate-away, same "Save and Continue" — against `:5174` leaves `settings.yaml` byte-identical (verified by checksum), with the write landing on the E2E backend and the page's `/api` requests provably confined to `:8090` and `:5174`.
- Isolation covers all runtime state, not just settings: the E2E backend generates its own `secrets.key`, `secrets.sqlite`, `dashboard-pages.json` and caches inside its volume, and the repo's `backend/data` is untouched.
- `HELMCENTRAL_STATE_DIR` is generally useful beyond E2E — it is the missing knob for running a second instance, or for a deployment that wants state outside the working directory.
- Unset, the variable changes nothing: existing dev, prod and CI paths keep their current relative-path behaviour.

Tradeoffs:
- **A second stack costs resources.** Two more containers, a second `npm install`, and a Go rebuild on first start (~40s). Mitigated by it being on-demand — `make e2e-up` when needed, `make e2e-down` after — rather than part of `make dev`.
- **The state root is honoured by convention, not enforced.** Backend code that reaches for a path without going through `cacheFilePath` bypasses isolation entirely. `cacheFilePath` is currently the sole choke point and the state-dir test sweeps a representative set of fallbacks, but nothing *prevents* a future direct `os.WriteFile("data/foo.json")`.
- **`SETTINGS_FILE` is still separate from the state root.** It predates it, has its own `../settings.yaml` default resolved at ~20 call sites, and folding it in would mean a broad mechanical refactor. The E2E profile sets both variables explicitly. This is a known seam.
- **Isolation now depends on two independent mechanisms staying in sync**, because the frontend has two ways of reaching the backend. A hook that hardcodes a URL without honouring `VITE_API_BASE_URL` would silently punch through the isolation again, and nothing detects that. The underlying smell — an app that assumes its backend is on port 8080 of the current host — is left alone here.
- **The seed's dead SignalK address makes the E2E stack wrong for data-dependent checks.** Anything needing live vessel values must use `:5173` read-only, which keeps a category of verification on the shared stack.

## Not addressed here

**Superseded by ADR 0027**, which added the validation described below. The reasoning is kept for the record.

`POST /api/settings` still persists an unvalidated, possibly unreachable SignalK address, and "Save and Continue" still makes that the default outcome of navigating away from an edit. This ADR removes the *blast radius* (a test can no longer reach the real config) but not the underlying asymmetry between the two save endpoints — a hand-typed typo in that field will still silently take the dashboard offline with no feedback. Whether the bulk path should validate the SignalK stanza, warn, or leave it to the operator is a separate decision.
