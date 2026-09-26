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
