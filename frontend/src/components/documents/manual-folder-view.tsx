import {
  ArrowLeft,
  ArrowUpDown,
  Check,
  ChevronDown,
  ChevronUp,
  Eye,
  FileText,
  Folder,
  Pencil,
  type LucideIcon,
} from 'lucide-react'
import { useCallback, useEffect, useState } from 'react'

import { Button } from '@/components/ui/button'
import { findManualTreeNode, useManuals, type ManualTreeNode } from '@/hooks/use-manuals'
import { useNotes } from '@/hooks/use-notes'
import type { NoteLink } from '@/lib/note-links'
import { noteTypeMeta } from '@/lib/note-type-meta'
import { cn } from '@/lib/utils'

import { NoteEditor } from '../note-editor'
import { NoteMarkdown } from '../note-markdown'

// Revision "one panel, not three" (2026-09-20): a Manual-kind folder's own
// rendering (ordering, Arrange, front-to-back reading), moved out of the
// deleted manuals-panel.tsx and composed by documents-panel.tsx wherever
// the folder currently browsed carries document_folders.role='manual'.
//
// The old panel's none/one/several state machine and its manual switcher
// are gone, on purpose, not merely trimmed: Documents' own folder browsing
// already IS that switcher (a manual is a folder among folders, and several
// manuals are just several folders you navigate between the ordinary way),
// so a second, parallel navigation surface for the same choice would be
// redundant. What is left is exactly the part that's genuinely different
// about a manual-kind folder once you're inside it: an ordered tree
// instead of a table, an Arrange mode, and a reading pane.

export interface ManualFolderViewProps {
  /** The manual's own top-level folder id - also manualId for
   * useManuals()/GET .../tree, since a manual IS a folder. */
  folderId: string
  /** Which section (a document node in this manual's tree) is open in the
   * reading pane - `?section=` (app-location.ts), sibling to
   * documents-panel.tsx's own folderId/documentId query params. */
  sectionId: string | null
  onSectionChange: (id: string | null) => void
  /** Called after "Stop treating as manual" succeeds - documents-panel.tsx
   * refreshes its own manuals list in response, which is what actually
   * swaps this view back out for the ordinary table (see that file's
   * folderIsManual derivation). This component does not decide when it
   * stops being shown; its parent does, from the same manuals list every
   * folder row's badge/menu already reads. */
  onDemoted: () => void
  /** A hc-note:/hc-doc: link that resolves to a document OUTSIDE this
   * manual's own tree (or that just isn't worth a special case - see
   * handleNavigate below) opens in Documents' own viewer Sheet instead of
   * this pane. Unlike the deleted manuals-panel.tsx, a link is never inert
   * here for being "outside the tree" - Documents already holds every kind
   * of document side by side, so there is no reason a manual's reading pane
   * should refuse to follow a link the ordinary viewer could open. */
  onNavigateDocument: (id: string) => void
}

