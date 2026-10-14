import { useCallback } from 'react'
import { apiRequest, invalidateQueries, setQueryData, useApiQuery } from '@/lib/api-query'
import { invalidateDocumentLists } from '@/hooks/use-documents'

// Plan "Notes and the Boat's Manual" §7 / ADR 0116: the Manuals panel's data
// layer. Reads go through useApiQuery and writes through apiRequest (ADR
// 0173), which throws the server's own message (AGENTS.md fallback policy).
// Like use-documents.ts's folderId, this hook is parametrised
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

/**
 * `manualId`: the manual currently open in the reading view, null when none
 * is (the "several, nothing picked yet" state, or "none exist at all").
 * Owned by the caller (manuals-panel.tsx), not this hook - same contract as
 * use-documents.ts's own folderId param.
 */
const MANUALS = '/api/manuals'
const NO_MANUALS: ManualSummary[] = []

export function useManuals(manualId: string | null = null) {
  // ── the library-wide list (plan §4's GET /api/manuals) ─────────────────
  const list = useApiQuery<ManualSummary[]>({
    queryKey: [MANUALS],
    path: MANUALS,
    select: (raw: { manuals?: ManualSummary[] }) => raw.manuals ?? [],
  })
  const refreshManuals = list.refetch

  // ── the currently open manual's own ordered subtree (GET .../tree) ─────
  // Nothing open is not an error, just the ordinary "no manual selected"
  // state (none exist yet, or the several-manuals list has none picked).
  const treePath = `${MANUALS}/${encodeURIComponent(manualId ?? '')}/tree`
  const treeQuery = useApiQuery<ManualTreeNode | null>({
    queryKey: [treePath],
    path: treePath,
    enabled: manualId !== null && manualId !== '',
  })
  const refreshTree = treeQuery.refetch

  // Resolves once the fresh list has landed; the invalidate comes first so a
  // read in flight from before the write is superseded, not awaited.
  // A manual is a folder with a role, and the folder tree says which are
  // manuals, so every write here refreshes the Documents readers too.
  const reloadManuals = useCallback(async () => {
    invalidateQueries([MANUALS])
    invalidateDocumentLists({ counts: true })
    await refreshManuals()
  }, [refreshManuals])

  // createManual/flagManual are plan §4's two ways to call POST
  // /api/manuals - {name} makes a brand new empty top-level folder (the
  // "Start a manual" dialog only ever asks for a name - plan §7: a starter
  // outline was considered and cut), {folder_id} flags one that already
  // exists. Both refresh the library list; neither is a no-op for the
  // none/one/several state the panel derives from `manuals.length`.
  const createManual = useCallback(async (name: string) => {
    const created = await apiRequest<ManualSummary>(MANUALS, { method: 'POST', json: { name } })
    await reloadManuals()
    return created
  }, [reloadManuals])

  const flagManual = useCallback(async (folderId: string) => {
    const flagged = await apiRequest<ManualSummary>(MANUALS, { method: 'POST', json: { folder_id: folderId } })
    await reloadManuals()
    return flagged
  }, [reloadManuals])

  // Demotes id back to a plain collection (plan §4: "moves and deletes
  // nothing beneath it"). Empties the tree held for it: GET .../tree on a
  // non-manual folder 404s (errNotAManual), so a refetch would only put an
  // error where there is nothing sensible left to show.
  const clearManual = useCallback(async (id: string) => {
    await apiRequest<void>(`${MANUALS}/${encodeURIComponent(id)}`, { method: 'DELETE' })
    setQueryData<ManualTreeNode | null>([`${MANUALS}/${encodeURIComponent(id)}/tree`], null)
    await reloadManuals()
  }, [reloadManuals])

  // Applies a fresh sort_index to every item in ONE sibling list (plan §4:
  // "whole-sibling-list, not per-item" - Arrange mode's move-up/move-down
  // commits exactly one of these per click, matching "a drop commits ONE
  // reorder call for the affected sibling list"). The response is the
  // manual's whole freshly-ordered tree (the same shape GET .../tree
  // returns), applied directly rather than triggering a second fetch - the
  // server already did the work of recomputing it.
  const reorder = useCallback(async (parentId: string, items: ManualReorderItem[]) => {
    if (!manualId) throw new Error('no manual is open')
    const updated = await apiRequest<ManualTreeNode>(
      `${MANUALS}/${encodeURIComponent(manualId)}/reorder`,
      { method: 'POST', json: { parent_id: parentId, items } },
    )
    setQueryData<ManualTreeNode | null>([`${MANUALS}/${encodeURIComponent(manualId)}/tree`], updated)
    return updated
  }, [manualId])

  return {
    manuals: list.data ?? NO_MANUALS,
    manualsLoading: list.isLoading,
    manualsError: list.error?.message ?? null,
    refreshManuals,

    tree: treeQuery.data ?? null,
    treeLoading: treeQuery.isLoading,
    treeError: treeQuery.error?.message ?? null,
    refreshTree,

    createManual,
    flagManual,
    clearManual,
    reorder,
  }
}
