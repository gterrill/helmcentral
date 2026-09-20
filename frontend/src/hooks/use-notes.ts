import { useCallback, useEffect, useRef, useState } from 'react'
import { apiBaseUrl } from '@/config/api'
import type { ChecklistItem, ChecklistRun } from './use-checklist-run'
import type { DocumentTag } from './use-documents'

// Plan "Notes and the Boat's Manual" §7 / ADR 0116: the Notes inbox's data
// layer. Deliberately small next to use-documents.ts - Phase 2 needs list,
// get-one, create and patch, nothing else (no folder browsing, no search,
// no tag/embeddings plumbing; those either don't apply to the inbox view or
// are already served by the Documents panel's own search reaching kind='note'
// rows for free). Same idiom as use-documents.ts throughout: no react-query,
// no shared store, one instance per caller, a plain fetch-and-throw-the-
// server's-own-message write helper (AGENTS.md fallback policy).

export type NoteType = 'contact' | 'quirk' | 'spec' | 'procedure' | 'recipe' | 'note'

// A note IS a documents row (kind='note') - see notes_handlers.go's
// toDocumentJSON, which this mirrors field-for-field, including the five
// notes-only columns (Kind/NoteType/NoteTypeSource/Pinned/SortIndex) that
// use-documents.ts's own DocumentRecord doesn't carry yet because the
// Documents panel has no use for them. DocumentTag is imported rather than
// redeclared - a note's tags are the exact same operator/suggested shape
// every other document's are.
export interface NoteRecord {
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
  kind: string
  note_type: string
  note_type_source: string
  pinned: boolean
  sort_index: number
}

// GET /api/notes/:id and every write below return this same general shape
// (notes_handlers.go's own comment: "one call serving both an editor and a
// reader") - the note's metadata plus its body, read fresh off disk.
// checklist/active_run (Phase 4, plan §4/ADR 0118) are GET /api/notes/:id's
// own addition only - createNoteHandler/patchNoteHandler still answer
// {document, body} alone, so both are typed optional here rather than
// promising every caller a field only the read path actually sends.
// checklist is [] on GET for a note with no checkbox items; active_run is
// null until the operator starts one - "you haven't started one yet" is a
// normal state, not an error, the same footing every other "nothing yet"
// response in this codebase gets.
export interface NoteDetail {
  document: NoteRecord
  body: string
  checklist?: ChecklistItem[]
  active_run?: ChecklistRun | null
}

export interface CreateNoteInput {
  /** The only required field (plan R1: capture demands nothing else). */
  body: string
  title?: string
  folder_id?: string | null
  tags?: string[]
  type?: NoteType
}

// Presence-aware, same idiom as patchDocumentHandler / DocumentPatch
// (use-documents.ts): a field left out of the object is left untouched by
// the server, not cleared. `folder_id: null` clears it to the root -
// distinct from leaving it out entirely - matching patchNoteHandler's own
// "null clears it to root" comment.
export interface NotePatch {
  title?: string
  tags?: string[]
  folder_id?: string | null
  type?: NoteType
  body?: string
}

export interface UseNotesOptions {
  /** Scopes the list to unfiled notes only (?filed=0) - the inbox's own
   * view (plan §4). False/omitted lists every note regardless of filing
   * state, for a future filed-notes view this phase doesn't build. */
  unfiledOnly?: boolean
  type?: string | null
  tag?: string | null
}

// Every write goes through this: a non-2xx response throws with the
// server's own message (AGENTS.md fallback policy - no invented "something
// went wrong" standing in for a 400 empty-body, a 413 oversize body, or a
// 409 sha collision's actual "note collides with existing document <id>").
// Copied from use-documents.ts's own submitJSON rather than imported: it's
// not exported there (kept private to that hook), and a two-line helper is
// cheaper to duplicate than to thread a shared-module dependency between
// two features that otherwise don't know about each other.
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
  return (await response.json()) as T
}

export function useNotes(options: UseNotesOptions = {}) {
  const { unfiledOnly = false, type = null, tag = null } = options

  const [notes, setNotes] = useState<NoteRecord[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  // Same ordering guard as use-documents.ts's refresh()/search(): a
  // caller-triggered refresh() (after a create/patch) can overlap an
  // earlier refresh() still in flight (e.g. the mount fetch, or a filter
  // prop changing mid-request). Without it, whichever fetch happens to
  // resolve LAST wins even if it was the OLDER call, silently overwriting
  // newer, more-correct state.
  const refreshSeqRef = useRef(0)

  const refresh = useCallback(async () => {
    const seq = (refreshSeqRef.current += 1)
    setLoading(true)
    try {
      const params = new URLSearchParams()
      if (unfiledOnly) params.set('filed', '0')
      if (type) params.set('type', type)
      if (tag) params.set('tag', tag)
      const qs = params.toString()
      const res = await fetch(`${apiBaseUrl}/api/notes${qs ? `?${qs}` : ''}`)
      if (!res.ok) throw new Error(`HTTP ${res.status}`)
      const data = (await res.json()) as { notes?: NoteRecord[] }

      if (seq !== refreshSeqRef.current) return // superseded by a newer call - see comment above
      setNotes(data.notes ?? [])
      setError(null)
    } catch (err) {
      if (seq !== refreshSeqRef.current) return
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      if (seq === refreshSeqRef.current) setLoading(false)
    }
  }, [unfiledOnly, type, tag])

  useEffect(() => { void refresh() }, [refresh])

  // Fetches one note directly by id, bypassing whatever the list currently
  // holds - the reader (notes-panel.tsx) needs this for a note reached via
  // a hc-note: link or a `/notes?note=` deep link, neither of which is
  // guaranteed to be in the current (possibly filtered) list at all.
  const getNote = useCallback(async (id: string) => {
    return submitJSON<NoteDetail>(`${apiBaseUrl}/api/notes/${encodeURIComponent(id)}`, 'GET')
  }, [])

  const createNote = useCallback(async (input: CreateNoteInput) => {
    const created = await submitJSON<NoteDetail>(`${apiBaseUrl}/api/notes`, 'POST', input)
    await refresh() // a fresh, unfiled note belongs at the top of the inbox immediately
    return created
  }, [refresh])

  const patchNote = useCallback(async (id: string, patch: NotePatch) => {
    const updated = await submitJSON<NoteDetail>(`${apiBaseUrl}/api/notes/${encodeURIComponent(id)}`, 'PATCH', patch)
    await refresh() // a type override or a file-out both change what the inbox shows
    return updated
  }, [refresh])

  return {
    notes,
    loading,
    error,
    refresh,

    getNote,
    createNote,
    patchNote,
  }
}
