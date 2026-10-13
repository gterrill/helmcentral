import { useCallback, useEffect, useSyncExternalStore } from 'react'
import { useAppRouter } from '@/hooks/use-app-location'
import type { DashboardPage } from '@/hooks/use-dashboard-pages'

const ACTIVE_DASHBOARD_PAGE_KEY = 'dashboard.activePageId'

function readStored(): string | null {
  return globalThis.localStorage?.getItem(ACTIVE_DASHBOARD_PAGE_KEY) ?? null
}

function writeStored(id: string | null) {
  if (id === null) globalThis.localStorage?.removeItem(ACTIVE_DASHBOARD_PAGE_KEY)
  else globalThis.localStorage?.setItem(ACTIVE_DASHBOARD_PAGE_KEY, id)
}

// ADR 0171: the router owns the active page id (it is part of the app
// location, `/dashboard/<id>`); this hook is the reconciler between that and
// the page list. It corrects an id the list no longer holds (a deleted page,
// a wrong deep link), and keeps what the browser remembers for a bare `/`.
//
// Reconciles the raw id against the current page list so callers never see a
// stale id (e.g. one pointing at a deleted page) diverge from the page that's
// actually displayed. While `pages` hasn't loaded yet (empty array), the raw
// id is returned as-is and nothing is corrected - otherwise an empty list
// during initial load would look like "every page was deleted" and clobber
// the id before it ever had a chance to resolve.
//
// The raw id is the one the location names (a deep link such as
// `/dashboard/<pageId>` wins over what the browser remembered), else the
// remembered one.
//
// `persist: false` is the wall display route (ADR 0110): its rotation drives
// this setter every few seconds, and it runs in a tab of the same browser as
// the operator's own dashboard, so those ids stay out of the shared key. On
// the wall the router holds the id in memory only: rotation never touches
// history or localStorage.
export function useActiveDashboardPageId(pages: DashboardPage[] = [], persist = true): [string | null, (id: string | null) => void] {
  const router = useAppRouter()
  const pageId = useSyncExternalStore(router.subscribe, () => router.getSnapshot().pageId)
  const raw = pageId ?? readStored()

  // ADR 0110: an id that IS in `pages` always wins, wall page or not - a
  // wall page is still an ordinary dashboard page an operator can navigate
  // to directly (the sidebar's display group does exactly that). Only the
  // fallback for an id that resolves to nothing (nothing stored yet, or a
  // deleted/unknown page) skips wall pages, so a wall page sitting at
  // position 0 in server order can never become the very first thing '/'
  // shows - falling back to `pages[0]` in that case only if every page is
  // a wall page, which is still better than resolving to nothing at all.
  const resolvedId = pages.length > 0
    ? (pages.find((p) => p.id === raw)?.id ?? pages.find((p) => !p.display_id)?.id ?? pages[0].id)
    : raw

  // The correction is a rewrite of where the app already is, not a place to
  // go Back from.
  useEffect(() => {
    if (pages.length > 0 && resolvedId !== pageId) {
      router.setPageId(resolvedId, { replace: true })
    }
  }, [router, pages.length, resolvedId, pageId])

  // A deep link's page, a Back/Forward onto another page, or the correction
  // above, changes the page without going through the setter below. Only an
  // id the list holds is remembered, so an unknown deep link never is.
  useEffect(() => {
    if (persist && pages.length > 0 && resolvedId !== null) writeStored(resolvedId)
  }, [persist, pages.length, resolvedId])

  const setId = useCallback((next: string | null) => {
    if (persist) writeStored(next)
    router.setPageId(next)
  }, [router, persist])

  return [resolvedId, setId]
}
