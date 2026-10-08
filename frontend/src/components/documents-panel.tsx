import {
  Download,
  File,
  FileText,
  Folder,
  FolderPlus,
  Image as ImageIcon,
  Info,
  Library,
  Loader2,
  Pencil,
  RefreshCw,
  Search,
  Trash2,
  Upload as UploadIcon,
  X,
  Sparkles,
} from 'lucide-react'
import type { ColumnDef } from '@tanstack/react-table'
import { lazy, Suspense, useCallback, useEffect, useMemo, useRef, useState, type ChangeEvent, type DragEvent, type KeyboardEvent, type ReactNode } from 'react'

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
import { MoveToFolderDialog } from '@/components/documents/move-to-folder-dialog'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { ScrollArea } from '@/components/ui/scroll-area'
import { Switch } from '@/components/ui/switch'
import {
  ConfirmDelete,
  EmptyState,
  IndexFilters,
  IndexTable,
  Page,
  ResourceItem,
  ResourceList,
  type IndexFilter,
  type PageAction,
  type RowAction,
} from '@/components/patterns'
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
import { downloadDocument } from '@/lib/document-download'
import { documentDisplayName, documentFailureMessage, formatBytes } from '@/lib/document-display'
import type { HelpTarget } from '@/lib/help-links'
import { NOTE_TYPE_META, NOTE_TYPE_ORDER } from '@/lib/note-type-meta'
import { cn } from '@/lib/utils'

import { DocumentViewerSheet } from './documents/document-viewer-sheet'
import { ManualFolderView } from './documents/manual-folder-view'
import { NoteTypeIconButton } from './documents/note-type-icon-button'
import { UnfiledNotesView } from './documents/unfiled-notes-view'
import { prefetchNoteEditor } from './note-editor'

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

// ── search overlay: recent searches (shadcn.io "navbar-search-overlay") ───
// Persisted client-side only - there is no backend concept of a search
// history - so every read and write is wrapped in try/catch and the panel
// works fine (just with an empty "Recent searches" list) if localStorage
// throws (private browsing, a full quota) or simply isn't there.
const RECENT_SEARCHES_KEY = 'helmcentral.documents.recentSearches'
const MAX_RECENT_SEARCHES = 5
const MAX_OVERLAY_TAG_SUGGESTIONS = 12

function loadRecentSearches(): string[] {
  try {
    const raw = localStorage.getItem(RECENT_SEARCHES_KEY)
    if (!raw) return []
    const parsed: unknown = JSON.parse(raw)
    if (!Array.isArray(parsed)) return []
    return parsed.filter((item): item is string => typeof item === 'string')
  } catch {
    return []
  }
}

function saveRecentSearches(queries: string[]): void {
  try {
    localStorage.setItem(RECENT_SEARCHES_KEY, JSON.stringify(queries))
  } catch {
    // Best-effort only - see loadRecentSearches' own comment above.
  }
}

/** Records the query in force when the search overlay closes. Most-recent
 * first, deduplicated, capped at MAX_RECENT_SEARCHES. */
function pushRecentSearch(query: string): string[] {
  const next = [query, ...loadRecentSearches().filter((q) => q !== query)].slice(0, MAX_RECENT_SEARCHES)
  saveRecentSearches(next)
  return next
}

/** Mac gets the ⌘ glyph, everything else gets the spelled-out "Ctrl K" -
 * matching how every other cross-platform shortcut hint in the OS itself
 * (and every other app that bothers) tells the two apart. userAgent is
 * checked alongside the older, more reliably-populated `platform` since the
 * latter is deprecated and some browsers now leave it blank. */
function isApplePlatform(): boolean {
  if (typeof navigator === 'undefined') return false
  const signal = `${navigator.userAgent ?? ''} ${navigator.platform ?? ''}`
  return /Mac|iPhone|iPad|iPod/i.test(signal)
}

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

// A single document's own row menu, and the selection bar's bulk action,
// both open the same confirmation AlertDialog below - this is what tells it
// which copy to render and what to actually reindex on confirm.
type ReindexTarget =
  | { kind: 'single'; id: string; name: string; pageCount: number; mime: string }
  | { kind: 'bulk'; ids: string[]; totalPages: number; ocrPages: number }

interface MoveTarget {
  ids: string[]
  label: string
}

/** One line of the listing: a folder or a document, in one IndexTable. */
type ListRow =
  | { kind: 'folder'; folder: DocumentFolder }
  | { kind: 'document'; doc: DocumentRecord }

