import { describe, it, expect } from 'vitest'

import { documentFailureMessage, formatDocumentTime } from '@/lib/document-display'

// ADR 0115: the Details page's Uploaded/Last indexed rows need a date, not
// just a time - formatAlarmTime (alarm-display.ts) is built for "raised a
// few minutes ago" and drops the date entirely for today, which is the
// wrong default for a document library where "eighteen months ago" is the
// normal case. formatDocumentTime always carries day/month/year.

describe('formatDocumentTime', () => {
  it('renders a known ISO string as "D Mon YYYY HH:MM"', () => {
    expect(formatDocumentTime('2026-09-20T06:28:00')).toBe('20 Sep 2026 06:28')
  })

  it('returns null for a missing value', () => {
    expect(formatDocumentTime(undefined)).toBeNull()
    expect(formatDocumentTime(null)).toBeNull()
    expect(formatDocumentTime('')).toBeNull()
  })

  it('returns null for a malformed string', () => {
    expect(formatDocumentTime('not a date')).toBeNull()
  })
})

// documentFailureMessage turns a failed document's stage/mime into an
// operator-facing sentence with a next step, instead of the raw stored
// error (which used to show up verbatim in the Documents list - an
// internal storage path, the word "panic", no indication of what to do
// about it). The raw error stays available elsewhere (a title attribute in
// the list, a "Details" disclosure on the Details page) - this function
// only ever supplies the headline sentence.
describe('documentFailureMessage', () => {
  it('gives PDF-specific guidance with a fallback path when the PDF text layer could not be read', () => {
    expect(documentFailureMessage({ stage: 'extract', mime: 'application/pdf' })).toBe(
      "Couldn't read the text in this PDF. Try Reindex; if it fails again, open it in a PDF viewer, save a copy and upload that.",
    )
  })

  it('gives image-specific guidance for a failed image extract', () => {
    expect(documentFailureMessage({ stage: 'extract', mime: 'image/jpeg' })).toBe(
      "Couldn't read this image. Try Reindex, or upload it again.",
    )
  })

  it('falls back to a generic extract message for any other mime', () => {
    expect(documentFailureMessage({ stage: 'extract', mime: 'text/plain' })).toBe(
      "Couldn't read this file. Try Reindex, or upload it again.",
    )
  })

  it('gives a different message once the document failed past extraction (the enrich stage)', () => {
    expect(documentFailureMessage({ stage: 'enrich', mime: 'application/pdf' })).toBe(
      "Mate couldn't finish indexing this document. Try Reindex.",
    )
  })
})
