import { useEffect, useRef } from 'react'

/**
 * Shared Server-Sent Events connection for backend-pushed telemetry (ADR 0037).
 *
 * One EventSource for the whole app, ref-counted. Browsers cap concurrent
 * connections per origin, and the backend holds a goroutine per connection, so
 * one stream per hook would spend both budgets on duplicate connections
 * carrying exactly the same events.
 */

let source: EventSource | null = null
let subscribers = 0

function acquire(): EventSource {
  if (!source) {
    source = new EventSource('/api/stream')
  }
  subscribers += 1
  return source
}

function release(): void {
  subscribers -= 1
  if (subscribers <= 0) {
    source?.close()
    source = null
    subscribers = 0
  }
}

/** Subscribes to one named event. Returns an unsubscribe function. */
export function subscribeTelemetry(event: string, onData: (raw: string) => void): () => void {
  const stream = acquire()
  const listener = (e: Event) => { onData((e as MessageEvent<string>).data) }

  stream.addEventListener(event, listener)

  return () => {
    stream.removeEventListener(event, listener)
    release()
  }
}

/**
 * Subscribes a component to one named telemetry event.
 *
 * No error state: EventSource reconnects on its own, and surfacing every
 * transient disconnect would flicker the UI. Upstream health is reported
 * honestly by the `source` field the vessel-state payload already carries.
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
