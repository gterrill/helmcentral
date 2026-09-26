import { describe, it, expect } from 'vitest'

import { citationIconKind, documentViewerHref, parseDocumentCitationHref } from '@/lib/document-citation'

// Mate UI cycle: document sources as icons. The backend cites a document as
// `[Title](/documents?document=<id>)` (assistant_prompt.go's citation
// guidance) - the same href the Documents viewer deep link
// (documentViewerHref, moved here from assistant-thread.tsx) already
// produces, so a citation opens the exact document the operator would reach
// by clicking an attachment chip. parseDocumentCitationHref is the inverse:
// it picks a document id back out of that href so assistant-markdown-impl.tsx
// can tell a citation link apart from an ordinary one, entirely client-side -
// no network round trip needed just to know a link IS a citation.

describe('documentViewerHref', () => {
  it('produces the /documents?document=<id> viewer deep link', () => {
    expect(documentViewerHref('abc-123')).toBe('/documents?document=abc-123')
  })
})

describe('parseDocumentCitationHref', () => {
  it('extracts the document id from a bare citation link', () => {
    expect(parseDocumentCitationHref('/documents?document=abc-123')).toBe('abc-123')
  })

  it('extracts the id even alongside other query params', () => {
    expect(parseDocumentCitationHref('/documents?folder=f1&document=abc-123')).toBe('abc-123')
  })

  it('returns null for a bare /documents link with no document param', () => {
    expect(parseDocumentCitationHref('/documents')).toBeNull()
  })

  it('returns null for an empty document param', () => {
    expect(parseDocumentCitationHref('/documents?document=')).toBeNull()
  })

  it('returns null for the Details page edit route (a different shape)', () => {
    expect(parseDocumentCitationHref('/documents/abc-123')).toBeNull()
  })

  it('returns null for an ordinary external link', () => {
    expect(parseDocumentCitationHref('https://openrouter.ai')).toBeNull()
  })

  it('returns null for an unrelated in-app path', () => {
    expect(parseDocumentCitationHref('/mate/abc-123')).toBeNull()
  })

  // Code-review finding: pathname-only matching treated ANY href whose
  // pathname happened to be /documents as a citation, regardless of origin -
  // an absolute other-origin URL got the in-app icon (and a local lookup for
  // a document id that means nothing on this server) instead of rendering
  // as the external link it actually is.
  it('returns null for an absolute URL on a different origin, even with the right path/query', () => {
    expect(parseDocumentCitationHref('https://example.com/documents?document=abc')).toBeNull()
  })

  // Same failure mode, protocol-relative: `new URL('//host/...', base)`
  // resolves against the base's OWN scheme, silently adopting `host` as if
  // it were this app.
  it('returns null for a protocol-relative //host/... URL', () => {
    expect(parseDocumentCitationHref('//example.com/documents?document=abc')).toBeNull()
  })

  // A relative path with no leading slash resolves against the CURRENT
  // page's own directory in a real browser, not against the root - from
  // /mate/<id>, "documents?document=abc" actually navigates to
  // /mate/documents?document=abc, nothing like the Documents viewer. Parsing
  // it against a rootless fake base silently "fixed" it into /documents,
  // which is not where a click would actually go.
  it('returns null for a relative path with no leading slash', () => {
    expect(parseDocumentCitationHref('documents?document=abc')).toBeNull()
  })

  it('still extracts the id from the one valid root-relative form', () => {
    expect(parseDocumentCitationHref('/documents?document=abc')).toBe('abc')
  })
})

describe('citationIconKind', () => {
  it('classes a note (kind=note) as note regardless of mime', () => {
    expect(citationIconKind({ mime: 'text/markdown', kind: 'note' })).toBe('note')
  })

  it('classes application/pdf as pdf', () => {
    expect(citationIconKind({ mime: 'application/pdf', kind: 'file' })).toBe('pdf')
  })

  it('classes any image/* mime as image', () => {
    expect(citationIconKind({ mime: 'image/jpeg', kind: 'file' })).toBe('image')
    expect(citationIconKind({ mime: 'image/heic', kind: 'file' })).toBe('image')
  })

  it('classes csv and spreadsheet mimes as spreadsheet', () => {
    expect(citationIconKind({ mime: 'text/csv', kind: 'file' })).toBe('spreadsheet')
    expect(citationIconKind({ mime: 'application/vnd.ms-excel', kind: 'file' })).toBe('spreadsheet')
    expect(citationIconKind({
      mime: 'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet',
      kind: 'file',
    })).toBe('spreadsheet')
  })

  it('falls back to file for anything else', () => {
    expect(citationIconKind({ mime: 'application/json', kind: 'file' })).toBe('file')
    expect(citationIconKind({ mime: 'text/plain', kind: 'file' })).toBe('file')
  })
})
