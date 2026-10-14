import { useCallback } from 'react'
import { toast } from 'sonner'
import { apiRequest, setQueryData, useApiQuery } from '@/lib/api-query'

// A pin dropped on the anchor-watch map — typically a bombie or the nearest
// shoreline — so the crew can watch how close the swing takes them to it.
//
// The server stores position only. Range and bearing are computed by each
// client from its own live vessel fix, so the number moves as the boat
// swings; a stored range would be frozen at drop time and useless.
export interface AnchorPlacemark {
  id: string
  lat: number
  lon: number
  label: string
  created_at: string
}

interface PlacemarksResponse {
  placemarks?: AnchorPlacemark[] | null
}

const PLACEMARKS_KEY = ['/api/anchor-watch/placemarks'] as const
const NO_PINS: AnchorPlacemark[] = []

// The cache holds the raw list response, so patches keep its shape.
const patch = (fn: (pins: AnchorPlacemark[]) => AnchorPlacemark[]) =>
  setQueryData<PlacemarksResponse>(PLACEMARKS_KEY, (prev) => ({
    ...prev,
    placemarks: fn(Array.isArray(prev?.placemarks) ? prev.placemarks : []),
  }))

export interface AnchorPlacemarksResult {
  placemarks: AnchorPlacemark[]
  createPlacemark: (lat: number, lon: number, label?: string) => Promise<void>
  removePlacemark: (id: string) => Promise<void>
}

/**
 * Polls the session's placemarks so pins dropped on one device show up on
 * every other device watching the same anchorage.
 *
 * `active` gates the polling: with no anchor watch running there is no
 * session, the server holds no pins, and polling would be pure noise.
 */
export function useAnchorPlacemarks(active: boolean, refreshIntervalSeconds = 10): AnchorPlacemarksResult {
  // A dropped poll is transient: the query keeps the last known pins rather
  // than blinking them off the chart. Writes below do surface their failures,
  // because a pin the user thinks they dropped and didn't is a hazard they'd
  // stop watching.
  const { data } = useApiQuery<AnchorPlacemark[]>({
    queryKey: PLACEMARKS_KEY,
    path: '/api/anchor-watch/placemarks',
    enabled: active,
    refetchInterval: refreshIntervalSeconds * 1000,
    select: (raw: PlacemarksResponse) => (Array.isArray(raw.placemarks) ? raw.placemarks : NO_PINS),
  })

  const createPlacemark = useCallback(async (lat: number, lon: number, label = '') => {
    try {
      const created = await apiRequest<AnchorPlacemark>('/api/anchor-watch/placemarks', {
        method: 'POST',
        json: { lat, lon, label },
      })
      patch((current) => [...current, created])
    } catch (err) {
      toast.error('Could not drop pin', { description: err instanceof Error ? err.message : 'network error' })
    }
  }, [])

  const removePlacemark = useCallback(async (id: string) => {
    try {
      await apiRequest<void>(`/api/anchor-watch/placemarks/${encodeURIComponent(id)}`, { method: 'DELETE' })
      patch((current) => current.filter((pm) => pm.id !== id))
    } catch (err) {
      toast.error('Could not remove pin', { description: err instanceof Error ? err.message : 'network error' })
    }
  }, [])

  return { placemarks: data ?? NO_PINS, createPlacemark, removePlacemark }
}
