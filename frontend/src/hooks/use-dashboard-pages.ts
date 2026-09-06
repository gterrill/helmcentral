import { useCallback, useEffect, useRef, useState } from 'react'
import { toast } from 'sonner'
import type { DashboardLayoutItem } from '@/lib/dashboard-widgets'

export interface DashboardPage {
  id: string
  name: string
  position?: number
  widgets: DashboardLayoutItem[]
  /** `instrument` keeps the whole page dark whatever the app theme is (ADR 0060). */
  skin?: 'default' | 'instrument'
  /**
   * The id of the one widget on this page that gets the enlarged, full-width
   * hero treatment, or empty/absent for none (ADR 0072). Always a widget id
   * present in `widgets` — the backend clears it automatically if that widget
   * is ever removed, so a stale reference can't persist here.
   */
  hero?: string
  created_at: string
  updated_at: string
}

interface DashboardPagesListResponse {
  pages?: DashboardPage[]
}

// Reads the server's `{"error": "<message>"}` body off a failed response for
// use in a toast description. Parsing must never throw: a non-JSON or empty
// body (e.g. a 500 from a proxy/load balancer) falls back to `HTTP <status>`.
async function readErrorMessage(res: Response): Promise<string> {
  try {
    const data = (await res.json()) as { error?: string }
    if (data && typeof data.error === 'string' && data.error.length > 0) {
      return data.error
    }
  } catch {
    // body missing or not JSON — fall through to the status-based message
  }
  return `HTTP ${res.status}`
}

export function useDashboardPages() {
  const [pages, setPages] = useState<DashboardPage[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [reordering, setReordering] = useState(false)
  const reorderPending = useRef(false)

  const fetchPages = useCallback(async () => {
    try {
      setLoading(true)
      const res = await fetch('/api/dashboard-pages')
      if (!res.ok) {
        throw new Error(`HTTP error! status: ${res.status}`)
      }
      const data = (await res.json()) as DashboardPagesListResponse
      setPages(Array.isArray(data.pages) ? data.pages : [])
      setError(null)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load dashboard pages')
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    void fetchPages()
  }, [fetchPages])

  const createPage = useCallback(async (name: string, widgets: DashboardLayoutItem[] = []): Promise<DashboardPage | null> => {
    const res = await fetch('/api/dashboard-pages', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ name, widgets }),
    })
    if (!res.ok) {
      const message = await readErrorMessage(res)
      toast.error('Could not create page', { description: message })
      return null
    }
    const page = (await res.json()) as DashboardPage
    // The server appends newly created pages after the saved navigation order.
    setPages((prev) => [...prev, page])
    return page
  }, [])

  const updatePage = useCallback(async (
    id: string,
    patch: Partial<Pick<DashboardPage, 'name' | 'widgets' | 'skin' | 'hero'>>,
  ): Promise<DashboardPage | null> => {
    const res = await fetch(`/api/dashboard-pages/${id}`, {
      method: 'PATCH',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(patch),
    })
    // Note: on failure we deliberately do NOT set the `error` state above —
    // that's reserved for the initial page-list load failure (fetchPages).
    // App.tsx gates the whole dashboard on `!pagesError`, so routing a save
    // failure through it would blank the screen on something as routine as
    // a failed drag; a toast surfaces the problem without doing that.
    if (!res.ok) {
      const message = await readErrorMessage(res)
      toast.error('Could not save dashboard', { description: message })
      return null
    }
    const page = (await res.json()) as DashboardPage
    setPages((prev) => prev.map((p) => (p.id === id ? page : p)))
    return page
  }, [])

  const deletePage = useCallback(async (id: string): Promise<boolean> => {
    const res = await fetch(`/api/dashboard-pages/${id}`, { method: 'DELETE' })
    if (!res.ok) {
      const message = await readErrorMessage(res)
      toast.error('Could not delete page', { description: message })
      return false
    }
    setPages((prev) => prev.filter((p) => p.id !== id))
    return true
  }, [])

  const reorderPages = useCallback(async (pageIds: string[]): Promise<boolean> => {
    // A ref also blocks clicks that arrive before React renders the pending state.
    if (reorderPending.current) return false
    reorderPending.current = true
    setReordering(true)
    try {
      const res = await fetch('/api/dashboard-pages/order', {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ page_ids: pageIds }),
      })
      if (!res.ok) throw new Error(await readErrorMessage(res))
      const data = (await res.json()) as DashboardPagesListResponse
      if (!Array.isArray(data.pages) || data.pages.length !== pageIds.length ||
          data.pages.some((page, index) => page?.id !== pageIds[index])) {
        throw new Error('Invalid page order response')
      }
      const orderedPages = data.pages
      setPages((current) => {
        // The response is an ordering snapshot, not a refresh of page content.
        // A widget PATCH or create/delete can finish while this request is in
        // flight; don't roll back its content or resurrect a deleted page.
        const byId = new Map(current.map((page) => [page.id, page]))
        const orderedIds = new Set(orderedPages.map((page) => page.id))
        return [
          ...orderedPages.flatMap((saved) => {
            const page = byId.get(saved.id)
            return page ? [{ ...page, position: saved.position }] : []
          }),
          ...current.filter((page) => !orderedIds.has(page.id)),
        ]
      })
      return true
    } catch (err) {
      // Keep the last confirmed order and leave the dashboard visible.
      toast.error('Could not reorder pages', {
        description: err instanceof Error ? err.message : 'Failed to save page order',
      })
      return false
    } finally {
      reorderPending.current = false
      setReordering(false)
    }
  }, [])

  return { pages, loading, error, refetch: fetchPages, createPage, updatePage, deletePage, reorderPages, reordering }
}
