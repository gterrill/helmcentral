import {
  Download,
  Eye,
  File,
  FileText,
  Folder,
  FolderPlus,
  Image as ImageIcon,
  Info,
  Library,
  ListChecks,
  Loader2,
  MoreVertical,
  Pencil,
  Pin,
  PinOff,
  Plus,
  RefreshCw,
  Trash2,
  Upload as UploadIcon,
  Sparkles,
} from 'lucide-react'
import { lazy, Suspense, useCallback, useEffect, useMemo, useRef, useState, type ChangeEvent, type DragEvent, type ReactNode } from 'react'

import { Badge } from '@/components/ui/badge'
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '@/components/ui/alert-dialog'
import {
  Breadcrumb,
  BreadcrumbItem,
  BreadcrumbLink,
  BreadcrumbList,
  BreadcrumbPage,
  BreadcrumbSeparator,
} from '@/components/ui/breadcrumb'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuSub,
  DropdownMenuSubContent,
  DropdownMenuSubTrigger,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { ScrollArea } from '@/components/ui/scroll-area'
import { Sheet, SheetContent, SheetDescription, SheetHeader, SheetTitle } from '@/components/ui/sheet'
import { Switch } from '@/components/ui/switch'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { ToggleGroup, ToggleGroupItem } from '@/components/ui/toggle-group'
import { apiBaseUrl } from '@/config/api'
import { NO_ATTACHMENT_CAP, useDocumentUploads, type StagedDocument } from '@/hooks/use-document-uploads'
import {
  useDocuments,
  type DocumentEmbeddingsStatus,
  type DocumentFolder,
  type DocumentRecord,
  type DocumentSearchResult,
} from '@/hooks/use-documents'
import { useAssistantStatus } from '@/hooks/use-assistant-status'
import { useManuals } from '@/hooks/use-manuals'
import { useNotes, type NoteType } from '@/hooks/use-notes'
import { documentDisplayName, formatBytes, mimeLabel } from '@/lib/document-display'
import type { HelpTarget } from '@/lib/help-links'
import type { NoteLink } from '@/lib/note-links'
import { NOTE_TYPE_META, NOTE_TYPE_ORDER } from '@/lib/note-type-meta'
import { cn } from '@/lib/utils'

import { AskMateSelection } from './ask-mate-selection'
import { ChecklistRunner } from './documents/checklist-runner'
import { ManualFolderView } from './documents/manual-folder-view'
import { NoteTypeIconButton } from './documents/note-type-icon-button'
import { UnfiledNotesView } from './documents/unfiled-notes-view'
import { NoteEditor, prefetchNoteEditor } from './note-editor'
import { NoteMarkdown } from './note-markdown'

// ADR 0121: this panel is now the ONLY place a note gets created - the
// global header action and Alt+N shortcut ADR 0119 built are gone. Lazy
// like every other sheet in the shell: NoteCaptureSheet fetches nothing and
// runs no effects until opened (useNotes() inside it fetches on mount), so
// there is nothing for it to do before the operator has ever picked New →
// Note - see captureHasOpenedRef's own comment, next to where it's
// rendered, for the mount-once-opened latch.
const NoteCaptureSheet = lazy(() => import('./documents/note-capture-sheet').then((mod) => ({ default: mod.NoteCaptureSheet })))

// ADR 0106 F1: the Documents panel. All data (folder browsing, search, tags,
// every write) comes from useDocuments(folderId); uploads reuse
// useDocumentUploads (ADR 0106 F2's composer hook, extended to take a target
// folder id) rather than a second uploader - see that hook's own doc comment.

const SEARCH_DEBOUNCE_MS = 250
const OCR_COST_PER_PAGE_USD = 0.002 // documentsOCRCostPerPageUSD, backend/documents_enrich.go

// Same shape as assistant-thread.tsx's stagedDocumentStatusLabel (ADR 0106
// F2's composer chip label) - duplicated locally rather than imported/shared,
// since that file is already committed and out of scope for this finding.
function stagedUploadStatusLabel(item: StagedDocument): string {
  switch (item.status) {
    case 'uploading':
      return `${item.progress}%`
    case 'pending':
      return 'Reading…'
    case 'indexed':
      return 'Indexed'
    case 'failed':
      return item.error ?? 'Failed'
  }
}

function rowIcon(mime: string) {
  if (mime.startsWith('image/')) return ImageIcon
  if (mime.startsWith('text/') || mime === 'application/pdf' || mime === 'application/json') return FileText
  return File
}

// Splits a search snippet on the FTS5 \x02/\x03 match markers
// (backend/documents_search.go) into plain text interleaved with <mark>
// elements - built as React nodes, never innerHTML, so a snippet that
// happens to contain markup (a receipt OCR'd with "<" in it, say) renders as
// inert text rather than being parsed.
function renderSnippet(snippet: string): ReactNode[] {
  const parts: ReactNode[] = []
  const re = /\x02([^\x03]*)\x03/g
  let lastIndex = 0
  let match: RegExpExecArray | null
  let key = 0
  while ((match = re.exec(snippet)) !== null) {
    if (match.index > lastIndex) parts.push(<span key={key++}>{snippet.slice(lastIndex, match.index)}</span>)
    parts.push(<mark key={key++}>{match[1]}</mark>)
    lastIndex = re.lastIndex
  }
  if (lastIndex < snippet.length) parts.push(<span key={key++}>{snippet.slice(lastIndex)}</span>)
  return parts
}

function statusBadge(doc: DocumentRecord) {
  switch (doc.status) {
    case 'pending':
      return <Badge variant="outline">Reading…</Badge>
    case 'failed':
      return <Badge variant="destructive">Failed</Badge>
    case 'indexed':
    default:
      return <Badge variant="secondary">Indexed</Badge>
  }
}

interface RenameTarget {
  kind: 'folder' | 'document'
  id: string
  name: string
}

type DeleteTarget =
  | { kind: 'folder'; id: string; name: string }
  | { kind: 'document'; id: string; name: string }
  | { kind: 'bulk'; ids: string[] }

interface ReindexTarget {
  id: string
  name: string
  pageCount: number
  mime: string
}

interface MoveTarget {
  ids: string[]
  label: string
}

/** A one-level-at-a-time folder browser embedded in the Move dialog. It
 * reuses useDocuments the same way the panel itself does - the picker is
 * just another folder view, and moves want the operator to be able to
 * descend into a destination the same way browsing does. */
function FolderPicker({
  onPick,
  onFolderCreated,
}: {
  onPick: (folderId: string | null) => void
  /** Review finding: the picker owns its own useDocuments(pickerFolderId)
   * instance, entirely separate from the panel's own - so a folder created
   * here used to refresh only the picker's view. Cancel the dialog and the
   * panel's own listing had never heard of it: missing from the table, and
   * a second attempt from the toolbar's New folder button hit a 409 for a
   * folder the operator couldn't see. This just tells the caller a folder
   * was created; DocumentsPanel below refreshes its own listing in
   * response, leaving the picker's own descend-into-it behaviour (below)
   * unchanged. */
  onFolderCreated: () => void
}) {
  const [pickerFolderId, setPickerFolderId] = useState<string | null>(null)
  const picker = useDocuments(pickerFolderId)

  // ADR 0115 §6: create-here, so a move no longer has to be
  // abandoned to go make the destination first. Goes through the picker's
  // own createFolder (same useDocuments instance as the browser above), so
  // the new folder shows up in whatever listing this picker is already
  // rendering - then descends into it (setPickerFolderId) so "Move here"
  // immediately means the folder just made, rather than leaving the
  // operator to notice it in the list and click it themselves.
  const [newFolderName, setNewFolderName] = useState('')
  const [creating, setCreating] = useState(false)
  const [createError, setCreateError] = useState<string | null>(null)
  const submitCreate = async () => {
    const name = newFolderName.trim()
    if (name === '' || creating) return
    setCreating(true)
    setCreateError(null)
    try {
      const created = await picker.createFolder(name, pickerFolderId)
      setPickerFolderId(created.id)
      setNewFolderName('')
      onFolderCreated()
    } catch (err) {
      // AGENTS.md fallback policy: the server's own message (e.g. the 409
      // "folder name already exists") rather than an invented one, and the
      // picker stays right where it was - the operator didn't ask to move
      // anywhere, only to create a folder that turned out to already exist.
      setCreateError(err instanceof Error ? err.message : String(err))
    } finally {
      setCreating(false)
    }
  }

  return (
    <div className="flex flex-col gap-2">
      <Breadcrumb>
        <BreadcrumbList>
          <BreadcrumbItem>
            <BreadcrumbLink href="#" onClick={(e) => { e.preventDefault(); setPickerFolderId(null) }}>
              Documents
            </BreadcrumbLink>
          </BreadcrumbItem>
          {picker.path.map((folder) => (
            <span key={folder.id} className="contents">
              <BreadcrumbSeparator />
              <BreadcrumbItem>
                <BreadcrumbLink href="#" onClick={(e) => { e.preventDefault(); setPickerFolderId(folder.id) }}>
                  {folder.name}
                </BreadcrumbLink>
              </BreadcrumbItem>
            </span>
          ))}
        </BreadcrumbList>
      </Breadcrumb>
      <ScrollArea className="h-48 rounded-md border">
        <div className="flex flex-col p-1">
          {picker.folders.length === 0 && (
            <p className="p-3 text-sm text-muted-foreground">No subfolders here.</p>
          )}
          {picker.folders.map((folder) => (
            <button
              key={folder.id}
              type="button"
              onClick={() => setPickerFolderId(folder.id)}
              className="flex items-center gap-2 rounded-sm px-2 py-1.5 text-left text-sm hover:bg-accent"
            >
              <Folder className="h-4 w-4 shrink-0 text-muted-foreground" aria-hidden="true" />
              {folder.name}
            </button>
          ))}
        </div>
      </ScrollArea>
      <div className="flex items-center gap-2">
        <Input
          aria-label="New folder name"
          placeholder="New folder name"
          value={newFolderName}
          onChange={(e) => setNewFolderName(e.target.value)}
          onKeyDown={(e) => { if (e.key === 'Enter') { e.preventDefault(); void submitCreate() } }}
          className="flex-1"
        />
        <Button type="button" variant="outline" disabled={newFolderName.trim() === '' || creating} onClick={() => { void submitCreate() }}>
          Create folder
        </Button>
      </div>
      {createError && <p role="alert" className="text-sm text-destructive">{createError}</p>}
      <Button type="button" onClick={() => onPick(pickerFolderId)}>Move here</Button>
    </div>
  )
}

