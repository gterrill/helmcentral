import { useCallback, useEffect, useRef, useState } from 'react'
import { apiBaseUrl } from '@/config/api'

// Plan "Notes and the Boat's Manual" §7 / ADR 0116: the Manuals panel's data
// layer. Same idiom as use-notes.ts/use-documents.ts throughout - no
// react-query, no shared store, one instance per caller, a plain
// fetch-and-throw-the-server's-own-message write helper (AGENTS.md fallback
// policy) - and, like use-documents.ts's folderId, this hook is parametrised
// by the caller-owned "which manual is currently open" selection rather than
// owning that selection itself.
//
// Two independent things live in one hook rather than two, deliberately:
// the library-wide list of manuals (ListManuals, backend/manuals_store.go -
// every role='manual' folder, needed for the none/one/several state and the
// header switcher) and the CURRENTLY OPEN manual's own ordered subtree
// (ManualTree). A component showing the switcher always needs both at once
// - the list to populate the switcher, the tree to render whichever one is
// selected - so splitting them into two hooks would just move the "which
// manual is this the tree of" bookkeeping into the component instead of
// removing it.

export interface ManualSummary {
  id: string
  name: string
  document_count: number
  updated_at: string
}

// manualTreeNode, backend/manuals_store.go: a node in a manual's whole
// ordered subtree in ONE response - folders and documents interleaved at
// every level, sort_index then name (plan §2). Type distinguishes a folder
// (its own further children) from a document (always a leaf); Kind/NoteType/
// MIME are populated only on a document node - manuals-panel.tsx picks an
// icon from them.
export interface ManualTreeNode {
  type: 'folder' | 'document'
  id: string
  name: string
  sort_index: number
  updated_at: string
  kind?: string
  note_type?: string
  mime?: string
  children?: ManualTreeNode[]
}

// manualReorderItem, backend/manuals_store.go: one sibling's new position,
// sent as a WHOLE list for the parent being reordered (plan §4 - "whole-
// sibling-list, not per-item" - an id this endpoint doesn't recognise as an
// actual child of parentId fails the entire call rather than silently
// dropping it and leaving two sections both claiming position 3).
export interface ManualReorderItem {
  kind: 'folder' | 'document'
  id: string
  sort_index: number
}

// Finds nodeId anywhere in tree's own subtree (tree itself, or any
// descendant), depth-first. Exported for the Notes inbox's File… picker
// (notes-panel.tsx): once a browsed folder turns out to sit inside a
// manual, the picker needs that folder's CURRENT children and their
// sort_index range to offer a "beginning"/"end" position - and a manual's
// tree (this hook's own `tree`, once fetched for that manual) is the only
// place sort_index is exposed at all. GET /api/document-folders
// (use-documents.ts) - what the picker otherwise browses with - doesn't
// carry it; documentFolder's own JSON (backend/documents_store.go) never
// serialises sort_index, since ordinary folder browsing has never needed
// it before this feature.
export function findManualTreeNode(tree: ManualTreeNode, nodeId: string): ManualTreeNode | null {
  if (tree.id === nodeId) return tree
  for (const child of tree.children ?? []) {
    const found = findManualTreeNode(child, nodeId)
    if (found) return found
  }
  return null
}

// Every write below goes through this: a non-2xx response throws with the
// server's own message (AGENTS.md fallback policy - no invented "something
// went wrong" standing in for a 409's actual "folder is not a manual" or
// "only a top-level folder can be a manual"). 204 (DELETE /api/manuals/:id)
// has no body to parse - copied from use-documents.ts's own submitJSON,
// which use-notes.ts's copy doesn't need (nothing on the notes API returns
// 204), rather than importing either: a two-line helper is cheaper to
// duplicate a third time than to thread a shared-module dependency between
// three features that otherwise don't know about each other.
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
 * `manualId`: the manual currently open in the reading view, null when none
 * is (the "several, nothing picked yet" state, or "none exist at all").
 * Owned by the caller (manuals-panel.tsx), not this hook - same contract as
 * use-documents.ts's own folderId param.
 */
