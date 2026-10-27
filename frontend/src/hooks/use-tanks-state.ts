import { useMemo } from 'react'

import { ageFromPayload, shownAge } from '@/lib/staleness'

import { createTelemetryStore, keepUnchangedItems, sameShallow, type TelemetryUpdate } from '@/lib/telemetry-store'

export type TankLevel = {
  id: string
  label: string
  category: string
  kind: 'water' | 'fuel' | 'waste'
  level_percent: number
  /** The tank's resolved levels (ADR 0214): what its colours and its alarms both use. */
  warn_percent: number
  alarm_percent: number
  /** Silences the alarms only; the tile still colours by the levels. */
  alarms_off: boolean
}

type TanksStateResponse = {
  tanks?: TankLevel[]
  /** Set when the server could not give the tanks their levels (ADR 0214). */
  error?: string
}

/** What the tanks store holds. */
export interface TanksStateValue {
  tanks: TankLevel[]
  error: string | null
  // Freshest of the per-tank ages (ADR 0068); null when no tank publishes a
  // timestamp.
  lastUpdateAgeS: number | null
}

const INITIAL: TanksStateValue = { tanks: [], error: null, lastUpdateAgeS: null }

function isTankLevel(tank: TankLevel): boolean {
  return typeof tank.id === 'string' &&
    typeof tank.label === 'string' &&
    typeof tank.category === 'string' &&
    (tank.kind === 'water' || tank.kind === 'fuel' || tank.kind === 'waste') &&
    typeof tank.level_percent === 'number' &&
    Number.isFinite(tank.level_percent) &&
    Number.isFinite(tank.warn_percent) &&
    Number.isFinite(tank.alarm_percent) &&
    typeof tank.alarms_off === 'boolean'
}

// Only the fields the tile reads: each tank's own age moves on every tick and
// nothing shows it, so it is left behind rather than compared.
function toTankLevel(tank: TankLevel): TankLevel {
  return {
    id: tank.id, label: tank.label, category: tank.category, kind: tank.kind, level_percent: tank.level_percent,
    warn_percent: tank.warn_percent, alarm_percent: tank.alarm_percent, alarms_off: tank.alarms_off,
  }
}

/*
 * One store for the `tanks-state` event (ADR 0215; lib/telemetry-store.ts).
 * On the boat a tick moves datetime and every age, and a level now and then.
 * A tank that reads the same keeps its object, the feed age counts only when
 * what the tile shows for it changes, and a tick that changes nothing shown
 * keeps the snapshot.
 */
function applyTanksState(previous: TanksStateValue, payload: unknown): TelemetryUpdate<TanksStateValue> | null {
  const data = payload as TanksStateResponse & { last_update_age_s?: unknown }
  const list = Array.isArray(data.tanks) ? data.tanks : []
  const tanks = keepUnchangedItems(previous.tanks, list.filter(isTankLevel).map(toTankLevel), (t) => t.id, sameShallow)
  const error = typeof data.error === 'string' && data.error !== '' ? data.error : null
  const age = ageFromPayload(data.last_update_age_s)
  const lastUpdateAgeS = shownAge(age) === shownAge(previous.lastUpdateAgeS) ? previous.lastUpdateAgeS : age
  if (tanks === previous.tanks && error === previous.error && lastUpdateAgeS === previous.lastUpdateAgeS) return null
  return { state: { tanks, error, lastUpdateAgeS } }
}

const store = createTelemetryStore('tanks-state', INITIAL, applyTanksState)

const selectAll = (state: TanksStateValue) => state
const selectTanks = (state: TanksStateValue) => state.tanks

function sameLevels(a: TankLevel[], b: TankLevel[]): boolean {
  return a.length === b.length && a.every((tank, i) => {
    const other = b[i]
    return tank.id === other.id && tank.kind === other.kind && tank.warn_percent === other.warn_percent
      && tank.alarm_percent === other.alarm_percent && tank.alarms_off === other.alarms_off
  })
}

/**
 * The tanks, for a reader that uses only each tank's kind and resolved alarm
 * levels (the engine cluster's fuel bars, ADR 0214): a level moving leaves it
 * alone.
 */
export function useTankAlarmLevels(): TankLevel[] {
  return store.useSelector(selectTanks, sameLevels)
}

/** The tanks, for the Tanks tile. `loading` until the first event. */
export function useTanksState() {
  const heard = store.useHeardSelector(selectAll)
  return useMemo(() => ({
    tanks: (heard ?? INITIAL).tanks,
    loading: heard === null,
    error: (heard ?? INITIAL).error,
    lastUpdateAgeS: (heard ?? INITIAL).lastUpdateAgeS,
  }), [heard])
}
