import { Loader2, Trash2 } from 'lucide-react'
import { useCallback, useState, type KeyboardEvent } from 'react'

import { AssistantMarkdown } from '@/components/assistant-markdown'
import { Button } from '@/components/ui/button'
import { Textarea } from '@/components/ui/textarea'
import { cn } from '@/lib/utils'
import { useAssistantChat } from '@/hooks/use-assistant-chat'
import { useAssistantConversations, type AssistantMessage } from '@/hooks/use-assistant-conversations'
import { useAssistantStatus } from '@/hooks/use-assistant-status'

interface AssistantDrawerProps {
  canWrite: boolean
  onOpenSettings: () => void
}

const EXAMPLE_QUESTION =
  "We're at Hook Reef. Should we visit Tongue Bay or Blue Pearl Bay first over the next two days?"

// A short, local formatter - not a shared primitive, just readable list
// rows. Coarsens to the largest unit that stays a whole number, the same
// call alarms-drawer.tsx's formatDwell makes for a duration read on a
// phone screen.
function formatRelativeTime(iso: string): string {
  const then = new Date(iso).getTime()
  if (Number.isNaN(then)) return '--'
  const diffSeconds = Math.round((Date.now() - then) / 1000)
  if (diffSeconds < 45) return 'just now'
  const diffMinutes = Math.round(diffSeconds / 60)
  if (diffMinutes < 60) return `${diffMinutes}m ago`
  const diffHours = Math.round(diffMinutes / 60)
  if (diffHours < 24) return `${diffHours}h ago`
  const diffDays = Math.round(diffHours / 24)
  return `${diffDays}d ago`
}

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

/**
 * The onboard assistant panel (ADR 0093). Owns its own data - status,
 * conversation list/thread, and the chat send - the same way RadarDrawer
 * owns its map state: nothing else in the app needs any of it.
 */
export function AssistantDrawer({ canWrite, onOpenSettings }: AssistantDrawerProps) {
  const status = useAssistantStatus()
  const conversations = useAssistantConversations()
  const chat = useAssistantChat()
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

  const body = (() => {
    if (status.loading) {
      return <p className="text-sm text-muted-foreground">Loading…</p>
    }

    if (status.status?.problem) {
      return (
        <div className="max-w-md space-y-3 rounded-lg border bg-card p-4">
          <p className="text-sm text-foreground">{status.status.problem}</p>
          <Button variant="outline" onClick={onOpenSettings}>
            Open Assistant settings
          </Button>
        </div>
      )
    }

    return (
      <div className="flex h-full min-h-0 w-full flex-col gap-4 lg:flex-row">
        <div className="flex min-w-0 shrink-0 gap-2 lg:w-56 lg:flex-col">
          <Button variant="outline" onClick={() => void conversations.create()}>
            New conversation
          </Button>
          <div className="flex min-h-0 min-w-0 flex-1 gap-1 overflow-x-auto lg:flex-col lg:overflow-x-visible lg:overflow-y-auto">
            {conversations.conversations.map((conversation) => (
              <div
                key={conversation.id}
                className={cn(
                  'group flex w-56 shrink-0 items-center gap-1 rounded-md px-2 py-1.5 lg:w-auto',
                  conversations.activeId === conversation.id ? 'bg-primary/10 text-primary' : 'hover:bg-muted',
                )}
              >
                <button
                  type="button"
                  className="min-w-0 flex-1 text-left"
                  onClick={() => void conversations.select(conversation.id)}
                >
                  <div className="truncate text-sm">{conversation.title}</div>
                  <div className="text-[11px] text-muted-foreground">{formatRelativeTime(conversation.updatedAt)}</div>
                </button>
                <Button
                  variant="ghost"
                  size="icon"
                  aria-label={`Delete ${conversation.title}`}
                  onClick={() => void conversations.remove(conversation.id)}
                >
                  <Trash2 className="h-4 w-4" />
                </Button>
              </div>
            ))}
          </div>
        </div>

        <div className="mx-auto flex min-h-0 w-full max-w-3xl min-w-0 flex-1 flex-col gap-3">
          <div className="flex min-h-0 flex-1 flex-col gap-6 overflow-y-auto px-1 py-2">
            {conversations.messages.length === 0 ? (
              <p className="text-sm text-muted-foreground">Try: &ldquo;{EXAMPLE_QUESTION}&rdquo;</p>
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
              rows={3}
              placeholder="Ask about the next couple of days…"
              value={content}
              onChange={(event) => setContent(event.target.value)}
              onKeyDown={handleKeyDown}
              disabled={!canWrite}
            />
            {!canWrite && <p className="text-[11px] text-muted-foreground">Read-only session</p>}
            <div className="flex justify-end">
              <Button onClick={() => void handleSend()} disabled={chat.sending || !canWrite || content.trim() === ''}>
                Send
              </Button>
            </div>
          </div>
        </div>
      </div>
    )
  })()

  return (
    <div data-testid="assistant-drawer" className="h-full min-h-0">
      {body}
    </div>
  )
}
