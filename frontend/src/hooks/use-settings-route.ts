import { useCallback, useMemo } from 'react'

import { useAppLocation, useAppRouter, useNavigate } from '@/hooks/use-app-location'
import type { SettingsRoute } from '@/lib/app-location'
import type { NavigateOptions } from '@/lib/app-router'

export interface SettingsRouteApi {
  route: SettingsRoute
  /** Goes to a view of Settings, replacing the whole slice. */
  go: (view: SettingsRoute, options?: NavigateOptions) => boolean
  /** Backs out of the current view: an import run returns to the import list.
   * A section has nothing above it inside Settings, so this does nothing there. */
  close: (options?: NavigateOptions) => boolean
}

/** Settings' slice of the app location (ADR 0171). */
export function useSettingsRoute(): SettingsRouteApi {
  const router = useAppRouter()
  const location = useAppLocation()
  const navigate = useNavigate()
  const route = useMemo<SettingsRoute>(() => {
    if (location.panel === 'settings') return location.route
    return router.resume('settings').route
  }, [location, router])

  const go = useCallback(
    (view: SettingsRoute, options?: NavigateOptions) => navigate({ panel: 'settings', route: view }, options),
    [navigate],
  )
  const close = useCallback((options?: NavigateOptions) => {
    const location = router.getSnapshot().location
    if (location.panel !== 'settings' || location.route.view !== 'import-run') return true
    return go({ view: 'section', section: 'import' }, options)
  }, [go, router])
  return { route, go, close }
}
