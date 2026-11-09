import type { TideChart } from '@/hooks/use-tide-chart'
import { normaliseTideDatum } from '@/lib/tide-datum'

// The tide chart payload as the browser holds it. Kept out of the hooks so a
// test that mocks one of them still gets the real mapping from the other.

interface TideChartApi {
  station?: {
    station_id?: string
    name?: string
    state?: string
    lat?: number
    lon?: number
    timezone?: string
  }
  extremes?: Array<{
    time?: string
    height_m?: number
    high?: boolean
  }>
  current_height_m?: number
  direction?: string
  cached?: boolean
  updated_at?: string
  ttl_seconds?: number
  tidal_phase?: string
  double_high_today?: boolean
  double_low_today?: boolean
  height_datum?: string
  provider?: string
  provider_name?: string
  position_based?: boolean
}

export interface MappedTideChart {
  chart: TideChart
  isCached: boolean
  updatedAt: string | null
  ttlSeconds: number | null
  /** Who answered, for the "Data:" line. */
  providerName: string
  /** True when the tide follows a position, so there is no station to choose. */
  positionBased: boolean
}

export function mapTideChart(data: TideChartApi): MappedTideChart {
  const chart: TideChart = {
    station: {
      stationId: data.station?.station_id ?? '',
      name: data.station?.name ?? '',
      state: data.station?.state,
      lat: typeof data.station?.lat === 'number' ? data.station.lat : 0,
      lon: typeof data.station?.lon === 'number' ? data.station.lon : 0,
      timezone: data.station?.timezone,
    },
    extremes: Array.isArray(data.extremes)
      ? data.extremes.map((extreme) => ({
          time: extreme.time ?? '',
          heightM: typeof extreme.height_m === 'number' ? extreme.height_m : 0,
          high: Boolean(extreme.high),
        }))
      : [],
    currentHeightM: typeof data.current_height_m === 'number' ? data.current_height_m : 0,
    direction: data.direction || '—',
    heightDatum: normaliseTideDatum(data.height_datum),
    tidalPhase: typeof data.tidal_phase === 'string' && data.tidal_phase !== '' ? data.tidal_phase : undefined,
    doubleHighToday: typeof data.double_high_today === 'boolean' ? data.double_high_today : undefined,
    doubleLowToday: typeof data.double_low_today === 'boolean' ? data.double_low_today : undefined,
  }
  return {
    chart,
    isCached: Boolean(data.cached),
    updatedAt: typeof data.updated_at === 'string' ? data.updated_at : null,
    ttlSeconds: typeof data.ttl_seconds === 'number' ? data.ttl_seconds : null,
    providerName: data.provider_name || data.provider || '',
    positionBased: data.position_based === true,
  }
}
