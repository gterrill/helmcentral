import { useEffect, useRef, useState } from 'react'

import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import type { DocumentSearchResult } from '@/hooks/use-documents'
import { apiRequest, useApiQuery } from '@/lib/api-query'
import { errorMessage } from '@/lib/errors'

// ADR 0123: "Add document" on the Equipment editor. Deliberately its own
// small read, not a second consumer of use-documents.ts's useDocuments
// hook - that hook is folder-scoped (path/folders/tags/embeddings status,
// none of which this needs) and parametrised by a currently-browsed folder
// this picker has no notion of. Search runs over the WHOLE library
// (docs/features/inventory-tracking.md: "search runs over the whole
// library"), the same GET /api/documents?q= endpoint, with no folder param
// at all so nothing scopes it to one.

export interface DocumentLinkPickerResult {
  document_id: string
  title: string
  filename: string
  /** From the document's own record, read at pick time: the search list
   * carries none of these, and the Documents list needs them to show NOTE,
   * a type badge and a date before the link is saved. */
  kind: string
  mime: string
  created_at: string
}

interface DocumentLinkPickerProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  onPick: (doc: DocumentLinkPickerResult) => void
  /** Document ids already linked - filtered out of the results so the
   * operator can't pick the same document twice from here (EquipmentEditor
   * itself also no-ops a duplicate add, this just keeps it from showing). */
  excludeIds?: string[]
  /** Narrows what can be picked by the document's type; a pick that fails it
   * is refused with `rejection` and the picker stays open. */
  accept?: (mime: string) => boolean
  rejection?: string
  /** Wording for a pick that is not "a document". */
  heading?: string
  prompt?: string
}

const NO_RESULTS: DocumentSearchResult[] = []

export function DocumentLinkPicker({ open, onOpenChange, onPick, excludeIds = [], accept, rejection = 'That document is not a type that can be used here.', heading = 'Add a document', prompt = 'Search the document library and pick one to link.' }: DocumentLinkPickerProps) {
  const [query, setQuery] = useState('')
  // A failed pick (the record could not be read), shown beside a search error.
  const [pickError, setPickError] = useState<string | null>(null)
  const trimmed = query.trim()

  const search = useApiQuery({
    queryKey: ['/api/documents', { q: trimmed }],
    path: `/api/documents?${new URLSearchParams({ q: trimmed }).toString()}`,
    enabled: open && trimmed !== '',
    select: (raw: { results?: DocumentSearchResult[] }) => raw.results ?? NO_RESULTS,
  })
  const results = trimmed === '' ? NO_RESULTS : (search.data ?? NO_RESULTS)
  const searching = search.isLoading
  const error = pickError ?? search.error?.message ?? null

  // A new search starts clean.
  useEffect(() => { setPickError(null) }, [trimmed])

  // Resets the search box on every close (whether from a pick or Cancel) so
  // reopening the picker for a different item never shows a stale query and
  // its now-irrelevant results.
  useEffect(() => {
    if (!open) { setQuery(''); setPickError(null) }
  }, [open])

  const visibleResults = results.filter((r) => !excludeIds.includes(r.document_id))

  // Which pick (if any) is waiting on its record, and whether the picker is
  // still open: a pick that resolves after Cancel/Escape must be dropped, not
  // added, and a second pick must not start while one is in flight.
  const [pickingId, setPickingId] = useState<string | null>(null)
  const pickSeq = useRef(0)
  useEffect(() => {
    if (!open) { pickSeq.current += 1; setPickingId(null) }
  }, [open])

  const pick = async (result: DocumentSearchResult) => {
    if (pickingId !== null) return
    const seq = (pickSeq.current += 1)
    setPickingId(result.document_id)
    try {
      const record = await apiRequest<{ kind: string; mime: string; created_at: string }>(
        `/api/documents/${encodeURIComponent(result.document_id)}`,
      )
      if (seq !== pickSeq.current) return
      if (accept && !accept(record.mime)) {
        setPickError(rejection)
        return
      }
      onPick({
        document_id: result.document_id,
        title: result.title || result.filename,
        filename: result.filename,
        kind: record.kind,
        mime: record.mime,
        created_at: record.created_at,
      })
      onOpenChange(false)
    } catch (err) {
      if (seq !== pickSeq.current) return
      // Fail loud: linking a document whose record cannot be read would put
      // a row with a guessed type in the list.
      setPickError(errorMessage(err))
    } finally {
      if (seq === pickSeq.current) setPickingId(null)
    }
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-lg">
        <DialogHeader>
          <DialogTitle>{heading}</DialogTitle>
          <DialogDescription>{prompt}</DialogDescription>
        </DialogHeader>

        <Input
          aria-label="Search documents"
          placeholder="Title, filename..."
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          autoFocus
        />

        {error && (
          <p role="alert" className="rounded-md border border-destructive/40 bg-destructive/10 px-3 py-2 text-sm text-destructive">
            {error}
          </p>
        )}

        <div className="max-h-72 divide-y divide-border overflow-y-auto rounded-md border border-border">
          {searching && <p className="p-3 text-sm text-muted-foreground">Searching...</p>}
          {!searching && !error && query.trim() !== '' && visibleResults.length === 0 && (
            <p className="p-3 text-sm text-muted-foreground">No documents found.</p>
          )}
          {!searching && visibleResults.map((result) => (
            <button
              key={result.document_id}
              type="button"
              disabled={pickingId !== null}
              onClick={() => { void pick(result) }}
              className="flex w-full flex-col items-start gap-0.5 px-3 py-2 text-left hover:bg-muted disabled:opacity-60 focus-visible:outline-hidden focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-inset"
            >
              <span className="text-sm font-medium">{result.title || result.filename}</span>
              <span className="text-xs text-muted-foreground">{result.filename}</span>
            </button>
          ))}
        </div>
      </DialogContent>
    </Dialog>
  )
}
