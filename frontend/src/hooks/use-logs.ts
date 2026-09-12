import { useEffect, useRef, useState } from 'react'

export interface LogEntry {
  id: number
  timestamp: string
  message: string
}

export function useLogs() {
  const [logs, setLogs] = useState<LogEntry[]>([])
  const [isLive, setIsLive] = useState(true)
  const [connected, setConnected] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const eventSourceRef = useRef<EventSource | null>(null)

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
          setLogs(Array.isArray(payload) ? payload : [])
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
        setLogs((previous) => {
          if (!entry?.id || previous.some((current) => current.id === entry.id)) {
            return previous
          }
          return [...previous, entry]
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
