import { useCallback, useEffect, useState } from 'react'

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

export function useRoutes() {
  const [routes, setRoutes] = useState<Route[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  const fetchRoutes = useCallback(async () => {
    try {
      setLoading(true)
      const res = await fetch('/api/routes')
      if (!res.ok) {
        throw new Error(`HTTP error! status: ${res.status}`)
      }
      const data = (await res.json()) as RoutesListResponse
      setRoutes(Array.isArray(data.routes) ? data.routes : [])
      setError(null)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load routes')
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    void fetchRoutes()
  }, [fetchRoutes])

  const createRoute = useCallback(async (
    name: string,
    waypoints: RouteWaypoint[],
    planningSpeedKts: number,
  ): Promise<Route | null> => {
    const res = await fetch('/api/routes', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ name, waypoints, planning_speed_kts: planningSpeedKts }),
    })
    if (!res.ok) return null
    const route = (await res.json()) as Route
    await fetchRoutes()
    return route
  }, [fetchRoutes])

  const updateRoute = useCallback(async (
    id: string,
    patch: Partial<Pick<Route, 'name' | 'waypoints' | 'planning_speed_kts'>>,
  ): Promise<Route | null> => {
    const res = await fetch(`/api/routes/${id}`, {
      method: 'PATCH',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(patch),
    })
    if (!res.ok) return null
    const route = (await res.json()) as Route
    await fetchRoutes()
    return route
  }, [fetchRoutes])

  const deleteRoute = useCallback(async (id: string): Promise<boolean> => {
    const res = await fetch(`/api/routes/${id}`, { method: 'DELETE' })
    if (!res.ok) return false
    await fetchRoutes()
    return true
  }, [fetchRoutes])

  return { routes, loading, error, refetch: fetchRoutes, createRoute, updateRoute, deleteRoute }
}
