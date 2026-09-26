import { describe, it, expect, vi, afterEach } from 'vitest'

import { filterConversationsByQuery, formatConversationRelativeTime, recentConversations } from '@/lib/assistant-conversation-search'
import type { AssistantConversation } from '@/hooks/use-assistant-conversations'

// Mate UI cycle: search the Mate sheet's conversations. The Mate sheet's new
// search overlay and the /mate page's existing inline "Search conversations"
// box (assistant-drawer.tsx) share this one filter rather than each
// reimplementing it - AGENTS.md's "don't build a second search" applied to a
// two-line predicate, not just a backend endpoint. The server already
// returns conversations newest-first (use-assistant-conversations.ts's own
// comment on this), so "8 most recent" is just the first 8 of the list handed in.

function conversation(overrides: Partial<AssistantConversation> = {}): AssistantConversation {
  return { id: 'c1', title: 'Untitled', createdAt: '2026-09-01T00:00:00Z', updatedAt: '2026-09-01T00:00:00Z', ...overrides }
}

describe('filterConversationsByQuery', () => {
  it('returns everything unfiltered for a blank query', () => {
    const list = [conversation({ id: 'a', title: 'Gloucester Island' }), conversation({ id: 'b', title: 'Hamilton Island' })]
    expect(filterConversationsByQuery(list, '')).toEqual(list)
    expect(filterConversationsByQuery(list, '   ')).toEqual(list)
  })

  it('filters case-insensitively by title substring', () => {
    const list = [conversation({ id: 'a', title: 'Gloucester Island Anchorages' }), conversation({ id: 'b', title: 'Hamilton Island Weather' })]
    expect(filterConversationsByQuery(list, 'gloucester').map((c) => c.id)).toEqual(['a'])
    expect(filterConversationsByQuery(list, 'ISLAND').map((c) => c.id)).toEqual(['a', 'b'])
  })

  it('returns an empty list when nothing matches', () => {
    const list = [conversation({ title: 'Gloucester Island' })]
    expect(filterConversationsByQuery(list, 'zzz')).toEqual([])
  })
})

describe('recentConversations', () => {
  it('takes the first `limit` conversations, assuming server order (newest first)', () => {
    const list = Array.from({ length: 12 }, (_, i) => conversation({ id: `c${i}`, title: `Conversation ${i}` }))
    const recent = recentConversations(list)
    expect(recent).toHaveLength(8)
    expect(recent.map((c) => c.id)).toEqual(['c0', 'c1', 'c2', 'c3', 'c4', 'c5', 'c6', 'c7'])
  })

  it('returns the whole list when it has fewer than the limit', () => {
    const list = [conversation({ id: 'a' }), conversation({ id: 'b' })]
    expect(recentConversations(list)).toEqual(list)
  })

  it('accepts a custom limit', () => {
    const list = Array.from({ length: 5 }, (_, i) => conversation({ id: `c${i}` }))
    expect(recentConversations(list, 2).map((c) => c.id)).toEqual(['c0', 'c1'])
  })
})

describe('formatConversationRelativeTime', () => {
  afterEach(() => {
    vi.useRealTimers()
  })

  it('renders -- for an unparseable timestamp', () => {
    expect(formatConversationRelativeTime('not a date')).toBe('--')
  })

  it('coarsens to the largest whole unit', () => {
    vi.useFakeTimers()
    vi.setSystemTime(new Date('2026-09-11T12:00:00Z'))
    expect(formatConversationRelativeTime('2026-09-11T11:59:30Z')).toBe('just now')
    expect(formatConversationRelativeTime('2026-09-11T11:55:00Z')).toBe('5m ago')
    expect(formatConversationRelativeTime('2026-09-11T09:00:00Z')).toBe('3h ago')
    expect(formatConversationRelativeTime('2026-09-09T12:00:00Z')).toBe('2d ago')
  })
})
