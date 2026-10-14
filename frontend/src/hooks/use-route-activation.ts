import { useCallback, useMemo, useState } from 'react'
import { apiRequest, useApiQuery } from '@/lib/api-query'

/** A chartplotter go-to destination with no Helmcentral route behind it (ADR 0125): SignalK's Course API `nextPoint`, reverse-geocoded server-side. `name` is `''` until the backend's cache resolves it - see GET /api/routes/active's own doc comment (backend/route_activation.go). */
export interface RouteActivationDestination {
  lat: number
  lon: number
  name: string
}

export type ActiveRouteStatus =
  | { state: 'unknown' }
  | { state: 'inactive'; destination: RouteActivationDestination | null }
  | {
      state: 'active'
      routeId: string | null
      pointIndex: number
      reverse: boolean
      /**
       * The Course API's nextPoint (ADR 0125), same as the inactive state's
       * own `destination` - present whenever GET /api/routes/active sent
       * one, which it does whenever the Course API carries a nextPoint,
       * REGARDLESS of whether routeId matched a Helmcentral route. A route
       * activated outside Helmcentral (routeId null here) has no leg-by-leg
       * ETA to compute, but the chartplotter's own destination is still
       * available - see App.tsx's clockTripEta.
       */
      destination: RouteActivationDestination | null
    }

interface ActiveRouteResponse {
  active: boolean
  route_id?: string | null
  route_name?: string
  point_index?: number
  reverse?: boolean
  destination?: { lat: number; lon: number; name: string }
}

const DEFAULT_POLL_INTERVAL_SECONDS = 15
const ACTIVE_KEY = ['/api/routes/active'] as const
const UNKNOWN: ActiveRouteStatus = { state: 'unknown' }

function toStatus(data: ActiveRouteResponse): ActiveRouteStatus {
  const destination = data.destination
    ? { lat: data.destination.lat, lon: data.destination.lon, name: data.destination.name }
    : null
  if (!data.active) return { state: 'inactive', destination }
  return {
    state: 'active',
    routeId: data.route_id ?? null,
    pointIndex: typeof data.point_index === 'number' ? data.point_index : 0,
    reverse: data.reverse === true,
    destination,
  }
}

export function useRouteActivation(pollIntervalSeconds = DEFAULT_POLL_INTERVAL_SECONDS) {
  const [activating, setActivating] = useState(false)
  const [deactivating, setDeactivating] = useState(false)
  const [activateError, setActivateError] = useState<string | null>(null)

  const { data, error, refetch } = useApiQuery<ActiveRouteResponse>({
    queryKey: ACTIVE_KEY,
    path: '/api/routes/active',
    refetchInterval: pollIntervalSeconds * 1000,
  })
  // Don't assume inactive on a failed poll: the route may still be active on
  // the boat's N2K bus even if we can't reach SignalK to confirm.
  const status = useMemo<ActiveRouteStatus>(
    () => (error !== null || data === undefined ? UNKNOWN : toStatus(data)),
    [data, error],
  )

  const settle = useCallback(async () => {
    await refetch()
  }, [refetch])

  const activate = useCallback(async (routeId: string): Promise<boolean> => {
    setActivateError(null)
    setActivating(true)
    try {
      await apiRequest<unknown>(`/api/routes/${routeId}/activate`, { method: 'POST' })
      await settle()
      return true
    } catch (err) {
      setActivateError(err instanceof Error ? err.message : 'Failed to activate route')
      return false
    } finally {
      setActivating(false)
    }
  }, [settle])

  const deactivate = useCallback(async (): Promise<boolean> => {
    setActivateError(null)
    setDeactivating(true)
    try {
      await apiRequest<unknown>('/api/routes/deactivate', { method: 'POST' })
      await settle()
      return true
    } catch (err) {
      setActivateError(err instanceof Error ? err.message : 'Failed to deactivate route')
      return false
    } finally {
      setDeactivating(false)
    }
  }, [settle])

  return { status, activating, deactivating, activateError, activate, deactivate }
}
