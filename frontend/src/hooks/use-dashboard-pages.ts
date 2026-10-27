import { useCallback, useRef, useState } from 'react'
import { toast } from 'sonner'
import type { DashboardLayoutItem } from '@/lib/dashboard-widgets'
import { apiRequest, refetchQuery, setQueryData, useApiQuerySelector } from '@/lib/api-query'
import { sameJSON } from '@/lib/telemetry-store'

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

const EMPTY_PAGES: DashboardPage[] = []

const PAGES_KEY = ['/api/dashboard-pages'] as const

// The cache holds the raw list response, so patches keep its shape.
function patchPages(fn: (pages: DashboardPage[]) => DashboardPage[]) {
  setQueryData<DashboardPagesListResponse>(PAGES_KEY, (prev) => ({
    ...prev,
    pages: fn(Array.isArray(prev?.pages) ? prev.pages : []),
  }))
}

const PAGES_PATH = '/api/dashboard-pages'

interface PagesSelection {
  pages: DashboardPage[]
  error: Error | null
  loading: boolean
}

/**
 * `next` with every part equal to `prev` put back to `prev`'s object: a widget
 * equal to the one with its id, a page whose every field and widget is equal,
 * a list whose every page is (ADR 0215 phase 7). A write lands the server's
 * whole page, every widget a new object; this is what lets it reach only the
 * tile whose record changed.
 */
function sharePages(prev: DashboardPage[] | undefined, next: DashboardPage[]): DashboardPage[] {
  if (!prev) return next
  const prevById = new Map(prev.map((page) => [page.id, page]))
  const shared = next.map((page) => {
    const old = prevById.get(page.id)
    if (!old) return page
    if (sameJSON(old, page)) return old
    const oldWidgets = new Map(old.widgets.map((w) => [w.id, w]))
    const widgets = page.widgets.map((w) => {
      const before = oldWidgets.get(w.id)
      return before && sameJSON(before, w) ? before : w
    })
    const sameWidgets = widgets.length === old.widgets.length && widgets.every((w, i) => w === old.widgets[i])
    return { ...page, widgets: sameWidgets ? old.widgets : widgets }
  })
  return shared.length === prev.length && shared.every((page, i) => page === prev[i]) ? prev : shared
}

// The error stands beside the last pages it had: a failed refresh is reported,
// and the panels that read it keep those pages on screen next to the notice.
// The server is Go, so a page with no widgets can arrive as `widgets: null`
// (a nil slice) or with none at all. That page is empty, not broken.
function parsePages(raw: DashboardPagesListResponse): DashboardPage[] {
  if (!Array.isArray(raw.pages)) return EMPTY_PAGES
  return raw.pages.map((page) => (Array.isArray(page.widgets) ? page : { ...page, widgets: [] }))
}

function selectPages(raw: DashboardPagesListResponse | undefined, error: Error | null, previous: PagesSelection | undefined): PagesSelection {
  const parsed = raw === undefined ? undefined : parsePages(raw)
  return {
    pages: parsed === undefined ? EMPTY_PAGES : sharePages(previous?.pages, parsed),
    error,
    loading: raw === undefined && error === null,
  }
}

function samePagesSelection(a: PagesSelection, b: PagesSelection): boolean {
  return a.pages === b.pages && a.error === b.error && a.loading === b.loading
}

const refetchPages = () => refetchQuery(PAGES_KEY, PAGES_PATH)

function messageOf(err: unknown): string {
  return err instanceof Error ? err.message : String(err)
}

export function useDashboardPages() {
  const [reordering, setReordering] = useState(false)
  const reorderPending = useRef(false)

  // Read through a selector, not useApiQuery: a poll that answers the same
  // pages renders nothing, and one that changes a widget hands every other
  // widget, page and the list itself back unchanged (sharePages).
  const { pages, error, loading } = useApiQuerySelector<DashboardPagesListResponse, PagesSelection>(
    { queryKey: PAGES_KEY, path: PAGES_PATH, structuralSharing: true },
    selectPages,
    samePagesSelection,
  )

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
    try {
      const page = await apiRequest<DashboardPage>('/api/dashboard-pages', { method: 'POST', json: body })
      // The server appends newly created pages after the saved navigation order.
      patchPages((prev) => [...prev, page])
      return page
    } catch (err) {
      toast.error('Could not create page', { description: messageOf(err) })
      return null
    }
  }, [])

  const updatePage = useCallback(async (
    id: string,
    patch: Partial<Pick<DashboardPage, 'name' | 'widgets' | 'skin' | 'hero' | 'display_id' | 'dwell_seconds' | 'show_when'>>,
  ): Promise<DashboardPage | null> => {
    // Note: on failure we deliberately do NOT surface `error` above: that's
    // reserved for the initial page-list load failure. App.tsx gates the
    // whole dashboard on `!pagesError`, so routing a save failure through it
    // would blank the screen on something as routine as a failed drag; a
    // toast surfaces the problem without doing that.
    try {
      const page = await apiRequest<DashboardPage>(`/api/dashboard-pages/${id}`, { method: 'PATCH', json: patch })
      patchPages((prev) => prev.map((p) => (p.id === id ? page : p)))
      return page
    } catch (err) {
      toast.error('Could not save dashboard', { description: messageOf(err) })
      return null
    }
  }, [])

  const deletePage = useCallback(async (id: string): Promise<boolean> => {
    try {
      await apiRequest<void>(`/api/dashboard-pages/${id}`, { method: 'DELETE' })
      patchPages((prev) => prev.filter((p) => p.id !== id))
      return true
    } catch (err) {
      toast.error('Could not delete page', { description: messageOf(err) })
      return false
    }
  }, [])

  const reorderPages = useCallback(async (pageIds: string[]): Promise<boolean> => {
    // A ref also blocks clicks that arrive before React renders the pending state.
    if (reorderPending.current) return false
    reorderPending.current = true
    setReordering(true)
    try {
      const data = await apiRequest<DashboardPagesListResponse>('/api/dashboard-pages/order', {
        method: 'PUT',
        json: { page_ids: pageIds },
      })
      if (!Array.isArray(data.pages) || data.pages.length !== pageIds.length ||
          data.pages.some((page, index) => page?.id !== pageIds[index])) {
        throw new Error('Invalid page order response')
      }
      const orderedPages = data.pages
      patchPages((current) => {
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
      toast.error('Could not reorder pages', { description: messageOf(err) })
      return false
    } finally {
      reorderPending.current = false
      setReordering(false)
    }
  }, [])

  return {
    pages,
    loading,
    // Reported alongside the pages: a failed refresh is not hidden, and the
    // panels that read it keep the cached pages on screen next to the notice.
    error: error?.message ?? null,
    refetch: refetchPages,
    createPage,
    updatePage,
    deletePage,
    reorderPages,
    reordering,
  }
}
