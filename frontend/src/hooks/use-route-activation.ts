import { useMemo } from 'react'
import { useApiQuery } from '@/lib/api-query'
import type { RouteWaypoint } from '@/lib/route-calc'

/** SignalK's Course API `nextPoint` (ADR 0125), reverse-geocoded server-side. `name` is `''` until the backend's cache resolves it - see GET /api/routes/active's own doc comment (backend/active_route.go). */
export interface RouteActivationDestination {
  lat: number
  lon: number
  name: string
}

/** The route SignalK is navigating, read from its route resource in authored order. */
export interface ActiveRoute {
  name: string
  waypoints: RouteWaypoint[]
}

export type ActiveRouteStatus =
  /** `loading`: the first poll has not been answered. `error`: the poll failed or the answer was unusable. */
  | { state: 'unknown'; reason: 'loading' | 'error' }
  | {
      state: 'inactive'
      destination: RouteActivationDestination | null
      /** Set when SignalK names an active route the backend could not read: the destination still stands, this says why the route is missing. */
      routeError?: string
    }
  | {
      state: 'active'
      route: ActiveRoute
      pointIndex: number
      reverse: boolean
      /** The Course API's nextPoint, present whenever the backend sent one. */
      destination: RouteActivationDestination | null
    }

interface ActiveRouteResponse {
  active: boolean
  route?: { name: string; waypoints: RouteWaypoint[] }
  point_index?: number
  reverse?: boolean
  route_error?: string
  destination?: { lat: number; lon: number; name: string }
}

const DEFAULT_POLL_INTERVAL_SECONDS = 15
const ACTIVE_KEY = ['/api/routes/active'] as const
const LOADING: ActiveRouteStatus = { state: 'unknown', reason: 'loading' }
const ERRORED: ActiveRouteStatus = { state: 'unknown', reason: 'error' }

function toStatus(data: ActiveRouteResponse): ActiveRouteStatus {
  const destination = data.destination
    ? { lat: data.destination.lat, lon: data.destination.lon, name: data.destination.name }
    : null
  if (!data.active) return { state: 'inactive', destination }
  if (!data.route && data.route_error) return { state: 'inactive', destination, routeError: data.route_error }
  // Active with no route to show is an unusable answer, not an inactive route.
  if (!data.route) return ERRORED
  return {
    state: 'active',
    route: { name: data.route.name, waypoints: data.route.waypoints },
    pointIndex: typeof data.point_index === 'number' ? data.point_index : 0,
    reverse: data.reverse === true,
    destination,
  }
}

/** Polls what SignalK is navigating. Read-only: Helmcentral no longer starts or stops routes. */
export function useRouteActivation(pollIntervalSeconds = DEFAULT_POLL_INTERVAL_SECONDS) {
  const { data, error } = useApiQuery<ActiveRouteResponse>({
    queryKey: ACTIVE_KEY,
    path: '/api/routes/active',
    refetchInterval: pollIntervalSeconds * 1000,
  })
  // Don't assume inactive on a failed poll: the route may still be active on
  // the boat's N2K bus even if we can't reach SignalK to confirm.
  const status = useMemo<ActiveRouteStatus>(
    () => (error !== null ? ERRORED : data === undefined ? LOADING : toStatus(data)),
    [data, error],
  )

  return { status }
}
