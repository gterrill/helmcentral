import { useCallback, useEffect, useRef, useState } from 'react'
import {
  apiRequest,
  getQueryData,
  invalidateQueries,
  setQueryData,
  useApiQuery,
} from '@/lib/api-query'

// ADR 0106 F1: the Documents panel's data layer - folder browsing, search,
// tags, and every document/folder write. Reads sit on the shared query store
// (ADR 0173), so the panel, the Move dialog's folder picker and the Details
// page see one copy of each listing, and a write here invalidates it for all
// of them. The folder being browsed is owned by the caller, mirroring how
// e.g. useTideChart(stationId) is parametrised by its own caller-owned
// selection.

const POLL_INTERVAL_MS = 3000

/** documentsRootFolderSentinel, backend/documents_store.go - the literal the
 * API takes for "the top level" wherever a folder id would otherwise go. */
const ROOT_FOLDER_PARAM = 'root'

export interface DocumentTag {
  tag: string
  source: 'operator' | 'suggested'
}

export interface DocumentRecord {
  id: string
  sha256: string
  folder_id: string | null
  filename: string
  title: string
  notes: string
  mime: string
  size_bytes: number
  page_count: number
  summary: string
  status: 'pending' | 'indexed' | 'failed'
  stage: string
  indexed_with: string
  error: string
  index_model: string
  index_cost_usd: number
  tags: DocumentTag[]
  created_at: string
  updated_at: string
  indexed_at: string | null
  // Revision "one panel, not three" (2026-09-20): documentJSON
  // (backend/documents_handlers.go) has always sent these five fields
  // unconditionally on every document, including a plain kind='file' row -
  // use-notes.ts's own NoteRecord already types them, this listing just
  // never had a reason to read them until Documents itself started
  // rendering the note-type facet and icon column. Not a backend change:
  // the wire shape is unchanged, only this type catches up to it.
  kind: string
  note_type: string
  note_type_source: string
  pinned: boolean
  sort_index: number
}

export interface DocumentFolder {
  id: string
  name: string
  parent_id: string | null
  created_at?: string
  updated_at?: string
}

export interface DocumentSearchResult {
  document_id: string
  filename: string
  title: string
  status: string
  page: number
  snippet: string
  folder_id: string | null
}

export interface DocumentTagCount {
  tag: string
  count: number
}

// documentEmbeddingCounts, backend/documents_store.go - a one-call snapshot
// of how much of the chunk library is embedded under the currently
// configured model, and how much backfill work (and rough OpenRouter spend,
// via chars_pending) is left.
export interface DocumentEmbeddingCounts {
  chunks_total: number
  chunks_embedded: number
  chunks_stale: number
  chunks_pending: number
  /** The automatic pass's own queue (enrich=1 documents only).
   *  chunks_pending above is library-wide and includes documents uploaded
   *  while Mate was off, which only an explicit backfill will ever reach - so
   *  it is the right number for the backfill offer and the wrong one for any
   *  "is something happening right now" gate, because it never falls on its
   *  own. */
  chunks_pending_auto: number
  chars_pending: number
}

// documentBackfillStatus, backend/documents_embed.go - chunks_embedded
// counts only the CURRENT (or most recent) backfill run, not a lifetime
// total. last_error is the most recent batch failure, if any: the backfill
// keeps retrying rather than stopping, so its presence is a warning to
// show beside the progress, not a reason to treat the run as failed.
export interface DocumentBackfillStatus {
  running: boolean
  chunks_embedded: number
  started_at: string
  last_error?: string
}

// documentEmbeddingsStatusJSON, backend/documents_handlers.go -
// GET /api/documents/embeddings. `enabled` is false whenever `problem` is
// set, including the ordinary case of no embedding model configured at all
// (the operator's own choice to run FTS5-only) - so a caller can branch on
// `enabled` alone without inspecting `problem` for that case.
export interface DocumentEmbeddingsStatus {
  enabled: boolean
  model: string
  dimensions: number
  problem?: string
  counts: DocumentEmbeddingCounts
  backfill: DocumentBackfillStatus
}

export interface ReindexOutcome {
  document: DocumentRecord
  enrich: boolean
  problem: string
}

export interface DocumentPatch {
  title?: string
  notes?: string
  tags?: string[]
  folder_id?: string | null
}

interface FolderListingResponse {
  path?: DocumentFolder[]
  folders?: DocumentFolder[]
  documents?: DocumentRecord[]
}

const FOLDERS_PATH = '/api/document-folders'
const FOLDER_TREE_PATH = '/api/document-folders/tree'
const DOCUMENTS_PATH = '/api/documents'
const TAGS_PATH = '/api/documents/tags'
const EMBEDDINGS_PATH = '/api/documents/embeddings'

