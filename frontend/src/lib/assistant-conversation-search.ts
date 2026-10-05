import type { AssistantConversation } from '@/hooks/use-assistant-conversations'

// Mate UI cycle: search the Mate sheet's conversations. Shared by the /mate
// page's inline "Search conversations" box (assistant-drawer.tsx) and the
// sheet's new command-palette style overlay (conversation-search-overlay.tsx)
// - one filter, not two, per AGENTS.md's "reuse whatever the /mate page
// search uses, don't build a second search". This is the instant title
// filter; words inside a conversation are found by the server, through
// hooks/use-conversation-search.ts, which merges the two.

/** Case-insensitive title-substring filter - a blank/whitespace query is "no filter". */
export function filterConversationsByQuery<T extends { title: string }>(conversations: T[], query: string): T[] {
  const needle = query.trim().toLowerCase()
  if (needle === '') return conversations
  return conversations.filter((conversation) => conversation.title.toLowerCase().includes(needle))
}

/**
 * The first `limit` conversations, relying on the server's own ordering
 * (use-assistant-conversations.ts: "the server is already sorted newest
 * first") rather than re-sorting by updatedAt here - a second, possibly
 * different, sort order would be its own source of drift from the plain
 * list the rest of the sheet/panel already trusts.
 */
export function recentConversations<T>(conversations: T[], limit = 8): T[] {
  return conversations.slice(0, limit)
}

// Moved from assistant-drawer.tsx (Mate UI cycle: search the Mate sheet's
// conversations) so the sheet's new search overlay can show the identical
// "5m ago"/"2h ago" rows the panel's own list already does, without a second
// copy of the same coarsening rule - the same call alarms-drawer.tsx's
// formatDwell makes for a duration read on a phone screen.
export function formatConversationRelativeTime(iso: string): string {
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

export type { AssistantConversation }
