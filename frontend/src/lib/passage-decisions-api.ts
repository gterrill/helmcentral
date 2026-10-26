import { apiRequest, invalidateQueries } from '@/lib/api-query'
import { decodePassageDecision, type PassageDecision, type PassageDecisionState } from '@/lib/mate-cards'
import { PASSAGES_KEY } from '@/lib/passages-api'

// What the operator did with a Mate passage plan, over apiRequest. A failed
// request throws with the server's own message; nothing is shown before the
// server has answered.

export interface PassageDecisionBody {
  /** '' clears the choice. */
  state: PassageDecisionState
  /** Required for `went`, refused for any other state. */
  row_date?: string
}

/** The server saved the choice, but what it sent back could not be read. */
export class PassageDecisionUnreadableError extends Error {
  constructor(detail: string) {
    super(`The saved answer could not be read: ${detail}`)
    this.name = 'PassageDecisionUnreadableError'
  }
}

export const passageDecisionsApi = {
  put: async (id: number, body: PassageDecisionBody): Promise<PassageDecision> => {
    const { decision } = await apiRequest<{ decision: unknown }>(`/api/passage-decisions/${id}`, { method: 'PUT', json: body })
    try {
      return decodePassageDecision(decision)
    } catch (err) {
      throw new PassageDecisionUnreadableError(err instanceof Error ? err.message : String(err))
    }
  },
}

type SavedListener = () => void
const savedListeners = new Set<SavedListener>()

/**
 * Called when a decision change has been saved, from a chat card or from the
 * Passages page. Used by whatever shows decisions and has to read the server's
 * state again, such as the open Mate thread. Returns the way to stop listening.
 */
export function onPassageDecisionSaved(listener: SavedListener): () => void {
  savedListeners.add(listener)
  return () => { savedListeners.delete(listener) }
}

/**
 * Reads again what a saved decision can have changed. The server re-matches
 * while it saves, so it can move the choice to another passage and change other
 * plans' inferred choices too. Only the list and the passages the decision
 * was and is linked to are read again. Without the new answer (`view` null)
 * every passage is read again. The listeners hear of it too, so an open Mate
 * thread shows every card as the server now has it.
 */
export function passageDecisionSaved(previousPassageId: string | null, view: PassageDecision | null): Promise<void> {
  savedListeners.forEach((l) => l())
  if (view === null) return invalidateQueries(PASSAGES_KEY)
  const affected = new Set([previousPassageId, view.passageId].filter((id): id is string => id !== null))
  return invalidateQueries(PASSAGES_KEY, (key) => key.length > 1 && !affected.has(String(key[1])))
}
