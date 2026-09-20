import {
  Download,
  File,
  FileText,
  Folder,
  FolderPlus,
  Image as ImageIcon,
  Info,
  Loader2,
  MoreVertical,
  Pencil,
  RefreshCw,
  Trash2,
  Upload as UploadIcon,
} from 'lucide-react'
import { useCallback, useEffect, useMemo, useRef, useState, type ChangeEvent, type DragEvent, type ReactNode } from 'react'

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
  type DocumentEmbeddingsBackfillDryRun,
  type DocumentEmbeddingsStatus,
  type DocumentFolder,
  type DocumentRecord,
  type DocumentSearchResult,
} from '@/hooks/use-documents'
import { documentDisplayName, formatBytes, mimeLabel } from '@/lib/document-display'
import { cn } from '@/lib/utils'

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
}

export function DocumentsPanel({ initialFolderId = null, onFolderChange, initialDocumentId = null, onEditDocument }: DocumentsPanelProps) {
  const [folderId, setFolderId] = useState<string | null>(initialFolderId)
  const documents = useDocuments(folderId)
  // No attachment cap here (review finding, use-document-uploads.ts:173):
  // MAX_ATTACHMENTS is Mate's per-message limit, and this panel isn't
  // building a message. `sequential` avoids opening one XMLHttpRequest per
  // file for a large dropped batch.
  const uploads = useDocumentUploads(folderId, { maxAttachments: NO_ATTACHMENT_CAP, sequential: true })

  const navigate = useCallback((id: string | null) => {
    setFolderId(id)
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
  const submitNewFolder = async () => {
    const name = newFolderName.trim()
    if (name === '') return
    await runAction(async () => {
      await documents.createFolder(name, folderId)
      setNewFolderOpen(false)
      setNewFolderName('')
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

  // ── embeddings backfill ("Index for semantic search") ──────────────
  // ADR 0106 E1c's consent gate: clicking the row's button first runs the
  // dry run (starts nothing, just reports what a real backfill would send)
  // and holds its result here to drive the confirmation dialog below -
  // Cancel (onOpenChange(false)) clears it with no POST ever made; only
  // Confirm calls startEmbeddingsBackfill, which is the operator's actual
  // consent for that text to reach OpenRouter.
  const [backfillDryRun, setBackfillDryRun] = useState<DocumentEmbeddingsBackfillDryRun | null>(null)
  const handleIndexClick = () => {
    void runAction(async () => {
      const dryRun = await documents.dryRunEmbeddingsBackfill()
      setBackfillDryRun(dryRun)
    })
  }
  const submitBackfill = async () => {
    await runAction(async () => {
      await documents.startEmbeddingsBackfill()
      setBackfillDryRun(null)
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

  useEffect(() => {
    if (!viewerDoc || !viewerId) return
    const isText = !viewerDoc.mime.startsWith('image/') && viewerDoc.mime !== 'application/pdf'
    if (!isText) { setViewerText(null); return }
    setViewerLoading(true)
    void fetch(`${apiBaseUrl}/api/documents/${encodeURIComponent(viewerId)}/text`)
      .then((res) => (res.ok ? res.json() : { text: '' }))
      .then((data) => setViewerText(data.text ?? ''))
      .finally(() => setViewerLoading(false))
  }, [viewerDoc, viewerId])

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
  const documentRows = useMemo(() => documents.documents, [documents.documents])
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
          <Button type="button" variant="outline" size="sm" onClick={() => setNewFolderOpen(true)}>
            <FolderPlus className="h-4 w-4" data-icon="inline-start" aria-hidden="true" />
            New folder
          </Button>
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
        {documents.tags.length > 0 && (
          <ToggleGroup
            aria-label="Filter by tag"
            value={documents.selectedTag ? [documents.selectedTag] : []}
            onValueChange={(value: string[]) => documents.setSelectedTag(value[value.length - 1] ?? null)}
            variant="outline"
            size="sm"
          >
            {documents.tags.map((t) => (
              <ToggleGroupItem key={t.tag} value={t.tag}>
                {t.tag} ({t.count})
              </ToggleGroupItem>
            ))}
          </ToggleGroup>
        )}
        <EmbeddingsStatusRow status={documents.embeddingsStatus} onIndexClick={handleIndexClick} />
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
              {folderRows.map((folder) => (
                <TableRow key={folder.id}>
                  <TableCell />
                  <TableCell>
                    <button
                      type="button"
                      onClick={() => navigate(folder.id)}
                      className="flex items-center gap-2 text-left font-medium hover:underline"
                    >
                      <Folder className="h-4 w-4 shrink-0 text-muted-foreground" aria-hidden="true" />
                      {folder.name}
                    </button>
                  </TableCell>
                  {/* Status, Tags and Size are document-only columns - a
                      folder row still needs a placeholder cell for each of
                      the header's six columns, or the actions cell below
                      shifts left under "Size" and the real w-10 actions
                      column sits empty (review finding). */}
                  <TableCell />
                  <TableCell />
                  <TableCell />
                  <TableCell>
                    <FolderRowMenu
                      folder={folder}
                      onRename={() => { setRenameTarget({ kind: 'folder', id: folder.id, name: folder.name }); setRenameValue(folder.name) }}
                      onMove={() => setMoveTarget({ ids: [folder.id], label: folder.name })}
                      onDelete={() => setDeleteTarget({ kind: 'folder', id: folder.id, name: folder.name })}
                    />
                  </TableCell>
                </TableRow>
              ))}
              {documentRows.map((doc) => {
                const Icon = rowIcon(doc.mime)
                const name = documentDisplayName(doc)
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
                      <button
                        type="button"
                        onClick={() => setViewerId(doc.id)}
                        className="flex flex-col items-start gap-0.5 text-left"
                      >
                        <span className="flex items-center gap-2 font-medium hover:underline">
                          <Icon className="h-4 w-4 shrink-0 text-muted-foreground" aria-hidden="true" />
                          {name}
                        </span>
                        {doc.status === 'failed' && doc.error && (
                          <span className="text-xs text-destructive">{doc.error}</span>
                        )}
                      </button>
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
      <Dialog open={newFolderOpen} onOpenChange={setNewFolderOpen}>
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

      {/* ── Index for semantic search (embeddings backfill) confirmation ── */}
      <AlertDialog open={backfillDryRun !== null} onOpenChange={(open) => { if (!open) setBackfillDryRun(null) }}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Index for semantic search?</AlertDialogTitle>
            <AlertDialogDescription>
              This sends the text of {backfillDryRun?.counts.chunks_pending ?? 0}{' '}
              {backfillDryRun?.counts.chunks_pending === 1 ? 'pending chunk' : 'pending chunks'} (about{' '}
              {backfillDryRun?.tokens_estimate ?? 0} tokens) to OpenRouter, which bills for it.
              Keyword search keeps working either way.
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>Cancel</AlertDialogCancel>
            <AlertDialogAction onClick={() => { void submitBackfill() }}>Index</AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>

      {/* ── Viewer ─────────────────────────────────────────────────── */}
      <Sheet open={viewerId !== null} onOpenChange={(open) => { if (!open) setViewerId(null) }}>
        <SheetContent side="right" className="w-full sm:max-w-2xl">
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
            </div>
          )}
          <div className="min-h-0 flex-1 overflow-auto rounded-md border">
            {viewerLoading && <p className="p-4 text-sm text-muted-foreground">Loading…</p>}
            {!viewerLoading && viewerDoc?.mime === 'application/pdf' && (
              <iframe title={documentDisplayName(viewerDoc)} src={contentUrlFor(viewerDoc.id)} className="h-full min-h-[70vh] w-full" />
            )}
            {!viewerLoading && viewerDoc?.mime.startsWith('image/') && (
              <img src={contentUrlFor(viewerDoc.id)} alt={documentDisplayName(viewerDoc)} className="max-w-full" />
            )}
            {!viewerLoading && viewerDoc && !viewerDoc.mime.startsWith('image/') && viewerDoc.mime !== 'application/pdf' && (
              <pre className="whitespace-pre-wrap p-4 text-sm">{viewerText ?? ''}</pre>
            )}
          </div>
        </SheetContent>
      </Sheet>
    </div>
  )
}

// ADR 0106 E1c's status row: library-wide, not folder-scoped, so it sits in
// the toolbar rather than inside the folder/search view below it. Renders
// nothing at all - not even an empty wrapper - while the status hasn't
// loaded yet or semantic search is simply off (`enabled:false`, e.g. no
// embedding model configured): an operator who never turned this on should
// see no permanent nag about it. `active` (something still needs
// embedding, or a backfill is already running) is what separates the quiet
// "up to date" line from the count-plus-button state; the two states never
// need a shared wrapper since a caller only ever sees one at a time.
function EmbeddingsStatusRow({
  status,
  onIndexClick,
}: {
  status: DocumentEmbeddingsStatus | null
  onIndexClick: () => void
}) {
  if (!status || !status.enabled) return null
  const { counts, backfill } = status
  const active = backfill.running || counts.chunks_pending > 0

  if (!active) {
    return (
      <span data-testid="documents-embeddings-status" className="ml-auto text-xs text-muted-foreground">
        Semantic search up to date.
      </span>
    )
  }

  return (
    <div data-testid="documents-embeddings-status" className="ml-auto flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
      {backfill.running && <Loader2 className="h-3 w-3 shrink-0 animate-spin" aria-hidden="true" />}
      <span>
        {backfill.running
          ? `Indexing for semantic search… ${backfill.chunks_embedded} embedded this run, ${counts.chunks_pending} left`
          : `${counts.chunks_pending} chunk${counts.chunks_pending === 1 ? '' : 's'} not yet searchable by meaning`}
      </span>
      <Button type="button" size="sm" variant="outline" disabled={backfill.running} onClick={onIndexClick}>
        Index for semantic search
      </Button>
      {backfill.last_error && (
        <span className="rounded-xs border border-amber-500/40 bg-amber-500/10 px-1.5 py-0.5 text-amber-600 dark:text-amber-400">
          {backfill.last_error}
        </span>
      )}
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
  onRename,
  onMove,
  onDelete,
}: {
  folder: DocumentFolder
  onRename: () => void
  onMove: () => void
  onDelete: () => void
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
        <DropdownMenuSeparator />
        <DropdownMenuItem variant="destructive" onClick={onDelete}>
          <Trash2 className="h-4 w-4" aria-hidden="true" /> Delete
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  )
}
