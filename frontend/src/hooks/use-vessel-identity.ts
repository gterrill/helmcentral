import { useMemo, useSyncExternalStore } from 'react'

import { useAppConfig } from '@/hooks/use-app-config'
import { subscribeTelemetry } from '@/hooks/use-telemetry-stream'

// The vessel's local zone (backend/weather_tide.go's vesselLocalTimezoneName,
// ADR 0035) can be an IANA name Intl has never heard of only if the backend
// itself starts sending something malformed — that upstream contract is
// worth failing loudly for, but not by crashing the wall display's clock.
// Warn once per bad zone name rather than throwing, and rather than warning
// on every per-second tick.
const warnedInvalidTimeZones = new Set<string>()

// Formatters cached per (timeZone, options) at module level rather than
// rebuilt on every call: this hook feeds `now` through formatClock/formatDate
// via useMemo once per second per consumer, and clock-tile.tsx calls both
// directly on top of that — constructing a fresh Intl.DateTimeFormat that
// often is pure waste.
const formatterCache = new Map<string, Intl.DateTimeFormat>()

function dateTimeFormat(options: Intl.DateTimeFormatOptions, timeZone?: string): Intl.DateTimeFormat {
  const key = `${timeZone ?? ''}|${JSON.stringify(options)}`
  const cached = formatterCache.get(key)
  if (cached) return cached

  let formatter: Intl.DateTimeFormat
  if (timeZone) {
    try {
      formatter = new Intl.DateTimeFormat('en-US', { ...options, timeZone })
    } catch (error) {
      if (!warnedInvalidTimeZones.has(timeZone)) {
        warnedInvalidTimeZones.add(timeZone)
        console.warn(`use-vessel-identity: unknown timezone "${timeZone}", falling back to the browser zone`, error)
      }
      formatter = new Intl.DateTimeFormat('en-US', options)
    }
  } else {
    formatter = new Intl.DateTimeFormat('en-US', options)
  }
  formatterCache.set(key, formatter)
  return formatter
}

export function formatClock(date: Date, timeZone?: string) {
  const value = dateTimeFormat(
    {
      hour: '2-digit',
      minute: '2-digit',
      second: '2-digit',
      hour12: true,
    },
    timeZone,
  ).format(date)

  const [timePart = '--:--:--', meridiem = ''] = value.toUpperCase().split(/\s+/)
  return { timePart, meridiem }
}

export function formatDate(date: Date, options?: { compact?: boolean; timeZone?: string }) {
  return dateTimeFormat(
    {
      weekday: options?.compact ? 'short' : 'long',
      month: 'short',
      day: 'numeric',
      year: 'numeric',
    },
    options?.timeZone,
  ).format(date)
}

/** 3-letter weekday in the given zone ("Thu"), for clock-tile.tsx's ETA line: HelmCast prefixed a cross-day ETA with the weekday rather than leaving it looking like today. */
export function formatWeekday(date: Date, timeZone?: string): string {
  return dateTimeFormat({ weekday: 'short' }, timeZone).format(date)
}

/** Whether `a` and `b` fall on the same calendar date in the given zone - the test clock-tile.tsx runs to decide whether an ETA needs formatWeekday's prefix. */
export function isSameLocalDate(a: Date, b: Date, timeZone?: string): boolean {
  const key = dateTimeFormat({ year: 'numeric', month: '2-digit', day: '2-digit' }, timeZone)
  return key.format(a) === key.format(b)
}

// ---- shared store (ADR: same module-singleton shape as use-app-config.ts
// and use-telemetry-stream.ts) ----------------------------------------------
//
// Vessel identity used to be re-fetched and re-ticked independently by every
// consumer (vessel-status-bar.tsx, marine-header.tsx, clock-tile.tsx): each
// ran its own 1Hz setInterval and its own poll of /api/vessel-state and
// /api/settings. GET /api/vessel-state turned out to be entirely redundant
// with the `vessel-state` SSE event every telemetry hook already shares
// (backend/main.go's buildVesselStatePayload backs both the REST handler and
// the stream emitter, so the wire payload is identical) — so this store reads
// name/vessel_prefix/status/datetime/timezone/source off that stream instead
// of polling REST on its own timer. boat.model is the one field that only
// ever came from /api/settings; it's read through the existing
// use-app-config.ts single-flight source below rather than a separate poll.

