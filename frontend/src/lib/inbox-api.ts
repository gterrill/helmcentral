import { apiRequest, invalidateQueries } from '@/lib/api-query'
import { REPLAY_SIGNS_KEY } from '@/lib/replay-signs-api'

// The Inbox over apiRequest (ADR 0173): reads go through useApiQuery in
// hooks/use-inbox.ts under these keys; every write throws the server's own
// message, and nothing retries or substitutes data.

export const INBOX_KEY = ['inbox'] as const
export const INBOX_COUNT_KEY = ['inbox-count'] as const
export const INBOX_PATH = '/api/inbox'
export const INBOX_COUNT_PATH = '/api/inbox/count'

export type InboxGroupKind = 'passage' | 'underway' | 'off_passage' | 'no_time'

export interface InboxItem {
  id: string
  filename: string
  mime: string
  size_bytes: number
  /** Unix ms the photo was taken, or null when the file says nothing. */
  capture_ms: number | null
  capture_offset_min: number | null
  bearing_deg: number | null
  has_bearing: boolean
  /** Perceptual hash, 16 hex characters, or empty for a PDF. */
  fingerprint: string
  sharpness: number
  created_ms: number
  content_url: string
  group: { kind: InboxGroupKind; passage_id?: string }
  /** The phone's own position was over a nautical mile from the track: the clock or zone is off. */
  gps_mismatch: boolean
}

/** What the phone knew about a file, sent beside it. A missing field is left out. */
export interface InboxUploadMeta {
  captureMs: number | null
  captureOffsetMin: number | null
  bearingDeg: number | null
  phoneLat: number | null
  phoneLon: number | null
  fingerprint: string
  sharpness: number
  /** Hash of the original file as the phone held it, or empty when it could not be worked out. */
  sourceSha256: string
}

export type InboxUploadResult =
  | { status: 'stored'; item: InboxItem }
  | { status: 'already_uploaded'; where: 'inbox' | 'documents' | 'filed' }

/** The outcome for one item of a filing call. */
export interface InboxFilingResult {
  id: string
  ok: boolean
  sign_id?: string
  document_id?: string
  error?: string
}

async function inboxChanged(also: readonly (readonly unknown[])[] = []): Promise<void> {
  await Promise.all([invalidateQueries(INBOX_KEY), invalidateQueries(INBOX_COUNT_KEY), ...also.map((k) => invalidateQueries(k))])
}

export const inboxApi = {
  /** One file; identical bytes already in the Inbox or Documents come back as already_uploaded. */
  upload: async (file: Blob, filename: string, meta: InboxUploadMeta): Promise<InboxUploadResult> => {
    const form = new FormData()
    if (meta.captureMs !== null) form.append('capture_ms', String(meta.captureMs))
    if (meta.captureOffsetMin !== null) form.append('capture_offset_min', String(meta.captureOffsetMin))
    if (meta.bearingDeg !== null) form.append('bearing_deg', String(meta.bearingDeg))
    if (meta.phoneLat !== null && meta.phoneLon !== null) {
      form.append('phone_lat', String(meta.phoneLat))
      form.append('phone_lon', String(meta.phoneLon))
    }
    if (meta.fingerprint) form.append('fingerprint', meta.fingerprint)
    form.append('sharpness', String(meta.sharpness))
    if (meta.sourceSha256) form.append('source_sha256', meta.sourceSha256)
    // The file goes last so the server has read every field by the time it has the bytes.
    form.append('file', file, filename)
    const result = await apiRequest<InboxUploadResult>(INBOX_PATH, { method: 'POST', body: form })
    await inboxChanged()
    return result
  },
  bin: async (ids: readonly string[]): Promise<number> => {
    const { deleted } = await apiRequest<{ deleted: number }>('/api/inbox/bin', { method: 'POST', json: { ids } })
    await inboxChanged()
    return deleted
  },
  fileToPassage: async (items: readonly { id: string; title: string; t_ms?: number }[]): Promise<InboxFilingResult[]> => {
    const { results } = await apiRequest<{ results: InboxFilingResult[] }>('/api/inbox/file-to-passage', { method: 'POST', json: { items } })
    await inboxChanged([REPLAY_SIGNS_KEY, ['/api/document-folders'], ['/api/documents']])
    return results
  },
  fileToDocuments: async (folderId: string, items: readonly { id: string; title?: string }[]): Promise<InboxFilingResult[]> => {
    const { results } = await apiRequest<{ results: InboxFilingResult[] }>('/api/inbox/file-to-documents', {
      method: 'POST',
      json: { folder_id: folderId, items },
    })
    await inboxChanged([['/api/document-folders'], ['/api/documents']])
    return results
  },
  /** The nearest named place to the boat when each photo was taken, by id; blank where there is none. */
  suggestedTitles: async (ids: readonly string[]): Promise<Record<string, string>> => {
    const { titles } = await apiRequest<{ titles: Record<string, string> }>(`/api/inbox/suggested-titles?ids=${ids.map(encodeURIComponent).join(',')}`)
    return titles
  },
}
