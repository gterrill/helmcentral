import { useCallback, useRef, useSyncExternalStore } from 'react'

import { ageFromPayload, shownAge } from '@/lib/staleness'
import { createTelemetryStore, sameShallow, type TelemetryUpdate } from '@/lib/telemetry-store'

/**
 * Current values for, and the age behind, every path a gauge is bound to.
 *
 * The backend works out which paths to send from the dashboard page config it
 * already owns (ADR 0039), so there is no subscription protocol here: the
 * browser just listens. Ages ride the same event (ADR 0083).
 *
 * One module-level store: one subscription to the `gauge-values` event and
 * one parse per tick, however many tiles read it. Each tile reads only the
 * paths it shows (ADR 0215), through useGaugeValue, useGaugeAge,
 * useGaugeSubset or useEngineStatus, and listeners are kept per path, so a
 * tick re-renders only the tiles whose own reading changed. Nothing hands the
 * whole map to a component.
 *
 * The store starts with its first reader and stays subscribed for the life of
 * the page (lib/telemetry-store.ts), so the last reading survives the moment
 * between one page's tiles unmounting and the next page's mounting.
 *
 * An age changes, for its listeners, only when what a tile shows for it
 * changes: unknown, fresh, or the stale label (see shownAge).
 */

type PathMap = Record<string, number | null>

const EMPTY: PathMap = {}

/**
 * Whether an engine reads as running, switched off or lost, per propulsion id
 * (`port`, `starboard`). Off means its computer powered down with the key;
 * lost means its whole connection went quiet. `lastUpdate` is set for both.
 */
export type EngineState = 'running' | 'off' | 'lost'
export interface EngineStatus {
  state: EngineState
  lastUpdate?: string
}
export type EngineStates = Record<string, EngineStatus>

/** The values and ages for a set of paths. A path the event does not carry has no key. */
export interface GaugeSubset {
  values: PathMap
  ages: PathMap
}

const EMPTY_ENGINES: EngineStates = {}
const ENGINE_STATES: readonly string[] = ['running', 'off', 'lost']

interface GaugeState {
  values: PathMap
  ages: PathMap
  engines: EngineStates
}

const INITIAL: GaugeState = { values: EMPTY, ages: EMPTY, engines: EMPTY_ENGINES }

// Listener keys, one namespace per map, so a value, an age and an engine
// with the same name never wake each other's readers.
const valueKey = (path: string) => `value:${path}`
const ageKey = (path: string) => `age:${path}`
const engineKey = (id: string) => `engine:${id}`

function sameStatus(a: EngineStatus | undefined, b: EngineStatus | undefined): boolean {
  return a?.state === b?.state && a?.lastUpdate === b?.lastUpdate
}

/** Keys whose value differs between two maps, including keys only one of them has. */
function changedKeys<T>(prev: Record<string, T>, next: Record<string, T>, same: (a: T | undefined, b: T | undefined) => boolean): string[] {
  const changed: string[] = []
  for (const key of Object.keys(next)) {
    if (!(key in prev) || !same(prev[key], next[key])) changed.push(key)
  }
  for (const key of Object.keys(prev)) {
    if (!(key in next)) changed.push(key)
  }
  return changed
}

const sameNumber = (a: number | null | undefined, b: number | null | undefined) => a === b

type EnginesPayload = Record<string, { state?: string; last_update?: string }>

