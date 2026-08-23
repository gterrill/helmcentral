import { useCallback, useEffect, useRef, useState } from 'react'
import { toast } from 'sonner'

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

// Reads the server's `{"error": "<message>"}` body for a toast description.
// Never throws: a non-JSON or empty body falls back to the status.
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
  const [placemarks, setPlacemarks] = useState<AnchorPlacemark[]>([])
  const timerRef = useRef<ReturnType<typeof setInterval> | null>(null)

  const fetchPlacemarks = useCallback(async () => {
    try {
      const res = await fetch('/api/anchor-watch/placemarks')
      if (!res.ok) return
      const data = (await res.json()) as PlacemarksResponse
      setPlacemarks(Array.isArray(data.placemarks) ? data.placemarks : [])
    } catch {
      // A dropped poll is transient — keep showing the last known pins
      // rather than blinking them off the chart. Writes below do surface
      // their failures, because a pin the user thinks they dropped and
      // didn't is a hazard they'd stop watching.
    }
  }, [])

  useEffect(() => {
    if (!active) {
      setPlacemarks([])
      return
    }
    void fetchPlacemarks()
    timerRef.current = setInterval(() => { void fetchPlacemarks() }, refreshIntervalSeconds * 1000)
    return () => {
      if (timerRef.current !== null) clearInterval(timerRef.current)
    }
  }, [active, fetchPlacemarks, refreshIntervalSeconds])

  const createPlacemark = useCallback(async (lat: number, lon: number, label = '') => {
    let res: Response
    try {
      res = await fetch('/api/anchor-watch/placemarks', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ lat, lon, label }),
      })
    } catch (err) {
      toast.error('Could not drop pin', { description: err instanceof Error ? err.message : 'network error' })
      return
    }
    if (!res.ok) {
      toast.error('Could not drop pin', { description: await readErrorMessage(res) })
      return
    }
    const created = (await res.json()) as AnchorPlacemark
    setPlacemarks((current) => [...current, created])
  }, [])

  const removePlacemark = useCallback(async (id: string) => {
    let res: Response
    try {
      res = await fetch(`/api/anchor-watch/placemarks/${encodeURIComponent(id)}`, { method: 'DELETE' })
    } catch (err) {
      toast.error('Could not remove pin', { description: err instanceof Error ? err.message : 'network error' })
      return
    }
    if (!res.ok) {
      toast.error('Could not remove pin', { description: await readErrorMessage(res) })
      return
    }
    setPlacemarks((current) => current.filter((pm) => pm.id !== id))
  }, [])

  return { placemarks, createPlacemark, removePlacemark }
}
