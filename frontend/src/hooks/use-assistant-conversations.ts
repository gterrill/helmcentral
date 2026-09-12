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
}

export interface AssistantConversation {
  id: string
  title: string
  createdAt: string
  updatedAt: string
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

  // Initial load opens `mountInitialId` when it names a conversation that
  // actually exists in the freshly fetched list, otherwise the most recently
  // updated thread rather than an empty pane: the panel is usually reopened
  // to reread a plan, and the list is already sorted newest first by the
  // server. A later refresh never changes the selection; only the operator
  // (or create/remove/the re-select effect below) does that.
  useEffect(() => {
    let cancelled = false
    void (async () => {
      try {
        const list = await fetchConversations()
        if (cancelled) return
        setConversations(list)
        setError(null)
        const target = mountInitialId !== null && list.some((c) => c.id === mountInitialId)
          ? mountInitialId
          : list[0]?.id ?? null
        if (target !== null) await select(target)
      } catch (err) {
        if (!cancelled) setError(err instanceof Error ? err.message : String(err))
      } finally {
        if (!cancelled) setLoading(false)
      }
    })()
    return () => {
      cancelled = true
    }
  }, [fetchConversations, select, mountInitialId])

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

  return { conversations, activeId, messages, loading, error, select, create, remove, appendLocal, refresh }
}