export function useManuals(manualId: string | null = null) {
  // ── the library-wide list (plan §4's GET /api/manuals) ─────────────────
  const [manuals, setManuals] = useState<ManualSummary[]>([])
  const [manualsLoading, setManualsLoading] = useState(true)
  const [manualsError, setManualsError] = useState<string | null>(null)

  // Same ordering guard as use-notes.ts/use-documents.ts's own refresh():
  // a caller-triggered refresh (after a create/clear) can overlap an
  // earlier one still in flight, and without this whichever call resolves
  // LAST wins even if it was the OLDER one.
  const manualsSeqRef = useRef(0)

  const refreshManuals = useCallback(async () => {
    const seq = (manualsSeqRef.current += 1)
    setManualsLoading(true)
    try {
      const res = await fetch(`${apiBaseUrl}/api/manuals`)
      if (!res.ok) throw new Error(`HTTP ${res.status}`)
      const data = (await res.json()) as { manuals?: ManualSummary[] }
      if (seq !== manualsSeqRef.current) return
      setManuals(data.manuals ?? [])
      setManualsError(null)
    } catch (err) {
      if (seq !== manualsSeqRef.current) return
      setManualsError(err instanceof Error ? err.message : String(err))
    } finally {
      if (seq === manualsSeqRef.current) setManualsLoading(false)
    }
  }, [])

  useEffect(() => { void refreshManuals() }, [refreshManuals])

  // ── the currently open manual's own ordered subtree (GET .../tree) ─────
  const [tree, setTree] = useState<ManualTreeNode | null>(null)
  const [treeLoading, setTreeLoading] = useState(false)
  const [treeError, setTreeError] = useState<string | null>(null)
  const treeSeqRef = useRef(0)

  const refreshTree = useCallback(async () => {
    const seq = (treeSeqRef.current += 1)
    if (!manualId) {
      // Nothing open - not an error, the ordinary "no manual selected"
      // state (none exist yet, or the several-manuals list hasn't had one
      // picked). Still bumps the sequence above so a tree fetch already in
      // flight for a manual the operator has since navigated away from is
      // superseded rather than landing late and repopulating the pane.
      setTree(null)
      setTreeError(null)
      setTreeLoading(false)
      return
    }
    setTreeLoading(true)
    try {
      const res = await fetch(`${apiBaseUrl}/api/manuals/${encodeURIComponent(manualId)}/tree`)
      if (!res.ok) throw new Error(`HTTP ${res.status}`)
      const data = (await res.json()) as ManualTreeNode
      if (seq !== treeSeqRef.current) return
      setTree(data)
      setTreeError(null)
    } catch (err) {
      if (seq !== treeSeqRef.current) return
      setTreeError(err instanceof Error ? err.message : String(err))
    } finally {
      if (seq === treeSeqRef.current) setTreeLoading(false)
    }
  }, [manualId])

  useEffect(() => { void refreshTree() }, [refreshTree])

  // createManual/flagManual are plan §4's two ways to call POST
  // /api/manuals - {name} makes a brand new empty top-level folder (the
  // "Start a manual" dialog only ever asks for a name - plan §7: a starter
  // outline was considered and cut), {folder_id} flags one that already
  // exists. Both refresh the library list; neither is a no-op for the
  // none/one/several state the panel derives from `manuals.length`.
  const createManual = useCallback(async (name: string) => {
    const created = await submitJSON<ManualSummary>(`${apiBaseUrl}/api/manuals`, 'POST', { name })
    await refreshManuals()
    return created
  }, [refreshManuals])

  const flagManual = useCallback(async (folderId: string) => {
    const flagged = await submitJSON<ManualSummary>(`${apiBaseUrl}/api/manuals`, 'POST', { folder_id: folderId })
    await refreshManuals()
    return flagged
  }, [refreshManuals])

  // Demotes id back to a plain collection (plan §4: "moves and deletes
  // nothing beneath it"). Clears the locally-held tree when the manual
  // demoted is the one currently open - GET .../tree on a non-manual folder
  // 404s (errNotAManual), and there is nothing sensible left to show in the
  // reading pane for it.
  const clearManual = useCallback(async (id: string) => {
    await submitJSON<void>(`${apiBaseUrl}/api/manuals/${encodeURIComponent(id)}`, 'DELETE')
    await refreshManuals()
    if (id === manualId) setTree(null)
  }, [refreshManuals, manualId])

  // Applies a fresh sort_index to every item in ONE sibling list (plan §4:
  // "whole-sibling-list, not per-item" - Arrange mode's move-up/move-down
  // commits exactly one of these per click, matching "a drop commits ONE
  // reorder call for the affected sibling list"). The response is the
  // manual's whole freshly-ordered tree (the same shape GET .../tree
  // returns), applied directly rather than triggering a second fetch - the
  // server already did the work of recomputing it.
  const reorder = useCallback(async (parentId: string, items: ManualReorderItem[]) => {
    if (!manualId) throw new Error('no manual is open')
    const updated = await submitJSON<ManualTreeNode>(
      `${apiBaseUrl}/api/manuals/${encodeURIComponent(manualId)}/reorder`,
      'POST',
      { parent_id: parentId, items },
    )
    setTree(updated)
    return updated
  }, [manualId])

  return {
    manuals,
    manualsLoading,
    manualsError,
    refreshManuals,

    tree,
    treeLoading,
    treeError,
    refreshTree,

    createManual,
    flagManual,
    clearManual,
    reorder,
  }
}
