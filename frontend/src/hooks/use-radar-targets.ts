import { useEffect, useState } from 'react'

import { subscribeTelemetry } from '@/hooks/use-telemetry-stream'

/**
 * Three source values, not two, so a disabled integration and a broken one
 * are never confused (ADR 0062 decision 5 / the mayara integration plan's
 * telemetry section). Mirrors nearbyVessels' own fallback-policy discipline.
 */
export type RadarSource = 'mayara' | 'mayara-unreachable' | 'disabled'

const RADAR_SOURCES: readonly RadarSource[] = ['mayara', 'mayara-unreachable', 'disabled']

function isRadarSource(value: unknown): value is RadarSource {
  return typeof value === 'string' && (RADAR_SOURCES as readonly string[]).includes(value)
}

// Mirrors the backend's radarInfo, which is built from the SignalK
// snapshot's radars.<id>.controls tree. That carries userName and modelName
// but no brand, so id and name are all there is. Requiring more here silently
// drops every radar and leaves the tile with a generic header.
export type RadarInfo = {
  id: string
  name: string
  // mayara's power control reading Transmit. False means off, standby,
  // preparing or faulted, in which case the backend serves no targets at all
  // (observedTargets, radar_source.go) and the tile must say so rather than
  // let an empty list read as clear water.
  transmitting: boolean
}

/**
 * One ARPA contact from mayara-server. `id` is "<radarID>:<targetID>" —
 * mayara numbers targets per radar, and this boat's Furuno already presents
 * two radar keys off one antenna (dual range), so a bare target id collides.
 *
 * Optional fields stay optional rather than defaulting to 0: a CPA of
 * exactly 0 metres is a real, alarming value, and collapsing "unknown" into
 * "zero" would render a fabricated collision course.
 */
export type RadarTarget = {
  id: string
  radar_id: string
  target_id: number
  status: string
  bearing_rad: number
  range_m: number
  lat?: number
  lon?: number
  position_derived: boolean
  course_rad?: number
  sog_knots?: number
  cpa_m?: number
  tcpa_seconds?: number
  is_dangerous: boolean
  acquisition: string
  source_zone?: number
  age_seconds: number
  last_seen_at?: string
}

type RadarTargetsResponse = {
  datetime?: unknown
  source?: unknown
  radars?: RadarInfo[]
  targets?: RadarTarget[]
}

function sanitizeRadar(item: RadarInfo): RadarInfo | null {
  if (typeof item.id !== 'string' || item.id === '' || typeof item.name !== 'string') {
    return null
  }
  return { id: item.id, name: item.name, transmitting: item.transmitting === true }
}

// Same per-field typeof / Number.isFinite discipline as
// use-nearby-vessels.ts: a malformed entry is dropped outright rather than
// rendered with a NaN bearing or range, and an optional field that fails
// validation is left undefined rather than coerced to 0.
function sanitizeTarget(item: RadarTarget): RadarTarget | null {
  if (
    typeof item.id !== 'string' ||
    item.id === '' ||
    typeof item.radar_id !== 'string' ||
    item.radar_id === '' ||
    typeof item.target_id !== 'number' ||
    !Number.isFinite(item.target_id) ||
    typeof item.status !== 'string' ||
    item.status === '' ||
    typeof item.bearing_rad !== 'number' ||
    !Number.isFinite(item.bearing_rad) ||
    typeof item.range_m !== 'number' ||
    !Number.isFinite(item.range_m) ||
    typeof item.position_derived !== 'boolean' ||
    typeof item.is_dangerous !== 'boolean' ||
    typeof item.acquisition !== 'string' ||
    item.acquisition === '' ||
    typeof item.age_seconds !== 'number' ||
    !Number.isFinite(item.age_seconds)
  ) {
    return null
  }

  return {
    id: item.id,
    radar_id: item.radar_id,
    target_id: item.target_id,
    status: item.status,
    bearing_rad: item.bearing_rad,
    range_m: item.range_m,
    position_derived: item.position_derived,
    is_dangerous: item.is_dangerous,
    acquisition: item.acquisition,
    age_seconds: item.age_seconds,
    lat: typeof item.lat === 'number' && Number.isFinite(item.lat) ? item.lat : undefined,
    lon: typeof item.lon === 'number' && Number.isFinite(item.lon) ? item.lon : undefined,
    course_rad: typeof item.course_rad === 'number' && Number.isFinite(item.course_rad) ? item.course_rad : undefined,
    sog_knots: typeof item.sog_knots === 'number' && Number.isFinite(item.sog_knots) ? item.sog_knots : undefined,
    cpa_m: typeof item.cpa_m === 'number' && Number.isFinite(item.cpa_m) ? item.cpa_m : undefined,
    tcpa_seconds: typeof item.tcpa_seconds === 'number' && Number.isFinite(item.tcpa_seconds) ? item.tcpa_seconds : undefined,
    source_zone: typeof item.source_zone === 'number' && Number.isFinite(item.source_zone) ? item.source_zone : undefined,
    last_seen_at: typeof item.last_seen_at === 'string' && item.last_seen_at !== '' ? item.last_seen_at : undefined,
  }
}

export function useRadarTargets() {
  const [targets, setTargets] = useState<RadarTarget[]>([])
  const [radars, setRadars] = useState<RadarInfo[]>([])
  const [source, setSource] = useState<RadarSource>('disabled')
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    const applyRadarTargets = (payload: unknown) => {
      try {
        const data = payload as RadarTargetsResponse
        const radarList = Array.isArray(data.radars) ? data.radars : []
        const targetList = Array.isArray(data.targets) ? data.targets : []

        setRadars(radarList.map(sanitizeRadar).filter((r): r is RadarInfo => r !== null))
        setTargets(targetList.map(sanitizeTarget).filter((t): t is RadarTarget => t !== null))
        // An unrecognised source string fails closed to 'disabled' rather
        // than propagating an unknown value the tile has no treatment for.
        setSource(isRadarSource(data.source) ? data.source : 'disabled')
      } catch (err) {
        console.error('Failed to process radar targets:', err)
      } finally {
        setLoading(false)
      }
    }

    return subscribeTelemetry('radar-targets', (raw) => {
      applyRadarTargets(JSON.parse(raw))
    })
  }, [])

  return { targets, radars, source, loading }
}
