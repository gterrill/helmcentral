import { Download, Eye, Info, ListChecks, Pencil, Pin, PinOff } from 'lucide-react'
import { useCallback, useEffect, useMemo, useState } from 'react'

import { AskMateSelection } from '@/components/ask-mate-selection'
import { NoteEditor } from '@/components/note-editor'
import { NoteMarkdown } from '@/components/note-markdown'
import { Button } from '@/components/ui/button'
import { Sheet, SheetContent, SheetDescription, SheetHeader, SheetTitle } from '@/components/ui/sheet'
import type { DocumentRecord } from '@/hooks/use-documents'
import type { NoteDetail, NotePatch } from '@/hooks/use-notes'
import { apiRequest, useApiQuery } from '@/lib/api-query'
import { documentContentUrl, downloadDocument } from '@/lib/document-download'
import { documentDisplayName, formatBytes, mimeLabel } from '@/lib/document-display'
import type { NoteLink } from '@/lib/note-links'

import { ChecklistRunner } from './checklist-runner'

// The right-hand document viewer: a PDF, an image, a note (read, edit, pin,
// run its checklist) or plain text. Extracted from documents-panel.tsx so an
// Equipment item's Documents list opens the same drawer the Documents page
// does. The caller owns WHICH document is open (documentId /
// onDocumentChange, null = closed) because the panel also opens it from
// search results and note capture.

export interface DocumentViewerSheetProps {
  documentId: string | null
  /** null closes; an id navigates (a link inside a rendered note). */
  onDocumentChange: (id: string | null) => void
  /** Documents the caller already holds. A viewer target not in here has its
   * own metadata fetched - a Mate chip or a note link can point at a
   * document filed anywhere. */
  knownDocuments?: DocumentRecord[]
  getNote: (id: string) => Promise<NoteDetail>
  patchNote: (id: string, patch: NotePatch) => Promise<NoteDetail>
  /** Shows the Details button when given. */
  onEditDocument?: (id: string) => void
  mateAvailable?: boolean
  onAskMate?: (question: string, options?: { newConversation?: boolean }) => void
  /** Wraps download and pin so the caller's own error banner catches a
   * failure. Without it the viewer shows the error itself. */
  runAction?: (action: () => Promise<unknown>) => Promise<void>
}