export function ManualFolderView({ folderId, sectionId, onSectionChange, onDemoted, onNavigateDocument }: ManualFolderViewProps) {
  const manuals = useManuals(folderId)
  // Only for its patchNote/getNote mutators (both work on any note id
  // regardless of filing state) - the reading pane's GET and the editor's
  // PATCH both go through use-notes.ts rather than a raw fetch, unlike the
  // deleted manuals-panel.tsx's own ManualReadingPane, which is exactly the
  // raw `fetch(PATCH /api/notes/:id)` the revision calls out for deletion.
  const notes = useNotes()
  const [arranging, setArranging] = useState(false)

  const handleDemote = useCallback(async () => {
    await manuals.clearManual(folderId)
    onDemoted()
  }, [manuals, folderId, onDemoted])

  const handleMove = useCallback((parentId: string, siblings: ManualTreeNode[], index: number, direction: 'up' | 'down') => {
    const targetIndex = direction === 'up' ? index - 1 : index + 1
    if (targetIndex < 0 || targetIndex >= siblings.length) return
    const reordered = [...siblings]
    const [moved] = reordered.splice(index, 1)
    reordered.splice(targetIndex, 0, moved)
    // Renumbered 0..n-1 in the new order, not a swap of the two old
    // sort_index values: existing rows commonly share sort_index 0 (the
    // schema default), and swapping two equal values would be a no-op that
    // leaves the operator's move looking like it did nothing.
    const items = reordered.map((n, i) => ({
      kind: (n.type === 'folder' ? 'folder' : 'document') as 'folder' | 'document',
      id: n.id,
      sort_index: i,
    }))
    void manuals.reorder(parentId, items)
  }, [manuals])

  const selectedNode = manuals.tree && sectionId ? findManualTreeNode(manuals.tree, sectionId) : null

  const [sectionBody, setSectionBody] = useState<string | null>(null)
  const [sectionBodyLoading, setSectionBodyLoading] = useState(false)
  const [sectionBodyError, setSectionBodyError] = useState<string | null>(null)
  const { getNote } = notes
  useEffect(() => {
    if (!selectedNode || selectedNode.kind !== 'note') {
      setSectionBody(null)
      setSectionBodyError(null)
      return
    }
    let cancelled = false
    setSectionBodyLoading(true)
    getNote(selectedNode.id)
      .then((detail) => { if (!cancelled) { setSectionBody(detail.body); setSectionBodyError(null) } })
      .catch((err) => { if (!cancelled) setSectionBodyError(err instanceof Error ? err.message : String(err)) })
      .finally(() => { if (!cancelled) setSectionBodyLoading(false) })
    return () => { cancelled = true }
  }, [selectedNode, getNote])

  // Anchor links still scroll within this pane; a note/document link always
  // opens in the shared viewer Sheet (documents-panel.tsx's own setViewerId,
  // via onNavigateDocument) rather than being restricted to this manual's
  // own tree - see the doc comment on onNavigateDocument above.
  const handleNavigate = useCallback((link: NoteLink) => {
    if (link.kind === 'anchor') {
      document.getElementById(link.hash)?.scrollIntoView({ block: 'start' })
      return
    }
    if (link.kind === 'note' || link.kind === 'document') {
      onNavigateDocument(link.id)
    }
  }, [onNavigateDocument])

  const handleSaveSection = useCallback(async (markdown: string) => {
    if (!selectedNode) return
    const updated = await notes.patchNote(selectedNode.id, { body: markdown })
    setSectionBody(updated.body)
  }, [selectedNode, notes])

  return (
    <div className="flex min-h-0 flex-1 flex-col gap-3">
      <div className="flex items-center justify-end gap-2">
        <ArrangeToggle arranging={arranging} onToggle={() => setArranging((prev) => !prev)} />
        <Button type="button" variant="ghost" size="sm" className="text-muted-foreground" onClick={() => { void handleDemote() }}>
          Stop treating as manual
        </Button>
      </div>

      {manuals.treeError && <p role="alert" className="text-sm text-destructive">{manuals.treeError}</p>}

      <div className="flex min-h-0 flex-1 flex-col gap-4 md:flex-row">
        <div className={cn(
          'min-h-0 flex-col overflow-auto rounded-md border md:flex md:w-72 md:shrink-0 lg:w-80',
          sectionId ? 'hidden md:flex' : 'flex',
        )}
        >
          {manuals.treeLoading && !manuals.tree && (
            <p className="p-3 text-sm text-muted-foreground">Loading…</p>
          )}
          {manuals.tree && manuals.tree.children && manuals.tree.children.length > 0 && (
            <ManualTreeList
              parentId={manuals.tree.id}
              nodes={manuals.tree.children}
              depth={0}
              arranging={arranging}
              selectedSectionId={sectionId}
              onSelectSection={(node) => onSectionChange(node.id)}
              onMove={handleMove}
            />
          )}
          {manuals.tree && (!manuals.tree.children || manuals.tree.children.length === 0) && (
            <p className="p-3 text-sm text-muted-foreground">
              Nothing filed here yet. Use File… on an unfiled note in Documents.
            </p>
          )}
        </div>

        <div className={cn(
          'min-h-0 flex-1 flex-col overflow-auto rounded-md border',
          sectionId ? 'flex' : 'hidden md:flex',
        )}
        >
          <ManualReadingPane
            node={selectedNode}
            body={sectionBody}
            loading={sectionBodyLoading}
            error={sectionBodyError}
            onNavigate={handleNavigate}
            onSave={handleSaveSection}
            onBack={() => onSectionChange(null)}
          />
        </div>
      </div>
    </div>
  )
}

