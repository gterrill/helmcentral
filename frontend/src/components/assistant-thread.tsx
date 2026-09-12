import { Loader2 } from 'lucide-react'
import { useCallback, useState, type KeyboardEvent, type Ref } from 'react'

import { AssistantMarkdown } from '@/components/assistant-markdown'
import { Button } from '@/components/ui/button'
import { Textarea } from '@/components/ui/textarea'
import type { useAssistantChat } from '@/hooks/use-assistant-chat'
import type { AssistantMessage, useAssistantConversations } from '@/hooks/use-assistant-conversations'

const EXAMPLE_QUESTION =
  "We're at Hook Reef. Should we visit Tongue Bay or Blue Pearl Bay first over the next two days?"

// ADR 0093: every reply's footer names what it cost, in full - which model
// answered, how many tokens it used, and the price - so the running cost of
// asking questions is never a surprise. Any part the server didn't report
// (an optimistic local bubble has none of these yet) reads as `--` rather
// than a confident-looking zero.
function formatMessageFooter(message: AssistantMessage): string {
  const model = message.model && message.model !== '' ? message.model : '--'
  const tokens =
    typeof message.promptTokens === 'number' && typeof message.completionTokens === 'number'
      ? (message.promptTokens + message.completionTokens).toLocaleString()
      : '--'
  const cost = typeof message.costUsd === 'number' ? `$${message.costUsd.toFixed(4)}` : '--'
  return `${model} · ${tokens} tokens · ${cost}`
}

interface AssistantThreadProps {
  canWrite: boolean
  conversations: ReturnType<typeof useAssistantConversations>
  chat: ReturnType<typeof useAssistantChat>
  /** Focuses the composer as soon as it mounts - the Mate sheet (ADR 0093
   * voice phase) wants this when it opens with no question already in
   * flight; the panel never sets it, matching its pre-extraction behaviour. */
  autoFocus?: boolean
  /** Forwarded straight to the composer's textarea (ADR 0094): the sheet's
   * "New conversation" button focuses it directly after `create()` resolves,
   * which `autoFocus` alone can't do since that only ever fires on mount. */
  composerRef?: Ref<HTMLTextAreaElement>
}

/**
 * The message list, footer, status/error rows and composer for one Mate
 * conversation (ADR 0093). Extracted from AssistantDrawer so both the full
 * panel (which also owns a conversation-list column) and the quick Mate
 * sheet can host the same thread over whatever page is behind it - neither
 * owns any data itself, both hand it a conversations/chat pair.
 */
export function AssistantThread({ canWrite, conversations, chat, autoFocus, composerRef }: AssistantThreadProps) {
  const [content, setContent] = useState('')

  const handleSend = useCallback(async () => {
    const trimmed = content.trim()
    if (trimmed === '' || chat.sending || !canWrite) return

    let conversationId = conversations.activeId
    if (conversationId === null) {
      conversationId = await conversations.create()
      if (conversationId === null) return
    }

    conversations.appendLocal({
      id: `local-${Date.now()}`,
      conversationId,
      seq: -1,
      role: 'user',
      content: trimmed,
      createdAt: new Date().toISOString(),
    })
    setContent('')

    const reply = await chat.send(conversationId, trimmed)
    if (reply) {
      conversations.appendLocal(reply)
      await conversations.refresh()
    }
  }, [content, chat, conversations, canWrite])

  const handleKeyDown = (event: KeyboardEvent<HTMLTextAreaElement>) => {
    if (event.key === 'Enter' && !event.shiftKey) {
      event.preventDefault()
      void handleSend()
    }
  }

  return (
    <div className="mx-auto flex min-h-0 w-full max-w-3xl min-w-0 flex-1 flex-col gap-3">
      <div className="flex min-h-0 flex-1 flex-col gap-6 overflow-y-auto px-1 py-2">
        {conversations.messages.length === 0 ? (
          <p className="text-sm text-muted-foreground">Ask Mate: &ldquo;{EXAMPLE_QUESTION}&rdquo;</p>
        ) : (
          conversations.messages.map((message) => (
            <div key={message.id} className={message.role === 'user' ? 'flex justify-end' : 'flex justify-start'}>
              {message.role === 'user' ? (
                <div className="max-w-[85%] min-w-0 whitespace-pre-wrap rounded-lg bg-muted px-4 py-3 text-sm leading-relaxed text-foreground">
                  {message.content}
                </div>
              ) : (
                <div className="w-full min-w-0">
                  <AssistantMarkdown content={message.content} />
                  <p className="mt-4 border-t border-border pt-2 text-[11px] tabular-nums text-muted-foreground">
                    {formatMessageFooter(message)}
                  </p>
                </div>
              )}
            </div>
          ))
        )}
      </div>

      {chat.sending && (
        <div className="flex items-center gap-2 text-sm text-muted-foreground">
          <Loader2 className="h-4 w-4 animate-spin" />
          <span>{chat.statusText ?? 'Thinking…'}</span>
        </div>
      )}

      {(chat.error || conversations.error) && (
        <p className="text-sm text-destructive">{chat.error ?? conversations.error}</p>
      )}

      <div className="flex flex-col gap-1">
        <Textarea
          ref={composerRef}
          rows={3}
          placeholder="Ask Mate about the next couple of days…"
          value={content}
          onChange={(event) => setContent(event.target.value)}
          onKeyDown={handleKeyDown}
          disabled={!canWrite}
          autoFocus={autoFocus}
        />
        {!canWrite && <p className="text-[11px] text-muted-foreground">Read-only session</p>}
        <div className="flex justify-end">
          <Button onClick={() => void handleSend()} disabled={chat.sending || !canWrite || content.trim() === ''}>
            Send
          </Button>
        </div>
      </div>
    </div>
  )
}
