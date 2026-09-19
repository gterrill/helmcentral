import { useCallback, useEffect, useRef, useState } from 'react'
import { toast } from 'sonner'
import type { DashboardLayoutItem } from '@/lib/dashboard-widgets'
import { readErrorMessage } from '@/lib/api-error'

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
   *
   * Mutually exclusive with `display_id` (ADR 0110): a page on a wall
   * display has no hero, since the hero eats the vertical budget the fold
   * guide measures. Assigning a display clears this; the backend rejects
   * setting both.
   */
  hero?: string
  /**
   * Wall display fields (ADR 0110, superseding ADR 0089's `kiosk`/
   * `kiosk_seconds`/`kiosk_when`): `display_id` names the display this page
   * belongs to (empty/absent for an ordinary dashboard page — not on any
   * wall); `dwell_seconds` is how long it shows before the rotation
   * advances; `show_when` restricts it to a condition ("anchored"), or shows
   * it always when absent. Dwell and condition are preserved by the backend
   * across a display re-assignment or clear, so re-assigning a page
   * remembers its dwell.
   */
  display_id?: string
  dwell_seconds?: number
  show_when?: 'always' | 'anchored' | 'motoring' | 'sailing' | 'moored'
  created_at: string
  updated_at: string
}

interface DashboardPagesListResponse {
  pages?: DashboardPage[]
}

/** Everything `createPage` can carry beyond the required `name` — the
 * duplicate-to-display action (ADR 0110 §6) needs to seed a new page with a
 * copied skin/dwell/condition and a target display in the same POST that
 * creates it. */
export interface CreatePageInit {
  widgets?: DashboardLayoutItem[]
  skin?: DashboardPage['skin']
  hero?: string
  display_id?: string
  dwell_seconds?: number
  show_when?: DashboardPage['show_when']
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

  // Object-form only (ADR 0110 §6 follow-up): App.tsx's one call site now
  // passes `{ widgets: [] }` explicitly rather than a bare array, so the
  // union this carried only for that call site's original shape has nothing
  // left to serve.
  const createPage = useCallback(async (
    name: string,
    init?: CreatePageInit,
  ): Promise<DashboardPage | null> => {
    // No second argument at all defaults to an empty page (widgets: []).
    const body = init === undefined
      ? { name, widgets: [] as DashboardLayoutItem[] }
      : { name, ...init }
    const res = await fetch('/api/dashboard-pages', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(body),
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
    patch: Partial<Pick<DashboardPage, 'name' | 'widgets' | 'skin' | 'hero' | 'display_id' | 'dwell_seconds' | 'show_when'>>,
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
