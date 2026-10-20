# ADR 0195: Routes page removed; route display follows SignalK

## Status

Accepted (2026-10-21). Supersedes
[ADR 0006](0006-manual-route-planning.md),
[ADR 0007](0007-signalk-route-activation.md) and
[ADR 0009](0009-gshhg-coastline-fallback.md), and supersedes
[ADR 0016](0016-cached-deeper-esri-imagery-and-area-prefetch.md) in part.
Amends [ADR 0125](0125-forecast-conditions-tile-and-trip-eta.md).

## Context

ADR 0006 gave Helmcentral a route planner: a Routes page where the operator
drew waypoints, saved them to `routes.json`, starred one for the dashboard and
activated it. ADR 0007 pushed an activated route to SignalK's Resources and
Course APIs so that autopilots and plotters could follow it.

On this boat that loop is not the one the operator uses. Routes are planned and
activated in TimeZero, which reaches SignalK only as RMB, APB and XTE
sentences: the Course API shows `activeRoute: null` and a `nextPoint` of type
Location. The Routes page was kept up for the sake of a feature nobody used, and
it carried a long tail with it: a map built on the Carto basemap with its own
hybrid-satellite restyling, a GSHHG coastline layer (ADR 0009), an in-app "cache
this area" imagery prefetch (ADR 0016), CRUD and activation endpoints, a
`routes.json` store, a dashboard star, and Mate's saved-waypoint step in
`find_places`.

The Route tile, the Nearby map's route line and the clock's trip ETA are still
worth having. They only ever needed to know which route the boat is following,
and SignalK already says.

## Decision

- **The Routes page is removed.** Nothing in Helmcentral creates, stars or
  activates a route. `/routes` is no longer a path; it opens the dashboard like
  any other unknown path.
- **Saved routes are removed.** `routes.json`, `ROUTES_FILE`, the CRUD and
  activate/deactivate endpoints, the route-to-SignalK-resource conversion and
  the dashboard route star are deleted. Nothing is migrated: routes planned
  here are not read from anywhere else.
- **`GET /api/routes/active` is the one route endpoint left, and it reports
  what SignalK says.**
  - With `activeRoute.href` set, the backend reads that route resource from
    SignalK's Resources API and returns `route: {name, waypoints}` in authored
    order (the resource's `[lon, lat]` coordinates flipped to lat/lon, names from
    `coordinatesMeta` where present), with `point_index` and `reverse` passed
    through unchanged. The frontend applies `reverse`, as before.
  - If the resource cannot be read, or the href is not a routes resource, the
    response is 502 with SignalK's error. It is never "inactive": a route SignalK
    says is active must not be reported as absent.
  - `destination` (ADR 0125) is unchanged: the Course API's `nextPoint` with its
    place name, present whenever SignalK carries one.
- **The Route tile follows the status, and does nothing when tapped.** An active
  route shows its name, the distance left to its end and the time to get there.
  With no route but a next point it shows that point, or "Next waypoint" until
  the place has a name. With neither it shows `--`. While the first answer is pending it shows plain `--`. When the route cannot
  be read it shows `--` and a muted "Route unavailable", never "inactive" and
  never a claim that SignalK is down. An active route with no waypoints is
  treated as no route, so the next point is used, as the clock does. This is
  not a masking fallback; the tile reports exactly what SignalK sends, and
  nothing when SignalK sends nothing.
- **ETA is speed over ground only.** A route in SignalK carries no planning
  speed, so the plan-speed basis ADR 0125 gave an activated Helmcentral route
  goes. A boat that is stopped has no ETA, as a bare destination already had
  none.
- **The map-only endpoints go with the planner.** `/api/gshhg-coastline`
  (with its embedded dataset), `POST /api/world-imagery/prefetch` and
  `GET /api/world-imagery/prefetch/:jobId` had no caller outside the route
  planner map and are deleted with their job machinery. The Esri imagery proxy,
  its cache and `DELETE /api/world-imagery/cache` remain; the anchor map still
  uses them.
- **Mate.** `find_places` no longer searches saved waypoints, so it always asks
  the place-names plugin for its broad search. The prompt no longer ends a
  passage recommendation by offering to create a route.
- **Unchanged.** The `route` tile id stays in `validDashboardWidgetIDs`, and
  the stale-alarm expiry for APB/RMB route alarms (ADR 0144) is untouched.

The shared SignalK REST helpers that lived beside the activation code
(`signalkRequestJSON*`, `resolveSignalKURL`) moved to `signalk_rest.go`
unchanged; autopilot, collision, radar, sensor names, alarms and auth use them.

## Consequences

- On this boat the Route tile mostly shows the TimeZero next waypoint, and the
  full route only when a SignalK client (Freeboard-SK, for example) activates a
  route resource.
- An operator who relied on the Routes page has to plan and activate routes on
  the plotter or in a SignalK app. The change is recorded as Breaking in the
  changelog.
- Stored `routes.json` files are left on disk, unread. The operator can delete
  them.
- The clock tile still accepts a "plan" basis it is no longer sent.

## Alternatives considered

- **Keep saved routes and drop only the page.** Rejected: with no way to
  create or activate a route, the store, endpoints and star would have no
  caller.
- **Keep the tile click-through to a read-only route view.** Rejected: the
  Routes page was the whole of that view, and a second map to maintain for a
  route the plotter owns is the cost this decision is removing.
- **Treat an unreadable route resource as an inactive route.** Rejected under
  the fail-fast policy; a 502 lets the tile say "Route unavailable" instead of
  presenting a guess.
