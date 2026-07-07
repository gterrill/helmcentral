import { useCallback, useEffect, useState } from 'react'
import type { DashboardLayoutItem } from '@/lib/dashboard-widgets'

interface DashboardLayoutResponse {
  widgets?: DashboardLayoutItem[]
}

export function useDashboardLayout() {
  const [widgets, setWidgets] = useState<DashboardLayoutItem[] | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [saving, setSaving] = useState(false)
  const [saveError, setSaveError] = useState<string | null>(null)

  useEffect(() => {
    let cancelled = false
    async function fetchLayout() {
      try {
        setLoading(true)
        const res = await fetch('/api/dashboard-layout')
        if (!res.ok) {
          throw new Error(`HTTP error! status: ${res.status}`)
        }
        const data = (await res.json()) as DashboardLayoutResponse
        if (!cancelled) {
          setWidgets(Array.isArray(data.widgets) ? data.widgets : [])
          setError(null)
        }
      } catch (err) {
        if (!cancelled) {
          setError(err instanceof Error ? err.message : 'Failed to load dashboard layout')
        }
      } finally {
        if (!cancelled) setLoading(false)
      }
    }
    void fetchLayout()
    return () => {
      cancelled = true
    }
  }, [])

  const saveLayout = useCallback(async (next: DashboardLayoutItem[]): Promise<boolean> => {
    setSaving(true)
    setWidgets(next)
    try {
      const res = await fetch('/api/dashboard-layout', {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ widgets: next }),
      })
      if (!res.ok) {
        throw new Error(`HTTP error! status: ${res.status}`)
      }
      setSaveError(null)
      return true
    } catch (err) {
      setSaveError(err instanceof Error ? err.message : 'Failed to save dashboard layout')
      return false
    } finally {
      setSaving(false)
    }
  }, [])

  return { widgets, loading, error, saveLayout, saving, saveError }
}
