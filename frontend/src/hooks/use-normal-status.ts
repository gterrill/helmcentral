import { useApiQuery } from '@/lib/api-query'

/**
 * What the learned normal holds (backend/anomaly_normal_status.go, ADR 0276):
 * when and from where it was learned, and each engine's and the house bank's
 * readings, grouped by subsystem, with the switch (the ignored-sensor list) and
 * the automatic verdict beside it. Every sentence arrives finished from the
 * server, in the report's own words.
 */
export interface NormalStatusReading {
  key: string
  /** The SignalK path: what the ignored-sensor list holds. */
  path: string
  /** Every path the switch puts on or off the ignored sensors, for a reading that is the sum of several. */
  paths?: string[]
  name: string
  /** The switch. False while the reading is on the ignored-sensor list. */
  on: boolean
  /** The subject cannot be judged without the reading, so it cannot be switched off. */
  required: boolean
  status: string
}

/** One engine, or the house bank. */
export interface NormalStatusSubject {
  id: string
  name: string
  bands: number
  problem?: string
  readings: NormalStatusReading[]
}

/** A group of subjects under its heading: the engines, or the house bank. */
export interface NormalStatusSubsystem {
  id: string
  title: string
  /** Why the group holds nothing, when the boat has nothing of the kind set up. */
  note?: string
  subjects: NormalStatusSubject[]
}

export interface NormalStatus {
  configured: boolean
  summary?: string
  /** What is wrong, as a finished sentence. Absent when nothing is. */
  note?: string
  subsystems: NormalStatusSubsystem[]
}

export const NORMAL_STATUS_PATH = '/api/anomaly/normal/status'

export function useNormalStatus() {
  const { data, isLoading, error, refetch } = useApiQuery<NormalStatus>({ queryKey: [NORMAL_STATUS_PATH], path: NORMAL_STATUS_PATH })
  return { status: data ?? null, loading: isLoading, error: error?.message ?? null, refresh: refetch }
}