const NO_DOCUMENTS: DocumentRecord[] = []
const NO_TAGS: DocumentTagCount[] = []
const NO_FOLDERS: DocumentFolder[] = []

function folderListingPath(parent: string | null): string {
  if (!parent) return FOLDERS_PATH
  return `${FOLDERS_PATH}?${new URLSearchParams({ parent }).toString()}`
}

function folderListingKey(parent: string | null) {
  return [FOLDERS_PATH, { parent }]
}

// The flat, tag-filtered listing of one folder's own documents. Not recursive:
// search is the one place descendants are pulled in.
function taggedListingKey(tag: string | null, folderId: string | null) {
  return [DOCUMENTS_PATH, { tag, folder: folderId }]
}

function taggedListingPath(tag: string, folderId: string | null): string {
  return `${DOCUMENTS_PATH}?${new URLSearchParams({ tag, folder: folderId ?? ROOT_FOLDER_PARAM }).toString()}`
}

function documentPath(id: string): string {
  return `${DOCUMENTS_PATH}/${encodeURIComponent(id)}`
}

function hasPendingDocument(documents: DocumentRecord[] | undefined): boolean {
  return documents?.some((d) => d.status === 'pending') ?? false
}

// What every document or folder write refreshes: the folder listings (which
// carry the documents), the flat tag-filtered listing and the tag counts.
// `counts` adds the folder tree, whose per-folder counts move when a document
// is filed, removed or added.
export function invalidateDocumentLists(opts: { counts: boolean }) {
  invalidateQueries([FOLDERS_PATH])
  invalidateQueries([DOCUMENTS_PATH])
  invalidateQueries([TAGS_PATH])
  if (opts.counts) invalidateQueries([FOLDER_TREE_PATH])
}

/**
 * The documents the Documents listing shows for a folder, as the server
 * returned them: the folder's own, or those carrying `tag` when one is
 * filtered on. The same queries the listing reads, so a Details page opened
 * from it hits the cache. `enabled` false fetches nothing.
 */
export function useDocumentListing(folderId: string | null, tag: string | null, enabled = true) {
  const folder = useApiQuery({
    queryKey: folderListingKey(folderId),
    path: folderListingPath(folderId),
    enabled: enabled && tag === null,
    select: (raw: FolderListingResponse) => raw.documents ?? NO_DOCUMENTS,
  })
  const tagged = useApiQuery({
    queryKey: taggedListingKey(tag, folderId),
    path: taggedListingPath(tag ?? '', folderId),
    enabled: enabled && tag !== null,
    select: (raw: { documents?: DocumentRecord[] }) => raw.documents ?? NO_DOCUMENTS,
  })
  const data = tag === null ? folder.data : tagged.data
  return { documents: data ?? NO_DOCUMENTS, loaded: data !== undefined }
}

/**
 * `folderId`: the folder currently being browsed, null for the root. Owned
 * by the caller (DocumentsPanel), not this hook - see app-location.ts's
 * documentFolderId, which is what the panel actually seeds it from.
 */
