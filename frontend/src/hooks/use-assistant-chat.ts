import { useCallback, useEffect, useRef, useState } from 'react'
import { apiBaseUrl } from '@/config/api'
import { readServerSentEvents } from '@/lib/sse-reader'
import { registerMateWatch, removeMateWatch } from '@/lib/mate-watch-store'
import type { AssistantConversation, AssistantMessage } from '@/hooks/use-assistant-conversations'

interface MessageAttachmentApi {
  document_id: string
  filename: string
}

interface MessageApi {
  id: string
  conversation_id: string
  seq: number
  role: 'user' | 'assistant'
  content: string
  model?: string
  prompt_tokens?: number
  completion_tokens?: number
  cost_usd?: number
  tool_rounds?: number
  created_at: string
  attachments?: MessageAttachmentApi[]
}

interface ConversationApi {
  id: string
  title: string
  created_at: string
  updated_at: string
}

function mapMessage(api: MessageApi): AssistantMessage {
  return {
    id: api.id,
    conversationId: api.conversation_id,
    seq: api.seq,
    role: api.role,
    content: api.content,
    model: api.model,
    promptTokens: api.prompt_tokens,
    completionTokens: api.completion_tokens,
    costUsd: api.cost_usd,
    toolRounds: api.tool_rounds,
    createdAt: api.created_at,
    attachments: api.attachments?.map((a) => ({ documentId: a.document_id, filename: a.filename })),
  }
}

function mapConversation(api: ConversationApi): AssistantConversation {
  return { id: api.id, title: api.title, createdAt: api.created_at, updatedAt: api.updated_at }
}

// ADR 0093 voice phase: the screen the operator is looking at when they ask
// Mate a question, passed through untouched to the backend so a reply can
// open with "The operator is looking at the Forecast panel" instead of the
// model guessing. `lib/mate-screen.ts` is what builds one of these from the
// shell's own state; this hook only needs the shape.
export interface AssistantScreenContext {
  panel?: string
  section?: string
  page?: string
}

export interface AssistantSendOptions {
  /** Voice phase: true when the question came in by speech, so the reply's
   * markdown ends with a `## Spoken summary` section the frontend can read
   * aloud. Omitted (not just false) when the send didn't come from voice. */
  spoken?: boolean
  screen?: AssistantScreenContext
  /** Document ids (ADR 0106) staged in the composer, at most 10 - see
   * use-document-uploads.ts. Omitted (not sent as `[]`) when nothing is
   * attached, matching how `spoken`/`screen` are only ever included when
   * the caller actually passed them. */
  attachments?: string[]
  onConversation?: (conversation: AssistantConversation) => void
}

// A non-2xx response carries a JSON `{error}` body per the API contract, but
// a 503 from a proxy in front of the backend (or any other layer that never
// reaches the handler) won't - read it defensively rather than letting a
// malformed body throw past the caller.
async function readErrorMessage(response: Response): Promise<string> {
  try {
    const data = (await response.json()) as { error?: string }
    if (typeof data.error === 'string' && data.error !== '') return data.error
  } catch {
    // Not JSON - fall through to the generic message below.
  }
  return `HTTP ${response.status}`
}

/**
 * Drives one Mate conversation's live reply, in flight or rejoined
 * (ADR 0093, streamed per ADR 0105, detached per ADR 0105's "the answer
 * outlives the page").
 *
 * `send` posts one message and reads the reply back as SSE: zero or more
 * `status` frames while the assistant works, zero or more `delta` frames
 * carrying the current round's answer text as it is written, an optional
 * `retract` when a round that streamed text turned into tool calls
 * instead, then exactly one `message` or `error` frame. `attach` rejoins
 * whatever reply is already being written server-side - after a navigation
 * away and back, a reload, or an iPad's screen lock having killed the
 * original `send`'s fetch outright - by reading the same event shape off
 * `GET .../run` instead of posting a new question.
 *
 * A second call to either `send` or `attach` closes whatever local stream
 * the previous one was reading (its events are ignored from that point on,
 * matching the existing "second send supersedes the first" behaviour), but
 * - unlike before ADR 0105 - this never stops the run itself: the backend
 * no longer ties a reply's lifetime to the HTTP request that started it, so
 * closing this tab's connection just means this tab stops watching. Only
 * `abort()` (the Stop button) actually tells the server to give up, via
 * `POST .../run/cancel`.
 */
