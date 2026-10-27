import { useCallback } from 'react'
import { toast } from 'sonner'
import type { LampStripWidgetConfig } from '@/lib/dashboard-widgets'
import { apiRequest, declareQuery, setQueryData, useApiQuery } from '@/lib/api-query'

interface DashboardRibbonResponse {
  ribbon: LampStripWidgetConfig | null
}

const RIBBON_KEY = ['/api/dashboard-ribbon'] as const

/**
 * The one vessel-level indicator ribbon (ADR 0082): a lamp strip promoted out
 * of the per-page widget so it renders identically above the grid on every
 * dashboard page instead of being copied by hand onto each one.
 *
 * `ribbon` is `null` when the operator has not pinned one, in which case
 * App.tsx renders nothing in the ribbon's slot.
 */
declareQuery(RIBBON_KEY, { structuralSharing: true })

export function useDashboardRibbon() {
  const { data, isLoading, error } = useApiQuery<LampStripWidgetConfig | null>({
    queryKey: RIBBON_KEY,
    path: '/api/dashboard-ribbon',
    select: (raw: DashboardRibbonResponse) => raw.ribbon ?? null,
  })

  // A null config clears the ribbon; anything else replaces it, validated
  // server-side by the same rules a per-page lamp strip widget uses.
  const saveRibbon = useCallback(async (config: LampStripWidgetConfig | null): Promise<boolean> => {
    try {
      const saved = await apiRequest<DashboardRibbonResponse>('/api/dashboard-ribbon', {
        method: 'PUT',
        json: { ribbon: config },
      })
      setQueryData<DashboardRibbonResponse>(RIBBON_KEY, { ribbon: saved.ribbon ?? null })
      return true
    } catch (err) {
      toast.error('Could not save the indicator ribbon', {
        description: err instanceof Error ? err.message : String(err),
      })
      return false
    }
  }, [])

  return { ribbon: data ?? null, loading: isLoading, error: error?.message ?? null, saveRibbon }
}
