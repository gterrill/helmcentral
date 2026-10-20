import { haversineMeters } from '@/lib/geo'
import type { RouteWaypoint } from '@/lib/route-calc'
import type { ActiveRouteStatus } from '@/hooks/use-route-activation'

const METERS_PER_SECOND_PER_KT = 1852 / 3600
const SOG_BASIS_THRESHOLD_KTS = 0.5

export interface NextWaypointResult {
  /** Index into the traversal-order array (see the module comment below). */
  index: number
  waypoint: RouteWaypoint
  /** waypoint.name if present and non-empty, else `WP n` (1-based traversal position). */
  label: string
}

/**
 * SignalK's Course API `pointIndex`/`reverse` convention, confirmed against
 * signalk-server's course-provider-plugin reference implementation
 * (src/lib/course.ts, routeRemaining): when `reverse` is false, `pointIndex`
 * indexes the route's waypoint array directly in its authored order
 * (waypoints[pointIndex]). When `reverse` is true, the SAME `pointIndex` is
 * counted from the END of that array instead - the point being headed
 * toward is waypoints[waypoints.length - 1 - pointIndex].
 *
 * The backend (active_route.go) never reorders waypoints based on
 * `reverse` - it serves them in authored order always, and passes
 * point_index/reverse straight through with zero transformation. So this
 * traversal-order mapping has to happen here, on the frontend, once.
 */

/**
 * The route's waypoints in the order `status.pointIndex` counts through -
 * reversed when `status.reverse` is set, authored order otherwise. Exported
 * (rather than kept private to nextWaypoint below) so a consumer that needs
 * the whole ordered route - not just the single next waypoint - gets it from
 * the one place this reversal is handled, instead of re-deriving it.
 *
 * Returns null when the status is not an active route, or the route has no
 * waypoints.
 */
export function traversalOrder(status: ActiveRouteStatus): RouteWaypoint[] | null {
  if (status.state !== 'active') return null
  const waypoints = status.route.waypoints
  if (waypoints.length === 0) return null
  return status.reverse ? [...waypoints].reverse() : waypoints
}

export function nextWaypoint(status: ActiveRouteStatus): NextWaypointResult | null {
  const traversal = traversalOrder(status)
  // traversal is only ever non-null when status.state === 'active', but
  // TypeScript can't see that through the function boundary.
  if (!traversal || status.state !== 'active') return null

  const index = Math.min(Math.max(status.pointIndex, 0), traversal.length - 1)
  const waypoint = traversal[index]
  const label = waypoint.name && waypoint.name.trim() !== '' ? waypoint.name : `WP ${index + 1}`

  return { index, waypoint, label }
}

export interface RouteEndEta {
  distanceM: number
  /** null when SOG gives no honest ETA - there is no planning speed to fall back to. */
  etaAt: Date | null
  /** The final waypoint's name, or `WP n` (1-based traversal position) if it has none. */
  label: string
}

/**
 * ETA to an active route's FINAL waypoint in traversal order (ADR 0125) - the
 * trip's arrival, not just the next leg. Distance is the vessel's own
 * remaining distance to the CURRENT target waypoint (nextWaypoint's result)
 * plus every leg from there to the end of the traversal-order array. SOG
 * only, like etaToDestination: SignalK's route carries no planning speed.
 * Returns null wherever nextWaypoint itself would (not active, or no
 * waypoints at all).
 */
export function etaToRouteEnd(
  vesselLat: number,
  vesselLon: number,
  status: ActiveRouteStatus,
  sogKts: number | null,
  now: Date,
): RouteEndEta | null {
  const next = nextWaypoint(status)
  const traversal = traversalOrder(status)
  if (!next || !traversal) return null

  const finalIndex = traversal.length - 1
  const finalWaypoint = traversal[finalIndex]
  const label = finalWaypoint.name && finalWaypoint.name.trim() !== '' ? finalWaypoint.name : `WP ${finalIndex + 1}`

  let remainingM = haversineMeters(vesselLat, vesselLon, next.waypoint.lat, next.waypoint.lon)
  for (let i = next.index; i < finalIndex; i++) {
    remainingM += haversineMeters(traversal[i].lat, traversal[i].lon, traversal[i + 1].lat, traversal[i + 1].lon)
  }

  return { ...etaFromDistance(remainingM, sogKts, now), label }
}

/**
 * Turns a distance into an ETA against SOG: a meaningful reading above
 * SOG_BASIS_THRESHOLD_KTS, otherwise no ETA at all - a becalmed or stationary
 * boat has no honest arrival time to report, and nothing here invents one.
 */
function etaFromDistance(distanceM: number, sogKts: number | null, now: Date): { distanceM: number; etaAt: Date | null } {
  if (distanceM === 0) return { distanceM, etaAt: new Date(now.getTime()) }
  if (sogKts === null || sogKts <= SOG_BASIS_THRESHOLD_KTS) return { distanceM, etaAt: null }

  const seconds = distanceM / (sogKts * METERS_PER_SECOND_PER_KT)
  if (!Number.isFinite(seconds)) return { distanceM, etaAt: null }
  return { distanceM, etaAt: new Date(now.getTime() + seconds * 1000) }
}

export interface DestinationEta {
  distanceM: number
  /** null when SOG gives no honest ETA (becalmed or stationary). */
  etaAt: Date | null
}

/**
 * ETA to a bare chartplotter destination (ADR 0125): SignalK's Course API
 * `nextPoint` with no route behind it. SOG only, like etaToRouteEnd, so a
 * becalmed or stationary boat has no honest ETA.
 */
export function etaToDestination(
  vesselLat: number,
  vesselLon: number,
  destination: { lat: number; lon: number },
  sogKts: number | null,
  now: Date,
): DestinationEta {
  return etaFromDistance(haversineMeters(vesselLat, vesselLon, destination.lat, destination.lon), sogKts, now)
}

/** Structurally identical to clock-tile.tsx's ClockTileTripEta - duck-typed rather than imported so this lib module has no dependency on a components/ file. */
export interface ClockTripEta {
  label: string | null
  etaAt: Date | null
}

/**
 * The clock tile's trip ETA (ADR 0092, ADR 0125): the whole trip's arrival,
 * from whatever SignalK is navigating. An active route reports to its FINAL
 * waypoint via etaToRouteEnd; otherwise a bare Course API destination (a
 * chartplotter go-to) reports via etaToDestination. Pulled out of App.tsx as
 * its own pure function so this decision is unit-testable without rendering
 * the whole app. Returns null when there is genuinely nothing to report (no
 * position fix, an unknown status, or neither a route nor a destination).
 */
export function computeClockTripEta(
  status: ActiveRouteStatus | null,
  vesselLat: number | null,
  vesselLon: number | null,
  sogKts: number | null,
  now: Date,
): ClockTripEta | null {
  if (!status || vesselLat === null || vesselLon === null) return null

  if (status.state === 'active') {
    const eta = etaToRouteEnd(vesselLat, vesselLon, status, sogKts, now)
    if (eta) return { label: eta.label, etaAt: eta.etaAt }
  }

  if ((status.state === 'inactive' || status.state === 'active') && status.destination) {
    const destination = status.destination
    const eta = etaToDestination(vesselLat, vesselLon, destination, sogKts, now)
    const label = destination.name.trim() !== '' ? destination.name : null
    return { label, etaAt: eta.etaAt }
  }

  return null
}
