import { apiBaseUrl } from '@/config/api'
import { readErrorMessage } from '@/lib/api-error'
import type {
  CruiseRecord,
  CruiseStats,
  ImportSummary,
  PassageDetail,
  PassageRecord,
  PassageSummary,
} from '@/lib/passages'

// Thin calls to the passage and cruise routes. A failed request throws with
// the server's own message; nothing here retries or substitutes data.

async function request<T>(method: string, path: string, body?: unknown): Promise<T> {
  const res = await fetch(`${apiBaseUrl}${path}`, {
    method,
    headers: body === undefined ? undefined : { 'Content-Type': 'application/json' },
    body: body === undefined ? undefined : JSON.stringify(body),
  })
  if (!res.ok) throw new Error(await readErrorMessage(res))
  return (await res.json()) as T
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
  list: async () => (await request<{ passages: PassageSummary[] }>('GET', '/api/passages')).passages,
  get: async (id: string) => (await request<{ passage: PassageDetail }>('GET', `/api/passages/${encodeURIComponent(id)}`)).passage,
  patch: async (id: string, patch: PassagePatch) =>
    (await request<{ passage: PassageRecord }>('PATCH', `/api/passages/${encodeURIComponent(id)}`, patch)).passage,
  split: async (id: string, at: string) =>
    (await request<{ passages: PassageRecord[] }>('POST', `/api/passages/${encodeURIComponent(id)}/split`, { at })).passages,
  trim: async (id: string, start: string, end: string) =>
    (await request<{ passage: PassageRecord }>('POST', `/api/passages/${encodeURIComponent(id)}/trim`, { start, end })).passage,
  merge: async (a: string, b: string) =>
    (await request<{ passage: PassageRecord }>('POST', '/api/passages/merge', { ids: [a, b] })).passage,
  importGPX: async (files: File[]) => {
    const form = new FormData()
    for (const f of files) form.append('files', f, f.name)
    const res = await fetch(`${apiBaseUrl}/api/passages/import`, { method: 'POST', body: form })
    if (!res.ok) throw new Error(await readErrorMessage(res))
    return (await res.json()) as ImportSummary
  },
  listCruises: async () => (await request<{ cruises: CruiseRecord[] }>('GET', '/api/cruises')).cruises,
  getCruise: (id: string) =>
    request<{ cruise: CruiseRecord; passages: PassageRecord[]; stats: CruiseStats }>('GET', `/api/cruises/${encodeURIComponent(id)}`),
  createCruise: async (name: string, notes = '') =>
    (await request<{ cruise: CruiseRecord }>('POST', '/api/cruises', { name, notes })).cruise,
  patchCruise: async (id: string, patch: { name?: string; notes?: string }) =>
    (await request<{ cruise: CruiseRecord }>('PATCH', `/api/cruises/${encodeURIComponent(id)}`, patch)).cruise,
  deleteCruise: async (id: string) => {
    await request<{ deleted: boolean }>('DELETE', `/api/cruises/${encodeURIComponent(id)}`)
  },
  labelWaterRise: async (cruiseId: string, at: string, label: string) => {
    await request<{ ok: boolean }>('PUT', `/api/cruises/${encodeURIComponent(cruiseId)}/water-rises`, { at, label })
  },
}