export function useAssistantChat() {
  const [sending, setSending] = useState(false)
  const [statusText, setStatusText] = useState<string | null>(null)
  const [draft, setDraft] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)
  const abortRef = useRef<AbortController | null>(null)
  // The conversation the current (or most recent) stream belongs to -
  // abort() needs this to know which run to POST .../run/cancel for.
  const currentConversationIdRef = useRef<string | null>(null)
  // ADR 0105: delta text accumulates here, not directly in state, so several
  // deltas arriving in one animation frame cost one re-render (and one
  // markdown re-parse) instead of one each.
  const draftBufferRef = useRef('')
  const draftRafRef = useRef<number | null>(null)

  const cancelDraftFlush = useCallback(() => {
    if (draftRafRef.current !== null) {
      cancelAnimationFrame(draftRafRef.current)
      draftRafRef.current = null
    }
  }, [])

  // Discards whatever draft text has been shown (or buffered) so far: used
  // when a round is retracted, when the authoritative message arrives, and
  // on error/abort/a superseding send or attach - every case where the
  // streamed draft must not linger.
  const clearDraft = useCallback(() => {
    cancelDraftFlush()
    draftBufferRef.current = ''
    setDraft(null)
  }, [cancelDraftFlush])

  // Schedules (at most once at a time) copying the buffer into `draft`
  // state on the next animation frame, so a burst of deltas within one
  // frame flushes once rather than once per token.
  const scheduleDraftFlush = useCallback(() => {
    if (draftRafRef.current !== null) return
    draftRafRef.current = requestAnimationFrame(() => {
      draftRafRef.current = null
      setDraft(draftBufferRef.current)
    })
  }, [])

  // Reads one SSE body - a send()'s POST response or an attach()'s GET
  // response, the two only differ in how `body` was obtained - through the
  // same status/delta/retract/message/error handling. `controller` is the
  // AbortController this particular call owns; events are ignored once a
  // later send()/attach() has superseded it (abortRef.current !== controller).
  const consumeStream = useCallback(async (
    body: ReadableStream<Uint8Array>,
    controller: AbortController,
    onConversation?: (conversation: AssistantConversation) => void,
  ): Promise<AssistantMessage | null> => {
    const isCurrent = () => abortRef.current === controller
    let resolved: AssistantMessage | null = null

    await readServerSentEvents(body, (event) => {
      if (!isCurrent()) return

      if (event.event === 'status') {
        const data = JSON.parse(event.data) as { text?: string }
        setStatusText(typeof data.text === 'string' ? data.text : null)
      } else if (event.event === 'delta') {
        const data = JSON.parse(event.data) as { text?: string }
        if (typeof data.text === 'string' && data.text !== '') {
          draftBufferRef.current += data.text
          scheduleDraftFlush()
        }
      } else if (event.event === 'retract') {
        // The round that produced this draft turned into tool calls
        // instead of an answer (ADR 0093 §2/ADR 0105) - discard it.
        clearDraft()
      } else if (event.event === 'message') {
        const data = JSON.parse(event.data) as { message: MessageApi; conversation: ConversationApi }
        resolved = mapMessage(data.message)
        clearDraft()
        onConversation?.(mapConversation(data.conversation))
      } else if (event.event === 'error') {
        const data = JSON.parse(event.data) as { error?: string }
        setError(typeof data.error === 'string' && data.error !== '' ? data.error : 'assistant error')
        clearDraft()
      }
    }, controller.signal)

    return resolved
  }, [clearDraft, scheduleDraftFlush])

  // The Stop button (ADR 0105): unlike a superseding send()/attach() or an
  // unmount, this actually tells the server to give up on the run - POSTing
  // .../run/cancel. The local stream, status and draft go at once so Stop
  // feels immediate, but `sending` (which locks the composer) holds until
  // the cancel has answered: the server answers only once the conversation
  // is free, and a question sent before then would be refused with 409.
  const abort = useCallback(async () => {
    const conversationId = currentConversationIdRef.current
    abortRef.current?.abort()
    abortRef.current = null
    setStatusText(null)
    clearDraft()
    if (conversationId === null) {
      setSending(false)
      return
    }
    try {
      const response = await fetch(
        `${apiBaseUrl}/api/assistant/conversations/${encodeURIComponent(conversationId)}/run/cancel`,
        { method: 'POST' },
      )
      if (!response.ok) setError(await readErrorMessage(response))
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      // A send()/attach() started while the cancel was in flight owns
      // `sending` now; leave it alone.
      if (abortRef.current === null) setSending(false)
    }
  }, [clearDraft])

  // Whether this hook's live local stream belongs to conversationId. The
  // thread uses it to tell "rejoin a different conversation" apart from
  // "this conversation's own send is already streaming".
  const isStreamingConversation = useCallback(
    (conversationId: string) => abortRef.current !== null && currentConversationIdRef.current === conversationId,
    [],
  )

  // Unmount cleanup only closes this tab's own stream and cancels any
  // pending draft flush - it must never cancel the run itself (ADR 0105:
  // the reply keeps being written server-side, and a later attach() picks
  // it back up), and it must not call any setState on an unmounted
  // component.
  useEffect(() => () => {
    abortRef.current?.abort()
    cancelDraftFlush()
  }, [cancelDraftFlush])

  const send = useCallback(async (
    conversationId: string,
    content: string,
    options?: AssistantSendOptions,
  ): Promise<AssistantMessage | null> => {
    // A send already in flight loses: its events are ignored below and its
    // local stream is closed, the same as a superseding attach() - but,
    // same as that case, this never stops whatever run it was watching
    // (ADR 0105).
    abortRef.current?.abort()
    const controller = new AbortController()
    abortRef.current = controller
    currentConversationIdRef.current = conversationId
    const isCurrent = () => abortRef.current === controller

    setSending(true)
    setError(null)
    setStatusText(null)
    clearDraft()

    // mate-answer-toast plan: registered here, before the fetch below is
    // even issued, rather than after the response comes back ok. An
    // interrupted send - the component unmounts, or a newer send
    // supersedes this one via the abortRef.current?.abort() above - can
    // still have reached the backend and started a run: ADR 0105 means the
    // run outlives the request that started it, so a question this tab
    // never saw answered for may nonetheless finish and deserve a "Mate
    // answered" toast (App.tsx's use-mate-answer-watcher reads this store
    // for whatever conversation isn't on screen). Registering early makes
    // sure that case is covered. The paths below that learn no run started
    // here - a rejected send (e.g. 409, another tab's run for this
    // conversation is still going), a body-less response, and a fetch that
    // failed outright rather than being aborted - remove the watch again,
    // since this tab has no reason to wait on an answer that will never
    // come. attach() deliberately never registers at all: rejoining a run
    // someone else started isn't "this tab asked a question".
    registerMateWatch(conversationId)

    try {
      // `spoken`/`screen` are included only when the caller actually passed
      // them - an ordinary panel send (no options at all) posts the same
      // `{ content }` body it always has, so the backend only sees a voice
      // question or a screen context when one genuinely applies.
      const body: { content: string; spoken?: boolean; screen?: AssistantScreenContext; attachments?: string[] } = { content }
      if (options?.spoken !== undefined) body.spoken = options.spoken
      if (options?.screen !== undefined) body.screen = options.screen
      if (options?.attachments !== undefined) body.attachments = options.attachments

      const response = await fetch(
        `${apiBaseUrl}/api/assistant/conversations/${encodeURIComponent(conversationId)}/messages`,
        {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify(body),
          signal: controller.signal,
        },
      )

      if (!response.ok) {
        const message = await readErrorMessage(response)
        removeMateWatch(conversationId)
        if (isCurrent()) setError(message)
        return null
      }

      if (response.body === null) {
        removeMateWatch(conversationId)
        if (isCurrent()) setError('streaming not supported')
        return null
      }

      return await consumeStream(response.body, controller, options?.onConversation)
    } catch (err) {
      if (controller.signal.aborted) return null
      removeMateWatch(conversationId)
      if (isCurrent()) {
        setError(err instanceof Error ? err.message : String(err))
        clearDraft()
      }
      return null
    } finally {
      if (isCurrent()) {
        setSending(false)
        setStatusText(null)
        abortRef.current = null
      }
    }
  }, [clearDraft, consumeStream])

  // Rejoins whatever reply is already being written for conversationId
  // (ADR 0105): GETs .../run, which answers 204 when nothing is in flight
  // (this resolves to null and touches no state at all - the caller already
  // has the finished answer, if any, from GET the conversation itself) or
  // streams the same status/delta/retract/message/error shape send() reads,
  // starting with whatever the run has already produced and continuing live
  // from there. A second attach()/send() supersedes this one exactly the
  // same way a second send() supersedes an earlier one.
  const attach = useCallback(async (
    conversationId: string,
    onConversation?: (conversation: AssistantConversation) => void,
  ): Promise<AssistantMessage | null> => {
    abortRef.current?.abort()
    const controller = new AbortController()
    abortRef.current = controller
    currentConversationIdRef.current = conversationId
    const isCurrent = () => abortRef.current === controller
    // Whatever stream this supersedes may belong to another conversation:
    // its status, draft and error must not carry over to this one.
    setSending(false)
    setStatusText(null)
    setError(null)
    clearDraft()

    try {
      const response = await fetch(
        `${apiBaseUrl}/api/assistant/conversations/${encodeURIComponent(conversationId)}/run`,
        { signal: controller.signal },
      )

      if (response.status === 204) {
        return null
      }

      if (!response.ok) {
        if (isCurrent()) setError(await readErrorMessage(response))
        return null
      }

      if (response.body === null) {
        if (isCurrent()) setError('streaming not supported')
        return null
      }

      setSending(true)
      setError(null)
      setStatusText(null)
      clearDraft()

      return await consumeStream(response.body, controller, onConversation)
    } catch (err) {
      if (controller.signal.aborted) return null
      if (isCurrent()) {
        setError(err instanceof Error ? err.message : String(err))
        clearDraft()
      }
      return null
    } finally {
      if (isCurrent()) {
        setSending(false)
        setStatusText(null)
        abortRef.current = null
      }
    }
  }, [clearDraft, consumeStream])

  return { send, sending, statusText, draft, error, abort, attach, isStreamingConversation }
}
