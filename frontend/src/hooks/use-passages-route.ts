import { useCallback } from 'react'

import { useAppLocation, useAppRouter, useNavigate } from '@/hooks/use-app-location'
import type { PassagesFilter, PassagesRoute } from '@/lib/app-location'
import type { AppRouter, NavigateOptions } from '@/lib/app-router'

export interface PassagesRouteApi {
  route: PassagesRoute
  go: (view: PassagesRoute, options?: NavigateOptions) => boolean
  /** Lists the passages a pane entry names. */
  showFilter: (filter: PassagesFilter, options?: NavigateOptions) => boolean
  openPassage: (id: string, options?: NavigateOptions) => boolean
  openCruise: (id: string, options?: NavigateOptions) => boolean
  openCombined: (ids: string[], options?: NavigateOptions) => boolean
  /** Back from a details page to the listing it was opened from. */
  close: (options?: NavigateOptions) => boolean
}

// Read at call time so the callbacks keep one identity for the router's life.
function liveRoute(router: AppRouter): PassagesRoute {
  const location = router.getSnapshot().location
  if (location.panel === 'passages') return location.route
  return router.resume('passages').route
}

function filterOf(route: PassagesRoute): PassagesFilter {
  if (route.view === 'cruise') return { kind: 'cruise', id: route.id }
  return route.filter
}

/** Passages' slice of the app location (ADR 0171). */
export function usePassagesRoute(): PassagesRouteApi {
  const router = useAppRouter()
  const location = useAppLocation()
  const navigate = useNavigate()
  const route = location.panel === 'passages' ? location.route : liveRoute(router)

  const go = useCallback(
    (view: PassagesRoute, options?: NavigateOptions) => navigate({ panel: 'passages', route: view }, options),
    [navigate],
  )
  const showFilter = useCallback(
    (filter: PassagesFilter, options?: NavigateOptions) => go({ view: 'index', filter }, options),
    [go],
  )
  const openPassage = useCallback(
    (id: string, options?: NavigateOptions) => go({ view: 'passage', id, filter: filterOf(liveRoute(router)) }, options),
    [go, router],
  )
  const openCruise = useCallback((id: string, options?: NavigateOptions) => go({ view: 'cruise', id }, options), [go])
  const openCombined = useCallback(
    (ids: string[], options?: NavigateOptions) => go({ view: 'combined', ids, filter: filterOf(liveRoute(router)) }, options),
    [go, router],
  )
  const close = useCallback(
    (options?: NavigateOptions) => {
      const current = liveRoute(router)
      if (current.view === 'index') return true
      return go({ view: 'index', filter: filterOf(current) }, options)
    },
    [go, router],
  )

  return { route, go, showFilter, openPassage, openCruise, openCombined, close }
}
