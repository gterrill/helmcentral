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
    let view: PassageDecision
    try {
      view = decodePassageDecision(decision)
    } catch (err) {
      throw new PassageDecisionUnreadableError(err instanceof Error ? err.message : String(err))
    }
    // The server may have re-matched passages while saving, so the passages
    // that read from decisions are read again, without holding up the answer.
    void invalidateQueries(PASSAGES_KEY)
    return view
  },
}