export interface DocumentsPanelProps {
  /** Seeds the panel's current folder on mount - App.tsx passes whatever
   * app-location.ts parsed off the URL (documentFolderId). Uncontrolled
   * after that, same pattern as AssistantDrawer's initialConversationId. */
  initialFolderId?: string | null
  /** Called whenever the panel navigates to a different folder, so App.tsx
   * can mirror it into the URL (?folder=). */
  onFolderChange?: (folderId: string | null) => void
  /** A document to open in the viewer as soon as the panel mounts - how a
   * Mate attachment chip's link (assistant-thread.tsx) lands here. */
  initialDocumentId?: string | null
  /** ADR 0115 §2: opens the Details page (document-details-page.tsx)
   * for one document - from the row menu's Details… item and from the
   * viewer Sheet's own Details button. App.tsx wires this to
   * setDocumentsEditId, the same way onFolderChange wires into its own
   * documentsFolderId state. Optional so every existing test that renders
   * this panel without it (nothing to navigate to) keeps working unchanged. */
  onEditDocument?: (id: string) => void
  /** Revision "one panel, not three" (2026-09-20): which section (a
   * document node in the current folder's manual tree) is open in
   * ManualFolderView's reading pane - `?section=`, seeded and mirrored the
   * same way onFolderChange/initialFolderId are. Meaningless unless the
   * current folder is a manual (ManualFolderView is the only thing that
   * ever reads it), same as app-location.ts's own documentSectionId. */
  initialSectionId?: string | null
  onSectionChange?: (id: string | null) => void
  /** Opens the in-app help at a given target - passed straight through the
   * "What to put in it" link in EmptyManualsState's replacement (the
   * revision drops the dedicated empty state, but a Manual-kind folder can
   * still want to link to the how-to). Optional, same reasoning as the
   * deleted manuals-panel.tsx's own onOpenHelp. */
  onOpenHelp?: (target: HelpTarget) => void
  /** ADR 0120: opens Settings → Assistant - the toolbar's failure line (the
   * one surfaced state left once the backlog banners were removed) links
   * here the same way AssistantDrawer's own "Open Mate settings" button
   * does (App.tsx wires both through requestNavigate('settings', ...)).
   * Optional, same reasoning as onOpenHelp above: a test that never
   * triggers a failure doesn't need it. */
  onOpenAssistantSettings?: () => void
  /** Ask Mate about a text selection (ask-mate-selection.tsx): matches
   * App.tsx's openMate signature exactly, the same way SettingsPage's own
   * onAskMate does - see logs-section.tsx for the identical prop shape.
   * Optional so a test that never selects text doesn't need it. */
  onAskMate?: (question: string, options?: { newConversation?: boolean }) => void
}

// Revision "one panel, not three" (2026-09-20): docs/features/notes-and-the-manual.md
// is the how-to for what a Manual-kind folder is and how to start one -
// linked from the New folder dialog's Manual checkbox, since the concept
// (ordering, Arrange, a reading view) needs a sentence nothing in a
// checkbox label can carry.
const MANUAL_HOWTO_HELP_TARGET: HelpTarget = { page: 'how-to/start-a-ships-manual' }

