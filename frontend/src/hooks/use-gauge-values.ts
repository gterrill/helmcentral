import { useSyncExternalStore } from 'react'

import { subscribeTelemetry } from '@/hooks/use-telemetry-stream'
import { ageFromPayload } from '@/lib/staleness'

/**
 * Current values for, and the age behind, every path a gauge is bound to.
 *
 * The backend works out which paths to send from the dashboard page config it
 * already owns (ADR 0039), so there is no subscription protocol here — the
 * browser just listens.
 *
 * Module-level shared store (same singleton shape as use-app-config.ts and
 * use-vessel-identity.ts): useGaugeValues() and useGaugeAges() used to each
 * open their own subscription to the same `gauge-values` SSE event and parse
 * the payload independently, so a single 1Hz tick did the JSON parse twice
 * and, since both handed back a fresh object literal every time, defeated
 * memoization on every tile reading either one — even on a tick where every
 * bound path's value and age were unchanged. Now there is exactly one
 * subscription (started on the first subscriber to either hook, stopped on
 * the last), one parse per tick, and each snapshot keeps its previous object
 * reference when it's shallow-equal to the new one.
 */

type PathMap = Record<string, number | null>

const EMPTY: PathMap = {}

let valuesSnapshot: PathMap = EMPTY
let agesSnapshot: PathMap = EMPTY
let subscriberCount = 0
let unsubscribeVesselStream: (() => void) | null = null
const valuesListeners = new Set<() => void>()
const agesListeners = new Set<() => void>()

function shallowEqual(a: PathMap, b: PathMap): boolean {
  if (a === b) return true
  const aKeys = Object.keys(a)
  if (aKeys.length !== Object.keys(b).length) return false
  return aKeys.every((key) => a[key] === b[key])
}

function applyGaugeValuesEvent(raw: string): void {
  let payload: { values?: PathMap; ages?: Record<string, number> }
  try {
    payload = JSON.parse(raw) as { values?: PathMap; ages?: Record<string, number> }
  } catch (err) {
    console.error('Failed to parse gauge-values event:', err)
    return
  }

  const nextValues = payload.values ?? {}
  if (!shallowEqual(valuesSnapshot, nextValues)) {
    valuesSnapshot = nextValues
    for (const listener of valuesListeners) listener()
  }

  // `-1` (ADR 0068's "no timestamp at all" sentinel) is mapped to `null`
  // through ageFromPayload, so a path with no freshness signal reads as
  // unknown rather than as impossibly fresh.
  const nextAges: PathMap = {}
  for (const [path, value] of Object.entries(payload.ages ?? {})) {
    nextAges[path] = ageFromPayload(value)
  }
  if (!shallowEqual(agesSnapshot, nextAges)) {
    agesSnapshot = nextAges
    for (const listener of agesListeners) listener()
  }
}

function acquire(): void {
  subscriberCount += 1
  if (subscriberCount === 1) {
    unsubscribeVesselStream = subscribeTelemetry('gauge-values', applyGaugeValuesEvent)
  }
}

function release(): void {
  subscriberCount = Math.max(0, subscriberCount - 1)
  if (subscriberCount === 0) {
    unsubscribeVesselStream?.()
    unsubscribeVesselStream = null
    // Nothing is listening, so there's no reason to keep stale data around —
    // the next subscriber starts exactly like a fresh mount would.
    valuesSnapshot = EMPTY
    agesSnapshot = EMPTY
  }
}

function subscribeValues(callback: () => void): () => void {
  valuesListeners.add(callback)
  acquire()
  return () => {
    valuesListeners.delete(callback)
    release()
  }
}

function subscribeAges(callback: () => void): () => void {
  agesListeners.add(callback)
  acquire()
  return () => {
    agesListeners.delete(callback)
    release()
  }
}

function getValuesSnapshot(): PathMap {
  return valuesSnapshot
}

function getAgesSnapshot(): PathMap {
  return agesSnapshot
}

export function useGaugeValues(): Record<string, number | null> {
  return useSyncExternalStore(subscribeValues, getValuesSnapshot)
}

/**
 * Age, in seconds, of the last update behind every path a gauge is bound to.
 *
 * Rides the same gauge-values event useGaugeValues does (ADR 0083) rather
 * than a stream of its own — the backend computes both from the same sample
 * on the same tick, so a sibling hook is all a widget needs.
 */
export function useGaugeAges(): Record<string, number | null> {
  return useSyncExternalStore(subscribeAges, getAgesSnapshot)
}