export function DocumentViewerSheet({
  documentId: viewerId,
  onDocumentChange,
  knownDocuments,
  getNote: getViewerNote,
  patchNote: patchViewerNote,
  onEditDocument,
  mateAvailable = false,
  onAskMate,
  runAction: runActionProp,
}: DocumentViewerSheetProps) {
  const [pinOverride, setPinOverride] = useState<{ doc: DocumentRecord; pinned: boolean } | null>(null)
  const [viewerText, setViewerText] = useState<string | null>(null)
  const [viewerLoading, setViewerLoading] = useState(false)
  // Covers reading a note, saving one, and (without a caller runAction) a
  // failed download or pin - same banner each time.
  const [viewerError, setViewerError] = useState<string | null>(null)

  const runAction = useCallback(async (action: () => Promise<unknown>) => {
    if (runActionProp) return runActionProp(action)
    try {
      await action()
      setViewerError(null)
    } catch (err) {
      setViewerError(err instanceof Error ? err.message : String(err))
    }
  }, [runActionProp])

  // A viewer target may not be in whatever the caller holds - a Mate
  // attachment chip can link to a document filed anywhere - so its own
  // metadata is read when it is not. The key is the Details page's own, so a
  // document opened there first is answered from the same copy. A failure
  // lands in the banner (AGENTS.md fallback policy: surface it, never a blank
  // sheet).
  const found = viewerId ? knownDocuments?.find((d) => d.id === viewerId) : undefined
  const docPath = `/api/documents/${encodeURIComponent(viewerId ?? '')}`
  const fetched = useApiQuery<DocumentRecord>({
    queryKey: [docPath],
    path: docPath,
    enabled: !!viewerId && !found,
  })
  const baseDoc = viewerId ? (found ?? fetched.data ?? null) : null
  // A pin applies to the record it was made on; a fresher record from the
  // caller or the store replaces it.
  const viewerDoc = useMemo(
    () => (baseDoc && pinOverride?.doc === baseDoc ? { ...baseDoc, pinned: pinOverride.pinned } : baseDoc),
    [baseDoc, pinOverride],
  )
  // The effects below depend on what they read of the document, not on the
  // record, so a refreshed listing or a pin does not reload the text.
  const docId = viewerDoc?.id
  const docKind = viewerDoc?.kind
  const docMime = viewerDoc?.mime
  const shownError = viewerError ?? fetched.error?.message ?? null
  const shownLoading = viewerLoading || fetched.isLoading

  // Whatever was showing belonged to the previous target: drop it at once so
  // it can never render under the new document's name while that loads. Keyed
  // to the id alone, so a refreshed knownDocuments list does not blank the
  // sheet.
  useEffect(() => {
    setPinOverride(null)
    setViewerText(null)
    setViewerError(null)
    setViewerLoading(false)
  }, [viewerId])

  useEffect(() => {
    if (docMime === undefined || !viewerId) return
    const isText = !docMime.startsWith('image/') && docMime !== 'application/pdf'
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
    const load = docKind === 'note'
      ? getViewerNote(viewerId).then((detail) => detail.body)
      : apiRequest<{ text?: string }>(`/api/documents/${encodeURIComponent(viewerId)}/text`).then((data) => data.text ?? '')

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
  }, [docMime, docKind, viewerId, getViewerNote])

  // Opening a DIFFERENT document always lands back in the read-only view -
  // an editing session belongs to the document that was open when it
  // started.
  const [viewerEditing, setViewerEditing] = useState(false)
  useEffect(() => { setViewerEditing(false) }, [viewerId])
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

  // Pinning a note for Mate (plan §8): beside Edit/Start checklist in this
  // toolbar, the one place documents.pinned can become true.
  const handleTogglePin = useCallback(async () => {
    if (!viewerDoc || viewerDoc.kind !== 'note') return
    await runAction(async () => {
      const updated = await patchViewerNote(viewerDoc.id, { pinned: !viewerDoc.pinned })
      if (baseDoc && updated.document.id === baseDoc.id) setPinOverride({ doc: baseDoc, pinned: updated.document.pinned })
    })
  }, [viewerDoc, baseDoc, patchViewerNote, runAction])

  // Running a checklist (plan §7, ADR 0118) is a MODE of this same Sheet.
  // viewerText comes from the generic /text (any mime, any kind), so whether
  // this document even HAS a checklist has to come from GET /api/notes/:id's
  // own `checklist` field - only relevant for kind='note'.
  const [viewerHasChecklist, setViewerHasChecklist] = useState(false)
  useEffect(() => {
    if (docId === undefined || docKind !== 'note') {
      setViewerHasChecklist(false)
      return
    }
    let cancelled = false
    // Promise.resolve(...) rather than calling getViewerNote's own promise
    // directly: it's a mocked jest-style fn in most of the panel's own
    // tests, and plenty of them never bother stubbing a resolved value for
    // a getNote() call they aren't testing - this must degrade to "no
    // checklist" rather than throw on a bare vi.fn()'s undefined return.
    Promise.resolve(getViewerNote(docId))
      .then((detail) => { if (!cancelled) setViewerHasChecklist((detail?.checklist?.length ?? 0) > 0) })
      .catch(() => { if (!cancelled) setViewerHasChecklist(false) })
    return () => { cancelled = true }
  }, [docId, docKind, getViewerNote])

  // A running checklist belongs to the document that was open when it
  // started, the same rule viewerEditing's reset follows.
  const [viewerChecklistRunning, setViewerChecklistRunning] = useState(false)
  useEffect(() => { setViewerChecklistRunning(false) }, [viewerId])

  // A link inside a rendered note's markdown (ADR 0116): switch the viewer
  // to that id. An anchor scrolls within whatever is already rendered.
  const handleNoteNavigate = useCallback((link: NoteLink) => {
    if (link.kind === 'note' || link.kind === 'document') {
      onDocumentChange(link.id)
      return
    }
    if (link.kind === 'anchor') {
      document.getElementById(link.hash)?.scrollIntoView({ block: 'start' })
    }
  }, [onDocumentChange])

  return (
    <Sheet open={viewerId !== null} onOpenChange={(open) => { if (!open) onDocumentChange(null) }}>
      <SheetContent side="right" className="flex h-full w-full flex-col gap-4 sm:max-w-2xl">
        <SheetHeader>
          <SheetTitle>{viewerDoc ? documentDisplayName(viewerDoc) : shownError ? 'Document unavailable' : 'Loading…'}</SheetTitle>
          {viewerDoc && <SheetDescription>{mimeLabel(viewerDoc.mime)} · {formatBytes(viewerDoc.size_bytes)}</SheetDescription>}
        </SheetHeader>
        {viewerDoc && (
          <div className="flex items-center gap-2">
            <Button type="button" size="sm" variant="outline" onClick={() => { void runAction(() => downloadDocument(viewerDoc.id, viewerDoc.filename)) }}>
              <Download className="h-4 w-4" data-icon="inline-start" aria-hidden="true" />
              Download
            </Button>
            {onEditDocument && (
              <Button type="button" size="sm" variant="outline" onClick={() => onEditDocument(viewerDoc.id)}>
                <Info className="h-4 w-4" data-icon="inline-start" aria-hidden="true" />
                Details
              </Button>
            )}
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
          {shownLoading && <p className="p-4 text-sm text-muted-foreground">Loading…</p>}
          {shownError && <p role="alert" className="p-4 text-sm text-destructive">{shownError}</p>}
          {!shownLoading && viewerDoc?.mime === 'application/pdf' && (
            <iframe title={documentDisplayName(viewerDoc)} src={documentContentUrl(viewerDoc.id)} className="h-full min-h-[70vh] w-full" />
          )}
          {!shownLoading && viewerDoc?.mime.startsWith('image/') && (
            <img src={documentContentUrl(viewerDoc.id)} alt={documentDisplayName(viewerDoc)} className="max-w-full" />
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
          {!shownLoading && viewerDoc?.mime === 'text/markdown' && viewerDoc.kind === 'note' && viewerChecklistRunning && (
            <ChecklistRunner
              noteId={viewerDoc.id}
              noteTitle={documentDisplayName(viewerDoc)}
              onExit={() => setViewerChecklistRunning(false)}
            />
          )}
          {!shownLoading && viewerDoc?.mime === 'text/markdown' && viewerDoc.kind === 'note' && viewerEditing && !viewerChecklistRunning && (
            <div className="p-1">
              <NoteEditor key={viewerDoc.id} value={viewerText ?? ''} onSave={handleSaveViewerNote} />
            </div>
          )}
          {!shownLoading && viewerDoc?.mime === 'text/markdown' && !viewerChecklistRunning && !(viewerDoc.kind === 'note' && viewerEditing) && (
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
          {!shownLoading && viewerDoc && viewerDoc.mime !== 'text/markdown' && !viewerDoc.mime.startsWith('image/') && viewerDoc.mime !== 'application/pdf' && (
            <pre className="whitespace-pre-wrap p-4 text-sm">{viewerText ?? ''}</pre>
          )}
        </div>
      </SheetContent>
    </Sheet>
  )
}
