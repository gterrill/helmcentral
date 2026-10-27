import { useCallback, useEffect, useRef, useSyncExternalStore } from 'react'

import { subscribeTelemetry } from '@/hooks/use-telemetry-stream'
import { ageFromPayload, shownAge } from '@/lib/staleness'

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
 * The store lives while anything holds it. AppShell holds it for the life of
 * the app through useRetainGaugeValues, without listening to it, so the last
 * reading survives the moment between one page's tiles unmounting and the
 * next page's mounting.
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

let valuesSnapshot: PathMap = EMPTY
let agesSnapshot: PathMap = EMPTY
let enginesSnapshot: EngineStates = EMPTY_ENGINES
let holders = 0
let unsubscribeStream: (() => void) | null = null

const valueListeners = new Map<string, Set<() => void>>()
const ageListeners = new Map<string, Set<() => void>>()
const engineListeners = new Map<string, Set<() => void>>()

function shallowEqual(a: PathMap, b: PathMap): boolean {
  if (a === b) return true
  const aKeys = Object.keys(a)
  if (aKeys.length !== Object.keys(b).length) return false
  return aKeys.every((key) => key in b && a[key] === b[key])
}

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

function notify(listeners: Map<string, Set<() => void>>, keys: readonly string[], seen: Set<() => void>): void {
  for (const key of keys) {
    const set = listeners.get(key)
    if (!set) continue
    for (const listener of set) seen.add(listener)
  }
}

type EnginesPayload = Record<string, { state?: string; last_update?: string }>

function applyGaugeValuesEvent(raw: string): void {
  let payload: { values?: PathMap; ages?: Record<string, number>; engines?: EnginesPayload }
  try {
    payload = JSON.parse(raw) as { values?: PathMap; ages?: Record<string, number>; engines?: EnginesPayload }
  } catch (err) {
    console.error('Failed to parse gauge-values event:', err)
    return
  }

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
    const prev = agesSnapshot[path]
    nextAges[path] = path in agesSnapshot && shownAge(prev) === shownAge(age) ? prev : age
  }

  // An engine whose status is unchanged keeps its object, so a tile reading
  // one engine does not re-render when the other one changes.
  const nextEngines: EngineStates = {}
  for (const [id, entry] of Object.entries(payload.engines ?? {})) {
    if (!entry || typeof entry.state !== 'string' || !ENGINE_STATES.includes(entry.state)) continue
    const status: EngineStatus = entry.last_update
      ? { state: entry.state as EngineState, lastUpdate: entry.last_update }
      : { state: entry.state as EngineState }
    const prev = enginesSnapshot[id]
    nextEngines[id] = prev && sameStatus(prev, status) ? prev : status
  }

  const valuesChanged = changedKeys(valuesSnapshot, nextValues, sameNumber)
  const agesChanged = changedKeys(agesSnapshot, nextAges, sameNumber)
  const enginesChanged = changedKeys(enginesSnapshot, nextEngines, sameStatus)

  // Each snapshot keeps its object while nothing in it changed.
  if (valuesChanged.length > 0) valuesSnapshot = nextValues
  if (agesChanged.length > 0) agesSnapshot = nextAges
  if (enginesChanged.length > 0) enginesSnapshot = Object.keys(nextEngines).length === 0 ? EMPTY_ENGINES : nextEngines

  // A listener on several changed paths (a subset) is called once.
  const toCall = new Set<() => void>()
  notify(valueListeners, valuesChanged, toCall)
  notify(ageListeners, agesChanged, toCall)
  notify(engineListeners, enginesChanged, toCall)
  for (const listener of toCall) listener()
}

function acquire(): void {
  holders += 1
  if (holders === 1) {
    unsubscribeStream = subscribeTelemetry('gauge-values', applyGaugeValuesEvent)
  }
}

function release(): void {
  holders = Math.max(0, holders - 1)
  if (holders === 0) {
    unsubscribeStream?.()
    unsubscribeStream = null
    // Nothing holds the store, so its readings would only go stale here; the
    // next holder starts exactly like a fresh mount.
    valuesSnapshot = EMPTY
    agesSnapshot = EMPTY
    enginesSnapshot = EMPTY_ENGINES
  }
}

function listen(listeners: Map<string, Set<() => void>>, keys: readonly string[], callback: () => void): () => void {
  for (const key of keys) {
    let set = listeners.get(key)
    if (!set) {
      set = new Set()
      listeners.set(key, set)
    }
    set.add(callback)
  }
  return () => {
    for (const key of keys) {
      const set = listeners.get(key)
      if (!set) continue
      set.delete(callback)
      if (set.size === 0) listeners.delete(key)
    }
  }
}

function subscribeTo(maps: Array<Map<string, Set<() => void>>>, keys: readonly string[], callback: () => void): () => void {
  const stops = maps.map((map) => listen(map, keys, callback))
  acquire()
  return () => {
    for (const stop of stops) stop()
    release()
  }
}

/**
 * Holds the store open for as long as the calling component is mounted,
 * without re-rendering it on any tick. AppShell calls this once.
 */
export function useRetainGaugeValues(): void {
  useEffect(() => {
    acquire()
    return release
  }, [])
}

/** The raw SI value on one path, or null when the event does not carry it. */
export function useGaugeValue(path: string): number | null {
  const subscribe = useCallback((cb: () => void) => subscribeTo([valueListeners], [path], cb), [path])
  return useSyncExternalStore(subscribe, () => valuesSnapshot[path] ?? null)
}

/** Age, in seconds, of the last update on one path (ADR 0083); null is unknown. */
export function useGaugeAge(path: string): number | null {
  const subscribe = useCallback((cb: () => void) => subscribeTo([ageListeners], [path], cb), [path])
  return useSyncExternalStore(subscribe, () => agesSnapshot[path] ?? null)
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

  const subscribe = useCallback(
    (cb: () => void) => subscribeTo([valueListeners, ageListeners], key === '' ? [] : key.split('\n'), cb),
    [key],
  )

  const getSnapshot = useCallback((): GaugeSubset => {
    const prev = cache.current
    if (prev && prev.key === key && prev.values === valuesSnapshot && prev.ages === agesSnapshot) return prev.subset
    const list = key === '' ? [] : key.split('\n')
    const values = pick(valuesSnapshot, list)
    const ages = pick(agesSnapshot, list)
    const subset = prev && prev.key === key && shallowEqual(prev.subset.values, values) && shallowEqual(prev.subset.ages, ages)
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
    (cb: () => void) => subscribeTo([engineListeners], id === null ? [] : [id], cb),
    [id],
  )
  return useSyncExternalStore(subscribe, () => (id === null ? undefined : enginesSnapshot[id]))
}
