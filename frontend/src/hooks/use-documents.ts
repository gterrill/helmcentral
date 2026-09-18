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

  // Polls the whole folder view (not just search results) every 3s while
  // any document currently shown is `pending`, and stops the instant none
  // are - keyed on the derived boolean, not on `documents` itself, so a poll
  // response that leaves the pending count unchanged doesn't tear down and
  // restart the timer (mirrors use-document-uploads.ts's own hasPending gate).
  const hasPending = documents.some((d) => d.status === 'pending')
  useEffect(() => {
    if (!hasPending) return
    // Tags come back with the document: enrichment writes its suggested ones
    // as the document finishes, so the filter row has to follow the same poll
    // or it keeps showing the tags from before anything was read.
    const id = setInterval(() => { void refresh(); void refreshTags() }, POLL_INTERVAL_MS)
    return () => clearInterval(id)
  }, [hasPending, refresh, refreshTags])

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
      const data = (await res.json()) as { results?: DocumentSearchResult[] }

      // Same ordering guard as refresh() above - an older search's
      // late-arriving result must not clobber a newer one's (or an
      // explicit clearSearch()'s) already-applied state.
      if (seq !== searchSeqRef.current) return

      setSearchResults(data.results ?? [])
      setSearchError(null)
    } catch (err) {
      if (seq !== searchSeqRef.current) return
      setSearchError(err instanceof Error ? err.message : String(err))
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
    search,
    clearSearch,

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
