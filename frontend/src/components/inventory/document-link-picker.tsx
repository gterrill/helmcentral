import { useEffect, useRef, useState } from 'react'

import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { apiBaseUrl } from '@/config/api'
import type { DocumentSearchResult } from '@/hooks/use-documents'

// ADR 0123: "Add document" on the Equipment editor. Deliberately its own
// small fetch, not a second consumer of use-documents.ts's useDocuments
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
}

interface DocumentLinkPickerProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  onPick: (doc: DocumentLinkPickerResult) => void
  /** Document ids already linked - filtered out of the results so the
   * operator can't pick the same document twice from here (EquipmentEditor
   * itself also no-ops a duplicate add, this just keeps it from showing). */
  excludeIds?: string[]
}

export function DocumentLinkPicker({ open, onOpenChange, onPick, excludeIds = [] }: DocumentLinkPickerProps) {
  const [query, setQuery] = useState('')
  const [results, setResults] = useState<DocumentSearchResult[]>([])
  const [searching, setSearching] = useState(false)
  const [error, setError] = useState<string | null>(null)

  // Ordering guard, same idiom as use-documents.ts's own search(): a fresh
  // keystroke's request can land before an older one still in flight, and
  // without this the OLDER result could overwrite the newer query's own.
  const seqRef = useRef(0)

  useEffect(() => {
    if (!open) return
    const trimmed = query.trim()
    if (trimmed === '') {
      seqRef.current += 1
      setResults([])
      setError(null)
      setSearching(false)
      return
    }
    const seq = (seqRef.current += 1)
    setSearching(true)
    let cancelled = false
    void (async () => {
      try {
        const params = new URLSearchParams({ q: trimmed })
        const res = await fetch(`${apiBaseUrl}/api/documents?${params.toString()}`)
        if (!res.ok) {
          const payload = (await res.json().catch(() => ({}))) as { error?: string }
          throw new Error(payload.error ?? `HTTP ${res.status}`)
        }
        const data = (await res.json()) as { results?: DocumentSearchResult[] }
        if (cancelled || seq !== seqRef.current) return
        setResults(data.results ?? [])
        setError(null)
      } catch (err) {
        if (cancelled || seq !== seqRef.current) return
        setError(err instanceof Error ? err.message : String(err))
      } finally {
        if (!cancelled && seq === seqRef.current) setSearching(false)
      }
    })()
    return () => { cancelled = true }
  }, [open, query])

  // Resets the search box on every close (whether from a pick or Cancel) so
  // reopening the picker for a different item never shows a stale query and
  // its now-irrelevant results.
  useEffect(() => {
    if (!open) { setQuery(''); setResults([]); setError(null) }
  }, [open])

  const visibleResults = results.filter((r) => !excludeIds.includes(r.document_id))

  const pick = (result: DocumentSearchResult) => {
    onPick({ document_id: result.document_id, title: result.title || result.filename, filename: result.filename })
    onOpenChange(false)
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-lg">
        <DialogHeader>
          <DialogTitle>Add a document</DialogTitle>
          <DialogDescription>Search the document library and pick one to link.</DialogDescription>
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
              onClick={() => pick(result)}
              className="flex w-full flex-col items-start gap-0.5 px-3 py-2 text-left hover:bg-muted focus-visible:outline-hidden focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-inset"
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
