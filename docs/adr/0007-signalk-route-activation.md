# ADR 0007: SignalK Route Activation

## Status
Accepted

## Context
ADR 0006 added manual route planning (CRUD + waypoint/distance/ETA helpers) but explicitly excluded any form of active route-following: no cross-track error, no bearing-to-waypoint live nav UI, no autopilot integration. That scope boundary remains unchanged here.

The operator's chartplotter (Timezero Professional) already pushes its active route onto the boat's SignalK server, which other NMEA2000 devices (autopilots, MFDs) can read and follow. Only the next few waypoints are broadcast at a time, advancing as the vessel progresses. The existing SignalK→N2K bridge handles this PGN 129285 windowing. The operator wants to activate HelmCentral's saved routes the same way. HelmCentral publishes SignalK's standard active-route state for other equipment to consume; it does not perform live navigation or follow the route itself.

The SignalK ecosystem defines two relevant standard APIs for this:
- The Resources API (`PUT /signalk/v1/api/resources/routes/{id}`), which stores route geometry as a GeoJSON-bearing resource.
- The Course API (`PUT/DELETE/GET /signalk/v1/api/vessels/self/navigation/course/activeRoute`, `GET .../navigation/course`), which tracks which resource (if any) is the vessel's currently active route.

This repo has no vendored SignalK schema or client to verify these shapes against; they were implemented against the documented standard SignalK spec, then verified live against the operator's actual boat SignalK server (v2.24.0, `@signalk/resources-provider` 1.5.1, `@signalk/course-provider` 1.4.0) during development. That live test surfaced two real discrepancies from the initial assumption, both fixed before this ADR was finalized:
- Both APIs are exposed under `/signalk/v2/api/...`, not `/signalk/v1/api/...` as initially assumed. The v1 paths returned a structured `{"state":"COMPLETED","statusCode":405,"message":"PUT not supported for ..."}` response — SignalK's core correctly routed the request through its generic PUT-handler dispatch, but no plugin had registered a v1 handler for these paths; the resources-provider and course-provider plugins both register their write handlers under v2 instead.
- Deactivating must `DELETE` the course resource itself (`/signalk/v2/api/vessels/self/navigation/course`), not the `activeRoute` sub-path (`DELETE .../course/activeRoute` 404s — `DELETE` isn't registered at that more specific path on this plugin version, only at the parent).

With those two fixes, a full activate → status → deactivate cycle was confirmed working end-to-end against the real boat, including the course-provider plugin correctly computing `nextPoint`/`previousPoint` from the vessel's live position once a route was active.

## Decision

### Backend: `backend/route_activation.go`
- Reuses HelmCentral's own route ID as the SignalK resource ID, for a simple 1:1 mapping between `routeData.ID` and the Course API's `href` field (`/resources/routes/{routeID}`).
- A route's waypoints convert to a GeoJSON `LineString` feature with coordinates flipped from HelmCentral's `{lat, lon}` to GeoJSON's `[lon, lat]` order — the Go-side equivalent of the frontend's existing `routeToGeoJSON` (`route-planner-map.tsx`), implemented separately since one is Go and one is TypeScript. Total distance reuses the existing `haversineMeters` helper (`signalk.go`) rather than introducing a second distance calculation.
- A new low-level helper, `signalkRequestJSON`/`signalkRequestJSONWithAuth`, generalizes `czone.go`'s `putSignalKValue` to arbitrary HTTP methods and raw (non-`{"value":...}`-wrapped) JSON bodies, since the Course/Resources APIs expect raw bodies — unlike every existing PUT-based SignalK write in this codebase, which targets data-model paths expecting the `{"value": ...}` envelope. It reuses the same auth/token-cache/retry-once mechanics as `generator.go`'s `generatorPut`, with one refinement: retry is scoped to auth-looking failures (401/403, or a transport error) rather than retrying unconditionally on any non-2xx, so a genuine 400 Bad Request from a malformed payload surfaces immediately rather than being retried pointlessly.
- Activate (`activateSignalKRoute`) upserts the resource, then PUTs the Course API's `activeRoute` pointing at it (`pointIndex: 0, reverse: false` — no UI exists yet to set a different start point or reverse direction; deliberate simplification, not an oversight).
- Deactivate (`deactivateSignalKRoute`) issues `DELETE` on the Course API's `activeRoute` only. It does not delete the underlying SignalK resource — resources are left upserted/stale after deactivation, since resource cleanup is out of scope and a stale resource with no active course pointing at it is harmless.
- Status (`fetchSignalKCourseStatus`) reads the Course API and reports whether an active route exists, and if so whether its href maps back to a known local route ID (it may not — e.g. a route activated directly from Timezero).
- All SignalK call failures propagate SignalK's raw HTTP status and response body back through HelmCentral's own API error response, rather than a generic "failed" message — deliberate, to make live debugging against the operator's real boat fast, since exact SignalK server behavior can't be verified from a dev sandbox.

### API
- `POST /api/routes/:id/activate` — upserts + activates; `502` with SignalK's raw error body embedded on any upstream failure.
- `POST /api/routes/deactivate` — clears the active route; `502` with raw error body on upstream failure.
- `GET /api/routes/active` — `{active: bool, route_id: string|null, route_name?, point_index?, reverse?}`; `route_id: null` while `active: true` indicates a foreign/unrecognized active route resource (a valid state, not an error). A `502` (SignalK unreachable) is distinct from `active: false` — callers must not conflate "can't confirm" with "confirmed inactive."

### Frontend
- A new `hooks/use-route-activation.ts` hook owns activation state (polling, in-flight activate/deactivate calls, error state), kept entirely separate from `hooks/use-routes.ts` (route CRUD, unpolled) and `hooks/use-dashboard-route.ts` (the local-only "pinned to dashboard" star toggle). These three remain independent by design — Activate/Deactivate (this feature), the dashboard star (ADR 0006), and route CRUD are orthogonal controls that must not be conflated, per explicit product direction.
- The hook polls `/api/routes/active` every 15 seconds (informational cadence, not the faster live-safety cadence used by anchor watch, since route activation state is not safety-critical and route-following itself remains out of scope).
- A failed poll sets status to `unknown`, never silently falls back to `inactive` — a transient SignalK/network failure must not make the UI claim "no route is active" when other N2K equipment may still be following it regardless of whether HelmCentral can currently reach SignalK to confirm.
- `components/route-planner-drawer.tsx` gains an Activate/Deactivate button and an "ACTIVE" badge per route row, a header-level note when an unrecognized route is active, and a status-unknown indicator when the poll has failed. The existing dashboard star toggle and `RouteTile` are unchanged and use none of this new state.

## Consequences
Positive:
- Other NMEA2000 devices on the bus (autopilots, MFDs) can follow a HelmCentral-planned route exactly as they already do for Timezero-activated routes, without HelmCentral needing to implement any live navigation itself.
- CRUD, dashboard pinning, and activation have independent hooks and controls.
- Activation failures show SignalK's exact error text, helping diagnose problems against a boat server that the dev sandbox cannot reach.

Negative / explicitly deferred:
- No support for activating a route at a `pointIndex` other than 0, or in `reverse`. This needs additional UI and is deferred until needed.
- No SignalK resource deletion on deactivate, or when a HelmCentral route itself is deleted; SignalK's `resources/routes/` collection will accumulate one stale entry per route ever activated.
- No cross-track error, bearing-to-waypoint nav, or other live-following UI in HelmCentral. The feature publishes state for other equipment, consistent with ADR 0006's scope.

## Related
- ADR 0006: Manual Route Planning with Smart Helpers (the feature this extends)
