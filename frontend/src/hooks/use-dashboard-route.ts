import { useState } from 'react'

const DASHBOARD_ROUTE_KEY = 'routes.dashboardRouteId'

export function useDashboardRouteId(): [string | null, (id: string | null) => void] {
  const [id, setIdState] = useState<string | null>(
    () => globalThis.localStorage?.getItem(DASHBOARD_ROUTE_KEY) ?? null,
  )

  function setId(next: string | null) {
    if (next === null) {
      globalThis.localStorage?.removeItem(DASHBOARD_ROUTE_KEY)
    } else {
      globalThis.localStorage?.setItem(DASHBOARD_ROUTE_KEY, next)
    }
    setIdState(next)
  }

  return [id, setId]
}
