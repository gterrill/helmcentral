import { Trash2 } from 'lucide-react'

import { AssistantThread } from '@/components/assistant-thread'
import { Button } from '@/components/ui/button'
import { cn } from '@/lib/utils'
import { useAssistantChat } from '@/hooks/use-assistant-chat'
import { useAssistantConversations } from '@/hooks/use-assistant-conversations'
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
}

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
export function AssistantDrawer({ canWrite, onOpenSettings, initialConversationId }: AssistantDrawerProps) {
  const status = useAssistantStatus()
  const conversations = useAssistantConversations({ initialId: initialConversationId })
  const chat = useAssistantChat()

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

        <AssistantThread canWrite={canWrite} conversations={conversations} chat={chat} />
      </div>
    )
  })()

  return (
    <div data-testid="assistant-drawer" className="h-full min-h-0">
      {body}
    </div>
  )
}
