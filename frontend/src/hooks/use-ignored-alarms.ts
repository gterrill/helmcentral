import { useCallback } from 'react'

import { apiRequest, setQueryData, useApiQuery } from '@/lib/api-query'

/**
 * The network alarms the operator has stopped showing
 * (backend/alarm_ignored_alarms.go). `kind` is what DELETE takes; `title` is
 * the wording the alarm card used, so the Settings list names what the
 * operator saw. Used by the alarms drawer (Stop showing) and the Alarms
 * settings section (Restore).
 */
export interface IgnoredAlarm {
  kind: string
  title: string
  ignored_at: string
}

interface IgnoredAlarmsBody {
  alarms?: IgnoredAlarm[]
}

const PATH = '/api/alarms/ignored-alarms'
const NO_ALARMS: IgnoredAlarm[] = []

function normalise(body: IgnoredAlarmsBody): IgnoredAlarm[] {
  return Array.isArray(body.alarms) ? body.alarms : NO_ALARMS
}

export function useIgnoredAlarms() {
  const { data, isLoading, error, refetch } = useApiQuery<IgnoredAlarm[]>({
    queryKey: [PATH],
    path: PATH,
    select: normalise,
  })

  const ignore = useCallback(async (kind: string): Promise<void> => {
    const body = await apiRequest<IgnoredAlarmsBody>(PATH, { method: 'POST', json: { kind } })
    setQueryData<IgnoredAlarmsBody>([PATH], { alarms: normalise(body) })
  }, [])

  const restore = useCallback(async (kind: string): Promise<void> => {
    await apiRequest(`${PATH}/${encodeURIComponent(kind)}`, { method: 'DELETE' })
    setQueryData<IgnoredAlarmsBody>([PATH], (prev) => ({ alarms: normalise(prev ?? {}).filter((a) => a.kind !== kind) }))
  }, [])

  return {
    alarms: data ?? NO_ALARMS,
    loading: isLoading,
    error: error?.message ?? null,
    ignore,
    restore,
    refresh: refetch,
  }
}
