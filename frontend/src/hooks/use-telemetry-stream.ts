import { useEffect, useRef, useState } from 'react'

/**
 * Shared Server-Sent Events connection for backend-pushed telemetry (ADR 0037).
 *
 * One EventSource for the whole app, ref-counted. Browsers cap concurrent
 * connections per origin, and the backend holds a goroutine per connection, so
 * one stream per hook would spend both budgets on duplicate connections
 * carrying exactly the same events.
 *
 * Per the WHATWG EventSource spec, only a transport-level drop (the TCP
 * connection dying mid-stream) is something the browser retries on its own.
 * A response carrying a non-200 status or the wrong content-type - exactly
 * what the Vite dev proxy, and nginx/the reverse proxy in production, answer
 * with while the Go backend is restarting - is a *permanent* failure:
 * readyState goes to CLOSED and the browser never retries. Left unhandled,
 * that silently froze every telemetry-driven tile at its last value forever,
 * including the position feed the anchor drag alarm reads
 * (use-anchor-watch.ts:181) - so a dead stream quietly disabled the alarm.
 * This module owns the reconnect loop below, plus a status broadcast so the
 * UI can say so instead of rendering stale data as if it were live.
 */

type Listener = (raw: string) => void
export type TelemetryStatus = 'connected' | 'reconnecting' | 'disconnected'

// Mirrors backend/signalk_stream.go's defaultStreamMinBackoff /
// defaultStreamMaxBackoff / streamStableDuration, so the browser leg and the
// Go->SignalK leg behave consistently and neither drifts out of sync with
// the other's tuning.
const MIN_BACKOFF_MS = 1_000
const MAX_BACKOFF_MS = 30_000
const STABLE_DURATION_MS = 10_000

let source: EventSource | null = null
let subscriberCount = 0
let backoff = MIN_BACKOFF_MS
let connectedAt: number | null = null
let reconnectTimer: ReturnType<typeof setTimeout> | null = null
// Optimistic default: mirrors the null-means-"assume fine until proven
// otherwise" convention useVesselIdentity's signalkConnected already uses,
// so a fresh page load doesn't flash a warning before the first connection
// attempt has even had a chance to succeed or fail.
let status: TelemetryStatus = 'connected'

// Listener registry keyed by event name, independent of any EventSource
// instance. Subscribers register here rather than on the EventSource
// directly: previously subscribeTelemetry's unsubscribe closure captured the
// EventSource instance itself, so recreating the connection on error would
// leave every existing subscriber still attached to the dead instance. The
// registry survives reconnects; only the instance underneath it changes.
const listeners = new Map<string, Set<Listener>>()
// One fan-out function per event name, re-attached to each new EventSource
// on reconnect. Looked up from `listeners` rather than recreated, so
// `removeEventListener` on teardown targets the exact function reference
// that was added.
const dispatchers = new Map<string, (e: Event) => void>()
const statusListeners = new Set<(status: TelemetryStatus) => void>()

function dispatcherFor(event: string): (e: Event) => void {
  let dispatcher = dispatchers.get(event)
  if (!dispatcher) {
    dispatcher = (e: Event) => {
      const raw = (e as MessageEvent<string>).data
      for (const listener of listeners.get(event) ?? []) {
        listener(raw)
      }
    }
    dispatchers.set(event, dispatcher)
  }
  return dispatcher
}

function setStatus(next: TelemetryStatus): void {
  if (status === next) return
  status = next
  for (const listener of statusListeners) listener(next)
}

function clearReconnectTimer(): void {
  if (reconnectTimer !== null) {
    clearTimeout(reconnectTimer)
    reconnectTimer = null
  }
}

