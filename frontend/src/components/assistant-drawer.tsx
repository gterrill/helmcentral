import { useEffect, useState } from 'react'
import { History, MessageSquarePlus, Search, Trash2 } from 'lucide-react'

import { AssistantThread } from '@/components/assistant-thread'
import { ConversationSearchOverlay } from '@/components/conversation-search-overlay'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { useConversationSearch } from '@/hooks/use-conversation-search'
import { formatConversationRelativeTime } from '@/lib/assistant-conversation-search'
import { cn } from '@/lib/utils'
import { useAssistantChat } from '@/hooks/use-assistant-chat'
import { useAssistantConversations } from '@/hooks/use-assistant-conversations'
import { useMateTelemetryWatch } from '@/hooks/use-mate-telemetry-watch'
import { useAssistantStatus } from '@/hooks/use-assistant-status'

interface AssistantDrawerProps {
  canWrite: boolean
  onOpenSettings: () => void
  /** Selects this conversation on mount (or re-selects it if it later
   * changes to a new id, without remounting the panel) rather than the
   * newest one - ADR 0094's "Open in Mate" hands the panel the sheet's
   * active thread this way. Absent, or falling outside the fetched list,
   * for the ordinary "Mate" nav click behaves exactly as before. */
  initialConversationId?: string | null
  onActiveConversationChange?: (id: string | null) => void
  /** Whether the composer holds an unsent draft; see AssistantThread. */
  onHasDraftChange?: (hasDraft: boolean) => void
}

/**
 * The onboard assistant panel (ADR 0093), user-facing as "Mate". Owns its
 * own data - status, conversation list/thread, and the chat send - the same
 * way RadarDrawer owns its map state: nothing else in the app needs any of
 * it. The list/thread split mirrors the Mate sheet (mate-sheet.tsx): this
 * component owns the conversation-list column and the problem/loading
 * states, and hands the thread column to the same AssistantThread the
 * sheet uses, so the long-session panel and the quick voice channel render
 * one conversation identically.
 */
export function AssistantDrawer({ canWrite, onOpenSettings, initialConversationId, onActiveConversationChange, onHasDraftChange }: AssistantDrawerProps) {
  const status = useAssistantStatus()
  const conversations = useAssistantConversations({ initialId: initialConversationId })
  const chat = useAssistantChat()
  const watch = useMateTelemetryWatch(conversations.activeId)
  const [query, setQuery] = useState('')
  const [historyOpen, setHistoryOpen] = useState(false)

  const search = useConversationSearch(conversations.conversations, query)
  const filteredConversations = search.results

  useEffect(() => {
    if (conversations.loading) return
    onActiveConversationChange?.(conversations.activeId)
  }, [onActiveConversationChange, conversations.activeId, conversations.loading])

  const body = (() => {
    if (status.loading) {
      return <p className="text-sm text-muted-foreground">Loading…</p>
    }

    if (status.status?.problem) {
      return (
        <div className="max-w-md space-y-3 rounded-lg border bg-card p-4">
          <p className="text-sm text-foreground">{status.status.problem}</p>
          <Button variant="outline" onClick={onOpenSettings}>
            Open Mate settings
          </Button>
        </div>
      )
    }

    return (
      <div className="flex h-full min-h-0 w-full flex-col gap-4 lg:flex-row">
        {/* Below lg there is no room for a list: a compact header row opens
            the same search overlay the Mate sheet uses (8 most recent chats,
            filter as you type). From lg up the sidebar list below is shown. */}
        <div className="flex min-w-0 shrink-0 gap-2 lg:hidden">
          <Button variant="outline" className="h-11 flex-1 justify-start gap-2" onClick={() => setHistoryOpen(true)}>
            <History className="h-4 w-4" />
            Chats
          </Button>
          <Button variant="outline" className="h-11 gap-2" onClick={() => conversations.startNew()}>
            <MessageSquarePlus className="h-4 w-4" />
            New
          </Button>
        </div>
        <ConversationSearchOverlay
          open={historyOpen}
          onOpenChange={setHistoryOpen}
          conversations={conversations.conversations}
          onSelect={(id) => void conversations.select(id)}
          onDelete={(id) => void conversations.remove(id)}
        />
        <div className="hidden min-w-0 shrink-0 gap-2 lg:flex lg:w-64 lg:flex-col">
          {/* Mate UI cycle ("Mate opens on an empty chat"): a local reset,
              not a POST - see conversations.startNew's own doc comment for
              why persisting a conversation here, before the operator has
              typed anything, was the "empty persisted draft" this cycle
              removes. The conversation is only ever actually created when
              the first message is sent. */}
          <Button variant="outline" onClick={() => conversations.startNew()}>
            New conversation
          </Button>
          <div className="relative">
            <Search className="pointer-events-none absolute left-2.5 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground" />
            <Input
              type="search"
              value={query}
              onChange={(event) => setQuery(event.target.value)}
              placeholder="Search conversations"
              aria-label="Search conversations"
              className="h-9 pl-9"
            />
          </div>
          {search.error && (
            <p role="alert" className="text-xs text-destructive">
              Message search failed: {search.error}. Showing title matches only.
            </p>
          )}
          <div className="flex min-h-0 min-w-0 flex-1 flex-col gap-1 overflow-y-auto">
            {filteredConversations.length === 0 ? (
              <div className="rounded-md border border-dashed border-border px-2 py-3 text-xs text-muted-foreground">
                {search.searching ? 'Searching…' : 'No matching conversations'}
              </div>
            ) : (
              filteredConversations.map((conversation) => (
                <div
                  key={conversation.id}
                  className={cn(
                    'group flex shrink-0 items-center gap-1 rounded-md px-2 py-1.5',
                    conversations.activeId === conversation.id ? 'bg-primary/10 text-primary' : 'hover:bg-muted',
                  )}
                >
                  <button
                    type="button"
                    className="min-w-0 flex-1 text-left"
                    onClick={() => void conversations.select(conversation.id)}
                  >
                    <div className="truncate text-sm">{conversation.title}</div>
                    {'snippet' in conversation && conversation.snippet && (
                      <div className="line-clamp-2 text-xs text-muted-foreground">{conversation.snippet}</div>
                    )}
                    <div className="text-xs text-muted-foreground">{formatConversationRelativeTime(conversation.updatedAt)}</div>
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
              ))
            )}
          </div>
        </div>

        <AssistantThread canWrite={canWrite} conversations={conversations} chat={chat} watch={watch} onHasDraftChange={onHasDraftChange} />
      </div>
    )
  })()

  return (
    <div data-testid="assistant-drawer" className="h-full min-h-0">
      {body}
    </div>
  )
}
