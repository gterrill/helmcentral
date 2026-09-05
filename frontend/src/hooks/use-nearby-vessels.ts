import { useEffect, useState } from 'react'

import { ageFromPayload } from '@/lib/staleness'

import { subscribeTelemetry } from '@/hooks/use-telemetry-stream'

export type NearbyVessel = {
  id: string
  name: string
  mmsi?: string
  range_m: number
  age_seconds: number
  sog_knots?: number
  lat?: number
  lon?: number
  seen_count?: number
  last_seen_at?: string
}

type NearbyVesselsResponse = {
  vessels?: NearbyVessel[]
}

export function useNearbyVessels() {
  const [vessels, setVessels] = useState<NearbyVessel[]>([])
  const [loading, setLoading] = useState(true)
  // Age of the freshest contact (ADR 0068), distinct from each vessel's
  // own age_seconds. Null with no contacts: an empty horizon is not a
  // dead feed.
  const [lastUpdateAgeS, setLastUpdateAgeS] = useState<number | null>(null)

  useEffect(() => {
    const applyNearbyVessels = (payload: unknown) => {
      try {
        const data = payload as NearbyVesselsResponse
        const list = Array.isArray(data.vessels) ? data.vessels : []
        setVessels(
          list.filter(
            (item) =>
              typeof item.id === 'string' &&
              item.id !== '' &&
              typeof item.name === 'string' &&
              typeof item.range_m === 'number' &&
              Number.isFinite(item.range_m) &&
              typeof item.age_seconds === 'number' &&
              Number.isFinite(item.age_seconds),
          ).map((item) => ({
            ...item,
            mmsi: typeof item.mmsi === 'string' && item.mmsi.trim() !== '' ? item.mmsi : undefined,
            lat: typeof item.lat === 'number' && Number.isFinite(item.lat) ? item.lat : undefined,
            lon: typeof item.lon === 'number' && Number.isFinite(item.lon) ? item.lon : undefined,
            seen_count: typeof item.seen_count === 'number' && Number.isFinite(item.seen_count) ? Math.max(0, Math.floor(item.seen_count)) : 0,
            last_seen_at: typeof item.last_seen_at === 'string' ? item.last_seen_at : undefined,
          })),
        )
        setLastUpdateAgeS(ageFromPayload((data as { last_update_age_s?: unknown }).last_update_age_s))
      } catch (err) {
        console.error('Failed to fetch nearby vessels:', err)
      } finally {
        setLoading(false)
      }
    }

    return subscribeTelemetry('nearby-vessels', (raw) => {
      applyNearbyVessels(JSON.parse(raw))
    })
  }, [])

  return { vessels, loading, lastUpdateAgeS }
}
