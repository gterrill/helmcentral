import { useCallback, useEffect, useState } from 'react'
import { toast } from 'sonner'
import type { DashboardLayoutItem } from '@/lib/dashboard-widgets'

export interface DashboardPage {
  id: string
  name: string
  widgets: DashboardLayoutItem[]
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
    // Append rather than refetch: the list is sorted oldest-created-first
    // server-side, and a newly created page has the newest created_at, so
    // appending preserves that order without a round trip.
    setPages((prev) => [...prev, page])
    return page
  }, [])

  const updatePage = useCallback(async (
    id: string,
    patch: Partial<Pick<DashboardPage, 'name' | 'widgets'>>,
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

  return { pages, loading, error, refetch: fetchPages, createPage, updatePage, deletePage }
}
