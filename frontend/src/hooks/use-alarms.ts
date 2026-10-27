import { useCallback, useEffect, useRef, useSyncExternalStore } from 'react'

import { subscribeTelemetry } from '@/hooks/use-telemetry-stream'
import { apiRequest } from '@/lib/api-query'

/** SignalK's notification states (ADR 0038), worst last. */
export const ALARM_STATES = ['normal', 'alert', 'warn', 'alarm', 'emergency'] as const
export type AlarmState = (typeof ALARM_STATES)[number]

export type AlarmPhase = 'normal' | 'pending' | 'active' | 'acknowledged'

export interface SensorHealthEntry {
  identifier: string
  name: string
  quantity?: string
  /** The thing and the quantity: "Port Engine Starter Battery voltage". */
  label: string
  /** The whole line: label, reading and why it is wrong. */
  text: string
  /** Only on the ignored-sensors list: when the operator ignored it (RFC 3339). */
  ignored_at?: string
}

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
   * Live radar figures for the target behind a guard-zone notification,
   * looked up server-side on every read (alarm_presentation.go). Bearing is
   * radians true. Absent when the target is not in the radar store.
   */
  radar_target?: {
    bearing_rad: number
    range_m: number
    cpa_m?: number
    tcpa_seconds?: number
  }

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
   * One named line per failing sensor, for the three sensor-health count
   * alarms (frozen, impossible reading, silent source) only. Built on the
   * server (backend/sensor_names.go) so the card, the banner, Mate and every
   * notification say the same thing. `sensors` is the set at the moment the
   * alarm raised; `live_sensors` is re-read on every poll and is what the card
   * shows and what the ignore actions are built from. `identifier` is the raw
   * path or $source id the ignore action submits; it is never displayed.
   */
  sensors?: SensorHealthEntry[]
  live_sensors?: SensorHealthEntry[]

  /**
   * SignalK's own alert status and capabilities (ADR 0038). Silencing stops the
   * sound; acknowledging also stops the visual alert and moves the alarm out of
   * the active phase. What a given alarm supports is the server's answer — an
   * emergency supports neither, and a rule-driven alarm has no silence action.
   */
  silenced: boolean
  can_silence: boolean
  can_acknowledge: boolean
  /**
   * True on a network alarm the operator may stop showing. The server decides
   * (safety alarms and emergencies are never ignorable); the card offers the
   * action from this alone.
   */
  ignorable?: boolean
}

interface AlarmsPayload {
  alarms: ActiveAlarm[]
  worst: AlarmState
}

/** What the alarms store holds: the active list and the worst state on it. */
export interface AlarmsSnapshot {
  alarms: ActiveAlarm[]
  worst: AlarmState
}

/*
 * One module-level store for the `alarms` event (ADR 0215), the same shape as
 * use-gauge-values.ts: one subscription and one parse per tick, however many
 * surfaces read it. Each surface reads what it shows through a selector, so
 * an alarms tick re-renders the banner, the drawer, the lamp strips and the
 * wall badge, and never the shell.
 *
 * The server sends the whole list every two seconds whether or not it
 * changed. An alarm equal by value to the one before keeps its object, and a
 * list in which every alarm and the worst state are unchanged keeps the
 * snapshot, so a repeat notifies nobody. Every field counts: the cards show
 * the reading, the radar figures and the live sensors, and all of them move.
 */

const EMPTY_SNAPSHOT: AlarmsSnapshot = { alarms: [], worst: 'normal' }

let snapshot: AlarmsSnapshot = EMPTY_SNAPSHOT
let holders = 0
let unsubscribeStream: (() => void) | null = null
const listeners = new Set<() => void>()

/** Structural equality for parsed JSON: objects, arrays and primitives. */
function sameJSON(a: unknown, b: unknown): boolean {
  if (a === b) return true
  if (typeof a !== 'object' || typeof b !== 'object' || a === null || b === null) return false
  if (Array.isArray(a) !== Array.isArray(b)) return false
  if (Array.isArray(a)) {
    const other = b as unknown[]
    return a.length === other.length && a.every((item, i) => sameJSON(item, other[i]))
  }
  const aRecord = a as Record<string, unknown>
  const bRecord = b as Record<string, unknown>
  const aKeys = Object.keys(aRecord)
  if (aKeys.length !== Object.keys(bRecord).length) return false
  return aKeys.every((key) => key in bRecord && sameJSON(aRecord[key], bRecord[key]))
}

function applyAlarmsEvent(raw: string): void {
  let payload: AlarmsPayload
  try {
    payload = JSON.parse(raw) as AlarmsPayload
  } catch (err) {
    console.error('Failed to parse alarms event:', err)
    return
  }

  const previous = new Map(snapshot.alarms.map((alarm) => [alarm.rule_id, alarm]))
  const incoming = Array.isArray(payload.alarms) ? payload.alarms : []
  const alarms = incoming.map((alarm) => {
    const prev = previous.get(alarm.rule_id)
    return prev && sameJSON(prev, alarm) ? prev : alarm
  })
  const worst = payload.worst ?? 'normal'

  const unchanged = worst === snapshot.worst
    && alarms.length === snapshot.alarms.length
    && alarms.every((alarm, i) => alarm === snapshot.alarms[i])
  if (unchanged) return

  snapshot = { alarms, worst }
  for (const listener of listeners) listener()
}

