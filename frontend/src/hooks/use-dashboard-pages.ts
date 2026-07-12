import { useCallback, useEffect, useState } from 'react'
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
    if (!res.ok) return null
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
    if (!res.ok) return null
    const page = (await res.json()) as DashboardPage
    setPages((prev) => prev.map((p) => (p.id === id ? page : p)))
    return page
  }, [])

  const deletePage = useCallback(async (id: string): Promise<boolean> => {
    const res = await fetch(`/api/dashboard-pages/${id}`, { method: 'DELETE' })
    if (!res.ok) return false
    setPages((prev) => prev.filter((p) => p.id !== id))
    return true
  }, [])

  return { pages, loading, error, refetch: fetchPages, createPage, updatePage, deletePage }
}
