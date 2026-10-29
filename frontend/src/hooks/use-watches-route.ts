import { useCallback } from 'react'

import { useAppLocation, useAppRouter, useNavigate } from '@/hooks/use-app-location'
import type { WatchesRoute, WatchTemplateId } from '@/lib/app-location'
import type { NavigateOptions } from '@/lib/app-router'

export interface WatchesRouteApi {
  route: WatchesRoute
  openIndex: (options?: NavigateOptions) => boolean
  openWatch: (id: string, options?: NavigateOptions) => boolean
  openNew: (template: WatchTemplateId | null, options?: NavigateOptions) => boolean
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
  const openIndex = useCallback((options?: NavigateOptions) => go({ view: 'index' }, options), [go])
  const openWatch = useCallback((id: string, options?: NavigateOptions) => go({ view: 'details', id }, options), [go])
  const openNew = useCallback(
    (template: WatchTemplateId | null, options?: NavigateOptions) => go({ view: 'new', template }, options),
    [go],
  )
  return { route, openIndex, openWatch, openNew }
}