export function ArrangeToggle({ arranging, onToggle }: { arranging: boolean; onToggle: () => void }) {
  return (
    <button
      type="button"
      onClick={onToggle}
      aria-pressed={arranging}
      className={cn(
        'inline-flex min-h-11 items-center gap-1 rounded-md border px-3 text-[10px] font-semibold uppercase tracking-[0.08em] transition-colors',
        arranging
          ? 'border-primary/40 bg-primary/10 text-primary'
          : 'border-border bg-background/70 text-muted-foreground hover:border-primary/40 hover:text-primary',
      )}
    >
      {arranging ? <Check className="h-3.5 w-3.5" aria-hidden="true" /> : <ArrowUpDown className="h-3.5 w-3.5" aria-hidden="true" />}
      {arranging ? 'Done' : 'Arrange'}
    </button>
  )
}

// A section's type icon. A folder is a chapter; a document is either a
// filed note (the common case - reuses lib/note-type-meta.ts, so a section
// shows the identical icon/colour it showed as an unfiled row before it was
// filed) or a filed non-note document, like a photographed data plate,
// which gets a plain file icon.
export function manualNodeIcon(node: ManualTreeNode): { Icon: LucideIcon; className: string } {
  if (node.type === 'folder') return { Icon: Folder, className: 'text-muted-foreground' }
  if (node.kind === 'note') {
    const meta = noteTypeMeta(node.note_type ?? '')
    return { Icon: meta.icon, className: meta.className }
  }
  return { Icon: FileText, className: 'text-muted-foreground' }
}

interface ManualTreeListProps {
  parentId: string
  nodes: ManualTreeNode[]
  depth: number
  arranging: boolean
  selectedSectionId: string | null
  onSelectSection: (node: ManualTreeNode) => void
  onMove: (parentId: string, siblings: ManualTreeNode[], index: number, direction: 'up' | 'down') => void
}

/**
 * The manual's whole table of contents, recursed all the way down in one
 * pass - ManualTree (backend/manuals_store.go) already returns the entire
 * ordered subtree in one call. sort_index order is never re-sorted here:
 * `nodes` is rendered in exactly the order the server sent it
 * (`ORDER BY sort_index, lower(name)`) - re-sorting client-side would be a
 * second, driftable copy of that rule.
 */
export function ManualTreeList({ parentId, nodes, depth, arranging, selectedSectionId, onSelectSection, onMove }: ManualTreeListProps) {
  return (
    <ul className={depth > 0 ? 'ml-3 border-l border-border pl-2' : undefined}>
      {nodes.map((node, index) => (
        <li key={node.id}>
          <ManualTreeRow
            node={node}
            arranging={arranging}
            selected={node.type === 'document' && node.id === selectedSectionId}
            canMoveUp={index > 0}
            canMoveDown={index < nodes.length - 1}
            onSelect={() => onSelectSection(node)}
            onMoveUp={() => onMove(parentId, nodes, index, 'up')}
            onMoveDown={() => onMove(parentId, nodes, index, 'down')}
          />
          {node.type === 'folder' && node.children && node.children.length > 0 && (
            <ManualTreeList
              parentId={node.id}
              nodes={node.children}
              depth={depth + 1}
              arranging={arranging}
              selectedSectionId={selectedSectionId}
              onSelectSection={onSelectSection}
              onMove={onMove}
            />
          )}
        </li>
      ))}
    </ul>
  )
}

