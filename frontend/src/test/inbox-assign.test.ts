import { describe, expect, it } from 'vitest'

import type { InboxItem } from '@/lib/inbox-api'
import { assignRequest, initialAssignRows, rowProblem } from '@/lib/inbox-assign'
import type { PassageSummary } from '@/lib/passages'
import { SIGN_TITLE_MAX } from '@/lib/replay-signs'

const T0 = Date.UTC(2026, 9, 14)
const min = 60_000

const passages = [
  {
    id: 'p1', start: new Date(T0).toISOString(), end: new Date(T0 + 120 * min).toISOString(), title: 'Out', from_name: '', to_name: '',
    timezone: 'Australia/Brisbane',
  } as PassageSummary,
]

const item = (id: string, kind: InboxItem['group']['kind'], captureMs: number | null, mime = 'image/jpeg'): InboxItem => ({
  id, filename: `${id}.jpg`, mime, size_bytes: 1, capture_ms: captureMs, bearing_deg: null,
  fingerprint: '', sharpness: 0, created_ms: 0, content_url: '', group: { kind }, gps_mismatch: false,
})

describe('initialAssignRows', () => {
  it('fills titles from the suggestions, blank where there is none', () => {
    const rows = initialAssignRows([item('a', 'passage', T0 + 10 * min), item('b', 'passage', T0 + 20 * min)], { a: 'Hook Island' }, passages)
    expect(rows.map((r) => r.title)).toEqual(['Hook Island', ''])
  })

  it('places a photo on a passage by its own time, so no time is sent', () => {
    expect(initialAssignRows([item('a', 'passage', T0 + 10 * min)], {}, passages)[0].tMs).toBeNull()
  })

  it('offers the nearest passage start or end for a photo between passages', () => {
    expect(initialAssignRows([item('a', 'off_passage', T0 + 130 * min)], {}, passages)[0].tMs).toBe(T0 + 120 * min)
  })

  it('makes the operator choose for a photo with no time', () => {
    expect(initialAssignRows([item('a', 'no_time', null)], {}, passages)[0].tMs).toBeNull()
  })

  it('cuts a suggestion to the sign limit', () => {
    const [row] = initialAssignRows([item('a', 'passage', T0)], { a: 'x'.repeat(200) }, passages)
    expect(row.title).toHaveLength(SIGN_TITLE_MAX)
  })
})

describe('rowProblem', () => {
  const rows = initialAssignRows([item('a', 'passage', T0), item('b', 'off_passage', T0 + 130 * min), item('c', 'no_time', null), item('d', 'passage', T0, 'application/pdf')], {}, passages)
  const titled = (i: number, title = 'Dolphins') => ({ ...rows[i], title })

  it('wants a title of 1 to 80 characters', () => {
    expect(rowProblem(titled(0, ''))).toMatch(/title/i)
    expect(rowProblem(titled(0, '   '))).toMatch(/title/i)
    expect(rowProblem(titled(0, 'x'.repeat(81)))).toMatch(/80/)
    expect(rowProblem(titled(0, 'x'.repeat(80)))).toBeNull()
  })

  it('wants a time for a photo that is not on a passage', () => {
    expect(rowProblem(titled(1))).toBeNull()
    expect(rowProblem(titled(2))).toMatch(/where/i)
    expect(rowProblem({ ...titled(2), tMs: T0 })).toBeNull()
  })

  it('refuses a PDF, which cannot be a sign', () => {
    expect(rowProblem(titled(3))).toMatch(/photo/i)
  })
})

describe('assignRequest', () => {
  it('sends a time only where the operator or the nearest-end rule gave one', () => {
    const rows = initialAssignRows([item('a', 'passage', T0), item('b', 'off_passage', T0 + 130 * min)], { a: ' Whale ', b: 'Dock' }, passages)
    expect(assignRequest(rows)).toEqual([{ id: 'a', title: 'Whale' }, { id: 'b', title: 'Dock', t_ms: T0 + 120 * min }])
  })
})