export function useDocuments(folderId: string | null, initialTag: string | null = null) {
  const [selectedTag, setSelectedTag] = useState<string | null>(initialTag)

  const [searchResults, setSearchResults] = useState<DocumentSearchResult[] | null>(null)
  const [searching, setSearching] = useState(false)
  const [searchError, setSearchError] = useState<string | null>(null)
  // A search response's semantic_problem (backend/documents_search.go's
  // hybridDocumentSearch, surfaced by listDocumentsHandler only when
  // semantic search was configured and failed to run for THIS query - never
  // when it's simply switched off) - a quiet, non-fatal companion to
  // searchResults, not an error: the results themselves are complete,
  // correct keyword hits. Cleared on the next successful search and on an
  // explicit clearSearch(), same lifecycle as searchError.
  const [semanticProblem, setSemanticProblem] = useState<string | null>(null)

  // The folder-browse endpoint (path/subfolders, and - with no tag filter -
  // the documents directly in this folder) has no `tag` parameter of its
  // own (it is a single-level listing, not the flat/searchable one), so a
  // selected tag switches the *document* half of this view over to the flat
  // GET /api/documents?tag=&folder=&recursive=0 endpoint instead, scoped to
  // this folder non-recursively - "folders first, then documents" still
  // shows subfolders under a tag filter, only the document rows are
  // filtered. recursive stays false here on purpose: search (below) is the
  // one place descendants are pulled in, matching the plan's "search within
  // the current folder, recursive" vs. plain browsing being one level.
  const listingKey = folderListingKey(folderId)
  const tagKey = taggedListingKey(selectedTag, folderId)

  // Polls the whole folder view every 3s while any document currently shown
  // is `pending`, and stops the instant none are. The same interval covers
  // embedding work in progress: unrelated to hasPending - embedding is a
  // library-wide background pass, not scoped to whichever folder happens to
  // be open - but it wants the identical "poll every 3s, stop the instant
  // there's nothing left to poll for" shape. Busy is not just a running
  // backfill: the automatic pass over already-consented documents has no
  // flag of its own, it simply works through whatever chunks are
  // outstanding, so its own pending count is the only signal there is that
  // the row is about to change. That count, not the library-wide one:
  // anything uploaded while Mate was off is pending library-wide forever,
  // and gating on that would poll this endpoint every three seconds for as
  // long as the panel is open, full-scanning the chunk table on every tick
  // for work no background pass is ever going to do.
  //
  // The interval is computed from the cached answer, read before the hooks
  // below: a response that changes it re-renders this component.
  const shownDocuments = selectedTag
    ? getQueryData<{ documents?: DocumentRecord[] }>(tagKey)?.documents
    : getQueryData<FolderListingResponse>(listingKey)?.documents
  const hasPending = hasPendingDocument(shownDocuments)
  const knownEmbeddings = getQueryData<DocumentEmbeddingsStatus>([EMBEDDINGS_PATH])
  const embeddingsBusy =
    (knownEmbeddings?.backfill.running ?? false) ||
    ((knownEmbeddings?.enabled ?? false) && (knownEmbeddings?.counts.chunks_pending_auto ?? 0) > 0)
  const pendingPoll = hasPending ? POLL_INTERVAL_MS : undefined

  const listing = useApiQuery({
    queryKey: listingKey,
    path: folderListingPath(folderId),
    refetchInterval: pendingPoll,
    select: (raw: FolderListingResponse) => ({
      path: raw.path ?? [],
      folders: raw.folders ?? [],
      documents: raw.documents ?? NO_DOCUMENTS,
    }),
  })
  const tagged = useApiQuery({
    queryKey: tagKey,
    path: taggedListingPath(selectedTag ?? '', folderId),
    enabled: selectedTag !== null,
    refetchInterval: pendingPoll,
    select: (raw: { documents?: DocumentRecord[] }) => raw.documents ?? NO_DOCUMENTS,
  })

  // Tags come back with the document: enrichment writes its suggested ones as
  // the document finishes, so the filter row follows the same poll or it
  // keeps showing the tags from before anything was read. A failure here is
  // not load-bearing - it must not blank the folder view it sits beside - so
  // `error` is never read.
  const tagCounts = useApiQuery({
    queryKey: [TAGS_PATH],
    path: TAGS_PATH,
    refetchInterval: pendingPoll,
    select: (raw: DocumentTagCount[]) => (Array.isArray(raw) ? raw : NO_TAGS),
  })

  // A convenience overlay, same reasoning as the tags: an offline or a 500
  // must not disturb the folder/search view it sits beside, so its error is
  // not surfaced. The status simply stays at whatever it last was (null
  // before the first answer, which renders like enabled:false - no status
  // line at all). Polled alongside a pending document too, not only when it
  // already says it is busy: the gate is derived from this very value, so a
  // panel that mounted with nothing outstanding would otherwise never learn
  // that a document finishing has given the automatic pass work to do.
  const embeddings = useApiQuery<DocumentEmbeddingsStatus>({
    queryKey: [EMBEDDINGS_PATH],
    path: EMBEDDINGS_PATH,
    refetchInterval: hasPending || embeddingsBusy ? POLL_INTERVAL_MS : undefined,
  })

  // The poll that sees the last pending document finish also stops the tag
  // and embeddings polls, which could leave them one step behind: enrichment
  // writes its suggested tags as the document finishes. One more read each.
  const wasPending = useRef(false)
  useEffect(() => {
    if (wasPending.current && !hasPending) {
      invalidateQueries([TAGS_PATH])
      invalidateQueries([EMBEDDINGS_PATH])
    }
    wasPending.current = hasPending
  }, [hasPending])

  const { refetch: refetchListing } = listing
  const { refetch: refetchTagged } = tagged
  const refresh = useCallback(async () => {
    await Promise.all([refetchListing(), refetchTagged()])
  }, [refetchListing, refetchTagged])

  // Search keeps the previous results on screen while the next keystroke's
  // request is out (the panel flips between browse and search on
  // `searchResults !== null`), which a keyed read does not do, so it stays a
  // request of its own. Debouncing is the caller's job (documents-panel.tsx).
  // Only the latest call's answer is applied; an empty/whitespace query
  // clears results with no request.
  const searchSeq = useRef(0)
  const search = useCallback(async (query: string, opts?: { allFolders?: boolean; tag?: string | null }) => {
    const trimmed = query.trim()
    if (trimmed === '') {
      searchSeq.current += 1
      setSearchResults(null)
      setSearchError(null)
      setSemanticProblem(null)
      return
    }
    const seq = (searchSeq.current += 1)
    setSearching(true)
    try {
      const params = new URLSearchParams({ q: trimmed, recursive: 'true' })
      params.set('folder', opts?.allFolders ? ROOT_FOLDER_PARAM : (folderId ?? ROOT_FOLDER_PARAM))
      const tag = opts?.tag !== undefined ? opts.tag : selectedTag
      if (tag) params.set('tag', tag)
      const data = await apiRequest<{ results?: DocumentSearchResult[]; semantic_problem?: string }>(
        `${DOCUMENTS_PATH}?${params.toString()}`,
      )
      if (seq !== searchSeq.current) return
      setSearchResults(data.results ?? [])
      setSearchError(null)
      // Present only when semantic search was configured and could not run
      // for THIS query - absent, not empty-string, when it is simply
      // switched off, so a fresh successful search always overwrites
      // whatever the previous one left here.
      setSemanticProblem(data.semantic_problem ?? null)
    } catch (err) {
      if (seq !== searchSeq.current) return
      setSearchError(err instanceof Error ? err.message : String(err))
      setSemanticProblem(null)
    } finally {
      if (seq === searchSeq.current) setSearching(false)
    }
  }, [folderId, selectedTag])

  const clearSearch = useCallback(() => {
    // Supersedes whatever search is in flight, so one about to land cannot
    // repopulate results right after the operator cleared the box.
    searchSeq.current += 1
    setSearchResults(null)
    setSearchError(null)
    setSemanticProblem(null)
  }, [])

  // Every write below throws the server's own message (AGENTS.md fallback
  // policy - a 409's actual "folder is not empty"/"folder move would create
  // a cycle"/"folder name already exists", not an invented "something went
  // wrong"), then refreshes what it changed for every reader of the store.
  const createFolder = useCallback(async (name: string, parentId: string | null) => {
    const folder = await apiRequest<DocumentFolder>(FOLDERS_PATH, { method: 'POST', json: { name, parent_id: parentId } })
    invalidateDocumentLists({ counts: true })
    return folder
  }, [])

  const renameFolder = useCallback(async (id: string, name: string) => {
    const folder = await apiRequest<DocumentFolder>(`${FOLDERS_PATH}/${encodeURIComponent(id)}`, { method: 'PATCH', json: { name } })
    invalidateDocumentLists({ counts: true })
    return folder
  }, [])

  const moveFolder = useCallback(async (id: string, parentId: string | null) => {
    const folder = await apiRequest<DocumentFolder>(`${FOLDERS_PATH}/${encodeURIComponent(id)}`, { method: 'PATCH', json: { parent_id: parentId } })
    invalidateDocumentLists({ counts: true })
    return folder
  }, [])

  const deleteFolder = useCallback(async (id: string) => {
    await apiRequest<void>(`${FOLDERS_PATH}/${encodeURIComponent(id)}`, { method: 'DELETE' })
    invalidateDocumentLists({ counts: true })
  }, [])

  const patchDocument = useCallback(async (id: string, patch: DocumentPatch) => {
    const doc = await apiRequest<DocumentRecord>(documentPath(id), { method: 'PATCH', json: patch })
    setQueryData([documentPath(id)], doc)
    invalidateDocumentLists({ counts: patch.folder_id !== undefined })
    return doc
  }, [])

  const deleteDocument = useCallback(async (id: string) => {
    await apiRequest<void>(documentPath(id), { method: 'DELETE' })
    invalidateDocumentLists({ counts: true })
  }, [])

  const reindexDocument = useCallback(async (id: string) => {
    const outcome = await apiRequest<ReindexOutcome>(`${documentPath(id)}/reindex`, { method: 'POST' })
    setQueryData([documentPath(id)], outcome.document)
    invalidateDocumentLists({ counts: false })
    return outcome
  }, [])

  const moveDocuments = useCallback(async (ids: string[], targetFolderId: string | null) => {
    await apiRequest<void>(`${DOCUMENTS_PATH}/move`, { method: 'POST', json: { ids, folder_id: targetFolderId } })
    invalidateDocumentLists({ counts: true })
  }, [])

  return {
    path: listing.data?.path ?? NO_FOLDERS,
    folders: listing.data?.folders ?? NO_FOLDERS,
    documents: (selectedTag ? tagged.data : listing.data?.documents) ?? NO_DOCUMENTS,
    loading: listing.isLoading || (selectedTag !== null && tagged.isLoading),
    error: listing.error?.message ?? tagged.error?.message ?? null,
    refresh,

    tags: tagCounts.data ?? NO_TAGS,
    selectedTag,
    setSelectedTag,

    searchResults,
    searching,
    searchError,
    semanticProblem,
    search,
    clearSearch,

    embeddingsStatus: embeddings.data ?? null,

    createFolder,
    renameFolder,
    moveFolder,
    deleteFolder,

    patchDocument,
    deleteDocument,
    reindexDocument,
    moveDocuments,
  }
}

