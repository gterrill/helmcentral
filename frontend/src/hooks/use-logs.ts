import { useEffect, useRef, useState } from 'react'

export interface LogEntry {
  id: number
  timestamp: string
  message: string
}

/**
 * F-2 (security audit, phase 1): caps how many live log entries the panel
 * keeps in memory. A Settings -> Logs panel left open on the helm tablet with
 * live update on used to accumulate every line for the lifetime of the
 * mount — a chatty backend (SSE reconnect storms on a flaky boat LAN) made
 * that a steady leak, and the dedup check below was an O(n) scan of the
 * whole array per arriving entry on top of it.
 *
 * Mirrors defaultLogBufferCapacity in backend/log_buffer.go rather than
 * inventing a new number: that is the server's own log ring buffer feeding
 * both GET /api/logs and the SSE stream, so it is also the most this hook
 * could ever usefully hold onto.
 */
export const LOG_BUFFER_CAPACITY = 2000

export function useLogs() {
  const [logs, setLogs] = useState<LogEntry[]>([])
  const [isLive, setIsLive] = useState(true)
  const [connected, setConnected] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const eventSourceRef = useRef<EventSource | null>(null)
  // Mirrors the ids currently held in `logs`, so the dedup check below is an
  // O(1) Set lookup instead of an O(n) scan of the array on every arriving
  // entry. Kept in a ref (not derived from `logs` on each render) because it
  // must be mutated in the same tick an entry is accepted, before the next
  // SSE message can be dispatched.
  const seenLogIdsRef = useRef<Set<number>>(new Set())

  useEffect(() => {
    let cancelled = false

    async function loadInitialLogs() {
      try {
        const response = await fetch('/api/logs')
        if (!response.ok) {
          throw new Error(`Request failed with status ${response.status}`)
        }

        const payload = (await response.json()) as LogEntry[]
        if (!cancelled) {
          const entries = Array.isArray(payload) ? payload : []
          // Defensive, not a masking fallback: GET /api/logs is already
          // bounded by the server's own ring buffer (defaultLogBufferCapacity
          // in backend/log_buffer.go, the same number LOG_BUFFER_CAPACITY
          // mirrors), so this slice is normally a no-op. It keeps the
          // invariant — `logs` never exceeds the cap — true unconditionally
          // rather than true "as long as the backend agrees", which matters
          // because seenLogIdsRef below is reseeded from exactly this array.
          const capped = entries.length > LOG_BUFFER_CAPACITY
            ? entries.slice(entries.length - LOG_BUFFER_CAPACITY)
            : entries
          setLogs(capped)
          seenLogIdsRef.current = new Set(capped.map((entry) => entry.id))
          setConnected(true)
          setError(null)
        }
      } catch (err) {
        if (!cancelled) {
          setError(err instanceof Error ? err.message : 'Unable to load logs')
          setConnected(false)
        }
      }
    }

    if (isLive) {
      void loadInitialLogs()
    }

    return () => {
      cancelled = true
    }
  }, [isLive])

  useEffect(() => {
    if (!isLive) {
      eventSourceRef.current?.close()
      eventSourceRef.current = null
      setConnected(false)
      return
    }

    const source = new EventSource('/api/logs/stream')
    eventSourceRef.current = source

    const handleOpen = () => {
      setConnected(true)
      setError(null)
    }

    const handleMessage = (event: MessageEvent<string>) => {
      try {
        const entry = JSON.parse(event.data) as LogEntry
        if (!entry?.id || seenLogIdsRef.current.has(entry.id)) return

        // Mutated here, once per real message, rather than inside the
        // setLogs updater below: React (in development Strict Mode) may
        // invoke a functional state updater twice to check it stays pure, and
        // this Set is not the kind of state that update is meant to guard —
        // there is nothing to derive it from except itself.
        seenLogIdsRef.current.add(entry.id)

        setLogs((previous) => {
          if (previous.length < LOG_BUFFER_CAPACITY) return [...previous, entry]
          // At capacity: drop the oldest entry to make room, and forget its
          // id too, or seenLogIdsRef would grow without bound even though
          // `logs` itself no longer does.
          const [dropped, ...rest] = previous
          seenLogIdsRef.current.delete(dropped.id)
          return [...rest, entry]
        })
      } catch {
        /* ignore malformed log payloads */
      }
    }

    source.addEventListener('open', handleOpen)
    source.addEventListener('log', handleMessage)
    source.onerror = () => {
      setConnected(false)
    }

    return () => {
      source.removeEventListener('open', handleOpen)
      source.removeEventListener('log', handleMessage)
      source.close()
      eventSourceRef.current = null
    }
  }, [isLive])

  const clearLogs = () => {
    setLogs([])
    seenLogIdsRef.current = new Set()
  }

  return {
    logs,
    isLive,
    setIsLive,
    clearLogs,
    connected,
    error,
  }
}
