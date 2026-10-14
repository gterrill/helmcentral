import { useMemo } from 'react'
import { ApiError, getQueryData, useApiQuery } from '@/lib/api-query'

// Mate UI cycle: document sources as icons. A citation link only ever
// carries a document id (assistant_prompt.go's citation guidance puts the
// title in the link TEXT, not the href) - the icon and its "not found"
// state both need one GET /api/documents/:id, the same endpoint the Details
// page's useDocument (hooks/use-documents.ts) already reads. Kept as its own
// small hook rather than reusing useDocument directly: that hook owns PATCH
// and a pending-status poll a read-only citation icon has no use for.

interface DocumentApiShape {
  id: string
  title: string
  filename: string
  mime: string
  kind: string
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
 *
 * Every icon citing the same document shares one read through the query
 * store, across one reply's several links, every message in a conversation
 * and remounts: no N+1 storm. Unlike a hand-kept cache it revalidates on
 * mount, so a transient failure never outlives the next one. The key is the
 * Details page's own (use-documents.ts), so a document already open there is
 * answered from the same copy.
 */
function toCitation(raw: DocumentApiShape) {
  return {
    title: raw.title.trim() !== '' ? raw.title : raw.filename,
    mime: raw.mime,
    kind: raw.kind,
  }
}

export function useDocumentCitation(id: string | null): UseDocumentCitationResult {
  const path = `/api/documents/${encodeURIComponent(id ?? '')}`
  // A document already resolved this page-session is answered from the store
  // without a request: every icon mount would otherwise revalidate it, and one
  // transient failure would be seen by every citation of that document. Read
  // once per id, so the icons that mount together still share the one fetch.
  const resolved = useMemo(
    () => (id === null ? undefined : getQueryData<DocumentApiShape>([path])),
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [path],
  )
  const query = useApiQuery({
    queryKey: [path],
    path,
    enabled: id !== null && resolved === undefined,
    select: toCitation,
  })

  if (id === null) return { status: 'idle' }
  if (resolved !== undefined) return { status: 'ok', ...toCitation(resolved) }
  // Data already shown wins over a failed revalidation. AGENTS.md fallback
  // policy still holds for a lookup that never succeeded: a confirmed 404 is
  // not-found and any other failure is error, each a visibly broken icon.
  if (query.data) return { status: 'ok', ...query.data }
  if (query.error) return { status: query.error instanceof ApiError && query.error.status === 404 ? 'not-found' : 'error' }
  return { status: 'loading' }
}
