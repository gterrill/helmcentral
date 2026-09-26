import { formatAppLocation } from '@/lib/app-location'

// Mate UI cycle: document sources as icons. Mate cites a document it found
// via search_documents/read_document (or an attached document) as
// `[Title](/documents?document=<id>)` (assistant_prompt.go's citation
// guidance) - exactly the Documents viewer deep link an attachment chip's
// own link already produces (see documentViewerHref, moved here from
// assistant-thread.tsx so both call sites share the one URL shape). The
// markdown renderer (assistant-markdown-impl.tsx) turns a link of that exact
// shape into a small icon; parseDocumentCitationHref is how it tells a
// citation apart from an ordinary link, purely by parsing the href client
// side - no network round trip needed just to recognise one.

/**
 * The Documents panel's viewer deep link for a document id - `?document=`
 * opens the panel's own viewer sheet in place, rather than navigating to the
 * Details page's `/documents/<id>` editor route (a different destination,
 * ADR 0115 §2). `firstPageId: null` is irrelevant here (formatAppLocation
 * only reads it for the dashboard panel===null case).
 */
export function documentViewerHref(id: string): string {
  return formatAppLocation({ panel: 'documents', documentId: id }, { firstPageId: null })
}

/**
 * The inverse of documentViewerHref: picks the document id back out of an
 * href, or null when it isn't one - including the Details page's
 * `/documents/<id>` route (a path segment, not `?document=`), any other
 * in-app path, and any ordinary external link.
 *
 * Code-review finding: matching on `pathname` alone (via `new URL(href,
 * <fake base>)`) accepted anything whose PATH happened to be `/documents`,
 * regardless of what actually produced it - `new URL` resolves an absolute
 * URL (`https://example.com/documents?...`) or a protocol-relative one
 * (`//example.com/documents?...`) to that OTHER origin, ignoring the fake
 * base entirely, so a different site's own `/documents` route got treated
 * as this app's citation (the in-app icon, a local lookup for an id that
 * means nothing on this server, and the external-link handling - target
 * `_blank`/`rel=noreferrer` - silently dropped). A relative path with no
 * leading slash (`documents?document=...`) has the opposite problem: a real
 * browser resolves it against the CURRENT page's own directory (from
 * `/mate/<id>` that's `/mate/documents?...`, nothing like the Documents
 * viewer), but the fake base above has no directory of its own, so it
 * quietly "corrected" it to `/documents` - a path the href would never
 * actually navigate to.
 *
 * Only a root-relative href - starting with exactly one `/`, never `//` -
 * can mean this app's own root, so that is checked on the raw string first;
 * `new URL` only ever runs on an href already known to be root-relative,
 * where resolving it against a fake base's root is correct.
 */
export function parseDocumentCitationHref(href: string): string | null {
  if (!href.startsWith('/') || href.startsWith('//')) return null

  let url: URL
  try {
    url = new URL(href, 'http://citation.invalid')
  } catch {
    return null
  }
  if (url.pathname !== '/documents') return null
  const id = url.searchParams.get('document')
  return id !== null && id.trim() !== '' ? id : null
}

export type CitationIconKind = 'pdf' | 'spreadsheet' | 'image' | 'note' | 'file'

const SPREADSHEET_MIMES = new Set([
  'text/csv',
  'application/vnd.ms-excel',
  'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet',
])

/**
 * Which icon a citation should show, derived from what the document store
 * actually knows about it - kind='note' (the notes feature, documents_store.go)
 * wins over mime since a note's mime is always text/markdown regardless of
 * what it's actually about; otherwise mime picks pdf/spreadsheet/image, and
 * anything else (including plain kind='file' text, JSON, or a mime this
 * doesn't recognise) falls back to a generic file icon rather than guessing.
 */
export function citationIconKind(doc: { mime: string; kind: string }): CitationIconKind {
  if (doc.kind === 'note') return 'note'
  if (doc.mime === 'application/pdf') return 'pdf'
  if (doc.mime.startsWith('image/')) return 'image'
  if (SPREADSHEET_MIMES.has(doc.mime)) return 'spreadsheet'
  return 'file'
}