export function ManualTreeRow({
  node,
  arranging,
  selected,
  canMoveUp,
  canMoveDown,
  onSelect,
  onMoveUp,
  onMoveDown,
}: {
  node: ManualTreeNode
  arranging: boolean
  selected: boolean
  canMoveUp: boolean
  canMoveDown: boolean
  onSelect: () => void
  onMoveUp: () => void
  onMoveDown: () => void
}) {
  const { Icon, className } = manualNodeIcon(node)

  return (
    <div className={cn('flex min-w-0 items-center gap-1', selected && 'bg-accent')}>
      {/* Arrange mode: two explicit 44px move controls rather than a
          mouse-only drag handle, so it stays operable without a mouse. Two
          buttons side by side, not stacked, so each keeps its own 44px hit
          target instead of splitting an 11-row's height between them. */}
      {arranging && (
        <span className="flex shrink-0 items-center">
          <button
            type="button"
            aria-label={`Move "${node.name}" up`}
            disabled={!canMoveUp}
            onClick={onMoveUp}
            className="flex h-11 w-11 items-center justify-center text-muted-foreground hover:bg-accent hover:text-foreground disabled:opacity-30"
          >
            <ChevronUp className="h-4 w-4" aria-hidden="true" />
          </button>
          <button
            type="button"
            aria-label={`Move "${node.name}" down`}
            disabled={!canMoveDown}
            onClick={onMoveDown}
            className="flex h-11 w-11 items-center justify-center text-muted-foreground hover:bg-accent hover:text-foreground disabled:opacity-30"
          >
            <ChevronDown className="h-4 w-4" aria-hidden="true" />
          </button>
        </span>
      )}
      <Icon className={cn('h-4 w-4 shrink-0', className)} aria-hidden="true" />
      {node.type === 'document' ? (
        <button
          type="button"
          onClick={onSelect}
          disabled={arranging}
          className="flex min-h-11 min-w-0 flex-1 items-center truncate px-1 text-left text-sm text-foreground hover:bg-accent disabled:hover:bg-transparent"
        >
          <span className="truncate">{node.name}</span>
        </button>
      ) : (
        <span className="flex min-h-11 min-w-0 flex-1 items-center truncate px-1 text-sm font-medium text-muted-foreground">
          {node.name}
        </span>
      )}
    </div>
  )
}

function ManualReadingPane({
  node,
  body,
  loading,
  error,
  onNavigate,
  onSave,
  onBack,
}: {
  node: ManualTreeNode | null
  body: string | null
  loading: boolean
  error: string | null
  onNavigate: (link: NoteLink) => void
  onSave: (markdown: string) => Promise<void>
  onBack: () => void
}) {
  // Reset whenever a DIFFERENT section is selected - an editing session
  // belongs to the section that was open when it started.
  const [editing, setEditing] = useState(false)
  const [saveError, setSaveError] = useState<string | null>(null)
  useEffect(() => {
    setEditing(false)
    setSaveError(null)
  }, [node?.id])

  const handleSave = useCallback(async (markdown: string) => {
    try {
      await onSave(markdown)
      setSaveError(null)
      setEditing(false)
    } catch (err) {
      setSaveError(err instanceof Error ? err.message : String(err))
      throw err // NoteEditor keeps its dirty flag on a failed save
    }
  }, [onSave])

  return (
    <div className="flex h-full min-h-0 flex-col gap-3 p-3">
      {/* Back affordance under md only - at md+ the tree is always visible
          alongside this pane, so there is nothing to "go back" to. */}
      <Button type="button" variant="ghost" size="sm" className="w-fit md:hidden" onClick={onBack}>
        <ArrowLeft className="h-4 w-4" aria-hidden="true" />
        Back to sections
      </Button>

      {!node && (
        <p className="flex flex-1 items-center justify-center text-sm text-muted-foreground">
          Pick a section to read it.
        </p>
      )}

      {node && node.type === 'document' && node.kind !== 'note' && (
        <div className="flex-1">
          <h3 className="text-base font-semibold text-foreground">{node.name}</h3>
          <p className="mt-2 text-sm text-muted-foreground">
            This section is a filed document, not a note. Open it from the listing to view it.
          </p>
        </div>
      )}

      {node && node.type === 'document' && node.kind === 'note' && (
        <div className="min-h-0 flex-1 overflow-auto">
          <div className="mb-3 flex items-center justify-between gap-2">
            <h3 className="text-base font-semibold text-foreground">{node.name}</h3>
            {!loading && !error && body !== null && (
              <Button type="button" variant="outline" size="sm" onClick={() => setEditing((prev) => !prev)}>
                {editing ? (
                  <>
                    <Eye className="h-4 w-4" data-icon="inline-start" aria-hidden="true" />
                    Read
                  </>
                ) : (
                  <>
                    <Pencil className="h-4 w-4" data-icon="inline-start" aria-hidden="true" />
                    Edit
                  </>
                )}
              </Button>
            )}
          </div>
          {loading && <p className="text-sm text-muted-foreground">Loading…</p>}
          {error && <p role="alert" className="text-sm text-destructive">{error}</p>}
          {saveError && <p role="alert" className="text-sm text-destructive">{saveError}</p>}
          {!loading && !error && body !== null && !editing && <NoteMarkdown content={body} onNavigate={onNavigate} />}
          {!loading && !error && body !== null && editing && (
            <NoteEditor key={node.id} value={body} onSave={handleSave} />
          )}
        </div>
      )}
    </div>
  )
}