function attachSource(): void {
  const es = new EventSource('/api/stream')
  source = es
  connectedAt = null

  es.addEventListener('open', () => {
    if (source !== es) return // superseded by a later reconnect; ignore
    connectedAt = Date.now()
    setStatus('connected')
  })

  for (const event of listeners.keys()) {
    es.addEventListener(event, dispatcherFor(event))
  }

  es.onerror = () => {
    if (source !== es) return // stale instance, already replaced

    if (es.readyState === EventSource.CONNECTING) {
      // Transport-level drop. The browser is already retrying this exact
      // instance per spec; reconnecting by hand too would just race it. The
      // status still has to move: for however long that retry takes, every
      // tile is frozen, and a badge reading "Live" over stale telemetry is
      // the failure this whole module exists to stop. The 'open' handler
      // above puts it back once the browser's own retry lands.
      setStatus('reconnecting')
      return
    }

    // CLOSED: the spec's permanent-failure state (non-200 status or wrong
    // content-type). The browser will never retry this instance itself, so
    // it has to be discarded and a fresh one dialed by hand.
    scheduleReconnect(es)
  }
}

function scheduleReconnect(deadSource: EventSource): void {
  deadSource.close()
  source = null

  // A connection that stayed up for a while is evidence the backend is
  // healthy again, so the next drop retries promptly instead of inheriting a
  // backoff grown by an earlier, unrelated outage.
  if (connectedAt !== null && Date.now() - connectedAt >= STABLE_DURATION_MS) {
    backoff = MIN_BACKOFF_MS
  }
  connectedAt = null

  setStatus('reconnecting')

  reconnectTimer = setTimeout(() => {
    reconnectTimer = null
    if (subscriberCount <= 0) return // last subscriber unsubscribed mid-backoff
    attachSource()
  }, backoff)

  backoff = Math.min(backoff * 2, MAX_BACKOFF_MS)
}

function acquire(): void {
  subscriberCount += 1
  if (!source && reconnectTimer === null) {
    backoff = MIN_BACKOFF_MS
    attachSource()
  }
}

function release(): void {
  subscriberCount = Math.max(0, subscriberCount - 1)
  if (subscriberCount === 0) {
    clearReconnectTimer()
    source?.close()
    source = null
    backoff = MIN_BACKOFF_MS
    connectedAt = null
    setStatus('disconnected')
  }
}

/** Subscribes to one named event. Returns an unsubscribe function. */
export function subscribeTelemetry(event: string, onData: Listener): () => void {
  acquire()

  let eventListeners = listeners.get(event)
  if (!eventListeners) {
    eventListeners = new Set()
    listeners.set(event, eventListeners)
  }
  eventListeners.add(onData)
  // Idempotent: if a connection is already live, wire this event's fan-out
  // onto it immediately rather than waiting for the next reconnect.
  source?.addEventListener(event, dispatcherFor(event))

  return () => {
    eventListeners.delete(onData)
    if (eventListeners.size === 0) {
      const dispatcher = dispatchers.get(event)
      if (dispatcher) source?.removeEventListener(event, dispatcher)
      listeners.delete(event)
      dispatchers.delete(event)
    }
    release()
  }
}

/**
 * Subscribes to shared connection status changes. Calls `onStatus`
 * immediately with the current value, then again on every change.
 */
export function subscribeTelemetryStatus(onStatus: (status: TelemetryStatus) => void): () => void {
  statusListeners.add(onStatus)
  onStatus(status)
  return () => { statusListeners.delete(onStatus) }
}

/**
 * Subscribes a component to one named telemetry event.
 *
 * No retry loop here: reconnection is owned centrally by this module (see
 * subscribeTelemetry above) and shared across every telemetry hook, so a
 * dead stream is recovered - and surfaced via useTelemetryStatus - once for
 * all five consumers rather than five times over.
 */
export function useTelemetryEvent<T>(event: string, onData: (data: T) => void): void {
  // Latest-ref idiom, matching useFetch: callers pass inline closures, and
  // re-subscribing on every render would tear down the shared connection.
  const handlerRef = useRef(onData)
  useEffect(() => { handlerRef.current = onData })

  useEffect(() => {
    return subscribeTelemetry(event, (raw) => {
      try {
        handlerRef.current(JSON.parse(raw) as T)
      } catch (err) {
        console.error(`Failed to parse ${event} event:`, err)
      }
    })
  }, [event])
}

/** The shared telemetry connection's current state, for status indicators in the UI. */
export function useTelemetryStatus(): TelemetryStatus {
  const [value, setValue] = useState<TelemetryStatus>(status)
  useEffect(() => subscribeTelemetryStatus(setValue), [])
  return value
}
