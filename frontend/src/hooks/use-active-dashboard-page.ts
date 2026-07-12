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
export function useActiveDashboardPageId(pages: DashboardPage[] = []): [string | null, (id: string | null) => void] {
  const [id, setId] = useLocalStorageId(ACTIVE_DASHBOARD_PAGE_KEY)

  const resolvedId = pages.length > 0 ? (pages.find((p) => p.id === id)?.id ?? pages[0].id) : id

  useEffect(() => {
    if (pages.length > 0 && resolvedId !== id) {
      setId(resolvedId)
    }
  }, [pages.length, resolvedId, id, setId])

  return [resolvedId, setId]
}