export function DocumentsPanel({
  initialFolderId = null,
  onFolderChange,
  initialDocumentId = null,
  onEditDocument,
  initialSectionId = null,
  onSectionChange,
  onOpenHelp,
  onOpenAssistantSettings,
  onAskMate,
}: DocumentsPanelProps) {
  const [folderId, setFolderId] = useState<string | null>(initialFolderId)
  const documents = useDocuments(folderId)
  // Ask Mate about a selection (ask-mate-selection.tsx) is hidden entirely
  // unless Mate itself is actually usable right now - the same status
  // AssistantDrawer's own "problem" card already gates its composer on
  // (hooks/use-assistant-status.ts), reused here rather than re-derived so
  // this popover can never appear while the panel would just fail to send.
  const assistantStatus = useAssistantStatus()
  const mateAvailable = assistantStatus.status !== null && !assistantStatus.status.problem
  // No attachment cap here (review finding, use-document-uploads.ts:173):
  // MAX_ATTACHMENTS is Mate's per-message limit, and this panel isn't
  // building a message. `sequential` avoids opening one XMLHttpRequest per
  // file for a large dropped batch.
  const uploads = useDocumentUploads(folderId, { maxAttachments: NO_ATTACHMENT_CAP, sequential: true })

  // ── Manual-kind folders (revision "one panel, not three") ─────────────
  // The library-wide list of manuals - the ONLY way to know a folder is a
  // manual at all: GET /api/document-folders never serialises
  // document_folders.role (ordinary folder browsing has never needed it),
  // so this is a second hook instance purely for its `manuals` array,
  // exactly the pattern the deleted notes-panel.tsx's own FileNotePicker
  // already used (`useManuals(null)` for the id set). Its own tree fetch is
  // always a no-op (manualId: null), so this costs one GET /api/manuals,
  // not a wasted tree fetch on every folder browsed.
  const manualsList = useManuals(null)
  const isCurrentFolderManual = folderId !== null && manualsList.manuals.some((m) => m.id === folderId)
  const manualFolderIds = useMemo(() => new Set(manualsList.manuals.map((m) => m.id)), [manualsList.manuals])

  // ── Unfiled notes saved view + the viewer's note editing (revision) ────
  // One useNotes() instance serves three things: the "Unfiled notes" view
  // and its visible count (unfiledOnly: true governs refresh()'s own
  // query), the type-override dropdown on its rows, and the general
  // viewer's Edit/Save for ANY note (patchNote/getNote work on any note id
  // regardless of the list filter) - see the "the viewer" section below.
  const unfiled = useNotes({ unfiledOnly: true })
  const [view, setView] = useState<'browse' | 'unfiled'>('browse')

  // ── capture sheet (ADR 0121) ────────────────────────────────────────────
  // The New → Note menu below is the only way in now - no more global
  // header button/Alt+N handing this panel a type through a callback prop.
  // Same lazy-mount-on-first-open latch App.tsx used to keep for its sheets:
  // this ref goes true the first time captureOpen does and never resets, so
  // NoteCaptureSheet mounts once, on first use, and then stays mounted
  // across later closes.
  const [captureOpen, setCaptureOpen] = useState(false)
  const [captureType, setCaptureType] = useState<NoteType | undefined>(undefined)
  const captureHasOpenedRef = useRef(false)
  if (captureOpen) captureHasOpenedRef.current = true
  const openCapture = useCallback((type?: NoteType) => {
    setCaptureType(type)
    setCaptureOpen(true)
  }, [])

  const navigate = useCallback((id: string | null) => {
    setFolderId(id)
    setView('browse') // browsing a folder always leaves the Unfiled notes view
    onFolderChange?.(id)
  }, [onFolderChange])

  // Re-syncs when `initialFolderId` changes after mount. App.tsx keeps the
  // Suspense key on 'documents' while browsing (only a panel switch
  // remounts it - see App.tsx's activePanelContent comment), so browser
  // Back/Forward can only reach this already-mounted panel by handing it a
  // new `initialFolderId` prop through its popstate handler and
  // applyAppLocation, not by remounting it - the `useState` above only
  // reads that prop once. Mirrors use-assistant-conversations.ts's own
  // re-select effect for `initialConversationId`, but without that one's
  // "only when non-null" guard: unlike a conversation id, where null means
  // "leave whatever's active alone", the root folder (null) is a real
  // destination Back/Forward can land on.
  const previousInitialFolderIdRef = useRef(initialFolderId)
  useEffect(() => {
    const previous = previousInitialFolderIdRef.current
    previousInitialFolderIdRef.current = initialFolderId
    if (initialFolderId !== previous) {
      setFolderId(initialFolderId)
    }
  }, [initialFolderId])

  // ── section (ManualFolderView's reading pane, `?section=`) ──────────────
  // Same re-sync-on-prop-change pattern as initialFolderId above, and for
  // the identical reason (Back/Forward hands this panel a new prop rather
  // than remounting it).
  const [sectionId, setSectionIdState] = useState<string | null>(initialSectionId)
  const setSectionId = useCallback((id: string | null) => {
    setSectionIdState(id)
    onSectionChange?.(id)
  }, [onSectionChange])
  const previousInitialSectionIdRef = useRef(initialSectionId)
  useEffect(() => {
    const previous = previousInitialSectionIdRef.current
    previousInitialSectionIdRef.current = initialSectionId
    if (initialSectionId !== previous) setSectionIdState(initialSectionId)
  }, [initialSectionId])
  // A section id is only meaningful against the tree it was picked from -
  // leaving it set while navigating to a DIFFERENT folder would point a
  // freshly opened manual's reading pane at a section that (probably)
  // isn't even in its tree. Skips the very first run (the ref already holds
  // the just-mounted folderId), or this would immediately clear a section
  // id this panel was seeded with, on mount, before it's ever rendered.
  const previousFolderIdForSectionRef = useRef(folderId)
  useEffect(() => {
    const previous = previousFolderIdForSectionRef.current
    previousFolderIdForSectionRef.current = folderId
    if (folderId !== previous) setSectionId(null)
  }, [folderId, setSectionId])

  // ── search ────────────────────────────────────────────────────────────
  const [query, setQuery] = useState('')
  const [allFolders, setAllFolders] = useState(false)
  // Destructured so the effect below depends on these two functions
  // directly, not on `documents.search`/`documents.clearSearch` member
  // expressions - both stay useCallback-stable per folderId/selectedTag
  // (use-documents.ts), so re-running the effect when they change simply
  // re-issues the same query against the new scope.
  const { search: searchDocuments, clearSearch } = documents
  useEffect(() => {
    const trimmed = query.trim()
    const id = setTimeout(() => {
      if (trimmed === '') {
        clearSearch()
      } else {
        void searchDocuments(trimmed, { allFolders })
      }
    }, SEARCH_DEBOUNCE_MS)
    return () => clearTimeout(id)
  }, [query, allFolders, searchDocuments, clearSearch])

  const searching = documents.searchResults !== null

  // The chip row's options: documents.tags plus, if the active filter has
  // fallen out of that list, the active filter itself. TagCounts
  // (backend/documents_store.go) now leaves out a tag used as a suggested
  // one on only a single document, so a filter selected while a tag still
  // qualified can outlive its own chip - the next refreshTags() (the 3s
  // poll, or a patch that drops the doc's other tag) simply stops
  // returning it. Without this, the ToggleGroup's value points at a tag
  // with no ToggleGroupItem, so nothing renders pressed and there is no
  // control left to clear it. The count shown for that synthesized entry
  // is how many of the currently listed documents carry it, not the
  // library-wide distinct-document count TagCounts itself reports - the
  // real figure isn't available once TagCounts has already dropped the
  // tag, and this row only exists to stay visible and clearable, not to
  // re-derive a number nothing asked for.
  const tagFilterOptions = useMemo(() => {
    const tags = documents.tags
    const selected = documents.selectedTag
    if (!selected || tags.some((t) => t.tag === selected)) return tags
    const count = documents.documents.filter((d) => d.tags.some((t) => t.tag === selected)).length
    return [...tags, { tag: selected, count }]
  }, [documents.tags, documents.selectedTag, documents.documents])

  // A search result carries only folder_id (documentSearchResult,
  // backend/documents_search.go never adds a name or path to it), so a
  // human label for it is resolved here, once per distinct folder,
  // reusing the same GET /api/document-folders?parent= endpoint folder
  // browsing already calls (its own `path` array is exactly a breadcrumb).
  const [folderLabels, setFolderLabels] = useState<Record<string, string>>({})
  useEffect(() => {
    const results = documents.searchResults
    if (!results) return
    const missing = Array.from(new Set(
      results.map((r) => r.folder_id).filter((id): id is string => id !== null && !(id in folderLabels)),
    ))
    if (missing.length === 0) return
    for (const id of missing) {
      void fetch(`${apiBaseUrl}/api/document-folders?parent=${encodeURIComponent(id)}`)
        .then((res) => (res.ok ? res.json() : null))
        .then((data: { path?: DocumentFolder[] } | null) => {
          const label = data?.path && data.path.length > 0 ? data.path.map((f) => f.name).join(' / ') : id
          setFolderLabels((prev) => ({ ...prev, [id]: label }))
        })
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps -- folderLabels is read only to compute `missing`; including it would re-run this effect on every label it itself just set.
  }, [documents.searchResults])
  const folderLabelFor = useCallback((folderId: string | null) => {
    if (folderId === null) return 'Documents'
    return folderLabels[folderId] ?? '…'
  }, [folderLabels])

  // ── selection ─────────────────────────────────────────────────────────
  const [selectedIds, setSelectedIds] = useState<Set<string>>(new Set())
  useEffect(() => { setSelectedIds(new Set()) }, [folderId])
  const toggleSelected = (id: string) => {
    setSelectedIds((prev) => {
      const next = new Set(prev)
      if (next.has(id)) next.delete(id); else next.add(id)
      return next
    })
  }

  // ── action feedback ──────────────────────────────────────────────────
  const [actionError, setActionError] = useState<string | null>(null)
  const runAction = useCallback(async (action: () => Promise<unknown>) => {
    try {
      await action()
      setActionError(null)
    } catch (err) {
      setActionError(err instanceof Error ? err.message : String(err))
    }
  }, [])

  // ── new folder ────────────────────────────────────────────────────────
  const [newFolderOpen, setNewFolderOpen] = useState(false)
  const [newFolderName, setNewFolderName] = useState('')
  // "Creating a folder offers the kind" (revision "one panel, not three") -
  // only meaningful at the root (folderId === null): a manual must be
  // top-level (backend's errManualNotTopLevel), so the checkbox itself is
  // hidden rather than offering a choice that would always 409.
  const [newFolderIsManual, setNewFolderIsManual] = useState(false)
  const submitNewFolder = async () => {
    const name = newFolderName.trim()
    if (name === '') return
    await runAction(async () => {
      const created = await documents.createFolder(name, folderId)
      if (newFolderIsManual) await manualsList.flagManual(created.id)
      setNewFolderOpen(false)
      setNewFolderName('')
      setNewFolderIsManual(false)
    })
  }

  // ── rename (folder or document title) ───────────────────────────────
  const [renameTarget, setRenameTarget] = useState<RenameTarget | null>(null)
  const [renameValue, setRenameValue] = useState('')
  const submitRename = async () => {
    if (!renameTarget) return
    const value = renameValue.trim()
    if (value === '') return
    await runAction(async () => {
      if (renameTarget.kind === 'folder') {
        await documents.renameFolder(renameTarget.id, value)
      } else {
        await documents.patchDocument(renameTarget.id, { title: value })
      }
      setRenameTarget(null)
    })
  }

  // ── move (single row or bulk selection) ─────────────────────────────
  const [moveTarget, setMoveTarget] = useState<MoveTarget | null>(null)
  const submitMove = async (destination: string | null) => {
    if (!moveTarget) return
    await runAction(async () => {
      if (moveTarget.ids.length === 1 && documents.folders.some((f) => f.id === moveTarget.ids[0])) {
        await documents.moveFolder(moveTarget.ids[0], destination)
      } else {
        await documents.moveDocuments(moveTarget.ids, destination)
      }
      setMoveTarget(null)
      setSelectedIds(new Set())
      // A moved id may have been an unfiled note draining out of the inbox
      // (File… on an Unfiled notes row, revision "one panel, not three") -
      // always refreshed rather than tracked per-id, the same "simpler over
      // cleverer" trade the rest of this file already makes for uploads.
      void unfiled.refresh()
    })
  }

  // ── delete (folder, single document, or the whole bulk selection) ────
  // All three go through the one AlertDialog below - a bulk delete is not
  // exempt from the same "are you sure" every other delete gets here.
  const [deleteTarget, setDeleteTarget] = useState<DeleteTarget | null>(null)
  const submitDelete = async () => {
    if (!deleteTarget) return
    await runAction(async () => {
      if (deleteTarget.kind === 'folder') {
        await documents.deleteFolder(deleteTarget.id)
      } else if (deleteTarget.kind === 'document') {
        await documents.deleteDocument(deleteTarget.id)
        setSelectedIds((prev) => {
          const next = new Set(prev)
          next.delete(deleteTarget.id)
          return next
        })
      } else {
        for (const id of deleteTarget.ids) {
          await documents.deleteDocument(id)
        }
        setSelectedIds(new Set())
      }
      setDeleteTarget(null)
      void unfiled.refresh() // a deleted document may have been an unfiled note
    })
  }

  // ── reindex ──────────────────────────────────────────────────────────
  const [reindexTarget, setReindexTarget] = useState<ReindexTarget | null>(null)
  const submitReindex = async () => {
    if (!reindexTarget) return
    await runAction(async () => {
      await documents.reindexDocument(reindexTarget.id)
      setReindexTarget(null)
    })
  }

  // ── viewer ───────────────────────────────────────────────────────────
  const [viewerId, setViewerId] = useState<string | null>(initialDocumentId)
  const [viewerDoc, setViewerDoc] = useState<DocumentRecord | null>(null)
  const [viewerText, setViewerText] = useState<string | null>(null)
  const [viewerLoading, setViewerLoading] = useState(false)
  useEffect(() => {
    if (!viewerId) {
      setViewerDoc(null)
      setViewerText(null)
      return
    }
    // A viewer target may not be in whatever folder is currently browsed -
    // a Mate attachment chip can link to a document filed anywhere - so its
    // own metadata is fetched directly rather than looked up in
    // documents.documents.
    const found = documents.documents.find((d) => d.id === viewerId)
    if (found) {
      setViewerDoc(found)
    } else {
      setViewerLoading(true)
      void fetch(`${apiBaseUrl}/api/documents/${encodeURIComponent(viewerId)}`)
        .then((res) => (res.ok ? res.json() : null))
        .then((data) => setViewerDoc(data))
        .finally(() => setViewerLoading(false))
    }
  }, [viewerId, documents.documents])

  // Covers both reading a note and saving one - same banner either way.
  const [viewerError, setViewerError] = useState<string | null>(null)
  const { getNote: getViewerNote } = unfiled
  useEffect(() => {
    if (!viewerDoc || !viewerId) return
    const isText = !viewerDoc.mime.startsWith('image/') && viewerDoc.mime !== 'application/pdf'
    if (!isText) { setViewerText(null); return }
    setViewerLoading(true)

    // A NOTE is read from its own bytes, never from /text.
    //
    // /text reassembles the indexed chunks, and splitMarkdownSections
    // (documents_chunk.go) lifts each section's heading into its own
    // column and strips it out of the chunk body - so a note read that
    // way comes back with every "#" line missing, blank lines
    // normalised, and, past documentTextChunkCharCap, simply truncated.
    // Harmless for search, which is what chunks are for. Ruinous here,
    // because this same string seeds NoteEditor, and Save replaces the
    // note's whole body: one edit would delete every heading in it, and
    // with them a manual's section structure.
    //
    // GET /api/notes/:id is readNoteBody straight off disk - the real
    // bytes, which are the note's identity. It is also fresh regardless
    // of whether the indexer has caught up, where chunks may still be
    // stale or absent for a note captured seconds ago.
    let cancelled = false
    const load = viewerDoc.kind === 'note'
      ? getViewerNote(viewerId).then((detail) => detail.body)
      : fetch(`${apiBaseUrl}/api/documents/${encodeURIComponent(viewerId)}/text`)
          .then((res) => (res.ok ? res.json() : { text: '' }))
          .then((data) => data.text ?? '')

    void load
      .then((text) => { if (!cancelled) setViewerText(text) })
      .catch((err) => {
        // Fail loud (AGENTS.md): an unreadable note must not render as an
        // empty one, which looks exactly like a note the operator emptied.
        if (!cancelled) setViewerText(null)
        if (!cancelled) setViewerError(err instanceof Error ? err.message : String(err))
      })
      .finally(() => { if (!cancelled) setViewerLoading(false) })
    return () => { cancelled = true }
  }, [viewerDoc, viewerId, getViewerNote])

  // ── editing a note from the general viewer (revision "one panel, not
  // three") - the Edit toggle NoteEditor is reachable through, moved out of
  // the deleted notes-panel.tsx's reader sheet. Opening a DIFFERENT
  // document always lands back in the read-only view - an editing session
  // belongs to the document that was open when it started.
  const [viewerEditing, setViewerEditing] = useState(false)
  useEffect(() => { setViewerEditing(false) }, [viewerId])
  const { patchNote: patchViewerNote } = unfiled
  const handleSaveViewerNote = useCallback(async (body: string) => {
    if (!viewerId) return
    try {
      const updated = await patchViewerNote(viewerId, { body })
      setViewerText(updated.body)
      setViewerError(null)
      setViewerEditing(false)
    } catch (err) {
      setViewerError(err instanceof Error ? err.message : String(err))
      throw err // NoteEditor keeps its dirty flag on a failed save
    }
  }, [viewerId, patchViewerNote])

  // ── pinning a note for Mate (plan §8) - "a control in the Documents UI
  // where a note is read", so it lives beside Edit/Start checklist in this
  // same viewer toolbar rather than as a new surface of its own. Without
  // this, documents.pinned (already a real column, already on the wire)
  // has no operator-facing way to ever become true, and the whole
  // pinned-notes prompt feature is unreachable.
  const handleTogglePin = useCallback(async () => {
    if (!viewerDoc || viewerDoc.kind !== 'note') return
    await runAction(async () => {
      const updated = await patchViewerNote(viewerDoc.id, { pinned: !viewerDoc.pinned })
      setViewerDoc((prev) => (prev && prev.id === updated.document.id ? { ...prev, pinned: updated.document.pinned } : prev))
    })
  }, [viewerDoc, patchViewerNote, runAction])

  // ── running a checklist from the general viewer (plan §7, ADR 0118) - a
  // MODE of this same Sheet, not a navigation elsewhere. viewerText above
  // comes from the generic /api/documents/:id/text (any mime, any kind), so
  // whether this document even HAS a checklist has to come from the notes-
  // specific GET /api/notes/:id instead (its own `checklist` field) -
  // fetched here rather than folded into the fetch above, since it's only
  // ever relevant for kind='note'.
  const [viewerHasChecklist, setViewerHasChecklist] = useState(false)
  useEffect(() => {
    if (!viewerDoc || viewerDoc.kind !== 'note') {
      setViewerHasChecklist(false)
      return
    }
    let cancelled = false
    // Promise.resolve(...) rather than calling getViewerNote's own promise
    // directly: it's a mocked jest-style fn in most of this file's own
    // tests (vi.mock('@/hooks/use-notes')), and plenty of them never bother
    // stubbing a resolved value for a getNote() call they aren't testing -
    // this must degrade to "no checklist" rather than throw on a bare
    // vi.fn()'s undefined return.
    Promise.resolve(getViewerNote(viewerDoc.id))
      .then((detail) => { if (!cancelled) setViewerHasChecklist((detail?.checklist?.length ?? 0) > 0) })
      .catch(() => { if (!cancelled) setViewerHasChecklist(false) })
    return () => { cancelled = true }
  }, [viewerDoc, getViewerNote])

  // Reset whenever a DIFFERENT document opens - a running checklist belongs
  // to the document that was open when it started, the same rule
  // viewerEditing's own reset above follows.
  const [viewerChecklistRunning, setViewerChecklistRunning] = useState(false)
  useEffect(() => { setViewerChecklistRunning(false) }, [viewerId])

  // A link inside a rendered note's markdown (ADR 0116). GET /api/documents/:id
  // and its /text sibling work on any document id regardless of kind, so
  // both hc-note: and hc-doc: links resolve the same way here: switch the
  // viewer to that id, reusing the exact fetches above rather than needing
  // note-specific ones. An anchor scrolls within whatever is already
  // rendered. `external`/`unsafe` never reach here - note-markdown-impl.tsx
  // handles both itself.
  const handleNoteNavigate = useCallback((link: NoteLink) => {
    if (link.kind === 'note' || link.kind === 'document') {
      setViewerId(link.id)
      return
    }
    if (link.kind === 'anchor') {
      document.getElementById(link.hash)?.scrollIntoView({ block: 'start' })
    }
  }, [])

  // ── upload ───────────────────────────────────────────────────────────
  const fileInputRef = useRef<HTMLInputElement | null>(null)
  const handleFilesSelected = (e: ChangeEvent<HTMLInputElement>) => {
    if (e.target.files && e.target.files.length > 0) uploads.add(Array.from(e.target.files))
    e.target.value = ''
  }
  const [dragOver, setDragOver] = useState(false)
  const handleDrop = (e: DragEvent<HTMLDivElement>) => {
    e.preventDefault()
    setDragOver(false)
    if (e.dataTransfer.files.length > 0) uploads.add(Array.from(e.dataTransfer.files))
  }

  // Once a staged upload reaches a terminal, document-backed state, its row
  // exists in whatever folder it was uploaded into - refreshing here is
  // what makes that row actually appear (nothing else is watching
  // uploads.items), and the chip is then redundant with the table row it
  // just produced, so it's dismissed rather than left duplicated beside it.
  // handledUploadKeysRef dedupes so this doesn't refresh/remove repeatedly
  // for the same item across re-renders before its chip is actually gone.
  const handledUploadKeysRef = useRef(new Set<string>())
  useEffect(() => {
    for (const item of uploads.items) {
      if (item.documentId === null) continue
      if (item.status !== 'indexed' && item.status !== 'failed') continue
      if (handledUploadKeysRef.current.has(item.key)) continue
      handledUploadKeysRef.current.add(item.key)
      void documents.refresh()
      uploads.remove(item.key)
    }
  }, [uploads.items, uploads, documents])

  const folderRows = useMemo(() => documents.folders, [documents.folders])
  // No note-type facet here. One was built and removed: a library folder
  // holds mostly kind='file' rows (PDFs, photos, the builder's handbook),
  // so a type filter is inapplicable to most of what is on screen and
  // silently empties the listing when a facet is active. The type is still
  // worth SEEING on a note row - that is the pre-attentive icon column, and
  // it stays - but it is not a useful axis to slice a mixed library on. The
  // Unfiled notes view already covers the one case that genuinely wanted a
  // notes-only listing.
  const documentRows = documents.documents
  const allDocumentsSelected = documentRows.length > 0 && documentRows.every((d) => selectedIds.has(d.id))

  const contentUrlFor = (id: string) => `${apiBaseUrl}/api/documents/${encodeURIComponent(id)}/content`

  // Fetches the file into a blob and saves it via a synthetic <a download>
  // click, rather than navigating the tab there directly (review finding).
  // A plain `window.location.href = contentUrlFor(id)` turned a 401 or the
  // endpoint's own 500 "document file missing on disk"
  // (documentContentHandler, backend/documents_handlers.go) into the SPA
  // itself being replaced by raw JSON, with no way back short of a reload.
  // A failure here throws instead, so runAction's existing catch routes it
  // into actionError - the same banner rename/move/delete already share -
  // and the app never leaves the page.
  const downloadDocument = async (id: string, filename: string) => {
    const response = await fetch(`${contentUrlFor(id)}?download=1`)
    if (!response.ok) {
      const body = (await response.json().catch(() => null)) as { error?: string } | null
      throw new Error(body?.error && body.error !== '' ? body.error : `Download failed (HTTP ${response.status})`)
    }
    const blob = await response.blob()
    const url = URL.createObjectURL(blob)
    try {
      const link = document.createElement('a')
      link.href = url
      link.download = filename
      link.click()
    } finally {
      URL.revokeObjectURL(url)
    }
  }

  return (
    <div className="flex h-full min-h-0 flex-col gap-4 p-4">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <Breadcrumb>
          <BreadcrumbList>
            <BreadcrumbItem>
              {documents.path.length === 0 ? (
                <BreadcrumbPage>Documents</BreadcrumbPage>
              ) : (
                <BreadcrumbLink href="#" onClick={(e) => { e.preventDefault(); navigate(null) }}>
                  Documents
                </BreadcrumbLink>
              )}
            </BreadcrumbItem>
            {documents.path.map((folder, index) => (
              <span key={folder.id} className="contents">
                <BreadcrumbSeparator />
                <BreadcrumbItem>
                  {index === documents.path.length - 1 ? (
                    <BreadcrumbPage>{folder.name}</BreadcrumbPage>
                  ) : (
                    <BreadcrumbLink href="#" onClick={(e) => { e.preventDefault(); navigate(folder.id) }}>
                      {folder.name}
                    </BreadcrumbLink>
                  )}
                </BreadcrumbItem>
              </span>
            ))}
          </BreadcrumbList>
        </Breadcrumb>
        <div className="flex items-center gap-2">
          {/* Revision "one panel, not three": ONE New menu, replacing the
              old plain "New folder" button and, briefly, a separate Add
              Note split button beside it. Two adjacent create controls in
              one toolbar was the wrong shape - everything this panel
              creates now hangs off a single verb, with Folder first
              (Manual is a checkbox inside its dialog when browsing the
              root) and Note carrying its kinds in a submenu.

              Auto leads that submenu, and picking it sends no `type` at
              all so the Go classifier answers (ADR 0116). ADR 0119 once
              also gave capture a global header button and an Alt+N
              shortcut reachable from any screen; ADR 0121 removed both -
              a note is a document, and this is where every other document
              this panel creates already starts, so this menu is now the
              only path in, not just the discoverable one. */}
          <DropdownMenu>
            <DropdownMenuTrigger
              render={
                <Button type="button" variant="outline" size="sm">
                  <Plus className="h-4 w-4" data-icon="inline-start" aria-hidden="true" />
                  New
                </Button>
              }
            />
            <DropdownMenuContent>
              <DropdownMenuItem onClick={() => setNewFolderOpen(true)}>
                <FolderPlus className="h-4 w-4" aria-hidden="true" /> Folder
              </DropdownMenuItem>
              <DropdownMenuSub>
                {/* ADR 0124: hovering or focusing this is the earliest
                    signal that the operator is about to open the editor -
                    warms note-editor-impl.tsx's chunk (and the capture
                    sheet's own) right away rather than waiting for the
                    click, the same "prefetch on hover/focus" App.tsx's own
                    idle prefetch complements. prefetchNoteEditor() is
                    memoised, so a hover that never leads to a click costs
                    nothing beyond the one fetch every other path already
                    pays for eventually. */}
                <DropdownMenuSubTrigger onPointerEnter={() => prefetchNoteEditor()} onFocus={() => prefetchNoteEditor()}>
                  <Pencil className="h-4 w-4" aria-hidden="true" /> Note
                </DropdownMenuSubTrigger>
                <DropdownMenuSubContent>
                  <DropdownMenuItem onClick={() => openCapture()}>
                    <Sparkles className="h-4 w-4 text-muted-foreground" aria-hidden="true" /> Auto
                  </DropdownMenuItem>
                  <DropdownMenuSeparator />
                  {NOTE_TYPE_ORDER.map((type) => {
                    const meta = NOTE_TYPE_META[type]
                    const Icon = meta.icon
                    // The catch-all kind is labelled "Note" everywhere else,
                    // which is fine when the question is "what kind of note
                    // is this". Here it would read New > Note > Note, so it
                    // says "Plain note" in this one menu. The stored value
                    // is 'note' either way - this is a label, not a type.
                    const label = type === 'note' ? 'Plain note' : meta.label
                    return (
                      <DropdownMenuItem key={type} onClick={() => openCapture(type)}>
                        <Icon className={cn('h-4 w-4', meta.className)} aria-hidden="true" /> {label}
                      </DropdownMenuItem>
                    )
                  })}
                </DropdownMenuSubContent>
              </DropdownMenuSub>
            </DropdownMenuContent>
          </DropdownMenu>
          <Button type="button" size="sm" onClick={() => fileInputRef.current?.click()}>
            <UploadIcon className="h-4 w-4" data-icon="inline-start" aria-hidden="true" />
            Upload
          </Button>
          <input
            ref={fileInputRef}
            type="file"
            multiple
            className="hidden"
            onChange={handleFilesSelected}
            data-testid="documents-file-input"
          />
        </div>
      </div>

      <div className="flex flex-wrap items-center gap-3">
        <Input
          type="search"
          aria-label="Search documents"
          placeholder="Search this folder…"
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          className="max-w-xs"
        />
        <div className="flex items-center gap-2">
          <Switch aria-label="All folders" checked={allFolders} onCheckedChange={setAllFolders} />
          <Label className="text-xs text-muted-foreground">All folders</Label>
        </div>
        {tagFilterOptions.length > 0 && (
          <ToggleGroup
            aria-label="Filter by tag"
            value={documents.selectedTag ? [documents.selectedTag] : []}
            onValueChange={(value: string[]) => documents.setSelectedTag(value[value.length - 1] ?? null)}
            variant="outline"
            size="sm"
          >
            {tagFilterOptions.map((t) => (
              <ToggleGroupItem key={t.tag} value={t.tag}>
                {t.tag} ({t.count})
              </ToggleGroupItem>
            ))}
          </ToggleGroup>
        )}
        {/* "Unfiled notes" as a saved view, not a place (revision) -
            kind='note' AND folder_id IS NULL, preserving the deleted
            notes-panel.tsx's drain-the-inbox loop and its visible count
            without owning a sidebar row. */}
        <Button
          type="button"
          variant="outline"
          size="sm"
          aria-pressed={view === 'unfiled'}
          className={cn(view === 'unfiled' && 'border-primary/40 bg-primary/10 text-primary')}
          onClick={() => setView((prev) => (prev === 'unfiled' ? 'browse' : 'unfiled'))}
        >
          Unfiled notes ({unfiled.notes.length})
        </Button>
        <MateFailureRow status={documents.embeddingsStatus} onOpenAssistantSettings={onOpenAssistantSettings} />
      </div>

      {uploads.items.length > 0 && (
        <div className="flex flex-wrap gap-1.5" data-testid="documents-upload-items">
          {uploads.items.map((item) => (
            <div
              key={item.key}
              className={cn(
                'flex items-center gap-1.5 rounded-md border px-2 py-1 text-[11px]',
                item.status === 'failed' ? 'border-destructive/40 bg-destructive/10' : 'border-border bg-muted/50',
              )}
            >
              {item.status === 'uploading' && <Loader2 className="h-3 w-3 shrink-0 animate-spin" aria-hidden="true" />}
              <span className="max-w-40 truncate" title={item.filename}>{item.filename}</span>
              <span className={cn('tabular-nums', item.status === 'failed' ? 'text-destructive' : 'text-muted-foreground')}>
                {stagedUploadStatusLabel(item)}
              </span>
            </div>
          ))}
        </div>
      )}

      {actionError && (
        <p role="alert" className="rounded-md border border-destructive/40 bg-destructive/10 px-3 py-2 text-sm text-destructive">
          {actionError}
        </p>
      )}
      {uploads.error && (
        <p role="alert" className="rounded-md border border-destructive/40 bg-destructive/10 px-3 py-2 text-sm text-destructive">
          {uploads.error}
        </p>
      )}
      {documents.error && <p role="alert" className="text-sm text-destructive">{documents.error}</p>}

      {selectedIds.size > 0 && (
        <div className="flex items-center gap-2 rounded-md border bg-muted/40 px-3 py-2 text-sm">
          <span className="font-medium">{selectedIds.size} selected</span>
          <Button
            type="button"
            size="sm"
            variant="outline"
            onClick={() => setMoveTarget({ ids: Array.from(selectedIds), label: `${selectedIds.size} document(s)` })}
          >
            Move to…
          </Button>
          <Button type="button" size="sm" variant="outline" onClick={() => setDeleteTarget({ kind: 'bulk', ids: Array.from(selectedIds) })}>
            Delete
          </Button>
        </div>
      )}

      <div
        className={cn('flex-1 min-h-0 overflow-auto rounded-md border', dragOver && 'ring-2 ring-primary')}
        data-testid="documents-dropzone"
        onDragOver={(e) => { e.preventDefault(); setDragOver(true) }}
        onDragLeave={() => setDragOver(false)}
        onDrop={handleDrop}
      >
        {searching ? (
          <>
            {documents.semanticProblem && (
              <p className="border-b border-amber-500/40 bg-amber-500/10 px-3 py-1.5 text-xs text-amber-600 dark:text-amber-400">
                Showing keyword results only. {documents.semanticProblem}
              </p>
            )}
            <SearchResultsTable
              results={documents.searchResults ?? []}
              searching={documents.searching}
              searchError={documents.searchError}
              onOpen={setViewerId}
              folderLabelFor={folderLabelFor}
            />
          </>
        ) : view === 'unfiled' ? (
          <UnfiledNotesView
            notes={unfiled.notes}
            loading={unfiled.loading}
            onOpen={setViewerId}
            onSetType={(id, type) => { void runAction(() => unfiled.patchNote(id, { type })) }}
            onFile={(id, title) => setMoveTarget({ ids: [id], label: title })}
          />
        ) : isCurrentFolderManual && folderId !== null ? (
          <ManualFolderView
            folderId={folderId}
            sectionId={sectionId}
            onSectionChange={setSectionId}
            onDemoted={() => { void manualsList.refreshManuals() }}
            onNavigateDocument={setViewerId}
            mateAvailable={mateAvailable}
            onAskMate={onAskMate}
          />
        ) : (
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead className="w-10">
                  <Checkbox
                    aria-label="Select all documents"
                    checked={allDocumentsSelected}
                    onCheckedChange={(checked) => {
                      setSelectedIds(checked ? new Set(documentRows.map((d) => d.id)) : new Set())
                    }}
                  />
                </TableHead>
                <TableHead>Name</TableHead>
                <TableHead>Status</TableHead>
                <TableHead>Tags</TableHead>
                <TableHead>Size</TableHead>
                <TableHead className="w-10" />
              </TableRow>
            </TableHeader>
            <TableBody>
              {folderRows.length === 0 && documentRows.length === 0 && (
                <TableRow>
                  <TableCell colSpan={6} className="py-8 text-center text-sm text-muted-foreground">
                    Nothing here yet. Upload a file or create a folder.
                  </TableCell>
                </TableRow>
              )}
              {folderRows.map((folder) => {
                const isManual = manualFolderIds.has(folder.id)
                return (
                  <TableRow key={folder.id}>
                    <TableCell />
                    <TableCell>
                      <button
                        type="button"
                        onClick={() => navigate(folder.id)}
                        className="flex items-center gap-2 text-left font-medium hover:underline"
                      >
                        {/* Library, not Folder, for a Manual-kind folder -
                            the only way to see that from the OUTSIDE
                            (browsing its parent) at all, since
                            GET /api/document-folders never serialises
                            document_folders.role. */}
                        {isManual ? (
                          <Library className="h-4 w-4 shrink-0 text-muted-foreground" aria-hidden="true" />
                        ) : (
                          <Folder className="h-4 w-4 shrink-0 text-muted-foreground" aria-hidden="true" />
                        )}
                        {folder.name}
                      </button>
                    </TableCell>
                    {/* Status, Tags and Size are document-only columns - a
                        folder row still needs a placeholder cell for each
                        of the header's six columns, or the actions cell
                        below shifts left under "Size" and the real w-10
                        actions column sits empty (review finding). */}
                    <TableCell />
                    <TableCell />
                    <TableCell />
                    <TableCell>
                      <FolderRowMenu
                        folder={folder}
                        isManual={isManual}
                        canPromote={folderId === null}
                        onRename={() => { setRenameTarget({ kind: 'folder', id: folder.id, name: folder.name }); setRenameValue(folder.name) }}
                        onMove={() => setMoveTarget({ ids: [folder.id], label: folder.name })}
                        onDelete={() => setDeleteTarget({ kind: 'folder', id: folder.id, name: folder.name })}
                        onPromote={() => { void runAction(() => manualsList.flagManual(folder.id)) }}
                        onDemote={() => { void runAction(() => manualsList.clearManual(folder.id)) }}
                      />
                    </TableCell>
                  </TableRow>
                )
              })}
              {documentRows.map((doc) => {
                const name = documentDisplayName(doc)
                const MimeIcon = rowIcon(doc.mime)
                return (
                  <TableRow key={doc.id}>
                    <TableCell>
                      <Checkbox
                        aria-label={`Select ${name}`}
                        checked={selectedIds.has(doc.id)}
                        onCheckedChange={() => toggleSelected(doc.id)}
                      />
                    </TableCell>
                    <TableCell>
                      <div className="flex items-center gap-2">
                        {/* The pre-attentive type icon column (revision
                            "Note types as a facet") - a note row's icon is
                            its own hit target opening a type-override menu,
                            same gesture as the Unfiled notes view's rows
                            (unfiled.patchNote works on any note id
                            regardless of filing state). A plain kind='file'
                            row keeps the ordinary mime-based icon, inert. */}
                        {doc.kind === 'note' ? (
                          <NoteTypeIconButton noteType={doc.note_type} onSetType={(type) => { void runAction(() => unfiled.patchNote(doc.id, { type })) }} />
                        ) : (
                          <MimeIcon className="h-4 w-4 shrink-0 text-muted-foreground" aria-hidden="true" />
                        )}
                        <button
                          type="button"
                          onClick={() => setViewerId(doc.id)}
                          className="flex flex-col items-start gap-0.5 text-left"
                        >
                          <span className="font-medium hover:underline">{name}</span>
                          {doc.status === 'failed' && doc.error && (
                            <span className="text-xs text-destructive">{doc.error}</span>
                          )}
                        </button>
                      </div>
                    </TableCell>
                    <TableCell>{statusBadge(doc)}</TableCell>
                    <TableCell>
                      <div className="flex flex-wrap gap-1">
                        {doc.tags.map((t) => (
                          <Badge key={t.tag} variant={t.source === 'operator' ? 'secondary' : 'outline'}>
                            {t.tag}
                          </Badge>
                        ))}
                      </div>
                    </TableCell>
                    <TableCell>{formatBytes(doc.size_bytes)}</TableCell>
                    <TableCell>
                      <DocumentRowMenu
                        doc={doc}
                        onOpen={() => setViewerId(doc.id)}
                        onDownload={() => { void runAction(() => downloadDocument(doc.id, doc.filename)) }}
                        onRename={() => { setRenameTarget({ kind: 'document', id: doc.id, name }); setRenameValue(name) }}
                        onDetails={() => onEditDocument?.(doc.id)}
                        onMove={() => setMoveTarget({ ids: [doc.id], label: name })}
                        onReindex={() => setReindexTarget({ id: doc.id, name, pageCount: doc.page_count, mime: doc.mime })}
                        onDelete={() => setDeleteTarget({ kind: 'document', id: doc.id, name })}
                      />
                    </TableCell>
                  </TableRow>
                )
              })}
            </TableBody>
          </Table>
        )}
      </div>

      {/* ── New folder ─────────────────────────────────────────────── */}
      <Dialog open={newFolderOpen} onOpenChange={(open) => { setNewFolderOpen(open); if (!open) setNewFolderIsManual(false) }}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>New folder</DialogTitle>
            <DialogDescription>Folders are virtual - the files themselves are untouched.</DialogDescription>
          </DialogHeader>
          <Label htmlFor="documents-new-folder-name">Name</Label>
          <Input
            id="documents-new-folder-name"
            value={newFolderName}
            onChange={(e) => setNewFolderName(e.target.value)}
            onKeyDown={(e) => { if (e.key === 'Enter') void submitNewFolder() }}
            autoFocus
          />
          {/* "Creating a folder offers the kind" - only at the root, since
              only a top-level folder can ever become a manual. */}
          {folderId === null && (
            <div className="flex items-center gap-2">
              <Checkbox id="documents-new-folder-manual" checked={newFolderIsManual} onCheckedChange={(checked) => setNewFolderIsManual(checked === true)} />
              <Label htmlFor="documents-new-folder-manual" className="text-sm font-normal">
                Manual - ordered, with a reading view
              </Label>
              {onOpenHelp && (
                <button
                  type="button"
                  onClick={() => onOpenHelp(MANUAL_HOWTO_HELP_TARGET)}
                  className="text-xs font-semibold uppercase tracking-[0.1em] text-primary hover:underline"
                >
                  What to put in it
                </button>
              )}
            </div>
          )}
          <DialogFooter>
            <Button type="button" variant="outline" onClick={() => setNewFolderOpen(false)}>Cancel</Button>
            <Button type="button" onClick={() => { void submitNewFolder() }}>Create</Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* ── Rename ─────────────────────────────────────────────────── */}
      <Dialog open={renameTarget !== null} onOpenChange={(open) => { if (!open) setRenameTarget(null) }}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Rename "{renameTarget?.name}"</DialogTitle>
          </DialogHeader>
          <Label htmlFor="documents-rename-value">{renameTarget?.kind === 'folder' ? 'Folder name' : 'Title'}</Label>
          <Input
            id="documents-rename-value"
            value={renameValue}
            onChange={(e) => setRenameValue(e.target.value)}
            onKeyDown={(e) => { if (e.key === 'Enter') void submitRename() }}
            autoFocus
          />
          <DialogFooter>
            <Button type="button" variant="outline" onClick={() => setRenameTarget(null)}>Cancel</Button>
            <Button type="button" onClick={() => { void submitRename() }}>Save</Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* ── Move ───────────────────────────────────────────────────── */}
      <Dialog open={moveTarget !== null} onOpenChange={(open) => { if (!open) setMoveTarget(null) }}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Move {moveTarget?.label}</DialogTitle>
            <DialogDescription>Pick the destination folder.</DialogDescription>
          </DialogHeader>
          {moveTarget && (
            <FolderPicker
              onPick={(destination) => { void submitMove(destination) }}
              onFolderCreated={() => { void documents.refresh() }}
            />
          )}
        </DialogContent>
      </Dialog>

      {/* ── Delete confirmation ────────────────────────────────────── */}
      <AlertDialog open={deleteTarget !== null} onOpenChange={(open) => { if (!open) setDeleteTarget(null) }}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>
              {deleteTarget?.kind === 'bulk' ? `Delete ${deleteTarget.ids.length} documents?` : `Delete "${deleteTarget?.name}"?`}
            </AlertDialogTitle>
            <AlertDialogDescription>
              {deleteTarget?.kind === 'folder'
                ? "This removes the folder. It has to be empty first."
                : "This removes the file(s). It can't be undone."}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>Cancel</AlertDialogCancel>
            <AlertDialogAction
              className="bg-destructive text-destructive-foreground hover:bg-destructive/90"
              onClick={() => { void submitDelete() }}
            >
              Delete
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>

      {/* ── Reindex confirmation ───────────────────────────────────── */}
      <AlertDialog open={reindexTarget !== null} onOpenChange={(open) => { if (!open) setReindexTarget(null) }}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Reindex "{reindexTarget?.name}"?</AlertDialogTitle>
            <AlertDialogDescription>
              This re-reads {reindexTarget?.pageCount ?? 1} page(s) from the beginning.
              {reindexTarget && (reindexTarget.mime === 'application/pdf' || reindexTarget.mime.startsWith('image/')) && (
                <> If Mate is on and OCR is needed, estimated cost: ${(Math.max(1, reindexTarget.pageCount) * OCR_COST_PER_PAGE_USD).toFixed(3)}.</>
              )}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>Cancel</AlertDialogCancel>
            <AlertDialogAction onClick={() => { void submitReindex() }}>Reindex</AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>

      {/* ── Viewer ─────────────────────────────────────────────────── */}
      <Sheet open={viewerId !== null} onOpenChange={(open) => { if (!open) setViewerId(null) }}>
        <SheetContent side="right" className="flex h-full w-full flex-col gap-4 sm:max-w-2xl">
          <SheetHeader>
            <SheetTitle>{viewerDoc ? documentDisplayName(viewerDoc) : 'Loading…'}</SheetTitle>
            {viewerDoc && <SheetDescription>{mimeLabel(viewerDoc.mime)} · {formatBytes(viewerDoc.size_bytes)}</SheetDescription>}
          </SheetHeader>
          {viewerDoc && (
            <div className="flex items-center gap-2">
              <Button type="button" size="sm" variant="outline" onClick={() => { void runAction(() => downloadDocument(viewerDoc.id, viewerDoc.filename)) }}>
                <Download className="h-4 w-4" data-icon="inline-start" aria-hidden="true" />
                Download
              </Button>
              <Button type="button" size="sm" variant="outline" onClick={() => onEditDocument?.(viewerDoc.id)}>
                <Info className="h-4 w-4" data-icon="inline-start" aria-hidden="true" />
                Details
              </Button>
              {/* Start checklist is a MODE of this same Sheet (plan §7,
                  ADR 0118) - gated on kind='note' (a checklist only ever
                  makes sense against a note's own body) and viewerHasChecklist
                  (GET /api/notes/:id's own `checklist` field), and hidden
                  while already editing or running - both would otherwise
                  compete for the same content area below. */}
              {viewerDoc.kind === 'note' && viewerHasChecklist && !viewerEditing && !viewerChecklistRunning && (
                <Button type="button" size="sm" variant="outline" onClick={() => setViewerChecklistRunning(true)}>
                  <ListChecks className="h-4 w-4" data-icon="inline-start" aria-hidden="true" />
                  Start checklist
                </Button>
              )}
              {/* Pin for Mate (plan §8) - gated on kind='note' the same way
                  every note-only control here is; a pinned note rides in
                  Mate's system prompt until unpinned, so the label always
                  says which state a click leads TO, not which state is
                  current. */}
              {viewerDoc.kind === 'note' && (
                <Button type="button" size="sm" variant="outline" aria-pressed={viewerDoc.pinned} onClick={() => { void handleTogglePin() }}>
                  {viewerDoc.pinned ? (
                    <>
                      <PinOff className="h-4 w-4" data-icon="inline-start" aria-hidden="true" />
                      Unpin
                    </>
                  ) : (
                    <>
                      <Pin className="h-4 w-4" data-icon="inline-start" aria-hidden="true" />
                      Pin for Mate
                    </>
                  )}
                </Button>
              )}
              {/* The Edit toggle NoteEditor is reachable through (moved out
                  of the deleted notes-panel.tsx's reader sheet) - gated on
                  kind='note', not just mime==='text/markdown': an uploaded
                  .md FILE has the same mime but no PATCH /api/notes/:id
                  route to save through (409 errNotANote). Hidden while a
                  checklist is running - the same "one mode at a time" rule
                  as Start checklist above. */}
              {viewerDoc.kind === 'note' && !viewerChecklistRunning && (
                <Button type="button" size="sm" variant="outline" onClick={() => setViewerEditing((prev) => !prev)}>
                  {viewerEditing ? (
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
          )}
          <div className="min-h-0 flex-1 overflow-auto rounded-md border">
            {viewerLoading && <p className="p-4 text-sm text-muted-foreground">Loading…</p>}
            {viewerError && <p role="alert" className="p-4 text-sm text-destructive">{viewerError}</p>}
            {!viewerLoading && viewerDoc?.mime === 'application/pdf' && (
              <iframe title={documentDisplayName(viewerDoc)} src={contentUrlFor(viewerDoc.id)} className="h-full min-h-[70vh] w-full" />
            )}
            {!viewerLoading && viewerDoc?.mime.startsWith('image/') && (
              <img src={contentUrlFor(viewerDoc.id)} alt={documentDisplayName(viewerDoc)} className="max-w-full" />
            )}
            {/* ADR 0116: a filed note is a document with mime: 'text/markdown'.
                Routed through the same NoteMarkdown the deleted
                notes-panel.tsx's own reader used, so a note reads the same
                way wherever it's opened from. text/plain, text/csv and
                application/json stay literal text - a CSV or a JSON blob
                rendered as markdown would be actively misleading, not an
                upgrade. Swapped for NoteEditor (ADR 0117) while editing a
                real note (kind='note'); an uploaded .md FILE has no editor
                to swap to (see the Edit toggle's own comment above), so it
                stays read-only regardless of viewerEditing. */}
            {!viewerLoading && viewerDoc?.mime === 'text/markdown' && viewerDoc.kind === 'note' && viewerChecklistRunning && (
              <ChecklistRunner
                noteId={viewerDoc.id}
                noteTitle={documentDisplayName(viewerDoc)}
                onExit={() => setViewerChecklistRunning(false)}
              />
            )}
            {!viewerLoading && viewerDoc?.mime === 'text/markdown' && viewerDoc.kind === 'note' && viewerEditing && !viewerChecklistRunning && (
              <div className="p-1">
                <NoteEditor key={viewerDoc.id} value={viewerText ?? ''} onSave={handleSaveViewerNote} />
              </div>
            )}
            {!viewerLoading && viewerDoc?.mime === 'text/markdown' && !viewerChecklistRunning && !(viewerDoc.kind === 'note' && viewerEditing) && (
              <div className="p-4">
                <AskMateSelection
                  noteId={viewerDoc.id}
                  noteTitle={documentDisplayName(viewerDoc)}
                  mateAvailable={mateAvailable}
                  onAskMate={onAskMate}
                >
                  <NoteMarkdown content={viewerText ?? ''} onNavigate={handleNoteNavigate} />
                </AskMateSelection>
              </div>
            )}
            {!viewerLoading && viewerDoc && viewerDoc.mime !== 'text/markdown' && !viewerDoc.mime.startsWith('image/') && viewerDoc.mime !== 'application/pdf' && (
              <pre className="whitespace-pre-wrap p-4 text-sm">{viewerText ?? ''}</pre>
            )}
          </div>
        </SheetContent>
      </Sheet>

      {/* ADR 0121: the one capture sheet, now reached only through New →
          Note above. onCaptured opens the fresh note straight in the
          viewer - setViewerId already fetches a document that isn't in
          whatever's currently loaded (see its own effect's comment), which
          covers a brand new unfiled note without this panel needing a
          second lookup path. The sheet's own useNotes() instance is
          separate from `unfiled` above (no shared cache between hook
          instances - use-notes.ts's own doc comment), so a plain
          setViewerId here would open the note without the Unfiled notes
          count or list ever learning it exists; unfiled.refresh() closes
          that gap now that both live in the same component. */}
      {captureHasOpenedRef.current && (
        <Suspense fallback={null}>
          <NoteCaptureSheet
            open={captureOpen}
            onOpenChange={setCaptureOpen}
            initialType={captureType}
            onCaptured={(id) => { setViewerId(id); void unfiled.refresh() }}
          />
        </Suspense>
      )}
    </div>
  )
}

// ADR 0120: "turning Mate on is the consent" retired the toolbar's three
// backlog banners (semantic search indexing, note classification, note
// enrichment), each with its own count, button and confirm dialog. A note
// is classified the moment it's captured (there was never a real
// classify backlog to show), and once Mate is ready the enrich/embed sweep
// runs itself, at boot and on every settings or secrets save (SweepIfReady,
// documents_embed.go) - repeating the same consent question on every visit
// to Documents just read as nagging. The one state still worth a line here
// is a FAILURE: status.backfill.last_error is the most recent embeddings
// batch that didn't go through (the pass keeps retrying on its own, so this
// is a heads-up, not a stuck spinner), named in plain language with a
// direct link to the setting most likely to fix it. Nothing renders at all
// - not even an empty wrapper - while there's no error, or while semantic
// search isn't configured (enabled:false): an operator who never turned
// this on, or whose library is healthy, sees no permanent chrome about it
// either way.
function MateFailureRow({
  status,
  onOpenAssistantSettings,
}: {
  status: DocumentEmbeddingsStatus | null
  onOpenAssistantSettings?: () => void
}) {
  if (!status || !status.enabled) return null
  const error = status.backfill.last_error
  if (!error) return null

  return (
    <div data-testid="documents-mate-error" className="ml-auto flex flex-wrap items-center gap-2 text-xs text-destructive">
      <span>Mate couldn&apos;t finish indexing your documents for search: {error}</span>
      <Button type="button" size="sm" variant="outline" onClick={onOpenAssistantSettings}>
        Open Mate settings
      </Button>
    </div>
  )
}

function SearchResultsTable({
  results,
  searching,
  searchError,
  onOpen,
  folderLabelFor,
}: {
  results: DocumentSearchResult[]
  searching: boolean
  searchError: string | null
  onOpen: (id: string) => void
  folderLabelFor: (folderId: string | null) => string
}) {
  if (searchError) return <p role="alert" className="p-4 text-sm text-destructive">{searchError}</p>
  if (searching && results.length === 0) return <p className="p-4 text-sm text-muted-foreground">Searching…</p>
  if (results.length === 0) return <p className="p-4 text-sm text-muted-foreground">No matches.</p>

  return (
    <ul className="flex flex-col divide-y divide-border" data-testid="documents-search-results">
      {results.map((r) => (
        <li key={`${r.document_id}-${r.page}`}>
          <button
            type="button"
            onClick={() => onOpen(r.document_id)}
            className="flex w-full flex-col gap-1 px-3 py-2 text-left hover:bg-accent"
          >
            <span className="font-medium">{r.title || r.filename}</span>
            <span className="text-sm text-muted-foreground">{renderSnippet(r.snippet)}</span>
            <span className="text-xs text-muted-foreground">
              Page {r.page} · {folderLabelFor(r.folder_id)}
            </span>
          </button>
        </li>
      ))}
    </ul>
  )
}

function DocumentRowMenu({
  doc,
  onOpen,
  onDownload,
  onRename,
  onDetails,
  onMove,
  onReindex,
  onDelete,
}: {
  doc: DocumentRecord
  onOpen: () => void
  onDownload: () => void
  onRename: () => void
  onDetails: () => void
  onMove: () => void
  onReindex: () => void
  onDelete: () => void
}) {
  const name = documentDisplayName(doc)
  return (
    <DropdownMenu>
      <DropdownMenuTrigger
        render={
          <Button variant="ghost" size="icon" aria-label={`Actions for ${name}`}>
            <MoreVertical className="h-4 w-4" aria-hidden="true" />
          </Button>
        }
      />
      <DropdownMenuContent>
        <DropdownMenuItem onClick={onOpen}>Open</DropdownMenuItem>
        <DropdownMenuItem onClick={onDownload}>
          <Download className="h-4 w-4" aria-hidden="true" /> Download
        </DropdownMenuItem>
        <DropdownMenuItem onClick={onRename}>
          <Pencil className="h-4 w-4" aria-hidden="true" /> Rename
        </DropdownMenuItem>
        {/* Rename stays the fast path for a title-only fix (ADR 0115 §3);
            Details… is the slower path onto the full metadata page - notes,
            tags, the read-only indexing facts - so it sits right after
            Rename rather than buried past Move/Reindex. */}
        <DropdownMenuItem onClick={onDetails}>
          <Info className="h-4 w-4" aria-hidden="true" /> Details…
        </DropdownMenuItem>
        <DropdownMenuItem onClick={onMove}>Move…</DropdownMenuItem>
        <DropdownMenuItem onClick={onReindex}>
          <RefreshCw className="h-4 w-4" aria-hidden="true" /> Reindex…
        </DropdownMenuItem>
        <DropdownMenuSeparator />
        <DropdownMenuItem variant="destructive" onClick={onDelete}>
          <Trash2 className="h-4 w-4" aria-hidden="true" /> Delete
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  )
}

function FolderRowMenu({
  folder,
  isManual,
  canPromote,
  onRename,
  onMove,
  onDelete,
  onPromote,
  onDemote,
}: {
  folder: DocumentFolder
  /** Whether this folder already carries document_folders.role='manual' -
   * decides Promote vs. Demote (revision "one panel, not three"). */
  isManual: boolean
  /** Only a top-level folder can ever become a manual (backend's own
   * errManualNotTopLevel) - true exactly when this row is being browsed at
   * the root, since every folder listed there IS top-level by definition. */
  canPromote: boolean
  onRename: () => void
  onMove: () => void
  onDelete: () => void
  onPromote: () => void
  onDemote: () => void
}) {
  return (
    <DropdownMenu>
      <DropdownMenuTrigger
        render={
          <Button variant="ghost" size="icon" aria-label={`Actions for ${folder.name}`}>
            <MoreVertical className="h-4 w-4" aria-hidden="true" />
          </Button>
        }
      />
      <DropdownMenuContent>
        <DropdownMenuItem onClick={onRename}>
          <Pencil className="h-4 w-4" aria-hidden="true" /> Rename
        </DropdownMenuItem>
        <DropdownMenuItem onClick={onMove}>Move…</DropdownMenuItem>
        {/* Promote/demote: a folder's kind, not a destructive act either
            way - "moves and deletes nothing" (POST/DELETE /api/manuals),
            so this is a direct toggle like Rename/Move above, not a
            confirmation dialog like Delete below. */}
        {isManual ? (
          <DropdownMenuItem onClick={onDemote}>
            <Library className="h-4 w-4" aria-hidden="true" /> Stop treating as manual
          </DropdownMenuItem>
        ) : canPromote ? (
          <DropdownMenuItem onClick={onPromote}>
            <Library className="h-4 w-4" aria-hidden="true" /> Treat as a manual
          </DropdownMenuItem>
        ) : null}
        <DropdownMenuSeparator />
        <DropdownMenuItem variant="destructive" onClick={onDelete}>
          <Trash2 className="h-4 w-4" aria-hidden="true" /> Delete
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  )
}