const rowName = (row: ListRow) => (row.kind === 'folder' ? row.folder.name : documentDisplayName(row.doc))
const rowKey = (row: ListRow) => (row.kind === 'folder' ? row.folder.id : row.doc.id)

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
  // Modelled on shadcn.io's "navbar-search-overlay" block: a small trigger
  // in the toolbar (below) rather than an always-on input competing for
  // space in the filter row, opening a full-page overlay (near this
  // panel's other Dialogs, below) that holds the actual input, the "All
  // folders" scope switch, recent searches/tag shortcuts when empty, and
  // the results themselves. `query`/`allFolders` and the debounce effect
  // are unchanged from the inline input this replaces - only where they're
  // rendered moved.
  const [query, setQuery] = useState('')
  const [allFolders, setAllFolders] = useState(false)
  const [searchOpen, setSearchOpen] = useState(false)
  const [activeResultIndex, setActiveResultIndex] = useState(0)
  const [recentSearches, setRecentSearches] = useState<string[]>(() => loadRecentSearches())
  // Destructured so the effect below depends on these two functions
  // directly, not on `documents.search`/`documents.clearSearch` member
  // expressions - both stay useCallback-stable per folderId/selectedTag
  // (use-documents.ts), so re-running the effect when they change simply
  // re-issues the same query against the new scope.
  const { search: searchDocuments, clearSearch } = documents
  // The query the debounce last handed to search. While the box holds
  // anything else, the results on screen belong to an older query: Enter
  // must not open one of them, and an empty list is not yet "No matches."
  const [searchedQuery, setSearchedQuery] = useState('')
  useEffect(() => {
    const trimmed = query.trim()
    const id = setTimeout(() => {
      setSearchedQuery(trimmed)
      if (trimmed === '') {
        clearSearch()
      } else {
        void searchDocuments(trimmed, { allFolders })
      }
    }, SEARCH_DEBOUNCE_MS)
    return () => clearTimeout(id)
  }, [query, allFolders, searchDocuments, clearSearch])

  const searching = documents.searchResults !== null
  const searchPending = query.trim() !== searchedQuery

  // Whichever result Enter/click opens - reset to the top result every time
  // the result set changes (a fresh keystroke's results, or the debounce
  // settling), so arrow keys always start from a sane position rather than
  // an index that belonged to a longer, now-stale list.
  useEffect(() => { setActiveResultIndex(0) }, [documents.searchResults])

  // A recent search is the query in force when the operator leaves the
  // overlay (opening a result, Escape, clicking away), not every query the
  // debounce settled on - a pause mid-word would otherwise save "impel"
  // alongside "impeller".
  const closeSearchOverlay = useCallback(() => {
    const trimmed = query.trim()
    if (trimmed !== '') setRecentSearches(pushRecentSearch(trimmed))
    setSearchOpen(false)
    setQuery('') // leaving the overlay always leaves search mode too
  }, [query])
  const handleSearchOpenChange = useCallback((open: boolean) => {
    if (open) setSearchOpen(true)
    else closeSearchOverlay()
  }, [closeSearchOverlay])
  const openSearchResult = useCallback((id: string) => {
    setViewerId(id)
    closeSearchOverlay()
  }, [closeSearchOverlay])
  const handleClearRecentSearches = useCallback(() => {
    saveRecentSearches([])
    setRecentSearches([])
  }, [])
  const handleSearchInputKeyDown = useCallback((e: KeyboardEvent<HTMLInputElement>) => {
    const results = documents.searchResults ?? []
    if (e.key === 'ArrowDown') {
      e.preventDefault()
      if (results.length > 0) setActiveResultIndex((i) => Math.min(i + 1, results.length - 1))
    } else if (e.key === 'ArrowUp') {
      e.preventDefault()
      if (results.length > 0) setActiveResultIndex((i) => Math.max(i - 1, 0))
    } else if (e.key === 'Enter') {
      e.preventDefault()
      if (searchPending) return
      const active = results[activeResultIndex]
      if (active) openSearchResult(active.document_id)
    }
  }, [documents.searchResults, activeResultIndex, openSearchResult, searchPending])

  // ⌘K/Ctrl+K opens the overlay from anywhere while this panel is mounted -
  // sidebar.tsx's own shortcut is Cmd/Ctrl+B (SIDEBAR_KEYBOARD_SHORTCUT), so
  // there's no clash. Not while another dialog or sheet is up (the viewer,
  // a note mid-edit, Mate): opening a result from there would swap the
  // viewer's document out from under unsaved edits.
  useEffect(() => {
    const handleGlobalKeyDown = (e: globalThis.KeyboardEvent) => {
      if (e.key.toLowerCase() === 'k' && (e.metaKey || e.ctrlKey)) {
        if (document.querySelector('[role="dialog"], [role="alertdialog"]')) return
        e.preventDefault()
        setSearchOpen(true)
      }
    }
    window.addEventListener('keydown', handleGlobalKeyDown)
    return () => window.removeEventListener('keydown', handleGlobalKeyDown)
  }, [])

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
  // The search overlay's own suggestions: the most-used tags only. A
  // library of any size carries dozens of tags, and listing them all
  // buries the search box under a wall of chips; the full set stays in the
  // filter row.
  const overlayTagSuggestions = useMemo(
    () => [...tagFilterOptions].sort((a, b) => b.count - a.count).slice(0, MAX_OVERLAY_TAG_SUGGESTIONS),
    [tagFilterOptions],
  )

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
  // All three go through the one ConfirmDelete below - a bulk delete is not
  // exempt from the same "are you sure" every other delete gets here. The
  // dialog stays open (and un-dismissable) while the delete is in flight, and
  // a refusal (the 409 "folder is not empty") is shown inside it.
  const [deleteTarget, setDeleteTargetState] = useState<DeleteTarget | null>(null)
  const [deleting, setDeleting] = useState(false)
  const [deleteError, setDeleteError] = useState<string | null>(null)
  const setDeleteTarget = (target: DeleteTarget | null) => {
    setDeleteTargetState(target)
    setDeleteError(null)
  }
  const submitDelete = async () => {
    if (!deleteTarget) return
    setDeleting(true)
    setDeleteError(null)
    try {
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
          // Drop each one from the pending list and the selection the moment
          // it is gone, so a retry after a later failure resumes with what is
          // left instead of starting on an already-deleted document.
          setDeleteTargetState((prev) => (prev?.kind === 'bulk' ? { ...prev, ids: prev.ids.filter((x) => x !== id) } : prev))
          setSelectedIds((prev) => {
            const next = new Set(prev)
            next.delete(id)
            return next
          })
        }
      }
      setDeleteTarget(null)
      void unfiled.refresh() // a deleted document may have been an unfiled note
    } catch (err) {
      setDeleteError(err instanceof Error ? err.message : String(err))
    } finally {
      setDeleting(false)
    }
  }

  // ── reindex ──────────────────────────────────────────────────────────
  const [reindexTarget, setReindexTarget] = useState<ReindexTarget | null>(null)
  const submitReindex = async () => {
    if (!reindexTarget) return
    await runAction(async () => {
      if (reindexTarget.kind === 'single') {
        await documents.reindexDocument(reindexTarget.id)
      } else {
        // Sequential, not Promise.all - stops at the first failure (fail
        // loud, AGENTS.md) rather than firing every request regardless and
        // reporting only the last rejection settled.
        for (const id of reindexTarget.ids) {
          await documents.reindexDocument(id)
        }
        setSelectedIds(new Set())
      }
      setReindexTarget(null)
    })
  }

  // ── viewer ───────────────────────────────────────────────────────────
  // The viewer's own state (metadata, text, editing, checklist) lives in
  // DocumentViewerSheet; the panel owns only WHICH document is open, because
  // search results and note capture open it too. getNote/patchNote come from
  // `unfiled` so a save or pin keeps the Unfiled list fresh.
  const [viewerId, setViewerId] = useState<string | null>(initialDocumentId)

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

  // No note-type facet here. One was built and removed: a library folder
  // holds mostly kind='file' rows (PDFs, photos, the builder's handbook),
  // so a type filter is inapplicable to most of what is on screen and
  // silently empties the listing when a facet is active. The type is still
  // worth SEEING on a note row - that is the pre-attentive icon column, and
  // it stays - but it is not a useful axis to slice a mixed library on. The
  // Unfiled notes view already covers the one case that genuinely wanted a
  // notes-only listing.
  const documentRows = documents.documents
  // Belt and braces with IndexTable's own pruning: a bulk action only ever
  // acts on documents that are on screen right now.
  const visibleSelectedIds = () => documentRows.filter((d) => selectedIds.has(d.id)).map((d) => d.id)

  // ── the listing: folders and documents in one IndexTable ───────────────
  const listRows = useMemo<ListRow[]>(
    () => [
      ...documents.folders.map((folder): ListRow => ({ kind: 'folder', folder })),
      ...documents.documents.map((doc): ListRow => ({ kind: 'document', doc })),
    ],
    [documents.folders, documents.documents],
  )
  const { patchNote: patchUnfiledNote } = unfiled
  const columns = useMemo<ColumnDef<ListRow, unknown>[]>(() => [
    {
      id: 'name',
      header: 'Name',
      enableSorting: false,
      accessorFn: rowName,
      cell: ({ row }) => {
        const item = row.original
        if (item.kind === 'folder') {
          return (
            <button
              type="button"
              onClick={(e) => { e.stopPropagation(); navigate(item.folder.id) }}
              className="flex items-center gap-2 text-left font-medium hover:underline"
            >
              {/* Library, not Folder, for a Manual-kind folder - the only way
                  to see that from the OUTSIDE (browsing its parent) at all,
                  since GET /api/document-folders never serialises
                  document_folders.role. */}
              {manualFolderIds.has(item.folder.id) ? (
                <Library className="h-4 w-4 shrink-0 text-muted-foreground" aria-hidden="true" />
              ) : (
                <Folder className="h-4 w-4 shrink-0 text-muted-foreground" aria-hidden="true" />
              )}
              {item.folder.name}
            </button>
          )
        }
        const doc = item.doc
        const MimeIcon = rowIcon(doc.mime)
        return (
          <div className="flex items-center gap-2">
            {/* The pre-attentive type icon column (revision "Note types as a
                facet") - a note row's icon is its own hit target opening a
                type-override menu, same gesture as the Unfiled notes view's
                rows (unfiled.patchNote works on any note id regardless of
                filing state). A plain kind='file' row keeps the ordinary
                mime-based icon, inert. The wrapper keeps the menu's clicks
                from also opening the row. */}
            {doc.kind === 'note' ? (
              <span className="inline-flex" onClick={(e) => e.stopPropagation()} onKeyDown={(e) => e.stopPropagation()}>
                <NoteTypeIconButton noteType={doc.note_type} onSetType={(type) => { void runAction(() => patchUnfiledNote(doc.id, { type })) }} />
              </span>
            ) : (
              <MimeIcon className="h-4 w-4 shrink-0 text-muted-foreground" aria-hidden="true" />
            )}
            <button
              type="button"
              onClick={(e) => { e.stopPropagation(); setViewerId(doc.id) }}
              className="flex min-w-0 flex-col items-start gap-0.5 text-left"
            >
              <span className="font-medium hover:underline">{documentDisplayName(doc)}</span>
              {doc.status === 'failed' && doc.error && (
                // The operator-facing sentence, not the raw stored error (an
                // internal storage path, the word "panic", a library's own
                // error text) - that stays available in the title attribute,
                // and in full on the Details page's "Error details"
                // disclosure.
                <span className="w-full truncate text-xs text-destructive" title={doc.error}>
                  {documentFailureMessage(doc)}
                </span>
              )}
            </button>
          </div>
        )
      },
    },
    {
      id: 'status',
      header: 'Status',
      enableSorting: false,
      accessorFn: (item) => (item.kind === 'document' ? item.doc.status : ''),
      cell: ({ row }) => (row.original.kind === 'document' ? statusBadge(row.original.doc) : null),
    },
    {
      id: 'tags',
      header: 'Tags',
      enableSorting: false,
      accessorFn: (item) => (item.kind === 'document' ? item.doc.tags.map((t) => t.tag).join(', ') : ''),
      cell: ({ row }) =>
        row.original.kind === 'document' ? (
          <div className="flex flex-wrap gap-1">
            {row.original.doc.tags.map((t) => (
              <Badge key={t.tag} variant={t.source === 'operator' ? 'secondary' : 'outline'}>
                {t.tag}
              </Badge>
            ))}
          </div>
        ) : null,
    },
    {
      id: 'size',
      header: 'Size',
      enableSorting: false,
      accessorFn: (item) => (item.kind === 'document' ? formatBytes(item.doc.size_bytes) : ''),
      cell: ({ getValue }) => getValue<string>(),
    },
  ], [manualFolderIds, navigate, runAction, patchUnfiledNote])

  // The current row menus, unchanged in what they offer (the old
  // DocumentRowMenu / FolderRowMenu): a document can be opened, downloaded,
  // renamed, detailed, moved, reindexed or deleted; a folder renamed, moved,
  // promoted to / demoted from a manual, or deleted.
  const rowActions = (item: ListRow): RowAction<ListRow>[] => {
    if (item.kind === 'folder') {
      const folder = item.folder
      const isManual = manualFolderIds.has(folder.id)
      return [
        { label: 'Rename', icon: <Pencil className="h-4 w-4" aria-hidden="true" />, onSelect: () => { setRenameTarget({ kind: 'folder', id: folder.id, name: folder.name }); setRenameValue(folder.name) } },
        { label: 'Move…', onSelect: () => setMoveTarget({ ids: [folder.id], label: folder.name }) },
        // Promote/demote: a folder's kind, not a destructive act either way -
        // "moves and deletes nothing" (POST/DELETE /api/manuals), so this is a
        // direct toggle like Rename/Move, not a confirmation like Delete.
        // Only a top-level folder can ever become a manual (backend's own
        // errManualNotTopLevel) - true exactly when browsing the root.
        ...(isManual
          ? [{ label: 'Stop treating as manual', icon: <Library className="h-4 w-4" aria-hidden="true" />, onSelect: () => { void runAction(() => manualsList.clearManual(folder.id)) } }]
          : folderId === null
            ? [{ label: 'Treat as a manual', icon: <Library className="h-4 w-4" aria-hidden="true" />, onSelect: () => { void runAction(() => manualsList.flagManual(folder.id)) } }]
            : []),
        { label: 'Delete', icon: <Trash2 className="h-4 w-4" aria-hidden="true" />, destructive: true, onSelect: () => setDeleteTarget({ kind: 'folder', id: folder.id, name: folder.name }) },
      ]
    }
    const doc = item.doc
    const name = documentDisplayName(doc)
    return [
      { label: 'Open', onSelect: () => setViewerId(doc.id) },
      { label: 'Download', icon: <Download className="h-4 w-4" aria-hidden="true" />, onSelect: () => { void runAction(() => downloadDocument(doc.id, doc.filename)) } },
      { label: 'Rename', icon: <Pencil className="h-4 w-4" aria-hidden="true" />, onSelect: () => { setRenameTarget({ kind: 'document', id: doc.id, name }); setRenameValue(name) } },
      // Rename stays the fast path for a title-only fix (ADR 0115 §3);
      // Details… is the slower path onto the full metadata page - notes,
      // tags, the read-only indexing facts - so it sits right after Rename.
      { label: 'Details…', icon: <Info className="h-4 w-4" aria-hidden="true" />, onSelect: () => onEditDocument?.(doc.id) },
      { label: 'Move…', onSelect: () => setMoveTarget({ ids: [doc.id], label: name }) },
      { label: 'Reindex…', icon: <RefreshCw className="h-4 w-4" aria-hidden="true" />, onSelect: () => setReindexTarget({ kind: 'single', id: doc.id, name, pageCount: doc.page_count, mime: doc.mime }) },
      { label: 'Delete', icon: <Trash2 className="h-4 w-4" aria-hidden="true" />, destructive: true, onSelect: () => setDeleteTarget({ kind: 'document', id: doc.id, name }) },
    ]
  }

  // ── page chrome ────────────────────────────────────────────────────────
  const currentFolder = documents.path.length > 0 ? documents.path[documents.path.length - 1] : null
  const openSearch = (seed?: string) => {
    setSearchOpen(true)
    if (seed) setQuery(seed)
  }
  // ONE New menu's contents, now in the page's overflow menu: Folder first
  // (Manual is a checkbox inside its dialog when browsing the root) and Note
  // carrying its kinds in a submenu. Auto leads that submenu, and picking it
  // sends no `type` at all so the Go classifier answers (ADR 0116). ADR 0121
  // made this the only path in for a note: a note is a document, and this is
  // where every other document this panel creates already starts.
  const secondaryActions: PageAction[] = [
    { label: 'New folder', icon: <FolderPlus className="h-4 w-4" aria-hidden="true" />, onClick: () => setNewFolderOpen(true) },
    {
      label: 'New note',
      icon: <Pencil className="h-4 w-4" aria-hidden="true" />,
      // ADR 0124: hovering or focusing this is the earliest signal that the
      // operator is about to open the editor - warms note-editor-impl.tsx's
      // chunk (and the capture sheet's own) right away rather than waiting
      // for the click. prefetchNoteEditor() is memoised, so a hover that
      // never leads to a click costs nothing beyond the one fetch every other
      // path already pays for eventually.
      onIntent: () => prefetchNoteEditor(),
      onClick: () => openCapture(),
      submenu: [
        { label: 'Auto', icon: <Sparkles className="h-4 w-4 text-muted-foreground" aria-hidden="true" />, onClick: () => openCapture() },
        ...NOTE_TYPE_ORDER.map((type) => {
          const meta = NOTE_TYPE_META[type]
          const Icon = meta.icon
          // The catch-all kind is labelled "Note" everywhere else, which is
          // fine when the question is "what kind of note is this". Here it
          // would read New note > Note, so it says "Plain note" in this one
          // menu. The stored value is 'note' either way.
          return { label: type === 'note' ? 'Plain note' : meta.label, icon: <Icon className={cn('h-4 w-4', meta.className)} aria-hidden="true" />, onClick: () => openCapture(type) }
        }),
      ],
    },
  ]
  const tagFilters: IndexFilter[] = tagFilterOptions.length > 0
    ? [{
        id: 'tag',
        label: 'Filter by tag',
        value: documents.selectedTag ?? '',
        allLabel: 'All tags',
        options: tagFilterOptions.map((t) => ({ value: t.tag, label: `${t.tag} (${t.count})` })),
        onChange: (value) => documents.setSelectedTag(value === '' ? null : value),
      }]
    : []

  const bordered = 'min-h-0 flex-1 overflow-auto rounded-md border border-border bg-card'

  return (
    <Page
      className="h-full min-h-0 p-4"
      title={currentFolder ? currentFolder.name : 'Documents'}
      breadcrumb={documents.path.length > 0 ? (
        <Breadcrumb>
          <BreadcrumbList>
            <BreadcrumbItem>
              <BreadcrumbLink href="#" onClick={(e) => { e.preventDefault(); navigate(null) }}>
                Documents
              </BreadcrumbLink>
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
      ) : undefined}
      primaryAction={{
        label: 'Upload',
        icon: <UploadIcon className="h-4 w-4" data-icon="inline-start" aria-hidden="true" />,
        onClick: () => fileInputRef.current?.click(),
      }}
      secondaryActions={secondaryActions}
    >
      <input
        ref={fileInputRef}
        type="file"
        multiple
        className="hidden"
        onChange={handleFilesSelected}
        data-testid="documents-file-input"
      />

      <div className="flex flex-wrap items-end gap-3">
        {/* The search field is the trigger for the ⌘K overlay below, not a
            second search: see IndexFilters' onSearchActivate. */}
        <IndexFilters
          className="min-w-0 flex-1"
          searchValue=""
          onSearchChange={() => {}}
          searchLabel="Search"
          searchPlaceholder={`Search documents (${isApplePlatform() ? '⌘K' : 'Ctrl K'})`}
          onSearchActivate={openSearch}
          filters={tagFilters}
          onClearAll={() => documents.setSelectedTag(null)}
        />
        {/* "Unfiled notes" as a saved view, not a place (revision) -
            kind='note' AND folder_id IS NULL, preserving the deleted
            notes-panel.tsx's drain-the-inbox loop and its visible count
            without owning a sidebar row. */}
        <Button
          type="button"
          variant="outline"
          className={cn('h-9', view === 'unfiled' && 'border-primary/40 bg-primary/10 text-primary')}
          aria-pressed={view === 'unfiled'}
          onClick={() => setView((prev) => (prev === 'unfiled' ? 'browse' : 'unfiled'))}
        >
          Unfiled notes ({unfiled.notes.length})
        </Button>
        <MateFailureRow status={documents.embeddingsStatus} onOpenAssistantSettings={onOpenAssistantSettings} />
      </div>

      {uploads.items.length > 0 && (
        <div data-testid="documents-upload-items">
          <ResourceList label="Uploads" header="Uploading" count={uploads.items.length}>
            {uploads.items.map((item) => (
              <ResourceItem
                key={item.key}
                item={item}
                media={item.status === 'uploading'
                  ? <Loader2 className="h-4 w-4 animate-spin" aria-hidden="true" />
                  : <FileText className="h-4 w-4" aria-hidden="true" />}
                title={<span title={item.filename}>{item.filename}</span>}
                meta={<span className={cn('tabular-nums', item.status === 'failed' && 'text-destructive')}>{stagedUploadStatusLabel(item)}</span>}
                className={item.status === 'failed' ? 'bg-destructive/10' : undefined}
              />
            ))}
          </ResourceList>
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

      <div
        className={cn('flex min-h-0 flex-1 flex-col rounded-md', dragOver && 'ring-2 ring-primary')}
        data-testid="documents-dropzone"
        onDragOver={(e) => { e.preventDefault(); setDragOver(true) }}
        onDragLeave={() => setDragOver(false)}
        onDrop={handleDrop}
      >
        {view === 'unfiled' ? (
          <div className={bordered}>
            <UnfiledNotesView
              notes={unfiled.notes}
              loading={unfiled.loading}
              onOpen={setViewerId}
              onSetType={(id, type) => { void runAction(() => unfiled.patchNote(id, { type })) }}
              onFile={(id, title) => setMoveTarget({ ids: [id], label: title })}
            />
          </div>
        ) : isCurrentFolderManual && folderId !== null ? (
          <div className={bordered}>
            <ManualFolderView
              folderId={folderId}
              sectionId={sectionId}
              onSectionChange={setSectionId}
              onDemoted={() => { void manualsList.refreshManuals() }}
              onNavigateDocument={setViewerId}
              mateAvailable={mateAvailable}
              onAskMate={onAskMate}
            />
          </div>
        ) : (
          <IndexTable
            columns={columns}
            rows={listRows}
            getRowId={rowKey}
            onOpen={(item) => { if (item.kind === 'folder') navigate(item.folder.id); else setViewerId(item.doc.id) }}
            rowActions={rowActions}
            rowActionsLabel={(item) => `Actions for ${rowName(item)}`}
            groupBy={(item) => (item.kind === 'folder' ? 'folders' : 'documents')}
            groupOrder={['folders', 'documents']}
            groupLabel={(key) => (key === 'folders' ? 'Folders' : 'Documents')}
            selectable
            isRowSelectable={(item) => item.kind === 'document'}
            selectedIds={selectedIds}
            onSelectionChange={setSelectedIds}
            rowSelectLabel={(item) => `Select ${rowName(item)}`}
            bulkActions={(
              <>
                <Button
                  type="button"
                  size="sm"
                  variant="outline"
                  onClick={() => { const ids = visibleSelectedIds(); setMoveTarget({ ids, label: `${ids.length} document(s)` }) }}
                >
                  Move to…
                </Button>
                <Button
                  type="button"
                  size="sm"
                  variant="outline"
                  onClick={() => {
                    const ids = visibleSelectedIds()
                    const selectedDocs = documentRows.filter((d) => selectedIds.has(d.id))
                    const totalPages = selectedDocs.reduce((sum, d) => sum + Math.max(1, d.page_count), 0)
                    const ocrPages = selectedDocs
                      .filter((d) => d.mime === 'application/pdf' || d.mime.startsWith('image/'))
                      .reduce((sum, d) => sum + Math.max(1, d.page_count), 0)
                    setReindexTarget({ kind: 'bulk', ids, totalPages, ocrPages })
                  }}
                >
                  <RefreshCw className="h-4 w-4" data-icon="inline-start" aria-hidden="true" />
                  Reindex…
                </Button>
                <Button type="button" size="sm" variant="outline" onClick={() => setDeleteTarget({ kind: 'bulk', ids: visibleSelectedIds() })}>
                  Delete
                </Button>
              </>
            )}
            loading={documents.loading && listRows.length === 0}
            empty={<EmptyState title="Nothing here yet" description="Upload a file or create a folder." />}
          />
        )}
      </div>

      {/* ── Search overlay (shadcn.io "navbar-search-overlay") ────────
          Reuses the query/allFolders state and debounce effect above -
          this Dialog is just where they're rendered, not a second source
          of search state. Positioned toward the top of the viewport
          (top-[15%], not vertically centred) and wide (max-w-2xl), the
          way a command-palette-style search reads, rather than looking
          like every other centred confirmation Dialog in this file. */}
      <Dialog open={searchOpen} onOpenChange={handleSearchOpenChange}>
        <DialogContent className="top-[15%] flex max-w-[calc(100%-2rem)] translate-y-0 sm:max-w-2xl flex-col gap-0 overflow-hidden p-0">
          <DialogTitle className="sr-only">Search documents</DialogTitle>
          <div className="relative shrink-0 border-b border-border">
            <Search className="pointer-events-none absolute left-4 top-1/2 h-5 w-5 -translate-y-1/2 text-muted-foreground" aria-hidden="true" />
            <Input
              type="search"
              aria-label="Search documents"
              placeholder={allFolders ? 'Search every folder…' : 'Search this folder…'}
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              onKeyDown={handleSearchInputKeyDown}
              autoFocus
              className="h-12 border-0 pl-11 pr-10 text-base shadow-none focus-visible:ring-0 [&::-webkit-search-cancel-button]:hidden"
            />
          </div>
          <div className="flex shrink-0 items-center justify-between gap-2 border-b border-border px-4 py-2">
            <span className="text-[10px] font-medium uppercase tracking-wider text-muted-foreground">
              {allFolders ? 'Searching all folders' : 'Searching this folder'}
            </span>
            <div className="flex min-w-0 items-center gap-3">
              {/* Searches stay narrowed by the library's tag filter, so the
                  overlay says so rather than returning a short list with
                  no explanation. */}
              {documents.selectedTag && (
                <button
                  type="button"
                  aria-label={`Clear tag filter ${documents.selectedTag}`}
                  onClick={() => documents.setSelectedTag(null)}
                  className="flex min-w-0 items-center gap-1 rounded-full border border-border px-2 py-0.5 text-xs hover:bg-accent"
                >
                  <span className="truncate">Tag: {documents.selectedTag}</span>
                  <X className="h-3 w-3 shrink-0" aria-hidden="true" />
                </button>
              )}
              <Label htmlFor="documents-search-all-folders" className="text-xs text-muted-foreground">
                All folders
              </Label>
              <Switch id="documents-search-all-folders" aria-label="All folders" checked={allFolders} onCheckedChange={setAllFolders} />
            </div>
          </div>
          <ScrollArea className="max-h-[60vh]">
            {searching || query.trim() !== '' ? (
              <>
                {documents.semanticProblem && (
                  <p className="border-b border-amber-500/40 bg-amber-500/10 px-3 py-1.5 text-xs text-amber-600 dark:text-amber-400">
                    Showing keyword results only. {documents.semanticProblem}
                  </p>
                )}
                <SearchResultsTable
                  results={documents.searchResults ?? []}
                  searching={documents.searching || searchPending}
                  searchError={documents.searchError}
                  activeIndex={activeResultIndex}
                  onOpen={openSearchResult}
                  folderLabelFor={folderLabelFor}
                />
              </>
            ) : (
              <div className="flex flex-col gap-4 p-4">
                {recentSearches.length === 0 && overlayTagSuggestions.length === 0 && (
                  <p className="p-4 text-center text-sm text-muted-foreground">
                    {allFolders ? "Type to search every folder's documents and notes." : "Type to search this folder's documents and notes."}
                  </p>
                )}
                {recentSearches.length > 0 && (
                  <div>
                    <div className="mb-2 flex items-center justify-between">
                      <span className="text-[10px] font-medium uppercase tracking-wider text-muted-foreground">
                        Recent searches
                      </span>
                      <button
                        type="button"
                        onClick={handleClearRecentSearches}
                        className="text-[10px] font-medium uppercase tracking-wider text-muted-foreground hover:text-foreground"
                      >
                        Clear
                      </button>
                    </div>
                    <ul className="flex flex-col gap-0.5">
                      {recentSearches.map((q) => (
                        <li key={q}>
                          <button
                            type="button"
                            onClick={() => setQuery(q)}
                            className="flex w-full min-w-0 items-center gap-2 rounded-md px-2 py-1.5 text-left text-sm hover:bg-accent"
                          >
                            <Search className="h-3.5 w-3.5 shrink-0 text-muted-foreground" aria-hidden="true" />
                            <span className="truncate">{q}</span>
                          </button>
                        </li>
                      ))}
                    </ul>
                  </div>
                )}
                {overlayTagSuggestions.length > 0 && (
                  <div>
                    <span className="mb-2 block text-[10px] font-medium uppercase tracking-wider text-muted-foreground">
                      Tags
                    </span>
                    <div className="flex flex-wrap gap-1.5">
                      {overlayTagSuggestions.map((t) => (
                        <button
                          key={t.tag}
                          type="button"
                          onClick={() => { documents.setSelectedTag(t.tag); closeSearchOverlay() }}
                          className="rounded-full border border-border px-2.5 py-1 text-xs hover:bg-accent"
                        >
                          {t.tag} ({t.count})
                        </button>
                      ))}
                    </div>
                  </div>
                )}
              </div>
            )}
          </ScrollArea>
        </DialogContent>
      </Dialog>

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
      <MoveToFolderDialog
        open={moveTarget !== null}
        onOpenChange={(open) => { if (!open) setMoveTarget(null) }}
        label={moveTarget?.label ?? ''}
        onPick={(destination) => { void submitMove(destination) }}
        onFolderCreated={() => { void documents.refresh() }}
      />

      {/* ── Delete confirmation ────────────────────────────────────── */}
      <ConfirmDelete
        open={deleteTarget !== null}
        onOpenChange={(open) => { if (!open) setDeleteTarget(null) }}
        title={deleteTarget?.kind === 'bulk' ? `Delete ${deleteTarget.ids.length} ${deleteTarget.ids.length === 1 ? 'document' : 'documents'}?` : `Delete "${deleteTarget?.name}"?`}
        description={deleteTarget?.kind === 'folder'
          ? 'This removes the folder. It has to be empty first.'
          : "This removes the file(s). It can't be undone."}
        deleting={deleting}
        onConfirm={() => { void submitDelete() }}
      >
        {deleteError && <p role="alert" className="text-sm text-destructive">{deleteError}</p>}
      </ConfirmDelete>

      {/* ── Reindex confirmation (single row menu, or the bulk selection
          bar's own "Reindex…") ────────────────────────────────────── */}
      <AlertDialog open={reindexTarget !== null} onOpenChange={(open) => { if (!open) setReindexTarget(null) }}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>
              {reindexTarget?.kind === 'bulk' ? `Reindex ${reindexTarget.ids.length} ${reindexTarget.ids.length === 1 ? 'document' : 'documents'}?` : `Reindex "${reindexTarget?.name}"?`}
            </AlertDialogTitle>
            <AlertDialogDescription>
              {reindexTarget?.kind === 'bulk' ? (
                <>
                  This re-reads {reindexTarget.totalPages} page(s) from the beginning, across {reindexTarget.ids.length} document(s).
                  {reindexTarget.ocrPages > 0 && (
                    <> If Mate is on and OCR is needed, estimated cost: ${(reindexTarget.ocrPages * OCR_COST_PER_PAGE_USD).toFixed(3)}.</>
                  )}
                </>
              ) : (
                <>
                  This re-reads {reindexTarget?.pageCount ?? 1} page(s) from the beginning.
                  {reindexTarget && (reindexTarget.mime === 'application/pdf' || reindexTarget.mime.startsWith('image/')) && (
                    <> If Mate is on and OCR is needed, estimated cost: ${(Math.max(1, reindexTarget.pageCount) * OCR_COST_PER_PAGE_USD).toFixed(3)}.</>
                  )}
                </>
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
      <DocumentViewerSheet
        documentId={viewerId}
        onDocumentChange={setViewerId}
        knownDocuments={documents.documents}
        getNote={unfiled.getNote}
        patchNote={unfiled.patchNote}
        onEditDocument={onEditDocument}
        mateAvailable={mateAvailable}
        onAskMate={onAskMate}
        runAction={runAction}
      />

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
    </Page>
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
  activeIndex,
  onOpen,
  folderLabelFor,
}: {
  results: DocumentSearchResult[]
  searching: boolean
  searchError: string | null
  /** The overlay's keyboard-navigable row (ArrowUp/ArrowDown move it, Enter
   * opens it) - highlighted the same way a hovered row is, so it's always
   * visible which result Enter targets. */
  activeIndex: number
  onOpen: (id: string) => void
  folderLabelFor: (folderId: string | null) => string
}) {
  if (searchError) return <p role="alert" className="p-4 text-sm text-destructive">{searchError}</p>
  if (searching && results.length === 0) return <p className="p-4 text-sm text-muted-foreground">Searching…</p>
  if (results.length === 0) return <p className="p-4 text-sm text-muted-foreground">No matches.</p>

  return (
    <ul className="flex flex-col divide-y divide-border" data-testid="documents-search-results">
      {results.map((r, index) => (
        <li key={`${r.document_id}-${r.page}`}>
          <button
            type="button"
            onClick={() => onOpen(r.document_id)}
            className={cn(
              'flex w-full flex-col gap-1 px-3 py-2 text-left hover:bg-accent',
              index === activeIndex && 'bg-accent',
            )}
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
