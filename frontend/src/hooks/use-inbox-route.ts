import { useCallback } from 'react'

import { useAppLocation, useAppRouter, useNavigate } from '@/hooks/use-app-location'
import type { InboxRoute } from '@/lib/app-location'
import type { NavigateOptions } from '@/lib/app-router'

export interface InboxRouteApi {
  route: InboxRoute
  openIndex: (options?: NavigateOptions) => boolean
  openUpload: (options?: NavigateOptions) => boolean
}

/** The Inbox's slice of the app location (ADR 0171). */
export function useInboxRoute(): InboxRouteApi {
  const router = useAppRouter()
  const location = useAppLocation()
  const navigate = useNavigate()
  const route = location.panel === 'inbox' ? location.route : router.resume('inbox').route
  const go = useCallback(
    (view: InboxRoute, options?: NavigateOptions) => navigate({ panel: 'inbox', route: view }, options),
    [navigate],
  )
  const openIndex = useCallback((options?: NavigateOptions) => go({ view: 'index' }, options), [go])
  const openUpload = useCallback((options?: NavigateOptions) => go({ view: 'upload' }, options), [go])
  return { route, openIndex, openUpload }
}
