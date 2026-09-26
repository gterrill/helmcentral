import { useEffect, useState } from 'react'
import { apiBaseUrl } from '@/config/api'

// Mate UI cycle: document sources as icons. A citation link only ever
// carries a document id (assistant_prompt.go's citation guidance puts the
// title in the link TEXT, not the href) - the icon and its "not found"
// state both need one GET /api/documents/:id, the same endpoint the Details
// page's useDocument (hooks/use-documents.ts) already reads. Kept as its own
// small hook rather than reusing useDocument directly: that hook owns PATCH
// and a pending-status poll a read-only citation icon has no use for, and
// its own per-instance state has no way to share a fetch across the several
// icons a single reply's markdown can render for the same document.

export interface DocumentCitationInfo {
  id: string
  title: string
  mime: string
  kind: string
}

type CitationLookup =
  | { status: 'ok'; doc: DocumentCitationInfo }
  | { status: 'not-found' }
  | { status: 'error' }

interface DocumentApiShape {
  id: string
  title: string
  filename: string
  mime: string
  kind: string
}

// One in-flight-or-resolved lookup per document id, shared by every citation
// icon that cites it - across one reply's several links, across every
// message in a conversation, and across remounts for the life of the page.
// This is the "no N+1 storm" guard AGENTS.md's fallback-policy neighbour
// (the citation contract's own spec) asks for: without it, a reply citing
// the same manual three times, or a long conversation citing it across many
// turns, would re-fetch it every single time an icon mounts.
//
// Only 'ok' and 'not-found' are worth keeping this way - both are real,
// stable answers. An 'error' result (a dropped link, a 500) is evicted the
// moment it resolves (below): caching it the same way used to mean one
// transient blip left every citation of that document showing a broken icon
// for the rest of the page's life, long after the document itself, and the
// network, were fine again. Evicting lets the next mount - the operator
// scrolling the reply back into view, or opening the conversation again -
// retry instead of replaying the same stale failure from cache.
const citationCache = new Map<string, Promise<CitationLookup>>()

function fetchDocumentCitation(id: string): Promise<CitationLookup> {
  const existing = citationCache.get(id)
  if (existing) return existing

  const promise = (async (): Promise<CitationLookup> => {
    try {
      const response = await fetch(`${apiBaseUrl}/api/documents/${encodeURIComponent(id)}`)
      if (response.status === 404) return { status: 'not-found' }
      if (!response.ok) return { status: 'error' }
      const data = (await response.json()) as DocumentApiShape
      const title = data.title.trim() !== '' ? data.title : data.filename
      return { status: 'ok', doc: { id: data.id, title, mime: data.mime, kind: data.kind } }
    } catch {
      // AGENTS.md fallback policy: this is not "pretend the document is
      // fine" - the citation icon renders its own distinct broken state for
      // 'error' (see useDocumentCitation below), same as 'not-found'. It is
      // simply not this hook's job to retry a dropped link on its own -
      // eviction below is what lets a LATER mount retry instead.
      return { status: 'error' }
    }
  })()
  citationCache.set(id, promise)

  void promise.then((lookup) => {
    // The `=== promise` guard is belt-and-braces: nothing else in this
    // module ever replaces a live entry out from under an in-flight lookup,
    // but this keeps a stale eviction from ever clobbering a fresher entry
    // if that ever changes.
    if (lookup.status === 'error' && citationCache.get(id) === promise) {
      citationCache.delete(id)
    }
  })

  return promise
}

/** Test-only: clears the shared cache between test cases. */
export function __resetDocumentCitationCacheForTests(): void {
  citationCache.clear()
}

export interface UseDocumentCitationResult {
  status: 'idle' | 'loading' | 'ok' | 'not-found' | 'error'
  title?: string
  mime?: string
  kind?: string
}

/**
 * Resolves a citation link's document id to what its icon needs: title
 * (falling back to filename), mime and kind for citationIconKind
 * (lib/document-citation.ts), or 'not-found'/'error' when it can't - an
 * unknown or deleted document id must render as a visibly broken icon, never
 * silently disappear. `id === null` (a citation link that failed to parse at
 * all) returns 'idle' and fetches nothing - there is no id to look up.
 */
export function useDocumentCitation(id: string | null): UseDocumentCitationResult {
  const [result, setResult] = useState<UseDocumentCitationResult>(id === null ? { status: 'idle' } : { status: 'loading' })

  useEffect(() => {
    if (id === null) {
      setResult({ status: 'idle' })
      return
    }
    let cancelled = false
    setResult({ status: 'loading' })
    void fetchDocumentCitation(id).then((lookup) => {
      if (cancelled) return
      if (lookup.status === 'ok') {
        setResult({ status: 'ok', title: lookup.doc.title, mime: lookup.doc.mime, kind: lookup.doc.kind })
      } else {
        setResult({ status: lookup.status })
      }
    })
    return () => {
      cancelled = true
    }
  }, [id])

  return result
}