function applyGaugeValuesEvent(previous: GaugeState, data: unknown): TelemetryUpdate<GaugeState> | null {
  const payload = data as { values?: PathMap; ages?: Record<string, number>; engines?: EnginesPayload }

  const nextValues = payload.values ?? {}

  // `-1` (ADR 0068's "no timestamp at all" sentinel) is mapped to `null`
  // through ageFromPayload, so a path with no freshness signal reads as
  // unknown rather than as impossibly fresh.
  //
  // The server rounds ages to 0.1 s, so most of them move on every tick. An
  // age that moves without changing what a tile shows keeps the number the
  // store last handed out, so it notifies nobody. A fresh age can therefore
  // read lower than the true one; it is never used as a figure until stale.
  const nextAges: PathMap = {}
  for (const [path, value] of Object.entries(payload.ages ?? {})) {
    const age = ageFromPayload(value)
    const prev = previous.ages[path]
    nextAges[path] = path in previous.ages && shownAge(prev) === shownAge(age) ? prev : age
  }

  // An engine whose status is unchanged keeps its object, so a tile reading
  // one engine does not re-render when the other one changes.
  const nextEngines: EngineStates = {}
  for (const [id, entry] of Object.entries(payload.engines ?? {})) {
    if (!entry || typeof entry.state !== 'string' || !ENGINE_STATES.includes(entry.state)) continue
    const status: EngineStatus = entry.last_update
      ? { state: entry.state as EngineState, lastUpdate: entry.last_update }
      : { state: entry.state as EngineState }
    const prev = previous.engines[id]
    nextEngines[id] = prev && sameStatus(prev, status) ? prev : status
  }

  const valuesChanged = changedKeys(previous.values, nextValues, sameNumber)
  const agesChanged = changedKeys(previous.ages, nextAges, sameNumber)
  const enginesChanged = changedKeys(previous.engines, nextEngines, sameStatus)
  if (valuesChanged.length === 0 && agesChanged.length === 0 && enginesChanged.length === 0) return null

  // Each map keeps its object while nothing in it changed.
  return {
    state: {
      values: valuesChanged.length > 0 ? nextValues : previous.values,
      ages: agesChanged.length > 0 ? nextAges : previous.ages,
      engines: enginesChanged.length > 0 ? (Object.keys(nextEngines).length === 0 ? EMPTY_ENGINES : nextEngines) : previous.engines,
    },
    // A listener on several changed keys (a subset) is called once.
    changed: [...valuesChanged.map(valueKey), ...agesChanged.map(ageKey), ...enginesChanged.map(engineKey)],
  }
}

const store = createTelemetryStore('gauge-values', INITIAL, applyGaugeValuesEvent)

/** The raw SI value on one path, or null when the event does not carry it. */
export function useGaugeValue(path: string): number | null {
  const subscribe = useCallback((cb: () => void) => store.subscribeKeys([valueKey(path)], cb), [path])
  return useSyncExternalStore(subscribe, () => store.getSnapshot().values[path] ?? null)
}

/** Age, in seconds, of the last update on one path (ADR 0083); null is unknown. */
export function useGaugeAge(path: string): number | null {
  const subscribe = useCallback((cb: () => void) => store.subscribeKeys([ageKey(path)], cb), [path])
  return useSyncExternalStore(subscribe, () => store.getSnapshot().ages[path] ?? null)
}

function pick(source: PathMap, paths: readonly string[]): PathMap {
  const out: PathMap = {}
  for (const path of paths) {
    if (path in source) out[path] = source[path]
  }
  return out
}

/**
 * Values and ages for the paths one tile shows. The result keeps its
 * reference while none of those paths changed, so it can feed a memoized
 * tile. `paths` may be a fresh array on every render.
 */
export function useGaugeSubset(paths: readonly string[]): GaugeSubset {
  const key = paths.join('\n')
  const cache = useRef<{ key: string; values: PathMap; ages: PathMap; subset: GaugeSubset } | null>(null)

  const subscribe = useCallback((cb: () => void) => {
    const list = key === '' ? [] : key.split('\n')
    return store.subscribeKeys([...list.map(valueKey), ...list.map(ageKey)], cb)
  }, [key])

  const getSnapshot = useCallback((): GaugeSubset => {
    const { values: valuesSnapshot, ages: agesSnapshot } = store.getSnapshot()
    const prev = cache.current
    if (prev && prev.key === key && prev.values === valuesSnapshot && prev.ages === agesSnapshot) return prev.subset
    const list = key === '' ? [] : key.split('\n')
    const values = pick(valuesSnapshot, list)
    const ages = pick(agesSnapshot, list)
    const subset = prev && prev.key === key && sameShallow(prev.subset.values, values) && sameShallow(prev.subset.ages, ages)
      ? prev.subset
      : { values, ages }
    cache.current = { key, values: valuesSnapshot, ages: agesSnapshot, subset }
    return subset
  }, [key])

  return useSyncExternalStore(subscribe, getSnapshot)
}

/**
 * Whether one engine reads as running, switched off or lost, by propulsion id
 * (`port`). Undefined when the server cannot classify it, or for no engine.
 */
export function useEngineStatus(id: string | null): EngineStatus | undefined {
  const subscribe = useCallback(
    (cb: () => void) => store.subscribeKeys(id === null ? [] : [engineKey(id)], cb),
    [id],
  )
  return useSyncExternalStore(subscribe, () => (id === null ? undefined : store.getSnapshot().engines[id]))
}
