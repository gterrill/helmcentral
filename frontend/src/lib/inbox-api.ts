import { invalidateDocumentLists } from '@/hooks/use-documents'
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
  /** The direction the phone faced, or null when the file carried no real one. */
  bearing_deg: number | null
  /** Perceptual hash, 16 hex characters, or empty for a PDF. */
  fingerprint: string
  sharpness: number
  created_ms: number
  /** The file itself: use it where a picture is shown large. */
  content_url: string
  /** A small preview, absent for an item with none (a PDF). */
  thumb_url?: string
  group: { kind: InboxGroupKind; passage_id?: string }
  /** The phone's own position was over a nautical mile from the track: the clock or zone is off. */
  gps_mismatch: boolean
}

/** The small picture for an item: its thumbnail, else the photo itself, else none (a PDF shows an icon). */
export function inboxPreviewUrl(item: Pick<InboxItem, 'mime' | 'thumb_url' | 'content_url'>): string | undefined {
  return item.thumb_url ?? (item.mime.startsWith('image/') ? item.content_url : undefined)
}

/** What the phone knew about a file, sent beside it. A missing field is left out. */
export interface InboxUploadMeta {
  captureMs: number | null
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

/** The most items the box takes in one request. */
const MAX_BATCH = 500

function* inChunks<T>(all: readonly T[]): Generator<readonly T[]> {
  for (let i = 0; i < all.length; i += MAX_BATCH) yield all.slice(i, i + MAX_BATCH)
}

export const inboxApi = {
  /**
   * One file, with its preview if it has one; identical bytes already in the
   * Inbox or Documents come back as already_uploaded. It does not refresh the
   * Inbox: a batch calls refresh once it is done.
   */
  upload: async (file: Blob, filename: string, meta: InboxUploadMeta, thumb: Blob | null = null): Promise<InboxUploadResult> => {
    const form = new FormData()
    if (meta.captureMs !== null) form.append('capture_ms', String(meta.captureMs))
    if (meta.bearingDeg !== null) form.append('bearing_deg', String(meta.bearingDeg))
    if (meta.phoneLat !== null && meta.phoneLon !== null) {
      form.append('phone_lat', String(meta.phoneLat))
      form.append('phone_lon', String(meta.phoneLon))
    }
    if (meta.fingerprint) form.append('fingerprint', meta.fingerprint)
    form.append('sharpness', String(meta.sharpness))
    if (meta.sourceSha256) form.append('source_sha256', meta.sourceSha256)
    if (thumb) form.append('thumb', thumb, 'thumb.jpg')
    // The file goes last so the server has read every field by the time it has the bytes.
    form.append('file', file, filename)
    return apiRequest<InboxUploadResult>(INBOX_PATH, { method: 'POST', body: form })
  },
  /** Which of these original-file hashes (at most 500) the Inbox already holds or has filed. */
  known: async (hashes: readonly string[]): Promise<string[]> => {
    const { known } = await apiRequest<{ known: string[] }>('/api/inbox/known', { method: 'POST', json: { hashes } })
    return known
  },
  /** Re-reads the list and the count, once a batch of uploads is done. */
  refresh: (): Promise<void> => inboxChanged(),
  /** Re-reads the count alone, for the badge while a long batch goes on. */
  refreshCount: (): Promise<void> => invalidateQueries(INBOX_COUNT_KEY),
  /** Deletes the items nobody is filing; `busy` lists those left alone because a request is filing them. */
  bin: async (ids: readonly string[]): Promise<{ deleted: number; busy: InboxFilingResult[] }> => {
    const out = { deleted: 0, busy: [] as InboxFilingResult[] }
    try {
      for (const chunk of inChunks(ids)) {
        const res = await apiRequest<{ deleted: number; busy: InboxFilingResult[] }>('/api/inbox/bin', { method: 'POST', json: { ids: chunk } })
        out.deleted += res.deleted
        out.busy.push(...res.busy)
      }
    } finally {
      await inboxChanged()
    }
    return out
  },
  fileToPassage: async (items: readonly { id: string; title: string; t_ms?: number }[]): Promise<InboxFilingResult[]> => {
    const results: InboxFilingResult[] = []
    try {
      for (const chunk of inChunks(items)) {
        const res = await apiRequest<{ results: InboxFilingResult[] }>('/api/inbox/file-to-passage', { method: 'POST', json: { items: chunk } })
        results.push(...res.results)
      }
    } finally {
      await inboxChanged([REPLAY_SIGNS_KEY])
      invalidateDocumentLists({ counts: true })
    }
    return results
  },
  fileToDocuments: async (folderId: string, items: readonly { id: string; title?: string }[]): Promise<InboxFilingResult[]> => {
    const results: InboxFilingResult[] = []
    try {
      for (const chunk of inChunks(items)) {
        const res = await apiRequest<{ results: InboxFilingResult[] }>('/api/inbox/file-to-documents', {
          method: 'POST',
          json: { folder_id: folderId, items: chunk },
        })
        results.push(...res.results)
      }
    } finally {
      await inboxChanged()
      invalidateDocumentLists({ counts: true })
    }
    return results
  },
  /** The nearest named place to the boat when each photo was taken, by id; blank where there is none. */
  suggestedTitles: async (ids: readonly string[]): Promise<Record<string, string>> => {
    const titles: Record<string, string> = {}
    for (const chunk of inChunks(ids)) {
      const res = await apiRequest<{ titles: Record<string, string> }>(`/api/inbox/suggested-titles?ids=${chunk.map(encodeURIComponent).join(',')}`)
      Object.assign(titles, res.titles)
    }
    return titles
  },
}
