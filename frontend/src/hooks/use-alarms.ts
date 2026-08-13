import { useCallback, useEffect, useState } from 'react'

import { subscribeTelemetry } from '@/hooks/use-telemetry-stream'

/** SignalK's notification states (ADR 0038), worst last. */
export const ALARM_STATES = ['normal', 'alert', 'warn', 'alarm', 'emergency'] as const
export type AlarmState = (typeof ALARM_STATES)[number]

export type AlarmPhase = 'normal' | 'pending' | 'active' | 'acknowledged'

export interface ActiveAlarm {
  rule_id: string
  label: string
  path: string
  phase: AlarmPhase
  state: AlarmState
  value: number
  message: string
  raised_at?: string
  acked_at?: string
}

interface AlarmsPayload {
  alarms: ActiveAlarm[]
  worst: AlarmState
}

export function alarmStateRank(state: AlarmState | string): number {
  const index = ALARM_STATES.indexOf(state as AlarmState)
  return index < 0 ? 0 : index
}

/**
 * Active alarms, pushed over the shared telemetry stream (ADR 0037) rather than
 * polled — an alarm the operator learns about ten seconds late is a worse alarm.
 */
export function useAlarms() {
  const [alarms, setAlarms] = useState<ActiveAlarm[]>([])
  const [worst, setWorst] = useState<AlarmState>('normal')

  useEffect(() => {
    return subscribeTelemetry('alarms', (raw) => {
      try {
        const payload = JSON.parse(raw) as AlarmsPayload
        setAlarms(Array.isArray(payload.alarms) ? payload.alarms : [])
        setWorst(payload.worst ?? 'normal')
      } catch (err) {
        console.error('Failed to parse alarms event:', err)
      }
    })
  }, [])

  const acknowledge = useCallback(async (ruleId: string) => {
    const response = await fetch(`/api/alarms/${encodeURIComponent(ruleId)}/acknowledge`, { method: 'POST' })
    if (!response.ok) {
      // A refusal is meaningful, not an error to swallow: an emergency cannot
      // be silenced, per the SignalK spec.
      const body = (await response.json().catch(() => ({}))) as { error?: string }
      throw new Error(body.error ?? 'Could not acknowledge alarm')
    }
  }, [])

  return { alarms, worst, acknowledge }
}

/** Rule id the server uses for its own anchor drag detection (ADR 0038). */
export const ANCHOR_DRAG_RULE_ID = 'helmcentral:anchor-drag'

export function findAnchorDragAlarm(alarms: ActiveAlarm[]): ActiveAlarm | null {
  return alarms.find((alarm) => alarm.rule_id === ANCHOR_DRAG_RULE_ID) ?? null
}