// ADR 0115 §2: the Details page (document-details-page.tsx) edits
// exactly one document, resolved by the `/documents/<id>` route rather than
// found in whatever folder happens to be open (a Mate attachment chip or a
// stale breadcrumb can link to a document filed anywhere) - so it gets its
// own small hook rather than a second consumer digging through useDocuments'
// folder-scoped `documents` array. `id === null` (the Details route not
// actually resolved to anything yet) fetches nothing, the same shape
// useDocuments(folderId) already gives folderId.
export function useDocument(id: string | null): {
  document: DocumentRecord | null
  loading: boolean
  error: string | null
  refresh: () => Promise<void>
  patch: (patch: DocumentPatch) => Promise<DocumentRecord>
  /** Files the document into a folder (null for the top level) and reloads it. */
  move: (folderId: string | null) => Promise<void>
} {
  const path = documentPath(id ?? '')
  const key = [path]
  // Review finding: a document fetched once per id never showed a pending
  // one finish reading - the Details page sat on "Reading…"/`--`/"$0.0000"/
  // "Not yet" while the listing behind it kept moving. Polls while the
  // cached copy says pending; the interval is read before the hook, as in
  // useDocuments.
  const pending = id !== null && getQueryData<DocumentRecord>(key)?.status === 'pending'
  const query = useApiQuery<DocumentRecord>({
    queryKey: key,
    path,
    enabled: id !== null,
    refetchInterval: pending ? POLL_INTERVAL_MS : undefined,
  })
  const { refetch } = query

  // Deliberately not wrapped in a try/catch here: the caller
  // (document-details-page.tsx's Save) is the one that has to show the
  // failure and keep the draft intact, so a rejection (a duplicate title, a
  // folder that no longer exists) has to reach it with the server's message.
  const patch = useCallback(async (patchBody: DocumentPatch) => {
    // AGENTS.md fallback policy: no id to PATCH is a caller bug (the Details
    // page never renders its Save button before a document has loaded), not
    // a case to paper over with a request to a malformed URL.
    if (id === null) throw new Error('useDocument: no document id to patch')
    const updated = await apiRequest<DocumentRecord>(documentPath(id), { method: 'PATCH', json: patchBody })
    setQueryData([documentPath(id)], updated)
    // The listings show title, tags and folder; notes alone appear in none.
    if (patchBody.folder_id !== undefined || patchBody.title !== undefined || patchBody.tags !== undefined) {
      invalidateDocumentLists({ counts: patchBody.folder_id !== undefined })
    }
    return updated
  }, [id])

  // Same endpoint the Documents listing's Move uses. A rejection reaches the
  // caller, which shows the server's message.
  const move = useCallback(async (folderId: string | null) => {
    if (id === null) throw new Error('useDocument: no document id to move')
    await apiRequest<void>(`${DOCUMENTS_PATH}/move`, { method: 'POST', json: { ids: [id], folder_id: folderId } })
    await refetch()
    invalidateDocumentLists({ counts: true })
  }, [id, refetch])

  return {
    document: query.data ?? null,
    loading: query.isLoading,
    error: query.error?.message ?? null,
    refresh: refetch,
    patch,
    move,
  }
}

/** The folder chain from the top level down to `folderId`, for a breadcrumb.
 * Empty for the top level. A failed lookup is reported, not hidden. */
export function useFolderPath(folderId: string | null): { path: DocumentFolder[]; error: string | null } {
  const query = useApiQuery({
    queryKey: [FOLDERS_PATH, { parent: folderId }],
    path: folderListingPath(folderId),
    enabled: folderId !== null,
    select: (raw: FolderListingResponse) => raw.path ?? NO_FOLDERS,
  })
  return { path: query.data ?? NO_FOLDERS, error: query.error?.message ?? null }
}
