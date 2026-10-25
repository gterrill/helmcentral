import { apiRequest, invalidateQueries } from '@/lib/api-query'
import { REPLAY_SIGNS_KEY } from '@/lib/replay-signs-api'
import type {
  CruiseRecord,
  CruiseStats,
  ImportSummary,
  PassageDetail,
  PassageRecord,
} from '@/lib/passages'

// Passage and cruise writes, over apiRequest (ADR 0173). Reads go through
// useApiQuery in hooks/use-passages.ts under these keys. A failed request
// throws with the server's own message; nothing retries or substitutes data.

export const PASSAGES_KEY = ['passages'] as const
export const CRUISES_KEY = ['cruises'] as const

export const passagesPath = (id?: string) => (id ? `/api/passages/${encodeURIComponent(id)}` : '/api/passages')
export const cruisesPath = (id?: string) => (id ? `/api/cruises/${encodeURIComponent(id)}` : '/api/cruises')

export type CruiseDetailResponse = { cruise: CruiseRecord; passages: PassageRecord[]; stats: CruiseStats }

// A passage edit changes its own record and list row, and the cruise pane
// counts and cruise figures that read it; ['passages'] also covers every
// ['passages', id] under it. `gone` leaves a record that no longer answers.
async function passagesChanged(gone?: readonly unknown[]): Promise<void> {
  const skip = gone
    ? (key: readonly unknown[]) => key.length === gone.length && key.every((k, i) => k === gone[i])
    : undefined
  await Promise.all([invalidateQueries(PASSAGES_KEY, skip), invalidateQueries(CRUISES_KEY, skip)])
}

export interface PassagePatch {
  title?: string
  notes?: string
  from_name?: string
  to_name?: string
  hidden?: boolean
  cruise_id?: string | null
}

export const passagesApi = {
  getPassage: async (id: string) => (await apiRequest<{ passage: PassageDetail }>(passagesPath(id))).passage,
  patch: async (id: string, patch: PassagePatch) => {
    const { passage } = await apiRequest<{ passage: PassageRecord }>(passagesPath(id), { method: 'PATCH', json: patch })
    await passagesChanged()
    return passage
  },
  split: async (id: string, at: string) => {
    const { passages } = await apiRequest<{ passages: PassageRecord[] }>(`${passagesPath(id)}/split`, { method: 'POST', json: { at } })
    await passagesChanged()
    return passages
  },
  trim: async (id: string, start: string, end: string) => {
    const { passage } = await apiRequest<{ passage: PassageRecord }>(`${passagesPath(id)}/trim`, { method: 'POST', json: { start, end } })
    await passagesChanged()
    return passage
  },
  /** Folds two or more neighbouring passages into one, keeping a join sign at
   * each boundary, so the signs are read again too. */
  merge: async (ids: readonly string[]) => {
    const { passage } = await apiRequest<{ passage: PassageRecord }>('/api/passages/merge', { method: 'POST', json: { ids } })
    await Promise.all([passagesChanged(), invalidateQueries(REPLAY_SIGNS_KEY)])
    return passage
  },
  importGPX: async (files: File[]) => {
    const form = new FormData()
    for (const f of files) form.append('files', f, f.name)
    const summary = await apiRequest<ImportSummary>('/api/passages/import', { method: 'POST', body: form })
    await passagesChanged()
    return summary
  },
  createCruise: async (name: string, notes = '') => {
    const { cruise } = await apiRequest<{ cruise: CruiseRecord }>('/api/cruises', { method: 'POST', json: { name, notes } })
    await invalidateQueries(CRUISES_KEY)
    return cruise
  },
  patchCruise: async (id: string, patch: { name?: string; notes?: string }) => {
    const { cruise } = await apiRequest<{ cruise: CruiseRecord }>(cruisesPath(id), { method: 'PATCH', json: patch })
    await invalidateQueries(CRUISES_KEY)
    return cruise
  },
  deleteCruise: async (id: string) => {
    await apiRequest<{ deleted: boolean }>(cruisesPath(id), { method: 'DELETE' })
    // Its passages are kept and become unfiled; the deleted cruise answers 404.
    await passagesChanged([...CRUISES_KEY, id])
  },
  labelWaterRise: async (cruiseId: string, at: string, label: string) => {
    await apiRequest<{ ok: boolean }>(`${cruisesPath(cruiseId)}/water-rises`, { method: 'PUT', json: { at, label } })
    await invalidateQueries([...CRUISES_KEY, cruiseId])
  },
}
