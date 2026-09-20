import { describe, it, expect } from 'vitest'

import { formatDocumentTime } from '@/lib/document-display'

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
