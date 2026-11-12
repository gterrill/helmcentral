import { invalidateDocumentLists } from '@/hooks/use-documents'
import { equipmentPhotosAdded } from '@/hooks/use-inventory'
import { openJobRecordApplied } from '@/hooks/use-open-jobs'
import { apiRequest, invalidateQueries } from '@/lib/api-query'
import { errorMessage } from '@/lib/errors'
import { REPLAY_SIGNS_KEY } from '@/lib/replay-signs-api'

// The Inbox over apiRequest (ADR 0173): reads go through useApiQuery in
// hooks/use-inbox.ts under these keys; every write throws the server's own
// message, and nothing retries or substitutes data.

export const INBOX_KEY = ['inbox'] as const
export const INBOX_COUNT_KEY = ['inbox-count'] as const
export const INBOX_PATH = '/api/inbox'
export const INBOX_COUNT_PATH = '/api/inbox/count'

export type InboxGroupKind = 'passage' | 'underway' | 'off_passage' | 'no_time'

/** What Mate made of an item: where it most likely belongs. */
export type InboxKind = 'scenery' | 'document' | 'equipment'

/** The Inbox as the box lists it. `mate_sorting` says Mate is set up, so an item with no kind yet is waiting for it. */
export interface InboxList {
  items?: InboxItem[]
  mate_sorting?: boolean
}

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
  /** Mate's view of the item, or null until it has looked and for good when Mate is not set up. */
  suggested_kind?: InboxKind | null
  /** Why Mate could not sort the item, when it tried and failed. */
  classify_error?: string
  /** The phone's own position was over a nautical mile from the track: the clock or zone is off. */
  gps_mismatch: boolean
}

/** The small picture for an item: its thumbnail, else the photo itself, else none (a PDF shows an icon). */
export function inboxPreviewUrl(item: Pick<InboxItem, 'mime' | 'thumb_url' | 'content_url'>): string | undefined {
  return item.thumb_url ?? (item.mime.startsWith('image/') ? item.content_url : undefined)
}

// The file types the box takes as a photo on equipment or a job (ADR 0127).
// Keep this in step with photoMIMEs in backend/documents_enrich.go, the one
// list the box checks against.
const PHOTO_MIMES: readonly string[] = ['image/jpeg', 'image/png']

/** Whether the box takes this item as a photo for equipment or a job. */
export const isPhotoItem = (item: Pick<InboxItem, 'mime'>): boolean => PHOTO_MIMES.includes(item.mime)

