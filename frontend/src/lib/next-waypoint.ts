import { haversineMeters } from '@/lib/geo'
import type { Route, RouteWaypoint } from '@/hooks/use-routes'
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
 * The backend (route_activation.go) never reorders waypoints based on
 * `reverse` - it stores and serves them in authored order always, and passes
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
 * Returns null under the same conditions nextWaypoint does: not active, a
 * routeId that doesn't match this route, or a route with no waypoints.
 */
export function traversalOrder(route: Route, status: ActiveRouteStatus): RouteWaypoint[] | null {
  if (status.state !== 'active') return null
  if (status.routeId !== route.id) return null
  if (route.waypoints.length === 0) return null
  return status.reverse ? [...route.waypoints].reverse() : route.waypoints
}

export function nextWaypoint(route: Route, status: ActiveRouteStatus): NextWaypointResult | null {
  const traversal = traversalOrder(route, status)
  // The second check is redundant with traversalOrder's own (traversal is
  // only ever non-null when status.state === 'active'), but TypeScript can't
  // see that through the function boundary, so it's what narrows `status`
  // enough to read pointIndex below.
  if (!traversal || status.state !== 'active') return null

  const index = Math.min(Math.max(status.pointIndex, 0), traversal.length - 1)
  const waypoint = traversal[index]
  const label = waypoint.name && waypoint.name.trim() !== '' ? waypoint.name : `WP ${index + 1}`

  return { index, waypoint, label }
}

export interface WaypointEta {
  distanceM: number
  /** null when the distance is 0, or when the resolved speed can't produce a finite ETA. */
  etaAt: Date | null
  basis: 'sog' | 'plan'
}

/**
 * ETA to a single waypoint. Uses SOG when it's a meaningful reading above
 * SOG_BASIS_THRESHOLD_KTS; otherwise falls back to the route's planning
 * speed. A resolved speed of zero (or non-finite) gives a null ETA rather
 * than an Infinity or a fabricated time - a genuinely becalmed/stationary
 * boat with zero plan speed has no honest ETA to report.
 */
export function etaToWaypoint(
  vesselLat: number,
  vesselLon: number,
  waypoint: RouteWaypoint,
  sogKts: number | null,
  planningSpeedKts: number,
  now: Date,
): WaypointEta {
  const distanceM = haversineMeters(vesselLat, vesselLon, waypoint.lat, waypoint.lon)
  const basis: WaypointEta['basis'] = sogKts !== null && sogKts > SOG_BASIS_THRESHOLD_KTS ? 'sog' : 'plan'
  const speedKts = basis === 'sog' ? (sogKts as number) : planningSpeedKts

  if (distanceM === 0) {
    return { distanceM, etaAt: new Date(now.getTime()), basis }
  }

  if (!Number.isFinite(speedKts) || speedKts <= 0) {
    return { distanceM, etaAt: null, basis }
  }

  const speedMps = speedKts * METERS_PER_SECOND_PER_KT
  const seconds = distanceM / speedMps
  if (!Number.isFinite(seconds)) {
    return { distanceM, etaAt: null, basis }
  }

  return { distanceM, etaAt: new Date(now.getTime() + seconds * 1000), basis }
}
