import { useCallback } from 'react'

import { useAppLocation, useAppRouter, useNavigate } from '@/hooks/use-app-location'
import type { WatchesRoute, WatchTemplateId } from '@/lib/app-location'
import type { AppRouter, NavigateOptions } from '@/lib/app-router'
import type { ListSort } from '@/lib/list-order'

export interface WatchesRouteApi {
  route: WatchesRoute
  /** The index, sorted as the operator had it. */
  openIndex: (options?: NavigateOptions) => boolean
  /** A watch's page. It carries the index's sort so its arrows step through the same list. */
  openWatch: (id: string, options?: NavigateOptions) => boolean
  /** Sorts the index. A header click is not a place to go Back to, so it replaces the entry. */
  setSort: (sort: ListSort | null) => boolean
  openNew: (template: WatchTemplateId | null, options?: NavigateOptions) => boolean
}

// The sort of the index the operator is on, or of the one the watch on screen
// was opened from. Read at call time so the callbacks keep one identity.
function liveSort(router: AppRouter): ListSort | undefined {
  const location = router.getSnapshot().location
  const route = location.panel === 'watches' ? location.route : router.resume('watches').route
  return route.view === 'new' ? undefined : route.sort
}

/** Watches' slice of the app location (ADR 0171). */
export function useWatchesRoute(): WatchesRouteApi {
  const router = useAppRouter()
  const location = useAppLocation()
  const navigate = useNavigate()
  const route: WatchesRoute = location.panel === 'watches' ? location.route : router.resume('watches').route

  const go = useCallback(
    (view: WatchesRoute, options?: NavigateOptions) => navigate({ panel: 'watches', route: view }, options),
    [navigate],
  )
  const openIndex = useCallback((options?: NavigateOptions) => {
    const sort = liveSort(router)
    return go({ view: 'index', ...(sort ? { sort } : {}) }, options)
  }, [go, router])
  const openWatch = useCallback((id: string, options?: NavigateOptions) => {
    const sort = liveSort(router)
    return go({ view: 'details', id, ...(sort ? { sort } : {}) }, options)
  }, [go, router])
  const setSort = useCallback(
    (sort: ListSort | null) => go({ view: 'index', ...(sort ? { sort } : {}) }, { replace: true }),
    [go],
  )
  const openNew = useCallback(
    (template: WatchTemplateId | null, options?: NavigateOptions) => go({ view: 'new', template }, options),
    [go],
  )
  return { route, openIndex, openWatch, openNew, setSort }
}
