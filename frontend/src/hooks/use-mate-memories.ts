import { apiRequest, invalidateQueries, useApiQuery } from '@/lib/api-query'

/**
 * Mate's memory (backend/mate_memory.go): lasting facts about the boat that
 * the skipper asked Mate to keep. Mate keeps one when asked, or offers it on a
 * card; this is where they are read, rewritten, removed and added by hand.
 */
export interface MateMemory {
  id: string
  text: string
  created_at: string
  updated_at: string
}

interface MateMemoryList {
  memories: MateMemory[]
  limit: number
  max_chars: number
}

export const MATE_MEMORIES_PATH = '/api/assistant/memories'

/** The most facts the list holds and the longest a fact may be, until the server says. */
export const DEFAULT_MEMORY_LIMIT = 50
export const DEFAULT_MEMORY_MAX_CHARS = 300

const NO_MEMORIES: MateMemory[] = []

async function send(path: string, init: Parameters<typeof apiRequest>[1]): Promise<void> {
  await apiRequest(path, init)
  await invalidateQueries([MATE_MEMORIES_PATH])
}

export const addMateMemory = (text: string) => send(MATE_MEMORIES_PATH, { method: 'POST', json: { text } })
export const changeMateMemory = (id: string, text: string) => send(`${MATE_MEMORIES_PATH}/${encodeURIComponent(id)}`, { method: 'PATCH', json: { text } })
/** Also what the Undo on a card under Mate's reply calls. */
export const removeMateMemory = (id: string) => send(`${MATE_MEMORIES_PATH}/${encodeURIComponent(id)}`, { method: 'DELETE' })

/** A reply can arrive with a fact already kept (the skipper said "remember
 * that"), before any tap. The list in Settings reads again when one does. */
export function refreshMemoriesForReply(proposals: ReadonlyArray<{ status: string; ops: ReadonlyArray<{ type: string }> }> | undefined): void {
  if (proposals?.some((p) => p.status === 'applied' && p.ops.some((op) => op.type === 'memory'))) {
    void invalidateQueries([MATE_MEMORIES_PATH])
  }
}

export function useMateMemories() {
  const query = useApiQuery<MateMemoryList>({ queryKey: [MATE_MEMORIES_PATH], path: MATE_MEMORIES_PATH })

  return {
    memories: query.data?.memories ?? NO_MEMORIES,
    limit: query.data?.limit ?? DEFAULT_MEMORY_LIMIT,
    maxChars: query.data?.max_chars ?? DEFAULT_MEMORY_MAX_CHARS,
    loading: query.isLoading,
    error: query.error?.message ?? null,
    refresh: query.refetch,
    add: addMateMemory,
    change: changeMateMemory,
    remove: removeMateMemory,
  }
}
