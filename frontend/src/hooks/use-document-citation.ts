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
const citationCache = new Map<string, Promise<CitationLookup>>()

function fetchDocumentCitation(id: string): Promise<CitationLookup> {
  let cached = citationCache.get(id)
  if (cached) return cached

  cached = (async (): Promise<CitationLookup> => {
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
      // simply not this hook's job to retry a dropped link on its own.
      return { status: 'error' }
    }
  })()
  citationCache.set(id, cached)
  return cached
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
