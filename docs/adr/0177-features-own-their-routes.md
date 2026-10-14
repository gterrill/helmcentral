# ADR 0177: Features own their routes

## Status

Accepted (2026-10-14). Amends [ADR 0040](0040-signalk-delegated-authentication.md)
section 5, the registration point.

## Context

[ADR 0040](0040-signalk-delegated-authentication.md) made `buildAPIRoutes` in
`main.go` the single table of `{method, path, tier, handler}`, so that a route
in the wrong tier is a visible mistake in one place rather than a forgotten tag
that silently defaults to open.

The table did its job, and it grew to 255 rows in a 430-line literal grouped by
tier. In practice almost every feature commit edited `main.go`, and one
feature's rows were scattered over four tier blocks: the read rows, the write
rows, and the admin rows for the same family sat hundreds of lines apart.
`main.go` also held the vessel, electrical, solar, tanks and nearby payload
builders, so it was the file everything touched.

## Decision

Each feature's handlers file exports its route rows as a function, for example
`inventoryRoutes()` in `inventory_handlers.go` and `assistantRoutes()` in
`assistant_handlers.go`. Every row still spells out its method, path, tier and
handler; the tier is written on every row, and a function groups its rows
however reads best. Single-endpoint families join the feature they belong to
(the weather, wave, tide, POI, upper-air and forecast-warning endpoints are one
`forecastProviderRoutes()`).

`buildAPIRoutes` keeps its signature and becomes a concatenation of those
functions. `registerAPIRoutes` is unchanged and remains the only place a route
reaches Echo. Startup order in `main()` does not change. Most handlers read
package globals at request time, but a few factory rows capture a global when
`buildAPIRoutes` runs (`getNearbyVesselSightingsHandler(globalNearbyContactStore)`,
and the tile and basemap factories over `globalTileCache`), so those globals
must be set before routes are registered in `main()`.

What the tests now cover:

- The coverage, duplicate and explicit-tier tests (`TestAPIRouteCoverage_*` in
  `auth_middleware_test.go`) catch a route with a missing tier, a route
  registered outside the table, and a duplicate.
- A composition test fails if a `xxxRoutes() []apiRoute` function exists but
  `buildAPIRoutes` never calls it.
- An admin-prefix test pins the admin surfaces (`/api/settings`,
  `/api/plugins/`, `/api/alarm-transports`) to `tierAdmin`.
- A wrong read or write tier on any other row is caught only in review, beside
  its handler.

The payload builders and their types moved out of `main.go` beside their
handlers (`vessel_state.go`, `electrical_state.go`, `solar_state.go`,
`tanks_state.go`, `nearby_vessels.go`), so `main.go` holds startup, middleware
and the composition of the table.

## Alternatives rejected

- **One `routes.go` table grouped by feature.** Still a shared file that every
  feature edits.
- **A deps struct replacing the package globals.** It would change startup
  ordering, since some stores are opened after route registration. A separate
  change if it is ever wanted.
- **Committing a golden route file.** It reintroduces a shared file that every
  route change has to touch.

## Consequences

A feature change edits the feature's own file. Adding an endpoint means adding a
row to that feature's function; a new feature adds a function and one line in
`buildAPIRoutes`. Reviewing a tier means reading the row next to its handler.
No route, method, path or tier changed with this move.
