import { useCallback, useEffect, useRef, useState } from 'react'

import { apiBaseUrl } from '@/config/api'

/**
 * A watch Mate started on live readings (ADR 0160), as the chat shows it.
 * Named "telemetry watch" in code to keep it apart from `mate-watch-store`,
 * which is the unrelated "watch for Mate's answer" toast bookkeeping.
 *
 * `subject` is the operator-words name the server built from Mate's labels
 * ("Port engine load and Starboard engine load"); paths never reach here.
 * `status` is `watching` while it samples and `reporting` while Mate is
 * being handed the report.
 */
export interface MateTelemetryWatch {
  id: string
  subject: string
  labels: string[]
  minutes: number
  startedAt: string
  endsAt: string
  status: 'watching' | 'reporting'
}

interface WatchApi {
  id: string
  conversation_id: string
  subject: string
  labels: string[]
  minutes: number
  started_at: string
  ends_at: string
  status: 'watching' | 'reporting'
}

function mapWatch(api: WatchApi): MateTelemetryWatch {
  return {
    id: api.id,
    subject: api.subject,
    labels: api.labels,
    minutes: api.minutes,
    startedAt: api.started_at,
    endsAt: api.ends_at,
    status: api.status,
  }
}

async function readError(response: Response): Promise<string> {
  try {
    const data = (await response.json()) as { error?: string }
    if (typeof data.error === 'string' && data.error !== '') return data.error
  } catch {
    // Not JSON - fall through.
  }
  return `HTTP ${response.status}`
}

/** "HH:MM" on this device's clock, for the chip's end time. */
export function formatWatchClock(iso: string): string {
  const date = new Date(iso)
  if (Number.isNaN(date.getTime())) return '--:--'
  return `${String(date.getHours()).padStart(2, '0')}:${String(date.getMinutes()).padStart(2, '0')}`
}

const DEFAULT_POLL_MS = 5000

/**
 * Follows the watch running in one conversation, if any.
 *
 * Reads `GET .../watch` when the conversation changes and whenever the
 * thread calls `refresh` (after a reply lands, since Mate may just have
 * started one), then polls every few seconds only while a watch is showing.
 * When a watch that was showing disappears on its own, `ended` goes up by
 * one: the server has by then registered Mate's follow-up turn, and the
 * thread uses that to rejoin it. A Stop, or switching conversation, clears
 * the watch without counting it; a Stop that arrives after the watch has
 * already ended (409) keeps following it instead.
 */
export function useMateTelemetryWatch(conversationId: string | null, options: { pollMs?: number } = {}) {
  const pollMs = options.pollMs ?? DEFAULT_POLL_MS
  const [watch, setWatch] = useState<MateTelemetryWatch | null>(null)
  const [ended, setEnded] = useState(0)
  const [error, setError] = useState<string | null>(null)
  // The watch last shown, and for which conversation, so a 204 can tell
  // "it ended" from "there never was one here".
  const shownRef = useRef<{ conversationId: string; watchId: string } | null>(null)
  const conversationRef = useRef(conversationId)
  conversationRef.current = conversationId

  const refresh = useCallback(async () => {
    const id = conversationRef.current
    if (id === null) return
    let response: Response
    try {
      response = await fetch(`${apiBaseUrl}/api/assistant/conversations/${encodeURIComponent(id)}/watch`, { method: 'GET' })
    } catch (err) {
      if (conversationRef.current === id) setError(err instanceof Error ? err.message : String(err))
      return
    }
    if (conversationRef.current !== id) return
    if (response.status === 204) {
      if (shownRef.current?.conversationId === id) setEnded((n) => n + 1)
      shownRef.current = null
      setWatch(null)
      return
    }
    if (!response.ok) {
      setError(await readError(response))
      return
    }
    const data = (await response.json()) as { watch: WatchApi }
    if (conversationRef.current !== id) return
    shownRef.current = { conversationId: id, watchId: data.watch.id }
    setError(null)
    setWatch(mapWatch(data.watch))
  }, [])

  useEffect(() => {
    shownRef.current = null
    setWatch(null)
    setError(null)
    if (conversationId !== null) void refresh()
  }, [conversationId, refresh])

  useEffect(() => {
    if (watch === null) return
    const timer = setInterval(() => { void refresh() }, pollMs)
    return () => clearInterval(timer)
  }, [watch, pollMs, refresh])

  const stop = useCallback(async () => {
    const id = conversationRef.current
    if (id === null) return
    try {
      const response = await fetch(`${apiBaseUrl}/api/assistant/conversations/${encodeURIComponent(id)}/watch`, { method: 'DELETE' })
      if (response.status === 409) {
        // The watch ended before the Stop arrived and Mate is already
        // reading the report: keep following it to the answer.
        await refresh()
        return
      }
      if (!response.ok) {
        setError(await readError(response))
        return
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
      return
    }
    if (conversationRef.current !== id) return
    shownRef.current = null
    setError(null)
    setWatch(null)
  }, [refresh])

  return { watch, ended, error, refresh, stop }
}
