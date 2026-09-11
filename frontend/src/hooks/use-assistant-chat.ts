import { useCallback, useEffect, useRef, useState } from 'react'
import { apiBaseUrl } from '@/config/api'
import { readServerSentEvents } from '@/lib/sse-reader'
import type { AssistantConversation, AssistantMessage } from '@/hooks/use-assistant-conversations'

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
  }
}

function mapConversation(api: ConversationApi): AssistantConversation {
  return { id: api.id, title: api.title, createdAt: api.created_at, updatedAt: api.updated_at }
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
 * Posts one message to a conversation and reads the reply back as SSE
 * (ADR 0093): zero or more `status` frames while the assistant works,
 * then exactly one `message` or `error` frame. A second `send` cancels
 * whatever the previous one was still doing, the same way navigating away
 * mid-request should - only the most recent question's outcome ever
 * reaches the caller.
 */
export function useAssistantChat() {
  const [sending, setSending] = useState(false)
  const [statusText, setStatusText] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)
  const abortRef = useRef<AbortController | null>(null)

  const abort = useCallback(() => {
    abortRef.current?.abort()
    abortRef.current = null
    setSending(false)
    setStatusText(null)
  }, [])

  // Unmount cleanup only cancels the request - it must not call any setState
  // on an unmounted component.
  useEffect(() => () => { abortRef.current?.abort() }, [])

  const send = useCallback(async (
    conversationId: string,
    content: string,
    onConversation?: (conversation: AssistantConversation) => void,
  ): Promise<AssistantMessage | null> => {
    // A send already in flight loses: its events are ignored below and its
    // request is cancelled, so only this call's outcome updates state.
    abortRef.current?.abort()
    const controller = new AbortController()
    abortRef.current = controller
    const isCurrent = () => abortRef.current === controller

    setSending(true)
    setError(null)
    setStatusText(null)

    try {
      const response = await fetch(
        `${apiBaseUrl}/api/assistant/conversations/${encodeURIComponent(conversationId)}/messages`,
        {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ content }),
          signal: controller.signal,
        },
      )

      if (!response.ok) {
        const message = await readErrorMessage(response)
        if (isCurrent()) setError(message)
        return null
      }

      if (response.body === null) {
        if (isCurrent()) setError('streaming not supported')
        return null
      }

      let resolved: AssistantMessage | null = null
      await readServerSentEvents(response.body, (event) => {
        if (!isCurrent()) return

        if (event.event === 'status') {
          const data = JSON.parse(event.data) as { text?: string }
          setStatusText(typeof data.text === 'string' ? data.text : null)
        } else if (event.event === 'message') {
          const data = JSON.parse(event.data) as { message: MessageApi; conversation: ConversationApi }
          resolved = mapMessage(data.message)
          onConversation?.(mapConversation(data.conversation))
        } else if (event.event === 'error') {
          const data = JSON.parse(event.data) as { error?: string }
          setError(typeof data.error === 'string' && data.error !== '' ? data.error : 'assistant error')
        }
      }, controller.signal)

      return resolved
    } catch (err) {
      if (controller.signal.aborted) return null
      if (isCurrent()) setError(err instanceof Error ? err.message : String(err))
      return null
    } finally {
      if (isCurrent()) {
        setSending(false)
        setStatusText(null)
        abortRef.current = null
      }
    }
  }, [])

  return { send, sending, statusText, error, abort }
}
