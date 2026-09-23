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
export function nextWaypoint(route: Route, status: ActiveRouteStatus): NextWaypointResult | null {
  if (status.state !== 'active') return null
  if (status.routeId !== route.id) return null
  if (route.waypoints.length === 0) return null

  const traversal = status.reverse ? [...route.waypoints].reverse() : route.waypoints
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
 * Turns a distance into an ETA against SOG-or-planning-speed, the basis
 * rule etaToWaypoint and etaToRouteEnd both apply: SOG when it's a
 * meaningful reading above SOG_BASIS_THRESHOLD_KTS, otherwise the supplied
 * planning speed. A resolved speed of zero (or non-finite) gives a null ETA
 * rather than an Infinity or a fabricated time - a genuinely
 * becalmed/stationary boat with zero plan speed has no honest ETA to
 * report.
 */
function etaFromDistance(distanceM: number, sogKts: number | null, planningSpeedKts: number, now: Date): WaypointEta {
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
  return etaFromDistance(distanceM, sogKts, planningSpeedKts, now)
}

export interface RouteEndEta extends WaypointEta {
  /** The final waypoint's name, or `WP n` (1-based traversal position) if it has none. */
  label: string
}

/**
 * ETA to a route's FINAL waypoint in traversal order (ADR 0125) - the
 * trip's arrival, not just the next leg etaToWaypoint reports. Distance is
 * the vessel's own remaining distance to the CURRENT target waypoint
 * (nextWaypoint's result) plus every leg from there to the end of the
 * traversal-order array; speed and the null-when-no-honest-speed rule are
 * exactly etaToWaypoint's. Returns null wherever nextWaypoint itself would
 * (no active route, a route mismatch, or no waypoints at all) - there is no
 * "end of the trip" to report without a "next waypoint" to start counting
 * legs from.
 */
export function etaToRouteEnd(
  vesselLat: number,
  vesselLon: number,
  route: Route,
  status: ActiveRouteStatus,
  sogKts: number | null,
  now: Date,
): RouteEndEta | null {
  if (status.state !== 'active') return null
  const next = nextWaypoint(route, status)
  if (!next) return null

  const traversal = status.reverse ? [...route.waypoints].reverse() : route.waypoints
  const finalIndex = traversal.length - 1
  const finalWaypoint = traversal[finalIndex]
  const label = finalWaypoint.name && finalWaypoint.name.trim() !== '' ? finalWaypoint.name : `WP ${finalIndex + 1}`

  let remainingM = haversineMeters(vesselLat, vesselLon, next.waypoint.lat, next.waypoint.lon)
  for (let i = next.index; i < finalIndex; i++) {
    remainingM += haversineMeters(traversal[i].lat, traversal[i].lon, traversal[i + 1].lat, traversal[i + 1].lon)
  }

  return { ...etaFromDistance(remainingM, sogKts, route.planning_speed_kts, now), label }
}

export interface DestinationEta {
  distanceM: number
  /** null when there is no honest ETA - see the function doc for why a bare destination has no planning-speed fallback. */
  etaAt: Date | null
}

/**
 * ETA to a bare chartplotter destination (ADR 0125): SignalK's Course API
 * `nextPoint` with no Helmcentral route behind it. SOG only - unlike
 * etaToWaypoint/etaToRouteEnd, there is no route and therefore no planning
 * speed to fall back to, so a becalmed or stationary boat has no honest ETA
 * here at all, even though the same vessel would still get one (on plan
 * speed) toward an actual route's waypoint.
 */
export function etaToDestination(
  vesselLat: number,
  vesselLon: number,
  destination: { lat: number; lon: number },
  sogKts: number | null,
  now: Date,
): DestinationEta {
  const distanceM = haversineMeters(vesselLat, vesselLon, destination.lat, destination.lon)

  if (distanceM === 0) {
    return { distanceM, etaAt: new Date(now.getTime()) }
  }
  if (sogKts === null || sogKts <= SOG_BASIS_THRESHOLD_KTS) {
    return { distanceM, etaAt: null }
  }

  const speedMps = sogKts * METERS_PER_SECOND_PER_KT
  const seconds = distanceM / speedMps
  if (!Number.isFinite(seconds)) {
    return { distanceM, etaAt: null }
  }

  return { distanceM, etaAt: new Date(now.getTime() + seconds * 1000) }
}
