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

/** Merge the server's hits (title or message matches, newest first, with snippets) with
 *  the instant title matches from the loaded list, without duplicates. */
export function mergeConversationSearchResults(
  server: ConversationSearchResult[],
  local: ConversationSearchResult[],
): ConversationSearchResult[] {
  const seen = new Set(server.map((r) => r.id))
  return [...server, ...local.filter((r) => !seen.has(r.id))]
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
    const serverResults = server?.query === trimmed ? server.results : []
    const settled = server?.query === trimmed || failure?.query === trimmed
    return {
      results: mergeConversationSearchResults(serverResults, local),
      searching: !settled,
      error: failure?.query === trimmed ? failure.message : null,
    }
  }, [conversations, trimmed, server, failure])
}
