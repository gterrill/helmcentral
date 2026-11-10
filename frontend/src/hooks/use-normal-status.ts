import { useApiQuery } from '@/lib/api-query'

/**
 * What the learned normal holds (backend/anomaly_normal_status.go, ADR 0276):
 * when and from where it was learned, and each engine's readings with the
 * switch (the ignored-sensor list) and the automatic verdict beside it. Every
 * sentence arrives finished from the server, in the report's own words.
 */
export interface NormalStatusReading {
  key: string
  /** The SignalK path: what the ignored-sensor list holds. */
  path: string
  name: string
  /** The switch. False while the reading is on the ignored-sensor list. */
  on: boolean
  /** The engine cannot be judged without the reading, so it cannot be switched off. */
  required: boolean
  status: string
}

export interface NormalStatusEngine {
  id: string
  name: string
  bands: number
  problem?: string
  readings: NormalStatusReading[]
}

export interface NormalStatus {
  configured: boolean
  summary?: string
  /** What is wrong, as a finished sentence. Absent when nothing is. */
  note?: string
  engines: NormalStatusEngine[]
}

export const NORMAL_STATUS_PATH = '/api/anomaly/normal/status'

export function useNormalStatus() {
  const { data, isLoading, error, refetch } = useApiQuery<NormalStatus>({ queryKey: [NORMAL_STATUS_PATH], path: NORMAL_STATUS_PATH })
  return { status: data ?? null, loading: isLoading, error: error?.message ?? null, refresh: refetch }
}
