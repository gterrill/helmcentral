import { useEffect, useState } from 'react'

import type { PoiFeature } from '@/lib/poi'

interface PoiFeatureApi {
  id?: string
  category?: string
  name?: string
  lat?: number
  lon?: number
  distance_m?: number
  bearing_deg?: number
  detail?: string
  source_url?: string
}

interface PoiResponseApi {
  center?: { lat?: number; lon?: number }
  provider?: string
  cached?: boolean
  fetched_at?: string
  features?: PoiFeatureApi[]
  truncated?: string[]
  unsupported?: string[]
}

export interface PoiCenter {
  lat: number
  lon: number
}

export interface UsePoiResult {
  features: PoiFeature[]
  center: PoiCenter | null
  provider: string | null
  cached: boolean
  truncated: string[]
  unsupported: string[]
  loading: boolean
  /** The server's own fetched_at for the data currently held, not this hook's last attempt. */
  fetchedAt: string | null
  /** Set on a failed poll; cleared on the next success. Existing features/fetchedAt are left alone. */
  error: string | null
  /** When the next retry is scheduled (Date.now()-comparable ms), or null while healthy. */
  retryAt: number | null
}

// The server's own cache cell (0.02 degrees, ADR 0091) makes a poll this
// frequent nearly free most of the time, and matches the plan's own choice
// of interval for this widget.
const POLL_INTERVAL_MS = 60_000
const BACKOFF_START_MS = 60_000
const BACKOFF_MAX_MS = 10 * 60_000

function toFeature(raw: PoiFeatureApi): PoiFeature | null {
  if (
    typeof raw.id !== 'string' || raw.id === ''
    || typeof raw.category !== 'string' || raw.category === ''
    || typeof raw.name !== 'string'
    || typeof raw.lat !== 'number' || !Number.isFinite(raw.lat)
    || typeof raw.lon !== 'number' || !Number.isFinite(raw.lon)
  ) {
    return null
  }
  return {
    id: raw.id,
    category: raw.category,
    name: raw.name,
    lat: raw.lat,
    lon: raw.lon,
    distanceM: typeof raw.distance_m === 'number' && Number.isFinite(raw.distance_m) ? raw.distance_m : 0,
    bearingDeg: typeof raw.bearing_deg === 'number' && Number.isFinite(raw.bearing_deg) ? raw.bearing_deg : 0,
    detail: typeof raw.detail === 'string' ? raw.detail : '',
    sourceUrl: typeof raw.source_url === 'string' ? raw.source_url : '',
  }
}

/**
 * Polls GET /api/poi for the poi-map widget (ADR 0091 phase 3b).
 *
 * No masking fallback: a failed poll never invents or clears features — the
 * last good list, its fetchedAt and the current provider stay exactly as
 * they were, and only `error`/`retryAt` change, so the tile can show the
 * data's age alongside the failure rather than an empty or fabricated state.
 * A 502 or 503 (the two statuses poiNearby returns when the upstream
 * provider or the vessel position is unavailable) backs off starting at 60s
 * and doubling to a 10-minute ceiling; any other failure (a network error,
 * or a 4xx that should have been caught by the config dialog's own
 * validation) just retries on the normal 60s cadence.
 */
export function usePoi(rangeNm: number, categories: readonly string[], limit: number): UsePoiResult {
  const [features, setFeatures] = useState<PoiFeature[]>([])
  const [center, setCenter] = useState<PoiCenter | null>(null)
  const [provider, setProvider] = useState<string | null>(null)
  const [cached, setCached] = useState(false)
  const [truncated, setTruncated] = useState<string[]>([])
  const [unsupported, setUnsupported] = useState<string[]>([])
  const [loading, setLoading] = useState(true)
  const [fetchedAt, setFetchedAt] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [retryAt, setRetryAt] = useState<number | null>(null)

  const categoriesKey = categories.join(',')

  useEffect(() => {
    if (categoriesKey === '') return

    const controller = new AbortController()
    let cancelled = false
    let timeoutId: ReturnType<typeof setTimeout> | null = null
    let backoffMs = BACKOFF_START_MS

    const schedule = (delayMs: number) => {
      if (cancelled) return
      timeoutId = setTimeout(() => { void run() }, delayMs)
    }

    const run = async () => {
      try {
        const params = new URLSearchParams({
          radius_nm: String(rangeNm),
          categories: categoriesKey,
          limit: String(limit),
        })
        const res = await fetch(`/api/poi?${params.toString()}`, { signal: controller.signal })

        if (!res.ok) {
          if (cancelled) return
          setError(`POI provider unavailable (${res.status})`)
          setLoading(false)
          if (res.status === 502 || res.status === 503) {
            const delay = Math.min(backoffMs, BACKOFF_MAX_MS)
            backoffMs = Math.min(backoffMs * 2, BACKOFF_MAX_MS)
            setRetryAt(Date.now() + delay)
            schedule(delay)
          } else {
            backoffMs = BACKOFF_START_MS
            setRetryAt(Date.now() + POLL_INTERVAL_MS)
            schedule(POLL_INTERVAL_MS)
          }
          return
        }

        const data = (await res.json()) as PoiResponseApi
        if (cancelled) return

        const nextFeatures = Array.isArray(data.features)
          ? data.features.map(toFeature).filter((f): f is PoiFeature => f !== null)
          : []
        setFeatures(nextFeatures)
        setCenter(
          data.center && typeof data.center.lat === 'number' && typeof data.center.lon === 'number'
            ? { lat: data.center.lat, lon: data.center.lon }
            : null,
        )
        setProvider(typeof data.provider === 'string' ? data.provider : null)
        setCached(Boolean(data.cached))
        setTruncated(Array.isArray(data.truncated) ? data.truncated : [])
        setUnsupported(Array.isArray(data.unsupported) ? data.unsupported : [])
        setFetchedAt(typeof data.fetched_at === 'string' ? data.fetched_at : null)
        setError(null)
        setRetryAt(null)
        setLoading(false)
        backoffMs = BACKOFF_START_MS
        schedule(POLL_INTERVAL_MS)
      } catch (err) {
        if (cancelled || controller.signal.aborted) return
        setError(err instanceof Error ? err.message : 'POI fetch failed')
        setLoading(false)
        setRetryAt(Date.now() + POLL_INTERVAL_MS)
        schedule(POLL_INTERVAL_MS)
      }
    }

    void run()

    return () => {
      cancelled = true
      controller.abort()
      if (timeoutId !== null) clearTimeout(timeoutId)
    }
  }, [rangeNm, categoriesKey, limit])

  return { features, center, provider, cached, truncated, unsupported, loading, fetchedAt, error, retryAt }
}