/** What the phone knew about a file, sent beside it. A missing field is left out. */
export interface InboxUploadMeta {
  captureMs: number | null
  bearingDeg: number | null
  /** Which north the bearing is measured from, T true or M magnetic; left out when the phone did not say. */
  bearingRef?: 'T' | 'M' | null
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

export interface SuggestedTitles {
  titles: Record<string, string>
  errors: Record<string, string>
}

/** The outcome for one item of a filing call. */
export interface InboxFilingResult {
  id: string
  ok: boolean
  sign_id?: string
  document_id?: string
  error?: string
}

async function inboxChanged(): Promise<void> {
  await Promise.all([invalidateQueries(INBOX_KEY), invalidateQueries(INBOX_COUNT_KEY)])
}

/** The most items the box takes in one request. */
const MAX_BATCH = 500

function* inChunks<T>(all: readonly T[]): Generator<readonly T[]> {
  for (let i = 0; i < all.length; i += MAX_BATCH) yield all.slice(i, i + MAX_BATCH)
}

/** "1 photo", "3 photos". */
export const plural = (n: number, noun: string): string => `${n} ${noun}${n === 1 ? '' : 's'}`

/**
 * Files items in requests of at most MAX_BATCH. The Inbox and its count
 * refresh whatever happened; afterChange (the destination's lists) runs if at
 * least one item was filed, or if a request failed after it was sent (the box
 * may have committed it and lost the answer). A total refusal re-reads nothing
 * else.
 */
async function fileInChunks(
  path: string,
  extraBody: Record<string, unknown>,
  items: readonly object[],
  afterChange: () => void,
): Promise<InboxFilingResult[]> {
  const results: InboxFilingResult[] = []
  let requestFailed = false
  try {
    for (const chunk of inChunks(items)) {
      try {
        const res = await apiRequest<{ results: InboxFilingResult[] }>(path, { method: 'POST', json: { ...extraBody, items: chunk } })
        results.push(...res.results)
      } catch (err) {
        requestFailed = true
        // Earlier requests may already have filed items: hand their answers back with the error.
        throw results.length > 0 ? new InboxFilingError(err, results) : err
      }
    }
  } finally {
    // A failed refresh must not replace the error that got us here (or fail a filing that worked): log it.
    try {
      await inboxChanged()
    } catch (refreshErr) {
      console.error('Could not refresh the Inbox after filing', refreshErr)
    }
    try {
      if (requestFailed || results.some((r) => r.ok)) afterChange()
    } catch (refreshErr) {
      console.error('Could not refresh the destination after filing', refreshErr)
    }
  }
  return results
}

/** A later request failed after earlier ones went through: `results` holds what those filed. */
export class InboxFilingError extends Error {
  readonly results: InboxFilingResult[]
  constructor(cause: unknown, results: InboxFilingResult[]) {
    super(errorMessage(cause))
    this.name = 'InboxFilingError'
    this.results = results
  }
}

/**
 * Awaits a filing call and returns what was filed together with the error, if
 * any, so a caller can report both: results is empty when nothing was filed.
 */
export async function filingOutcome(call: Promise<InboxFilingResult[]>): Promise<{ results: InboxFilingResult[]; error: string | null }> {
  try {
    return { results: await call, error: null }
  } catch (err) {
    return { results: err instanceof InboxFilingError ? err.results : [], error: errorMessage(err) }
  }
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
    if (meta.bearingDeg !== null) {
      form.append('bearing_deg', String(meta.bearingDeg))
      if (meta.bearingRef) form.append('bearing_ref', meta.bearingRef)
    }
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
  fileToPassage: (items: readonly { id: string; title: string; t_ms?: number }[]): Promise<InboxFilingResult[]> =>
    fileInChunks('/api/inbox/file-to-passage', {}, items, () => {
      void invalidateQueries(REPLAY_SIGNS_KEY)
      invalidateDocumentLists({ counts: true })
    }),
  fileToDocuments: (folderId: string, items: readonly { id: string; title?: string }[]): Promise<InboxFilingResult[]> =>
    fileInChunks('/api/inbox/file-to-documents', { folder_id: folderId }, items, () => invalidateDocumentLists({ counts: true })),
  /** Photos only: a PDF comes back as an item that stayed, with the reason. */
  fileToEquipment: (equipmentId: string, items: readonly { id: string }[]): Promise<InboxFilingResult[]> =>
    fileInChunks('/api/inbox/file-to-equipment', { equipment_id: equipmentId }, items, () => {
      equipmentPhotosAdded(equipmentId)
      invalidateDocumentLists({ counts: true })
    }),
  /** Photos only, onto a job that is still open. */
  fileToOpenJob: (jobId: string, items: readonly { id: string }[]): Promise<InboxFilingResult[]> =>
    fileInChunks('/api/inbox/file-to-open-job', { job_id: jobId }, items, () => {
      openJobRecordApplied()
      invalidateDocumentLists({ counts: true })
    }),
  /**
   * A suggested title for each photo, by id: Mate's when it is set up, else the
   * nearest named place to the boat when the photo was taken (or the feature in
   * the direction it faced). Blank where there is none. `errors` gives the
   * reason a title could not be made, by id.
   */
  suggestedTitles: async (ids: readonly string[]): Promise<SuggestedTitles> => {
    const out: SuggestedTitles = { titles: {}, errors: {} }
    for (const chunk of inChunks(ids)) {
      const res = await apiRequest<Partial<SuggestedTitles>>(`/api/inbox/suggested-titles?ids=${chunk.map(encodeURIComponent).join(',')}`)
      Object.assign(out.titles, res.titles)
      Object.assign(out.errors, res.errors)
    }
    return out
  },
}
