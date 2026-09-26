import { useCallback, useEffect, useRef, useState } from 'react'
import { apiBaseUrl } from '@/config/api'

// ADR 0093: the wire shape (Conversation/Message) is snake_case, persisted
// as-is by the assistant's SQLite store. Mapped to camelCase here the same
// way use-tide-chart.ts / use-weather-forecast.ts do it, so nothing else in
// the frontend has to think about the wire format.

interface ConversationApi {
  id: string
  title: string
  created_at: string
  updated_at: string
}

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

export interface AssistantConversation {
  id: string
  title: string
  createdAt: string
  updatedAt: string
}

export interface AssistantMessageAttachment {
  documentId: string
  filename: string
}

export interface AssistantMessage {
  id: string
  conversationId: string
  seq: number
  role: 'user' | 'assistant'
  content: string
  model?: string
  promptTokens?: number
  completionTokens?: number
  costUsd?: number
  toolRounds?: number
  createdAt: string
  /** Documents (ADR 0106) attached to this message - only ever present on a
   * user message; the backend never sets it on an assistant reply. */
  attachments?: AssistantMessageAttachment[]
}

function mapConversation(api: ConversationApi): AssistantConversation {
  return { id: api.id, title: api.title, createdAt: api.created_at, updatedAt: api.updated_at }
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

export interface UseAssistantConversationsOptions {
  /** Selects this conversation on mount, when it is present in the freshly
   * fetched list (ADR 0094: "Open in Mate" hands the panel a thread the
   * sheet already has active). Falls back to the newest conversation the
   * same as ever when absent, unset, or not found in the list - a stale or
   * deleted id is never a reason to leave the panel on no thread at all. */
  initialId?: string | null
}

// A failed load surfaces `error` as the raw thrown message (an `HTTP <n>`
// string from fetchConversations/select, or whatever a network failure's
// TypeError carries) so tests and callers that want the exact wire detail
// still have it. Nothing operator-facing should ever print that string
// directly, though - this turns it into a plain sentence instead. 502/503/504
// specifically call out a dropped link, since on this boat that is what they
// almost always mean; any other HTTP status still names the code, since that
// is a real server-side detail worth keeping rather than masking; anything
// else (a network failure - "Failed to fetch", or any other TypeError) never
// got as far as a status code at all, so it reads that way instead of
// guessing at one.
export function describeLoadError(err: string): string {
  const httpMatch = /^HTTP (\d+)$/.exec(err)
  if (httpMatch) {
    const code = httpMatch[1]
    if (code === '502' || code === '503' || code === '504') {
      return `Mate's conversations could not be loaded. The server answered ${code}. This is usually the boat's link dropping for a moment.`
    }
    return `Mate's conversations could not be loaded (HTTP ${code}).`
  }
  return "Mate's conversations could not be loaded. The server did not answer."
}

export function useAssistantConversations(options?: UseAssistantConversationsOptions) {
  const initialId = options?.initialId ?? null
  // Captured once, at mount, via a lazy initializer the same way
  // App.tsx's `initialLocation` is - only the very first render's value
  // matters for the initial-selection effect below; a later change to
  // `initialId` is handled by the separate re-select effect further down,
  // not by re-running the mount fetch.
  const [mountInitialId] = useState(() => initialId)
  const [conversations, setConversations] = useState<AssistantConversation[]>([])
  const [activeId, setActiveId] = useState<string | null>(null)
  const [messages, setMessages] = useState<AssistantMessage[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  const fetchConversations = useCallback(async (): Promise<AssistantConversation[]> => {
    const response = await fetch(`${apiBaseUrl}/api/assistant/conversations`)
    if (!response.ok) throw new Error(`HTTP ${response.status}`)
    const data = (await response.json()) as { conversations?: ConversationApi[] }
    return Array.isArray(data.conversations) ? data.conversations.map(mapConversation) : []
  }, [])

  const refresh = useCallback(async () => {
    try {
      const list = await fetchConversations()
      setConversations(list)
      setError(null)
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setLoading(false)
    }
  }, [fetchConversations])

  const select = useCallback(async (id: string) => {
    setActiveId(id)
    try {
      const response = await fetch(`${apiBaseUrl}/api/assistant/conversations/${encodeURIComponent(id)}`)
      if (!response.ok) throw new Error(`HTTP ${response.status}`)
      const data = (await response.json()) as { conversation?: ConversationApi; messages?: MessageApi[] }
      setMessages(Array.isArray(data.messages) ? data.messages.map(mapMessage) : [])
      setError(null)
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
      setMessages([])
    }
  }, [])

  // Shared by the mount effect below and by `reload`: fetches the list, then
  // opens `targetId` only when it names a conversation that actually exists
  // in the freshly fetched list. Mate UI cycle ("Mate opens on an empty
  // chat"): this used to fall back to the most recently updated thread when
  // `targetId` was absent or stale, so a plain open silently resumed
  // whatever the server considered newest instead of starting blank, and a
  // link naming a since-deleted conversation landed on an unrelated one with
  // no indication anything was wrong. Neither happens any more - no match
  // means no selection, the same "fresh empty chat" state a mount with no
  // conversations at all has always shown. The operator (or an explicit
  // `initialId`/`select()`/`create()`) is the only thing that ever opens a
  // conversation now. `isCancelled` lets the mount effect's cleanup skip
  // state updates after an unmount without `reload` (which always runs to
  // completion) having to carry that same plumbing.
  const loadConversationsAndSelect = useCallback(async (targetId: string | null, isCancelled: () => boolean) => {
    try {
      const list = await fetchConversations()
      if (isCancelled()) return
      setConversations(list)
      setError(null)
      const target = targetId !== null && list.some((c) => c.id === targetId) ? targetId : null
      if (target !== null) await select(target)
    } catch (err) {
      if (!isCancelled()) setError(err instanceof Error ? err.message : String(err))
    } finally {
      if (!isCancelled()) setLoading(false)
    }
  }, [fetchConversations, select])

  // A later refresh never changes the selection; only the operator (or
  // create/remove/the re-select effect below, or an explicit `reload`) does
  // that.
  useEffect(() => {
    let cancelled = false
    void loadConversationsAndSelect(mountInitialId, () => cancelled)
    return () => {
      cancelled = true
    }
  }, [loadConversationsAndSelect, mountInitialId])

  // Repeats the initial load on demand: a failed load otherwise has no way
  // out short of remounting the whole hook. Clears the stale error and
  // shows `loading` while it runs, then re-selects `mountInitialId` only
  // when one was given and still exists - the same rule the mount effect
  // uses (loadConversationsAndSelect's own doc comment), since a caller
  // asking to reload wants the same starting point it would have gotten on
  // a fresh mount. With no `mountInitialId` this only ever repopulates
  // `conversations` - it never selects anything on its own.
  const reload = useCallback(async (): Promise<void> => {
    setError(null)
    setLoading(true)
    await loadConversationsAndSelect(mountInitialId, () => false)
  }, [loadConversationsAndSelect, mountInitialId])

  // Re-selects when `initialId` changes to a new non-null value after mount
  // (ADR 0094): "Open in Mate" can send an already-showing panel a different
  // conversation than the one it has open, and re-selecting in place is what
  // lets the caller avoid remounting the panel just to pick it up. Guarded
  // against firing on mount itself - the effect above already resolved the
  // initial selection - by comparing against the previous value rather than
  // running unconditionally whenever `initialId` is non-null.
  const previousInitialIdRef = useRef(initialId)
  useEffect(() => {
    const previous = previousInitialIdRef.current
    previousInitialIdRef.current = initialId
    if (initialId !== null && initialId !== previous) {
      void select(initialId)
    }
  }, [initialId, select])

  const create = useCallback(async (): Promise<string | null> => {
    try {
      const response = await fetch(`${apiBaseUrl}/api/assistant/conversations`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({}),
      })
      if (!response.ok) throw new Error(`HTTP ${response.status}`)
      const created = mapConversation((await response.json()) as ConversationApi)
      await refresh()
      await select(created.id)
      return created.id
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
      return null
    }
  }, [refresh, select])

  const remove = useCallback(async (id: string) => {
    try {
      const response = await fetch(`${apiBaseUrl}/api/assistant/conversations/${encodeURIComponent(id)}`, {
        method: 'DELETE',
      })
      // A 404 here means the conversation is already gone server-side, which
      // is the outcome the caller wanted anyway - not a failure to surface.
      if (!response.ok && response.status !== 404) throw new Error(`HTTP ${response.status}`)

      const list = await fetchConversations()
      setConversations(list)
      setError(null)

      if (activeId === id) {
        const next = list[0] ?? null
        if (next) {
          await select(next.id)
        } else {
          setActiveId(null)
          setMessages([])
        }
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    }
  }, [activeId, fetchConversations, select])

  // Appends a message already known to the caller - the optimistic user
  // bubble sent before the server has responded, and the final assistant
  // message once it has. Never issues a request itself.
  const appendLocal = useCallback((message: AssistantMessage) => {
    setMessages((previous) => [...previous, message])
  }, [])

  // Mate UI cycle ("Mate opens on an empty chat"): the sheet/panel's own
  // "New conversation" button used to call `create()`, which POSTs and
  // persists a brand new conversation row immediately - before the operator
  // had typed a word. Closing the sheet right after left an empty,
  // permanent row cluttering the list forever (the "empty persisted draft"
  // this cycle removes). `startNew` is the local-only equivalent: it clears
  // the active selection and the thread exactly the way a fresh mount
  // already does, with no request at all. The conversation is only ever
  // actually created, by `create()`, at the moment `chat.send()` posts the
  // first real message (AssistantThread's handleSend already does this
  // lazily) - so pressing "New conversation" and never sending anything now
  // leaves the list exactly as it was.
  const startNew = useCallback(() => {
    setActiveId(null)
    setMessages([])
    setError(null)
  }, [])

  const errorMessage = error !== null ? describeLoadError(error) : null

  return {
    conversations,
    activeId,
    messages,
    loading,
    error,
    errorMessage,
    select,
    create,
    startNew,
    remove,
    appendLocal,
    refresh,
    reload,
  }
}
