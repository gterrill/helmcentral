import { useEffect, useMemo, useRef, useState, type KeyboardEvent } from 'react'

import { Trash2 } from 'lucide-react'

import { Dialog, DialogContent, DialogTitle } from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import type { AssistantConversation } from '@/hooks/use-assistant-conversations'
import { filterConversationsByQuery, formatConversationRelativeTime, recentConversations } from '@/lib/assistant-conversation-search'
import { cn } from '@/lib/utils'

const RECENT_LIMIT = 8

interface ConversationSearchOverlayProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  conversations: AssistantConversation[]
  onSelect: (id: string) => void
  /** When given, each row gets a delete button (the drawer's phone layout has no sidebar to delete from). */
  onDelete?: (id: string) => void
}

/**
 * The Mate sheet's search control (Mate UI cycle: search the Mate sheet's
 * conversations) - a command-palette style overlay over the Dialog
 * primitive: a search box, and below it either the 8 most recent
 * conversations (an empty query) or every title match, arrow-key/Enter
 * navigable, Esc closes (the Dialog primitive's own built-in handling - this
 * component never intercepts Escape itself). Filtering is the SAME predicate
 * the /mate page's inline "Search conversations" box uses
 * (lib/assistant-conversation-search.ts), not a second implementation, and
 * both draw on `conversations`, which the caller already has loaded - this
 * never issues a fetch of its own.
 *
 * DOM focus stays on the input throughout (the same combobox-style pattern
 * shadcn's own Command component uses): arrow keys move a virtual
 * `aria-selected` highlight across `role="option"` rows rather than moving
 * real focus, so typing and navigating never fight each other.
 */
export function ConversationSearchOverlay({ open, onOpenChange, conversations, onSelect, onDelete }: ConversationSearchOverlayProps) {
  const [query, setQuery] = useState('')
  const [highlighted, setHighlighted] = useState(0)
  const inputRef = useRef<HTMLInputElement>(null)

  const visible = useMemo(() => {
    const filtered = filterConversationsByQuery(conversations, query)
    return query.trim() === '' ? recentConversations(filtered, RECENT_LIMIT) : filtered
  }, [conversations, query])

  // A fresh open (or a query that changes what's visible) always starts back
  // at the top row - carrying a stale highlight across a filter change could
  // point Enter at a row that has since scrolled out of the list entirely.
  useEffect(() => {
    setHighlighted(0)
  }, [query, open])

  // The query itself resets on every close/reopen (same as
  // DocumentLinkPicker's own reset-on-close, inventory/document-link-picker.tsx)
  // so reopening the overlay later never shows a stale search.
  useEffect(() => {
    if (!open) setQuery('')
  }, [open])

  const select = (id: string) => {
    onSelect(id)
    onOpenChange(false)
  }

  const handleKeyDown = (event: KeyboardEvent<HTMLInputElement>) => {
    if (event.key === 'ArrowDown') {
      event.preventDefault()
      setHighlighted((current) => Math.min(current + 1, Math.max(visible.length - 1, 0)))
    } else if (event.key === 'ArrowUp') {
      event.preventDefault()
      setHighlighted((current) => Math.max(current - 1, 0))
    } else if (event.key === 'Enter') {
      event.preventDefault()
      const target = visible[highlighted]
      if (target) select(target.id)
    }
    // Escape is deliberately left alone - the Dialog primitive already
    // closes on it, and intercepting it here would be a second, redundant
    // handler for the same key.
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      {/* Below sm the panel drops from the top of the screen like a search
          bar (full width less a 16px gutter, clear of the on-screen
          keyboard); from sm up it is the centred palette it always was. */}
      <DialogContent className="top-4 w-[calc(100%-2rem)] max-w-none -translate-y-0 gap-3 p-3 sm:top-1/2 sm:max-w-md sm:-translate-y-1/2">
        {/* sr-only: the visible surface is just the search box and its
            results, matching a command-palette's own convention (no header
            bar) - composition.md still requires every Dialog to carry a
            real DialogTitle for assistive tech. */}
        <DialogTitle className="sr-only">Search conversations</DialogTitle>
        <Input
          ref={inputRef}
          aria-label="Search conversations"
          placeholder="Search conversations…"
          value={query}
          onChange={(event) => setQuery(event.target.value)}
          onKeyDown={handleKeyDown}
          autoFocus
        />
        <div role="listbox" aria-label="Conversations" className="max-h-80 min-w-0 overflow-y-auto rounded-md border border-border">
          {visible.length === 0 ? (
            <p className="p-3 text-sm text-muted-foreground">No matching conversations</p>
          ) : (
            visible.map((conversation, index) => (
              <div
                key={conversation.id}
                role="presentation"
                className={cn(
                  'flex min-w-0 items-center border-b border-border last:border-b-0',
                  index === highlighted ? 'bg-primary/10 text-primary' : 'hover:bg-muted',
                )}
                onMouseEnter={() => setHighlighted(index)}
              >
                <button
                  type="button"
                  role="option"
                  aria-selected={index === highlighted}
                  onClick={() => select(conversation.id)}
                  className="flex min-w-0 flex-1 flex-col items-start gap-0.5 px-3 py-2 text-left"
                >
                  <span className="w-full truncate text-sm">{conversation.title}</span>
                  <span className="text-xs text-muted-foreground">{formatConversationRelativeTime(conversation.updatedAt)}</span>
                </button>
                {onDelete && (
                  <button
                    type="button"
                    aria-label={`Delete ${conversation.title}`}
                    tabIndex={-1}
                    // Keep DOM focus on the search input so arrows and Enter keep working.
                    onMouseDown={(event) => event.preventDefault()}
                    onClick={() => onDelete(conversation.id)}
                    className="flex h-11 w-11 shrink-0 items-center justify-center text-muted-foreground hover:text-foreground"
                  >
                    <Trash2 className="h-4 w-4" />
                  </button>
                )}
              </div>
            ))
          )}
        </div>
      </DialogContent>
    </Dialog>
  )
}
