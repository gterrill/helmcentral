import { useCallback, useEffect, useState } from 'react'
import { toast } from 'sonner'
import type { LampStripWidgetConfig } from '@/lib/dashboard-widgets'

interface DashboardRibbonResponse {
  ribbon: LampStripWidgetConfig | null
}

// Reads the server's `{"error": "<message>"}` body off a failed response for
// use in a toast description. Parsing must never throw: a non-JSON or empty
// body (e.g. a 500 from a proxy/load balancer) falls back to `HTTP <status>`.
// Mirrors readErrorMessage in use-dashboard-pages.ts.
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

/**
 * The one vessel-level indicator ribbon (ADR 0082): a lamp strip promoted out
 * of the per-page widget so it renders identically above the grid on every
 * dashboard page instead of being copied by hand onto each one.
 *
 * `ribbon` is `null` when the operator has not pinned one, in which case
 * App.tsx renders nothing in the ribbon's slot.
 */
export function useDashboardRibbon() {
  const [ribbon, setRibbon] = useState<LampStripWidgetConfig | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  const fetchRibbon = useCallback(async () => {
    try {
      setLoading(true)
      const res = await fetch('/api/dashboard-ribbon')
      if (!res.ok) {
        throw new Error(`HTTP error! status: ${res.status}`)
      }
      const data = (await res.json()) as DashboardRibbonResponse
      setRibbon(data.ribbon ?? null)
      setError(null)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load the indicator ribbon')
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    void fetchRibbon()
  }, [fetchRibbon])

  // A null config clears the ribbon; anything else replaces it, validated
  // server-side by the same rules a per-page lamp strip widget uses.
  const saveRibbon = useCallback(async (config: LampStripWidgetConfig | null): Promise<boolean> => {
    const res = await fetch('/api/dashboard-ribbon', {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ ribbon: config }),
    })
    if (!res.ok) {
      const message = await readErrorMessage(res)
      toast.error('Could not save the indicator ribbon', { description: message })
      return false
    }
    const data = (await res.json()) as DashboardRibbonResponse
    setRibbon(data.ribbon ?? null)
    return true
  }, [])

  return { ribbon, loading, error, saveRibbon }
}
