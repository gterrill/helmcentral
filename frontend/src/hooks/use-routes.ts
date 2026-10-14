import { useCallback } from 'react'
import { ApiError, apiRequest, useApiQuery } from '@/lib/api-query'

export interface RouteWaypoint {
  lat: number
  lon: number
  name?: string
}

export interface Route {
  id: string
  name: string
  waypoints: RouteWaypoint[]
  planning_speed_kts: number
  created_at: string
  updated_at: string
}

interface RoutesListResponse {
  routes?: Route[]
}

const ROUTES_KEY = ['/api/routes'] as const
const EMPTY_ROUTES: Route[] = []

// A rejected write answers null/false to the caller; a network failure still
// rejects, as it always has here.
async function orNull<T>(request: Promise<T>): Promise<T | null> {
  try {
    return await request
  } catch (err) {
    if (err instanceof ApiError) return null
    throw err
  }
}

export function useRoutes() {
  const { data, isLoading, error, refetch } = useApiQuery<Route[]>({
    queryKey: ROUTES_KEY,
    path: '/api/routes',
    select: (raw: RoutesListResponse) => (Array.isArray(raw.routes) ? raw.routes : []),
  })
  const routes = data ?? EMPTY_ROUTES

  // Callers rely on the fresh list being in place when a write resolves.
  const refreshed = useCallback(async () => {
    await refetch()
  }, [refetch])

  const createRoute = useCallback(async (
    name: string,
    waypoints: RouteWaypoint[],
    planningSpeedKts: number,
  ): Promise<Route | null> => {
    const route = await orNull(apiRequest<Route>('/api/routes', {
      method: 'POST',
      json: { name, waypoints, planning_speed_kts: planningSpeedKts },
    }))
    if (route) await refreshed()
    return route
  }, [refreshed])

  const updateRoute = useCallback(async (
    id: string,
    patch: Partial<Pick<Route, 'name' | 'waypoints' | 'planning_speed_kts'>>,
  ): Promise<Route | null> => {
    const route = await orNull(apiRequest<Route>(`/api/routes/${id}`, { method: 'PATCH', json: patch }))
    if (route) await refreshed()
    return route
  }, [refreshed])

  const deleteRoute = useCallback(async (id: string): Promise<boolean> => {
    const done = await orNull(apiRequest<void>(`/api/routes/${id}`, { method: 'DELETE' }).then(() => true))
    if (!done) return false
    await refreshed()
    return true
  }, [refreshed])

  return { routes, loading: isLoading, error: error?.message ?? null, refetch, createRoute, updateRoute, deleteRoute }
}
