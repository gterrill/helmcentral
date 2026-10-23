import { apiRequest, invalidateQueries } from '@/lib/api-query'
import type { ReplaySign } from '@/lib/replay-signs'

// Sign reads and writes, the camera shots' pattern (ADR 0173). A failed write
// throws with the server's own message; nothing retries or substitutes.

export const REPLAY_SIGNS_KEY = ['passage-signs'] as const

const SIGNS_PATH = '/api/passages/signs'

export const replaySignsPath = (fromMs: number, toMs: number) => `${SIGNS_PATH}?from=${Math.round(fromMs)}&to=${Math.round(toMs)}`

/** What a new sign is made from; the server fills the id and the photo link. */
export type NewReplaySign = Pick<ReplaySign, 't_ms' | 'title' | 'note' | 'document_id' | 'visibility' | 'show_s'>
export type ReplaySignPatch = Partial<NewReplaySign>

export const replaySignsApi = {
  create: async (sign: NewReplaySign) => {
    const { sign: created } = await apiRequest<{ sign: ReplaySign }>(SIGNS_PATH, { method: 'POST', json: sign })
    await invalidateQueries(REPLAY_SIGNS_KEY)
    return created
  },
  patch: async (id: string, patch: ReplaySignPatch) => {
    const { sign } = await apiRequest<{ sign: ReplaySign }>(`${SIGNS_PATH}/${encodeURIComponent(id)}`, { method: 'PATCH', json: patch })
    await invalidateQueries(REPLAY_SIGNS_KEY)
    return sign
  },
  remove: async (id: string) => {
    await apiRequest<{ deleted: boolean }>(`${SIGNS_PATH}/${encodeURIComponent(id)}`, { method: 'DELETE' })
    await invalidateQueries(REPLAY_SIGNS_KEY)
  },
}
