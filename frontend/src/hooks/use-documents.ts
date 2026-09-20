import { useCallback, useEffect, useRef, useState } from 'react'
import { apiBaseUrl } from '@/config/api'

// ADR 0106 F1: the Documents panel's data layer - folder browsing, search,
// tags, and every document/folder write. No react-query (this app owns its
// own loading/error state throughout, same as use-alarm-rules.ts/use-routes.ts),
// and no shared store: one instance per <DocumentsPanel/>, parametrised by
// whichever folder is currently open, mirroring how e.g. useTideChart(stationId)
// is parametrised by its own caller-owned selection rather than owning it.

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

// documentEmbeddingsBackfillDryRunJSON, backend/documents_handlers.go -
// POST .../backfill?dry_run=1. Starts nothing; tokens_estimate is
// chars_pending/4, a rough scale-setting figure for the confirmation
// dialog, not a price quote.
export interface DocumentEmbeddingsBackfillDryRun {
  counts: DocumentEmbeddingCounts
  tokens_estimate: number
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

// Every write below goes through this: a non-2xx response throws with the
// server's own message (AGENTS.md fallback policy - no invented "something
// went wrong" standing in for a 409's actual "folder is not empty"/"folder
// move would create a cycle"/"folder name already exists"). 204 No Content
// (DELETE, move) has no body to parse.
async function submitJSON<T>(url: string, method: string, body?: unknown): Promise<T> {
  const response = await fetch(url, {
    method,
    headers: body !== undefined ? { 'Content-Type': 'application/json' } : undefined,
    body: body !== undefined ? JSON.stringify(body) : undefined,
  })
  if (!response.ok) {
    const payload = (await response.json().catch(() => ({}))) as { error?: string }
    throw new Error(payload.error ?? `HTTP ${response.status}`)
  }
  if (response.status === 204) return undefined as T
  return (await response.json()) as T
}

/**
 * `folderId`: the folder currently being browsed, null for the root. Owned
 * by the caller (DocumentsPanel), not this hook - see app-location.ts's
 * documentFolderId, which is what the panel actually seeds it from.
 */
export function useDocuments(folderId: string | null) {
  const [path, setPath] = useState<DocumentFolder[]>([])
  const [folders, setFolders] = useState<DocumentFolder[]>([])
  const [documents, setDocuments] = useState<DocumentRecord[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  const [tags, setTags] = useState<DocumentTagCount[]>([])
  const [selectedTag, setSelectedTag] = useState<string | null>(null)

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

  // The library-wide (not folder-scoped) semantic-search/embeddings status -
  // GET /api/documents/embeddings, ADR 0106 E1c. Loaded once on mount below,
  // then kept fresh by the same pending-poll effect `hasPending` already
  // uses (see that effect further down) while a backfill is running.
  const [embeddingsStatus, setEmbeddingsStatus] = useState<DocumentEmbeddingsStatus | null>(null)

  // Ordering guards (review finding): refresh() and search() can each be
  // called again while a previous call of the same kind is still in
  // flight - a 3s poll tick overlapping a post-delete refresh(), or a
  // fresh keystroke's search() overlapping the previous one's still-open
  // request. Without a check, whichever fetch happens to resolve LAST wins
  // even if it was the OLDER call, silently overwriting newer, more-correct
  // state (e.g. resurrecting a just-deleted row). Same idiom as
  // use-assistant-chat.ts's abortRef identity check and use-poi.ts's
  // controller/cancelled guards, but as a plain monotonically-incrementing
  // sequence number rather than a real AbortController - these are plain
  // GETs, not long-lived streams, so there is nothing to actually cancel
  // and no AbortError handling to thread through both the success and
  // catch paths; a sequence number is just as valid an implementation of
  // the same "does this call still matter" check.
  const refreshSeqRef = useRef(0)
  const searchSeqRef = useRef(0)
  // Same idiom, for refreshEmbeddingsStatus() below: its poll tick and its
  // own mount-time call can overlap the same way refresh()'s poll and a
  // post-write refresh() can.
  const embeddingsStatusSeqRef = useRef(0)

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
  const refresh = useCallback(async () => {
    const seq = (refreshSeqRef.current += 1)
    setLoading(true)
    try {
      const folderParams = new URLSearchParams()
      if (folderId) folderParams.set('parent', folderId)
      const folderQs = folderParams.toString()
      const res = await fetch(`${apiBaseUrl}/api/document-folders${folderQs ? `?${folderQs}` : ''}`)
      if (!res.ok) throw new Error(`HTTP ${res.status}`)
      const data = (await res.json()) as FolderListingResponse

      let nextDocuments = data.documents ?? []
      if (selectedTag) {
        const docParams = new URLSearchParams({ tag: selectedTag, folder: folderId ?? ROOT_FOLDER_PARAM })
        const docsRes = await fetch(`${apiBaseUrl}/api/documents?${docParams.toString()}`)
        if (!docsRes.ok) throw new Error(`HTTP ${docsRes.status}`)
        const docsData = (await docsRes.json()) as { documents?: DocumentRecord[] }
        nextDocuments = docsData.documents ?? []
      }

      // A newer refresh() started (and possibly already finished) while
      // this one was still in flight - applying this call's now-stale
      // result would silently overwrite whatever the newer call already
      // set (e.g. resurrecting a just-deleted row - review finding).
      if (seq !== refreshSeqRef.current) return

      setPath(data.path ?? [])
      setFolders(data.folders ?? [])
      setDocuments(nextDocuments)
      setError(null)
    } catch (err) {
      if (seq !== refreshSeqRef.current) return
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      if (seq === refreshSeqRef.current) setLoading(false)
    }
  }, [folderId, selectedTag])

  useEffect(() => { void refresh() }, [refresh])

  const refreshTags = useCallback(async () => {
    try {
      const res = await fetch(`${apiBaseUrl}/api/documents/tags`)
      if (!res.ok) return
      const data = (await res.json()) as DocumentTagCount[]
      setTags(Array.isArray(data) ? data : [])
    } catch {
      // The tag filter list is a convenience, not load-bearing - a failure
      // here must not blank the folder view it sits beside.
    }
  }, [])

  useEffect(() => { void refreshTags() }, [refreshTags])

  // A convenience overlay, not load-bearing - same reasoning as refreshTags
  // just above: a failure here (offline, a 500) must not disturb the
  // folder/search view it sits beside, so it fails silently rather than
  // populating `error`. embeddingsStatus simply stays at whatever it last
  // was (null on the very first failure, which renders the same as
  // enabled:false - no status line at all).
  const refreshEmbeddingsStatus = useCallback(async () => {
    const seq = (embeddingsStatusSeqRef.current += 1)
    try {
      const res = await fetch(`${apiBaseUrl}/api/documents/embeddings`)
      if (!res.ok) return
      const data = (await res.json()) as DocumentEmbeddingsStatus
      // Same ordering guard as refresh()/search() above.
      if (seq !== embeddingsStatusSeqRef.current) return
      setEmbeddingsStatus(data)
    } catch {
      // See comment above the function.
    }
  }, [])

  useEffect(() => { void refreshEmbeddingsStatus() }, [refreshEmbeddingsStatus])

  // Polls the whole folder view (not just search results) every 3s while
  // any document currently shown is `pending`, and stops the instant none
  // are - keyed on the derived boolean, not on `documents` itself, so a poll
  // response that leaves the pending count unchanged doesn't tear down and
  // restart the timer (mirrors use-document-uploads.ts's own hasPending gate).
  //
  // The same interval also covers embedding work in progress
  // (embeddingsBusy): unrelated to hasPending - embedding is a library-wide
  // background pass, not scoped to whichever folder happens to be open - but
  // it wants the identical "poll every 3s, stop the instant there's nothing
  // left to poll for" shape, so one timer serves both rather than running a
  // second one alongside it. Busy is not just a running backfill: the
  // automatic pass over already-consented documents has no flag of its own,
  // it simply works through whatever chunks are outstanding, so its own
  // pending count is the only signal there is that the row is about to
  // change. That count, not the library-wide one: anything uploaded while
  // Mate was off is pending library-wide forever, and gating on that would
  // poll this endpoint every three seconds for as long as the panel is open,
  // full-scanning the chunk table on every tick for work no background pass
  // is ever going to do.
  const hasPending = documents.some((d) => d.status === 'pending')
  const backfillRunning = embeddingsStatus?.backfill.running ?? false
  const embeddingsBusy =
    backfillRunning || ((embeddingsStatus?.enabled ?? false) && (embeddingsStatus?.counts.chunks_pending_auto ?? 0) > 0)
  useEffect(() => {
    if (!hasPending && !embeddingsBusy) return
    const id = setInterval(() => {
      // Tags come back with the document: enrichment writes its suggested
      // ones as the document finishes, so the filter row has to follow the
      // same poll or it keeps showing the tags from before anything was
      // read.
      // The embeddings status is refreshed alongside a pending document too,
      // not only when it already says it is busy: the gate above is derived
      // from the very value this poll refreshes, so a panel that mounted with
      // nothing outstanding would otherwise never learn that a document
      // finishing has given the automatic pass work to do, and the row would
      // sit on "up to date" until a remount.
      if (hasPending) { void refresh(); void refreshTags(); void refreshEmbeddingsStatus() }
      else if (embeddingsBusy) void refreshEmbeddingsStatus()
    }, POLL_INTERVAL_MS)
    return () => clearInterval(id)
  }, [hasPending, embeddingsBusy, refresh, refreshTags, refreshEmbeddingsStatus])

  // Debouncing is the caller's job (documents-panel.tsx) - this just issues
  // one search per call. An empty/whitespace query clears results locally
  // with no request, so clearing the search box doesn't leave a stale
  // result set with no query behind it.
  const search = useCallback(async (query: string, opts?: { allFolders?: boolean; tag?: string | null }) => {
    const trimmed = query.trim()
    if (trimmed === '') {
      // Also supersedes whatever search is still in flight - see
      // clearSearch's own comment below.
      searchSeqRef.current += 1
      setSearchResults(null)
      setSearchError(null)
      setSemanticProblem(null)
      return
    }
    const seq = (searchSeqRef.current += 1)
    setSearching(true)
    try {
      const params = new URLSearchParams({ q: trimmed, recursive: 'true' })
      params.set('folder', opts?.allFolders ? ROOT_FOLDER_PARAM : (folderId ?? ROOT_FOLDER_PARAM))
      const tag = opts?.tag !== undefined ? opts.tag : selectedTag
      if (tag) params.set('tag', tag)
      const res = await fetch(`${apiBaseUrl}/api/documents?${params.toString()}`)
      if (!res.ok) throw new Error(`HTTP ${res.status}`)
      const data = (await res.json()) as { results?: DocumentSearchResult[]; semantic_problem?: string }

      // Same ordering guard as refresh() above - an older search's
      // late-arriving result must not clobber a newer one's (or an
      // explicit clearSearch()'s) already-applied state.
      if (seq !== searchSeqRef.current) return

      setSearchResults(data.results ?? [])
      setSearchError(null)
      // Present only when semantic search was configured and could not run
      // for THIS query (a failed query embedding, a corrupt vector row) -
      // absent, not empty-string, when semantic search is simply switched
      // off (listDocumentsHandler, backend/documents_handlers.go), so a
      // fresh successful search always overwrites whatever the previous one
      // left here, clearing it the instant semantic search is healthy again.
      setSemanticProblem(data.semantic_problem ?? null)
    } catch (err) {
      if (seq !== searchSeqRef.current) return
      setSearchError(err instanceof Error ? err.message : String(err))
      setSemanticProblem(null)
    } finally {
      if (seq === searchSeqRef.current) setSearching(false)
    }
  }, [folderId, selectedTag])

  const clearSearch = useCallback(() => {
    // Supersedes whatever search is in flight - without bumping this too,
    // an in-flight search that was about to land could still repopulate
    // results right after the operator explicitly cleared the box.
    searchSeqRef.current += 1
    setSearchResults(null)
    setSearchError(null)
    setSemanticProblem(null)
  }, [])

  const createFolder = useCallback(async (name: string, parentId: string | null) => {
    const folder = await submitJSON<DocumentFolder>(`${apiBaseUrl}/api/document-folders`, 'POST', { name, parent_id: parentId })
    await refresh()
    return folder
  }, [refresh])

  const renameFolder = useCallback(async (id: string, name: string) => {
    const folder = await submitJSON<DocumentFolder>(`${apiBaseUrl}/api/document-folders/${encodeURIComponent(id)}`, 'PATCH', { name })
    await refresh()
    return folder
  }, [refresh])

  const moveFolder = useCallback(async (id: string, parentId: string | null) => {
    const folder = await submitJSON<DocumentFolder>(`${apiBaseUrl}/api/document-folders/${encodeURIComponent(id)}`, 'PATCH', { parent_id: parentId })
    await refresh()
    return folder
  }, [refresh])

  const deleteFolder = useCallback(async (id: string) => {
    await submitJSON<void>(`${apiBaseUrl}/api/document-folders/${encodeURIComponent(id)}`, 'DELETE')
    await refresh()
  }, [refresh])

  const patchDocument = useCallback(async (id: string, patch: DocumentPatch) => {
    const doc = await submitJSON<DocumentRecord>(`${apiBaseUrl}/api/documents/${encodeURIComponent(id)}`, 'PATCH', patch)
    await refresh()
    await refreshTags()
    return doc
  }, [refresh, refreshTags])

  const deleteDocument = useCallback(async (id: string) => {
    await submitJSON<void>(`${apiBaseUrl}/api/documents/${encodeURIComponent(id)}`, 'DELETE')
    await refresh()
  }, [refresh])

  const reindexDocument = useCallback(async (id: string) => {
    const outcome = await submitJSON<ReindexOutcome>(`${apiBaseUrl}/api/documents/${encodeURIComponent(id)}/reindex`, 'POST')
    await refresh()
    return outcome
  }, [refresh])

  const moveDocuments = useCallback(async (ids: string[], targetFolderId: string | null) => {
    await submitJSON<void>(`${apiBaseUrl}/api/documents/move`, 'POST', { ids, folder_id: targetFolderId })
    await refresh()
  }, [refresh])

  // Starts nothing - the dry run just reports what a real backfill would
  // do, for the confirmation dialog (documents-panel.tsx) to show the
  // operator before any text actually goes to OpenRouter. Goes through
  // submitJSON like every other write here, so a readiness failure (a
  // broken settings file, a secrets-store read error - the one way this
  // particular call can fail; the handler itself starts no work either way)
  // surfaces as the server's own message rather than a generic one.
  const dryRunEmbeddingsBackfill = useCallback(async () => {
    return submitJSON<DocumentEmbeddingsBackfillDryRun>(`${apiBaseUrl}/api/documents/embeddings/backfill?dry_run=1`, 'POST')
  }, [])

  // The real thing - POSTs with no ?dry_run, which is itself the operator's
  // consent for whatever text is currently unembedded to reach OpenRouter
  // (documents_embed.go's own comment on StartBackfill). Deliberately does
  // NOT go through submitJSON: a 409 ("a backfill is already running") is
  // not a failure the operator caused by clicking the button - it means the
  // exact state they wanted (a backfill in progress) already holds, so its
  // response is folded into embeddingsStatus the same as a plain 200 rather
  // than thrown as an error. Any other non-2xx (400 semantic search off,
  // 500) still throws the server's own message.
  const startEmbeddingsBackfill = useCallback(async () => {
    const response = await fetch(`${apiBaseUrl}/api/documents/embeddings/backfill`, { method: 'POST' })
    const body = (await response.json().catch(() => ({}))) as { error?: string; backfill?: DocumentBackfillStatus }
    if (!response.ok && response.status !== 409) {
      throw new Error(body.error ?? `HTTP ${response.status}`)
    }
    if (body.backfill) {
      const backfill = body.backfill
      setEmbeddingsStatus((prev) => (prev ? { ...prev, backfill } : prev))
    }
  }, [])

  return {
    path,
    folders,
    documents,
    loading,
    error,
    refresh,

    tags,
    selectedTag,
    setSelectedTag,

    searchResults,
    searching,
    searchError,
    semanticProblem,
    search,
    clearSearch,

    embeddingsStatus,
    dryRunEmbeddingsBackfill,
    startEmbeddingsBackfill,

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
// actually resolved to anything yet) clears state and fetches nothing, the
// same shape useDocuments(folderId) already gives folderId.
export function useDocument(id: string | null): {
  document: DocumentRecord | null
  loading: boolean
  error: string | null
  refresh: () => Promise<void>
  patch: (patch: DocumentPatch) => Promise<DocumentRecord>
} {
  const [document, setDocument] = useState<DocumentRecord | null>(null)
  const [loading, setLoading] = useState(id !== null)
  const [error, setError] = useState<string | null>(null)

  // Same ordering guard as useDocuments' own refresh() above, and for the
  // same reason: a GET for an id the operator has since navigated away from
  // (or a second refresh() fired before the first lands) must not have its
  // late reply overwrite whatever a newer call already set.
  const seqRef = useRef(0)

  const refresh = useCallback(async () => {
    if (id === null) {
      seqRef.current += 1
      setDocument(null)
      setError(null)
      setLoading(false)
      return
    }
    const seq = (seqRef.current += 1)
    setLoading(true)
    try {
      const res = await fetch(`${apiBaseUrl}/api/documents/${encodeURIComponent(id)}`)
      if (!res.ok) {
        // AGENTS.md fallback policy: the server's own message (e.g.
        // documentByIDHandler's "document not found"), never an invented
        // "something went wrong" standing in for it.
        const payload = (await res.json().catch(() => ({}))) as { error?: string }
        throw new Error(payload.error ?? `HTTP ${res.status}`)
      }
      const data = (await res.json()) as DocumentRecord
      if (seq !== seqRef.current) return
      setDocument(data)
      setError(null)
    } catch (err) {
      if (seq !== seqRef.current) return
      setDocument(null)
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      if (seq === seqRef.current) setLoading(false)
    }
  }, [id])

  useEffect(() => { void refresh() }, [refresh])

  // Review finding: this hook fetched once per id and never again, so the
  // Details page never saw a pending document finish reading - it sat on
  // "Reading…"/`--`/"$0.0000"/"Not yet" forever while the listing behind it
  // kept moving. Same POLL_INTERVAL_MS and the same "keyed on the derived
  // boolean, not the object" reasoning as useDocuments' own hasPending
  // effect above: a poll response that leaves `pending` true must not tear
  // down and restart the timer.
  const pending = document?.status === 'pending'
  useEffect(() => {
    if (!pending) return
    const intervalId = setInterval(() => { void refresh() }, POLL_INTERVAL_MS)
    return () => clearInterval(intervalId)
  }, [pending, refresh])

  // Goes through the same submitJSON every other write in this file uses, so
  // a rejection (a duplicate title, a folder that no longer exists) carries
  // the server's own message. Deliberately not wrapped in a try/catch here:
  // the caller (document-details-page.tsx's Save) is the one that has to
  // show the failure and keep the draft intact, so the rejection has to
  // reach it rather than be swallowed at this layer.
  const patch = useCallback(async (patchBody: DocumentPatch) => {
    // AGENTS.md fallback policy: no id to PATCH is a caller bug (the Details
    // page never renders its Save button before a document has loaded), not
    // a case to paper over with a request to a malformed URL.
    if (id === null) throw new Error('useDocument: no document id to patch')
    const updated = await submitJSON<DocumentRecord>(`${apiBaseUrl}/api/documents/${encodeURIComponent(id)}`, 'PATCH', patchBody)
    setDocument(updated)
    return updated
  }, [id])

  return { document, loading, error, refresh, patch }
}
