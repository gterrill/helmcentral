# ADR 0040: SignalK Delegated Authentication

## Status
Accepted

Independent of ADR 0041 (autopilot widget), but if both ship, ADR 0041's write-tier endpoints are gated by this change's write tier — see "Consequences" below.

## Context

Helmcentral had no authentication at all. It was the top item on the README roadmap, and the README carried a warning naming the exposure: the API can start a generator, throw CZone switches, and rewrite the encrypted secrets store, and the server sent `Access-Control-Allow-Origin: *`.

[halos-org/skip](https://github.com/halos-org/skip) solves the equivalent problem by riding the SignalK server's own session, so it has no user database and no second set of credentials to manage. Helmcentral wanted the same property — SignalK stays the only place accounts exist — without inheriting a design that doesn't fit its deployment shape.

## Decision

### 1. Delegated login, not cookie SSO — Helmcentral is cross-origin from SignalK

Skip is served *by* the SignalK server, so SK's session cookie is same-origin to it and rides along automatically. Helmcentral runs on its own port (`:8080` by default, SignalK on `:3000`), so an SK session cookie set by SignalK's own login page is never sent to Helmcentral's API — cross-origin cookie isolation makes a shared-cookie design a non-starter here, not a policy choice.

Instead, Helmcentral presents its own login form, forwards the submitted credentials to SignalK's `/signalk/v1/auth/login`, and on success mints its **own** HttpOnly session cookie scoped to its own origin (`hc_session`, `backend/auth_session.go` + `backend/auth_handlers.go`). SignalK remains the only place accounts exist and the only password Helmcentral ever asks for — there is no Helmcentral user store, and no second credential to provision, rotate, or leak.

### 2. Role resolution is a two-step call, not a JWT decode

SignalK's login response carries a JWT that does **not** include the user's access level — confirmed against [SignalK/signalk-server issue #1336](https://github.com/SignalK/signalk-server/issues/1336), where clients only get an `APPROVED`-style status back, not a role claim. The authoritative source is `GET {sk}/skServer/loginStatus`, called *with* that JWT, which returns `{"status":"loggedIn","username":"...","userLevel":"admin"|"readonly"|...}`.

`backend/auth_handlers.go` implements exactly that two-step flow (`signalKLoginWithCredentials` then `signalKLoginStatus`) and deliberately does **not** also decode the JWT locally as a cross-check — the two-step call is authoritative, and a belt-and-braces decode would just be dead code pretending to add safety it doesn't.

**One value in the mapping is unverified.** `admin` and `readonly` are documented SignalK/signalk-server `userLevel` values. `readwrite` is strongly implied — a three-tier ACL needs something between readonly and admin — but was never observed against a live server in this environment. `skUserLevelToRole` (`backend/auth_handlers.go`) is the single function this mapping lives in, with the gap recorded in a comment there, so confirming it against a live server later touches exactly one place. Any `userLevel` string not in the map is rejected outright — login fails, naming the unexpected value — rather than defaulted to a permissive tier: fail closed on anything auth-related, per this project's policy.

### 3. No Helmcentral user store, and the SK JWT itself is never kept

The session row is `sessions(token_hash, sk_username, role, created_at, expires_at, last_seen_at)` — the resolved role, not the JWT. Storing the JWT would create a second, confusing credential path with its own independent expiry that Helmcentral would then have to reason about on every request; discarding it once `loginStatus` has answered means there is exactly one expiry to manage (the session's own, below), and exactly one thing a leaked session row can be used for (Helmcentral API access, not direct SignalK access).

Only a SHA-256 hash of the 32-byte `crypto/rand` session token is persisted — the base64url-encoded token itself only ever exists in the cookie and the moment `Create()` returns it. This mirrors the encrypted-secrets-store precedent (ADR 0023) of never keeping a credential in a form a database read alone can weaponize: a stolen `sessions.sqlite` file yields hashes, not usable cookies.

Sessions are a 7-day sliding window: `Validate()` extends `expires_at` by a fresh 7 days whenever a session is used and its `last_seen_at` is more than an hour stale, so a tablet left on the nav station overnight isn't logged out by morning, without writing to the database on every single request. A sweep on startup and hourly (`startSessionSweeper`) removes expired rows on top of `Validate`'s lazy per-row cleanup.

### 4. The service account and user sessions are two unrelated identities — kept unrelated in code, not just in principle

There are two SignalK identities live in the process simultaneously:

| | Who it is | Where it lives | What it does |
| --- | --- | --- | --- |
| **Service account** (pre-existing) | Helmcentral itself | `SIGNALK_USERNAME`/`SIGNALK_PASSWORD` in the secrets store, cached JWT in `skTokenCache` (`backend/signalk.go`) | Delta-stream subscription, `putSignalKValue` writes, route activation, alarm `notifications.*` writes |
| **User session** (this change) | The person at the helm | `hc_session` cookie → `sessions` table | Authorizes inbound `/api` calls only |

`signalKLoginWithCredentials`/`signalKLoginStatus` (the user-login path) are separate functions from `acquireSignalKToken`/`skTokenCache` (the service-account path) and never call into each other or share state — a user's login can never populate, read, or invalidate the service account's cached token, and vice versa. `TestLoginHandler_SuccessCallsLoginThenLoginStatusAndSetsCookie` asserts `skTokenCache` is still nil after a successful user login, as a direct check on this property, not just an inference from the code being separate.

The converse matters more: **no outbound SignalK call is ever routed through a user session.** `putSignalKNotification` (the alarm delivery path), `putSignalKValue`, and route activation all authenticate exclusively via `acquireSignalKToken`, which reads only the service account's env-sourced credentials — nothing in that call chain touches `sessionStore`, a cookie, or `sessionCookieName` anywhere. `TestPutSignalKNotification_ReachableWithNoUserSessionPresent` (`backend/alarm_notify_test.go`) proves this directly: it calls the real notification-write function against a stub SignalK server without ever constructing a `sessionStore`, and the call succeeds. Alarms, the anchor-drag watchdog, and the off-boat heartbeat must keep working with every browser closed and nobody logged in; a user session becoming a dependency of that path would be a regression this test exists to catch.

### 5. Three tiers, enforced as Echo groups, not per-route tags

| Tier | Minimum role | Covers |
| --- | --- | --- |
| `public` | none | `/api/health`, `/api/auth/*`, the embedded SPA |
| `read` | `readonly` | every `GET /api/*` |
| `write` | `readwrite` | anything that commands equipment or changes stored state that isn't itself a security setting — switches, generator, routes, alarm acknowledgement/rules, anchor watch, sat charts, and (if ADR 0041 has shipped) `/api/autopilot/*` |
| `admin` | `admin` | `/api/settings/*`, `/api/settings/secrets/*`, `/api/plugins/*`, `/api/alarm-transports*` |

`backend/main.go`'s `buildAPIRoutes` is a single table of `{method, path, tier, handler}` — data, not a sequence of `e.GET`/`e.POST` calls with an auth tag bolted onto each — registered through `registerAPIRoutes` (`backend/auth_middleware.go`), the *only* place any `/api` route is allowed to reach the Echo instance. Groups over per-route tags is deliberate: a route added to the wrong tier is a visible, reviewable mistake in one table, whereas a forgotten per-route tag silently defaults to open and is easy to miss in review.

This is enforced, not just documented: `registerAPIRoutes` returns a `"METHOD path" -> tier` registry built from exactly what it registered, and `TestAPIRouteCoverage_EveryRegisteredAPIRouteHasATier` (`backend/auth_middleware_test.go`) walks Echo's live router after calling the same `buildAPIRoutes`/`registerAPIRoutes` main() uses, and fails if any `/api` route exists that the registry doesn't know about. That can only happen if a future change registers a route directly on the `echo.Echo` instead of adding a table entry — which is exactly the "next endpoint added unprotected" scenario this test exists to catch before it ships. A second test (`TestAPIRouteCoverage_EveryProductionRouteHasAnExplicitTier`) additionally guards against a table entry that forgot to set `Tier` at all: the tier enum's zero value is `tierUnset`, not `tierPublic`, specifically so an omitted field fails loudly (`registerAPIRoutes` panics) instead of silently registering as the most permissive tier.

### 6. `auth.mode` is an explicit setting, not auto-detected

A fresh SignalK install has security disabled, and `/auth/login` fails for everyone in that state. Auto-detecting "security is off" and silently running Helmcentral open would be exactly the masking fallback this project's fallback policy forbids: it would turn a SignalK misconfiguration into an invisible wide-open Helmcentral, with no log line and no operator decision point. So `auth.mode: signalk | none` in `settings.yaml` is explicit, and the two states behave differently on purpose:

- `none` — no auth (Helmcentral's pre-existing behaviour). Every startup logs a warning naming the exposure (equipment control, secrets endpoints) and pointing at this ADR.
- `signalk` — delegated login as described above, enforced on every non-public request. At startup, `checkAuthModeAtStartup` (`backend/auth_startup.go`) probes SignalK once via `probeSignalKSecurityEnabled` and **fails fast** — the process does not start — if SignalK's security is off, because "Helmcentral requires login" against a server with no login to require is unsatisfiable and must not boot into a half-state where the login screen is unskippable but always fails.

`settings.yaml` is the only source for `auth.mode`; it is read per request, so a change takes effect without a restart. An earlier revision of this ADR specified a `HELMCENTRAL_AUTH_MODE` environment override as a recovery hatch for an operator locked out of `mode: signalk`. It was removed: it was redundant, since editing `settings.yaml` is what the override amounted to anyway and a locked-out operator has exactly the same access to that file, and a second source that silently outranked the Settings page made "I changed it and nothing happened" a likely outcome during setup.

The lockout it was meant to undo is now **prevented** instead. `validateSettingsChange` (`backend/signalk.go`) probes SignalK when a save would turn authentication on and refuses `mode: signalk` against a server whose security is off — at save time, in the UI, rather than at the next boot. Turning authentication *off* is deliberately never gated on SignalK being reachable: that is the way back, and requiring a working SignalK to disable a setting that a broken SignalK made unsatisfiable would be circular.

**`probeSignalKSecurityEnabled`'s method is an assumption, stated rather than hidden**, matching ADR 0041's precedent for recording an unverified-against-a-live-server detail instead of glossing over it: it POSTs deliberately bad credentials to `/signalk/v1/auth/login` and reads a `404` as "security disabled" (signalk-server only wires that route when a security strategy is active) versus any other response as "security enabled" (the route exists and answered, even to reject the bad credentials). This was implemented and tested against an `httptest` stub built to that assumption, not against a real SignalK server with security switched off. If a live check finds signalk-server actually 200s (or otherwise doesn't 404) that route with security off, this is the one function that needs to change.

### 7. Default is `none` for this release

Existing installs are unauthenticated today. Shipping `signalk` as the default would silently lock every existing boat's dashboard behind a login screen on the next `install.sh` re-run — turning an upgrade into an outage, on a device that may be hard to reach. `none` stays the default for one release; the README and `docs/configuration.md` say plainly that `signalk` is recommended and why, and a later major version can flip the default once operators have had a release to opt in deliberately.

### 8. CORS is tightened alongside auth, not left for later

`AllowOrigins: []string{"*"}` combined with credentialed requests is rejected by every modern browser anyway, so the pre-existing config was simultaneously advertising an open API in the README's warning *and* not functioning for any credentialed cross-origin caller — the worst of both. `corsMiddleware` (`backend/cors.go`) replaces it with an allowlist computed per request: the server's own origin (whatever host:port/scheme the browser used to reach it — a LAN IP, a hostname, `localhost` in dev) plus an optional `CORS_ALLOWED_ORIGINS` (comma-separated) for a deployment that genuinely needs a second origin, with `AllowCredentials: true` so the session cookie can actually be used. This has to land with the rest of this change: a session cookie is useless if the browser won't send it cross-origin in the first place, and `AllowCredentials: true` next to a wildcard origin is simply invalid per the CORS spec.

### 9. The client-side gate fails closed on an unknown mode

`App.tsx` renders one of four things, checked in order: a "checking sign-in" state while the probe is in flight, an explicit "could not determine" state with a retry when `mode` is `null`, the login screen when `mode` is `signalk` with no user, and only then the dashboard.

The two negative branches exist because the UI's role gates — `canWrite`, `canAdmin` — are derived from `auth.mode !== 'signalk'`, so an *unknown* mode reads as permissive. "Render the dashboard whenever mode isn't literally `signalk`" would therefore hand a visitor full admin affordances whenever the `/api/auth/mode` probe merely failed, which is a fail-open gate reached by a transient 500. The server is still the enforcement point and nothing is actually authorised by this, so the exposure is cosmetic — but a UI that silently guesses "probably no auth, then" when it could not ask is the same masking-fallback shape this project rejects everywhere else, and `use-auth.ts` already (correctly) refuses to coerce an *unrecognised* mode value. The two behaviours now agree.

The cost is that tests rendering `<App />` must state their auth precondition rather than inheriting one; the five dashboard tests that previously relied on the fall-through now mock `use-auth` with `mode: 'none'` explicitly.

## Consequences

- The three-tier model is coarser than SignalK's own per-path ACLs — Helmcentral does not attempt to mirror SignalK's fine-grained resource permissions, only its three access levels. A `readonly` SignalK user is `readonly` in Helmcentral for everything Helmcentral exposes, not path-by-path.
- `readwrite`'s mapping is unverified (decision 2) and `probeSignalKSecurityEnabled`'s detection method is unverified (decision 6) — both are isolated to one function each specifically so a live-server check is a small, contained change rather than a redesign.
- `mode: none` remains the shipped default, so a fresh install or an upgraded existing one is unauthenticated until an operator opts in — the startup warning is the only safeguard, which is a deliberate, documented trade-off (decision 7), not an oversight.
- If ADR 0041 (autopilot) has shipped, its `/api/autopilot/*` control endpoints are gated at the write tier here — engage/disengage/tack/gybe/dodge now require a `readwrite` session under `mode: signalk`, same as the generator and CZone switches.
- Login itself is only as available as SignalK: if SignalK is unreachable, nobody can log in, including an admin — there is no local fallback credential. Given there is deliberately no second credential store, this is the accepted trade-off, not a gap to close.

## Verification

1. `cd backend && go vet ./... && gofmt -l . && go test -short -count=1 ./...` — all backend tests, including:
   - `auth_session_test.go` — mint/validate/expire/sweep/renew, and that a stored hash cannot round-trip into a usable token.
   - `auth_handlers_test.go` — the full login flow against an `httptest` SignalK stub (success including the `loginStatus` second call, bad credentials, SignalK unreachable, an unrecognised `userLevel` rejected), logout, and that a successful user login never touches `skTokenCache`.
   - `auth_middleware_test.go` — the `{path, method, role} -> status` table across all four tiers, `mode: none` passthrough, and the route-coverage walk described in decision 5.
   - `main_test.go` — `checkAuthModeAtStartup` against a security-off stub returning the fail-fast error, an unrecognised `auth.mode` value doing the same, and `mode: none` never probing SignalK at all.
   - `cors_test.go` — the allowlist reflects the request's own origin (not `*`), rejects an origin outside it, and honours `CORS_ALLOWED_ORIGINS`.
   - `alarm_notify_test.go`'s `TestPutSignalKNotification_ReachableWithNoUserSessionPresent` — the regression check from decision 4.
2. **Outstanding, not yet run** (no security-enabled SignalK server was reachable in this environment, matching ADR 0041's precedent for recording this rather than hiding it):
   - Confirm `readwrite` is really the `userLevel` string a live server reports for a readwrite user (decision 2).
   - Confirm `probeSignalKSecurityEnabled`'s 404-means-disabled assumption against a real security-off SignalK server (decision 6).
   - Log in as each of readonly/readwrite/admin against a real server and confirm the Settings page, generator button, and switches respond per tier.
   - Confirm the dashboard still streams after login — the SSE cookie path (`EventSource` sends same-origin cookies automatically but cannot set an `Authorization` header, which is why the cookie model was chosen over a bearer token) is the most likely thing to break.
   - With `mode: signalk` and no browser open at all, trip an alarm rule and confirm it still fires and delivers over ntfy, then confirm the heartbeat still arrives — the live-server counterpart to decision 4's unit test.
   - Set `mode: signalk` against a security-off SignalK server and confirm the process exits with a message naming the problem, not just that `checkAuthModeAtStartup` returns an error in a test.
