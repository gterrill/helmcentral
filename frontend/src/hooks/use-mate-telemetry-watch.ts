import { useCallback, useEffect, useRef, useState } from 'react'

import { apiBaseUrl } from '@/config/api'

/**
 * A watch Mate started on live readings (ADR 0160), as the chat shows it.
 * Named "telemetry watch" in code to keep it apart from `mate-watch-store`,
 * which is the unrelated "watch for Mate's answer" toast bookkeeping.
 *
 * `subject` is the operator-words name the server built from Mate's labels
 * ("Port engine load and Starboard engine load"); paths never reach here.
 * `endsAtLocal` is the end time as Mate states it, "HH:MM" on the boat's
 * clock, formatted by the server. `status` is `watching` while it samples
 * and `reporting` while Mate is being handed the report. A `finished` watch
 * is never shown; it only tells the hook that Mate's follow-up has started.
 */
export interface MateTelemetryWatch {
  id: string
  subject: string
  labels: string[]
  minutes: number
  startedAt: string
  endsAt: string
  endsAtLocal: string
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
  ends_at_local: string
  status: 'watching' | 'reporting' | 'finished'
}

function mapWatch(api: WatchApi & { status: 'watching' | 'reporting' }): MateTelemetryWatch {
  return {
    id: api.id,
    subject: api.subject,
    labels: api.labels,
    minutes: api.minutes,
    startedAt: api.started_at,
    endsAt: api.ends_at,
    endsAtLocal: api.ends_at_local,
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

const DEFAULT_POLL_MS = 5000

/**
 * Follows the watch running in one conversation, if any.
 *
 * Reads `GET .../watch` when the conversation changes and whenever the
 * thread calls `refresh` (after a reply lands, since Mate may just have
 * started one), then polls every few seconds only while a watch is showing.
 *
 * `ended` goes up by one each time a watch ends on its own, which is the
 * thread's cue to rejoin Mate's follow-up turn: when the server answers
 * `finished` for a watch not yet counted (including one that started and
 * ended between two looks and was never shown), when a shown watch
 * disappears (204), or when Stop finds nothing left to stop (404). A
 * `finished` watch found on the first look at a conversation is not
 * counted: opening the conversation already rejoins any reply in progress.
 * A Stop that stops the watch (204), or switching conversation, clears it
 * without counting; a Stop refused because the watch is handing over its
 * report (409) keeps following it instead.
 */
export function useMateTelemetryWatch(conversationId: string | null, options: { pollMs?: number } = {}) {
  const pollMs = options.pollMs ?? DEFAULT_POLL_MS
  const [watch, setWatch] = useState<MateTelemetryWatch | null>(null)
  const [ended, setEnded] = useState(0)
  const [error, setError] = useState<string | null>(null)
  // The watch last shown, and for which conversation, so a 204 can tell
  // "it ended" from "there never was one here".
  const shownRef = useRef<{ conversationId: string; watchId: string } | null>(null)
  // The last watch counted as ended (or seen finished on first look), so a
  // finished watch rejoins Mate once however often it is read.
  const countedRef = useRef<{ conversationId: string; watchId: string } | null>(null)
  // True until the first answer for the current conversation arrives.
  const firstLookRef = useRef(true)
  const conversationRef = useRef(conversationId)
  conversationRef.current = conversationId

  const countEnded = useCallback((id: string, watchId: string | null) => {
    const counted = countedRef.current
    if (watchId !== null && counted?.conversationId === id && counted.watchId === watchId) return
    if (watchId !== null) countedRef.current = { conversationId: id, watchId }
    setEnded((n) => n + 1)
  }, [])

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
      firstLookRef.current = false
      const shown = shownRef.current
      if (shown?.conversationId === id) countEnded(id, shown.watchId)
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
    const firstLook = firstLookRef.current
    firstLookRef.current = false
    setError(null)
    if (data.watch.status === 'finished') {
      if (firstLook) {
        countedRef.current = { conversationId: id, watchId: data.watch.id }
      } else {
        countEnded(id, data.watch.id)
      }
      shownRef.current = null
      setWatch(null)
      return
    }
    shownRef.current = { conversationId: id, watchId: data.watch.id }
    setWatch(mapWatch({ ...data.watch, status: data.watch.status }))
  }, [countEnded])

  useEffect(() => {
    shownRef.current = null
    countedRef.current = null
    firstLookRef.current = true
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
      if (response.status === 404) {
        // Nothing left to stop: the watch had already finished and Mate's
        // follow-up is under way (or done). Treat it as a natural end.
        if (conversationRef.current !== id) return
        const shown = shownRef.current
        shownRef.current = null
        setError(null)
        setWatch(null)
        if (shown?.conversationId === id) countEnded(id, shown.watchId)
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
  }, [refresh, countEnded])

  return { watch, ended, error, refresh, stop }
}
