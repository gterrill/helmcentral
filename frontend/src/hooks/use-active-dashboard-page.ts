import { useEffect } from 'react'
import { useLocalStorageId } from '@/hooks/use-local-storage-id'
import type { DashboardPage } from '@/hooks/use-dashboard-pages'

const ACTIVE_DASHBOARD_PAGE_KEY = 'dashboard.activePageId'

// Reconciles the raw stored id against the current page list so callers never
// see a stale id (e.g. one pointing at a deleted page) diverge from the page
// that's actually displayed. While `pages` hasn't loaded yet (empty array),
// the raw stored id is returned as-is and no write-back happens — otherwise
// an empty list during initial load would look like "every page was deleted"
// and clobber the stored id before it ever had a chance to resolve.
//
// `initialId` (ADR 0074) is App.tsx forwarding the page id parsed off a deep
// link, e.g. `/dashboard/<pageId>` — it wins over the stored id on mount.
// Reconciliation below is unchanged either way: an id that isn't in `pages`
// (deleted, or simply wrong) still resolves to `pages[0]`.
// `persist: false` is the wall display route (ADR 0110): its rotation
// drives this setter every few seconds, and it runs in a tab of the same
// browser as the operator's own dashboard, so those ids must stay out of
// the shared key.
export function useActiveDashboardPageId(pages: DashboardPage[] = [], initialId?: string | null, persist = true): [string | null, (id: string | null) => void] {
  const [id, setId] = useLocalStorageId(ACTIVE_DASHBOARD_PAGE_KEY, initialId, persist)

  // ADR 0110: an id that IS in `pages` always wins, wall page or not — a
  // wall page is still an ordinary dashboard page an operator can navigate
  // to directly (the sidebar's display group does exactly that). Only the
  // fallback for an id that resolves to nothing (nothing stored yet, or a
  // deleted/unknown page) skips wall pages, so a wall page sitting at
  // position 0 in server order can never become the very first thing '/'
  // shows — falling back to `pages[0]` in that case only if every page is
  // a wall page, which is still better than resolving to nothing at all.
  const resolvedId = pages.length > 0
    ? (pages.find((p) => p.id === id)?.id ?? pages.find((p) => !p.display_id)?.id ?? pages[0].id)
    : id

  useEffect(() => {
    if (pages.length > 0 && resolvedId !== id) {
      setId(resolvedId)
    }
  }, [pages.length, resolvedId, id, setId])

  return [resolvedId, setId]
}
