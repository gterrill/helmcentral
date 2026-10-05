import { useEffect, useMemo, useState } from 'react'

import { apiBaseUrl } from '@/config/api'
import type { AssistantConversation } from '@/hooks/use-assistant-conversations'
import { filterConversationsByQuery } from '@/lib/assistant-conversation-search'

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
  const [server, setServer] = useState<{ query: string; results: ConversationSearchResult[] } | null>(null)
  const [failure, setFailure] = useState<{ query: string; message: string } | null>(null)

  useEffect(() => {
    if (trimmed === '') return
    const controller = new AbortController()
    const timer = setTimeout(async () => {
      try {
        const response = await fetch(`${apiBaseUrl}/api/assistant/conversations/search?q=${encodeURIComponent(trimmed)}`, {
          signal: controller.signal,
        })
        if (!response.ok) throw new Error(`Search failed (${response.status})`)
        const body = (await response.json()) as SearchResponseApi
        setServer({
          query: trimmed,
          results: body.results.map((r) => ({ id: r.id, title: r.title, updatedAt: r.updated_at, snippet: r.snippet })),
        })
        setFailure(null)
      } catch (err) {
        if (controller.signal.aborted) return
        setFailure({ query: trimmed, message: err instanceof Error ? err.message : 'Search failed' })
      }
    }, DEBOUNCE_MS)
    return () => {
      clearTimeout(timer)
      controller.abort()
    }
  }, [trimmed])

  return useMemo(() => {
    if (trimmed === '') return { results: conversations, searching: false, error: null }
    const local = filterConversationsByQuery(conversations, trimmed)
    // Server hits are checked against the loaded list: a conversation deleted
    // since the response is dropped, and a renamed one shows its current title.
    const loaded = new Map(conversations.map((c) => [c.id, c]))
    const serverResults = (server?.query === trimmed ? server.results : []).flatMap((r) => {
      const current = loaded.get(r.id)
      return current ? [{ ...r, title: current.title }] : []
    })
    const settled = server?.query === trimmed || failure?.query === trimmed
    return {
      results: mergeConversationSearchResults(serverResults, local),
      searching: !settled,
      error: failure?.query === trimmed ? failure.message : null,
    }
  }, [conversations, trimmed, server, failure])
}