interface VesselIdentitySnapshot {
  now: Date
  vesselStatus: string
  boatName: string | null
  signalkConnected: boolean | null
  timeZone: string | undefined
}

const INITIAL_SNAPSHOT: VesselIdentitySnapshot = {
  now: new Date(),
  vesselStatus: 'At Anchor',
  boatName: null,
  signalkConnected: null,
  timeZone: undefined,
}

let snapshot: VesselIdentitySnapshot = INITIAL_SNAPSHOT
let subscriberCount = 0
let clockTimer: ReturnType<typeof setInterval> | null = null
let unsubscribeVesselState: (() => void) | null = null
const storeListeners = new Set<() => void>()

function publish(next: VesselIdentitySnapshot): void {
  snapshot = next
  for (const listener of storeListeners) listener()
}

function tickClock(): void {
  publish({ ...snapshot, now: new Date(snapshot.now.getTime() + 1000) })
}

interface VesselStateStreamPayload {
  status?: string
  datetime?: string
  timezone?: string
  name?: string
  vessel_prefix?: string
  source?: string
}

function applyVesselStateEvent(raw: string): void {
  let data: VesselStateStreamPayload
  try {
    data = JSON.parse(raw) as VesselStateStreamPayload
  } catch (err) {
    console.error('use-vessel-identity: failed to parse vessel-state event:', err)
    return
  }

  const next: VesselIdentitySnapshot = { ...snapshot }

  if (data.status) next.vesselStatus = data.status

  if (data.name) {
    const prefix = data.vessel_prefix?.trim() ?? 'M/V'
    const vesselName = data.name.trim()
    next.boatName = vesselName ? `${prefix} ${vesselName}`.trim() : null
  }

  if (data.datetime) {
    const backendTime = new Date(data.datetime)
    if (!Number.isNaN(backendTime.getTime())) next.now = backendTime
  }

  if (data.timezone) next.timeZone = data.timezone

  // Read every tick, not just once: SignalK can drop out and come back while
  // the stream itself stays connected (the backend keeps answering, just with
  // a different `source`), and that has to keep tracking live.
  next.signalkConnected = data.source === 'signalk'

  publish(next)
}

function subscribeStore(callback: () => void): () => void {
  storeListeners.add(callback)
  subscriberCount += 1
  if (subscriberCount === 1) {
    clockTimer = setInterval(tickClock, 1000)
    unsubscribeVesselState = subscribeTelemetry('vessel-state', applyVesselStateEvent)
  }
  return () => {
    storeListeners.delete(callback)
    subscriberCount = Math.max(0, subscriberCount - 1)
    if (subscriberCount === 0) {
      if (clockTimer !== null) clearInterval(clockTimer)
      clockTimer = null
      unsubscribeVesselState?.()
      unsubscribeVesselState = null
    }
  }
}

function getSnapshot(): VesselIdentitySnapshot {
  return snapshot
}

export function useVesselIdentity() {
  const state = useSyncExternalStore(subscribeStore, getSnapshot)
  const { boatModel } = useAppConfig()

  const currentDate = useMemo(
    () => formatDate(state.now, { timeZone: state.timeZone }).toUpperCase(),
    [state.now, state.timeZone],
  )
  const clock = useMemo(() => formatClock(state.now, state.timeZone), [state.now, state.timeZone])

  return {
    now: state.now,
    currentDate,
    clock,
    vesselStatus: state.vesselStatus,
    boatName: state.boatName,
    boatModel,
    signalkConnected: state.signalkConnected,
    timeZone: state.timeZone,
  }
}
