import { useCallback } from 'react'

import { useAppLocation, useAppRouter, useNavigate } from '@/hooks/use-app-location'
import type { DocumentsRoute } from '@/lib/app-location'
import type { AppRouter, NavigateOptions } from '@/lib/app-router'

export interface DocumentsRouteApi {
  route: DocumentsRoute
  /** Goes to a view of Documents, replacing the whole slice. */
  go: (view: DocumentsRoute, options?: NavigateOptions) => boolean
  /** Details returns to the listing it was opened from. The listing itself
   * has nothing above it (the folder pane owns folders), so nothing happens there. */
  close: (options?: NavigateOptions) => boolean
  /** Lists a folder. A reading-pane section belongs to its manual's folder, so
   * it is dropped when the folder changes. */
  openFolder: (folderId: string | null, options?: NavigateOptions) => boolean
  /** Opens a section of the current folder's manual, or closes it with null. */
  openSection: (sectionId: string | null, options?: NavigateOptions) => boolean
  /** Opens a document's Details page, remembering the listing it came from. */
  openDetails: (id: string, options?: NavigateOptions) => boolean
  /** Details only: follows a Move to the document's new folder without adding a
   * history entry, since Back would otherwise land on the old folder. */
  followFolder: (folderId: string | null) => boolean
}

// What the panel is showing right now, read at call time so the callbacks
// below keep one identity for the life of the router. The panels they are
// handed to re-sync on prop identity, and a fresh callback per navigation
// would make every move look like a change of props.
function liveRoute(router: AppRouter): DocumentsRoute {
  const location = router.getSnapshot().location
  if (location.panel === 'documents') return location.route
  return router.resume('documents').route
}

/** Documents' slice of the app location (ADR 0171). */
export function useDocumentsRoute(): DocumentsRouteApi {
  const router = useAppRouter()
  const location = useAppLocation()
  const navigate = useNavigate()
  // `location` is the subscription: it re-renders the caller on every move.
  const route = location.panel === 'documents' ? location.route : liveRoute(router)

  const go = useCallback(
    (view: DocumentsRoute, options?: NavigateOptions) => navigate({ panel: 'documents', route: view }, options),
    [navigate],
  )
  const openFolder = useCallback((folderId: string | null, options?: NavigateOptions) => {
    const current = liveRoute(router)
    return go({
      view: 'browse',
      folderId,
      sectionId: folderId === current.folderId ? current.sectionId : null,
      documentId: null,
    }, options)
  }, [go, router])
  const openSection = useCallback((sectionId: string | null, options?: NavigateOptions) => {
    const current = liveRoute(router)
    return go({ view: 'browse', folderId: current.folderId, sectionId, documentId: null }, options)
  }, [go, router])
  const openDetails = useCallback((id: string, options?: NavigateOptions) => {
    const current = liveRoute(router)
    return go({ view: 'details', id, folderId: current.folderId, sectionId: current.sectionId }, options)
  }, [go, router])
  const close = useCallback((options?: NavigateOptions) => {
    const current = liveRoute(router)
    if (current.view !== 'details') return true
    return go({ view: 'browse', folderId: current.folderId, sectionId: current.sectionId, documentId: null }, options)
  }, [go, router])
  const followFolder = useCallback((folderId: string | null) => {
    const current = liveRoute(router)
    if (current.view !== 'details') return true
    return go({
      view: 'details',
      id: current.id,
      folderId,
      sectionId: folderId === current.folderId ? current.sectionId : null,
    }, { replace: true })
  }, [go, router])

  return { route, go, close, openFolder, openSection, openDetails, followFolder }
}
