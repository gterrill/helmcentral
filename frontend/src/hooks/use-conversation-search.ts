import { useEffect, useMemo, useState } from 'react'

import type { AssistantConversation } from '@/hooks/use-assistant-conversations'
import { filterConversationsByQuery } from '@/lib/assistant-conversation-search'
import { useApiQuery } from '@/lib/api-query'

/** A conversation row in search results; `snippet` is set when message text matched. */
export interface ConversationSearchResult {
  id: string
  title: string
  updatedAt: string
  snippet?: string
}

interface SearchResponseApi {
  results: Array<{ id: string; title: string; updated_at: string; snippet?: string }>
}

const DEBOUNCE_MS = 200

/** Merge the instant title matches from the loaded list with the server's hits
 *  (title or message matches, with snippets), without duplicates. Title matches
 *  stay on top, taking a snippet if the server has one, and server-only hits
 *  follow: rows already on screen never move when the server answers, so Enter
 *  opens the row the operator saw highlighted. */
export function mergeConversationSearchResults(
  server: ConversationSearchResult[],
  local: ConversationSearchResult[],
): ConversationSearchResult[] {
  const byId = new Map(server.map((r) => [r.id, r]))
  const localIds = new Set(local.map((r) => r.id))
  return [
    ...local.map((r) => ({ ...r, snippet: byId.get(r.id)?.snippet ?? r.snippet })),
    ...server.filter((r) => !localIds.has(r.id)),
  ]
}

/**
 * One search for both the /mate page's inline box and the Mate sheet's
 * overlay. Title matches from the already-loaded list show at once; a
 * non-empty query also asks the server (debounced), which finds words
 * anywhere in a conversation and returns a snippet of the matching message.
 * An empty query returns the list untouched and never calls the server.
 */
export function useConversationSearch(
  conversations: AssistantConversation[],
  query: string,
): { results: ConversationSearchResult[]; searching: boolean; error: string | null } {
  const trimmed = query.trim()
  // The server is asked once typing pauses; the query it is asked about
  // trails the box by DEBOUNCE_MS. A superseded request needs no abort: its
  // answer lands under its own key and is never read.
  const [asked, setAsked] = useState('')
  useEffect(() => {
    if (trimmed === '') return
    const timer = setTimeout(() => setAsked(trimmed), DEBOUNCE_MS)
    return () => clearTimeout(timer)
  }, [trimmed])

  const search = useApiQuery({
    queryKey: ['/api/assistant/conversations/search', { q: asked }],
    path: `/api/assistant/conversations/search?q=${encodeURIComponent(asked)}`,
    enabled: asked !== '' && asked === trimmed,
    select: (raw: SearchResponseApi): ConversationSearchResult[] =>
      raw.results.map((r) => ({ id: r.id, title: r.title, updatedAt: r.updated_at, snippet: r.snippet })),
  })
  const server = asked === trimmed ? search.data : undefined
  const failure = asked === trimmed ? (search.error?.message ?? null) : null

  return useMemo(() => {
    if (trimmed === '') return { results: conversations, searching: false, error: null }
    const local = filterConversationsByQuery(conversations, trimmed)
    // Server hits are checked against the loaded list: a conversation deleted
    // since the response is dropped, and a renamed one shows its current title.
    const loaded = new Map(conversations.map((c) => [c.id, c]))
    const serverResults = (server ?? []).flatMap((r) => {
      const current = loaded.get(r.id)
      return current ? [{ ...r, title: current.title }] : []
    })
    const settled = server !== undefined || failure !== null
    return {
      results: mergeConversationSearchResults(serverResults, local),
      searching: !settled,
      error: failure,
    }
  }, [conversations, trimmed, server, failure])
}