function acquire(): void {
  holders += 1
  if (holders === 1) {
    unsubscribeStream = subscribeTelemetry('alarms', applyAlarmsEvent)
  }
}

function release(): void {
  holders = Math.max(0, holders - 1)
  if (holders === 0) {
    unsubscribeStream?.()
    unsubscribeStream = null
    snapshot = EMPTY_SNAPSHOT
  }
}

function subscribe(listener: () => void): () => void {
  listeners.add(listener)
  acquire()
  return () => {
    listeners.delete(listener)
    release()
  }
}

/**
 * Holds the alarms store open for as long as the calling component is
 * mounted, without re-rendering it on any tick. AppShell calls this once, so
 * the list survives page switches, panel changes and wall rotations.
 */
export function useRetainAlarms(): void {
  useEffect(() => {
    acquire()
    return release
  }, [])
}

/**
 * One slice of the alarms store. The component re-renders only when `select`
 * returns something `isEqual` calls different from what it returned before.
 * `select` may be a fresh function on every render.
 */
export function useAlarmsSelector<T>(select: (state: AlarmsSnapshot) => T, isEqual: (a: T, b: T) => boolean = Object.is): T {
  const cache = useRef<{ source: AlarmsSnapshot; select: (state: AlarmsSnapshot) => T; value: T } | null>(null)

  const getSnapshot = useCallback((): T => {
    const prev = cache.current
    if (prev && prev.source === snapshot && prev.select === select) return prev.value
    const next = select(snapshot)
    const value = prev && isEqual(prev.value, next) ? prev.value : next
    cache.current = { source: snapshot, select, value }
    return value
  }, [select, isEqual])

  return useSyncExternalStore(subscribe, getSnapshot)
}

const selectAlarms = (state: AlarmsSnapshot) => state.alarms
const selectWorst = (state: AlarmsSnapshot) => state.worst

/** The active alarms. Keeps its array while nothing on it changed. */
export function useAlarmList(): ActiveAlarm[] {
  return useAlarmsSelector(selectAlarms)
}

/** The worst state on the active list, `normal` when there is none. */
export function useWorstAlarmState(): AlarmState {
  return useAlarmsSelector(selectWorst)
}

function sameStateMap(a: Map<string, AlarmState>, b: Map<string, AlarmState>): boolean {
  if (a.size !== b.size) return false
  for (const [key, value] of a) {
    if (b.get(key) !== value) return false
  }
  return true
}

const selectCollisionStates = (state: AlarmsSnapshot) => collisionAlarmStatesByVessel(state.alarms)

/**
 * Vessel id -> worst live collision state, for the AIS markers and the Nearby
 * Vessels list (ADR 0088). Keeps its Map while no collision alarm changed, so
 * a rule alarm coming and going does not redraw a map.
 */
export function useAisCollisionAlarms(): Map<string, AlarmState> {
  return useAlarmsSelector(selectCollisionStates, sameStateMap)
}

const selectAnchorDrag = (state: AlarmsSnapshot) => findAnchorDragAlarm(state.alarms)

/** The server's own anchor drag alarm while it is up, else null. */
export function useAnchorDragAlarm(): ActiveAlarm | null {
  return useAlarmsSelector(selectAnchorDrag)
}

// A refusal is meaningful, not an error to swallow: an emergency cannot be
// silenced, and SignalK reports per notification which actions it offers.
async function act(ruleId: string, action: 'acknowledge' | 'silence'): Promise<void> {
  await apiRequest(`/api/alarms/${encodeURIComponent(ruleId)}/${action}`, { method: 'POST' })
}

/** Acknowledges one alarm on the server. Stable: it closes over nothing. */
export function acknowledgeAlarm(ruleId: string): Promise<void> {
  return act(ruleId, 'acknowledge')
}

/** Silences one alarm on the server. Stable: it closes over nothing. */
export function silenceAlarm(ruleId: string): Promise<void> {
  return act(ruleId, 'silence')
}

const selectAll = (state: AlarmsSnapshot) => state

/**
 * Active alarms, pushed over the shared telemetry stream (ADR 0037) rather than
 * polled — an alarm the operator learns about ten seconds late is a worse alarm.
 * The whole list and the actions together; a surface that shows less reads a
 * selector instead.
 */
export function useAlarms() {
  const { alarms, worst } = useAlarmsSelector(selectAll)
  return { alarms, worst, acknowledge: acknowledgeAlarm, silence: silenceAlarm }
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
