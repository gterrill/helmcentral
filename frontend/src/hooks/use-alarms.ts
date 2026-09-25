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

  /**
   * Rule-alarm fields (absent on bus notifications, which have no rule
   * behind them). `op` is the comparison the rule evaluates; `threshold` and
   * `hysteresis` are the rule's configured values in SI; `clear_value` is
   * the SI value the server will accept as cleared, present only for
   * `above`/`below` (there is no "clears" point for equal/notEqual/stale).
   */
  op?: 'above' | 'below' | 'equal' | 'notEqual' | 'stale'
  threshold?: number
  hysteresis?: number
  clear_value?: number
  /** SI unit string from SignalK meta or the derived-path table; absent when unknown. */
  unit?: string

  /**
   * The COLREGS "situation + role" line for a collision alarm -- what kind
   * of encounter this is and what the rules ask of us, e.g. "Crossing, she
   * is on our starboard bow (040° rel). We give way (Rule 15): alter to
   * starboard, pass astern." Built server-side (collision_colregs.go, ADR
   * 0098); absent for every non-collision alarm, and for a collision alarm
   * whose geometry the classifier can't yet vouch for (stopped, opening, or
   * missing an input).
   */
  encounter?: string

  /**
   * The "why" sentence behind an anomaly-detection alarm, e.g. "House bank
   * 96% SoC, 28.90 V, charging 42 A" or "At 2,400 rpm port 84.0 degC, peers
   * 79.0 degC; usually 1.2 degC off (38m learned); now 3.8 degC beyond
   * that." Built server-side and frozen at the moment the alarm raised
   * (anomaly_detector.go's per-tick evidence, captured once in
   * advanceAlarmRule) -- unlike `encounter` above, it does not keep
   * following the live reading while the alarm stays up. Absent for every
   * non-anomaly alarm.
   */
  evidence?: string

  /**
   * The sensor identifiers (SignalK paths or $source ids) actually failing
   * RIGHT NOW, for the three sensor-health count alarms (frozen/impossible/
   * silent-source) only -- absent for every other alarm. Unlike `evidence`
   * above, this is recomputed on every /api/alarms read from the live
   * detector state (alarm_engine.go's withLiveSensorEvidence), because a
   * count alarm can stay continuously active for a long time while its
   * specific offenders drift: one sensor recovers, another starts failing,
   * and the count itself never dips enough to clear and re-raise. The
   * "Ignore this sensor" action is built from this field, not `evidence`,
   * so it always offers what is failing now rather than whatever first
   * tripped the alarm.
   */
  live_evidence?: string

  /**
   * SignalK's own alert status and capabilities (ADR 0038). Silencing stops the
   * sound; acknowledging also stops the visual alert and moves the alarm out of
   * the active phase. What a given alarm supports is the server's answer — an
   * emergency supports neither, and a rule-driven alarm has no silence action.
   */
  silenced: boolean
  can_silence: boolean
  can_acknowledge: boolean
}

interface AlarmsPayload {
  alarms: ActiveAlarm[]
  worst: AlarmState
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

  // A refusal is meaningful, not an error to swallow: an emergency cannot be
  // silenced, and SignalK reports per notification which actions it offers.
  const act = useCallback(async (ruleId: string, action: 'acknowledge' | 'silence') => {
    const response = await fetch(`/api/alarms/${encodeURIComponent(ruleId)}/${action}`, { method: 'POST' })
    if (!response.ok) {
      const body = (await response.json().catch(() => ({}))) as { error?: string }
      throw new Error(body.error ?? `Could not ${action} alarm`)
    }
  }, [])

  const acknowledge = useCallback((ruleId: string) => act(ruleId, 'acknowledge'), [act])
  const silence = useCallback((ruleId: string) => act(ruleId, 'silence'), [act])

  return { alarms, worst, acknowledge, silence }
}

/** Rule id the server uses for its own anchor drag detection (ADR 0038). */
export const ANCHOR_DRAG_RULE_ID = 'helmcentral:anchor-drag'

export function findAnchorDragAlarm(alarms: ActiveAlarm[]): ActiveAlarm | null {
  return alarms.find((alarm) => alarm.rule_id === ANCHOR_DRAG_RULE_ID) ?? null
}

/**
 * Rule id prefix of an AIS collision alarm (ADR 0057). The SignalK vessel id
 * follows the '@', and it is the same string the nearby-vessels payload uses
 * as NearbyVessel.id, so the map can match the two by plain equality.
 */
export const COLLISION_ALARM_RULE_PREFIX = 'notifications:navigation.closestApproach@'

/** Vessel id -> worst live collision state, for the map's AIS markers. Empty when none is in force. */
export function collisionAlarmStatesByVessel(alarms: ActiveAlarm[]): Map<string, AlarmState> {
  const result = new Map<string, AlarmState>()
  for (const alarm of alarms) {
    if (!alarm.rule_id.startsWith(COLLISION_ALARM_RULE_PREFIX)) continue
    const vesselId = alarm.rule_id.slice(COLLISION_ALARM_RULE_PREFIX.length)
    if (vesselId === '') continue
    const existing = result.get(vesselId)
    if (existing === undefined || ALARM_STATES.indexOf(alarm.state) > ALARM_STATES.indexOf(existing)) {
      result.set(vesselId, alarm.state)
    }
  }
  return result
}
