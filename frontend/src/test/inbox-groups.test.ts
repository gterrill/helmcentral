import { describe, expect, it } from 'vitest'

import type { InboxItem } from '@/lib/inbox-api'
import { findBlurry, findDuplicates, groupInbox, nearestEdge, placementOptions } from '@/lib/inbox-groups'
import { formatLocalDate, type PassageSummary } from '@/lib/passages'

const T0 = Date.UTC(2026, 9, 14, 0, 0, 0)
const min = 60_000

let seq = 0
function item(over: Partial<InboxItem> = {}): InboxItem {
  seq += 1
  return {
    id: `i${seq}`,
    filename: `IMG_${seq}.jpg`,
    mime: 'image/jpeg',
    size_bytes: 1000,
    capture_ms: T0,
    capture_offset_min: 600,
    bearing_deg: null,
    has_bearing: false,
    fingerprint: '0000000000000000',
    sharpness: 100,
    created_ms: T0 + seq,
    content_url: `/api/inbox/i${seq}/content`,
    group: { kind: 'off_passage' },
    gps_mismatch: false,
    ...over,
  }
}

function passage(id: string, startMs: number, endMs: number, title = ''): PassageSummary {
  return {
    id, start: new Date(startMs).toISOString(), end: new Date(endMs).toISOString(), from_name: 'Airlie', to_name: 'Hook', title,
    notes: '', hidden: false, edited: false, cruise_id: null, created_at: new Date(startMs).toISOString(), timezone: 'Australia/Brisbane',
    stats: {} as PassageSummary['stats'],
  }
}

describe('groupInbox', () => {
  const p1 = passage('p1', T0, T0 + 120 * min)
  const p2 = passage('p2', T0 + 24 * 60 * min, T0 + 26 * 60 * min, 'Whitehaven')

  it('orders groups: underway, passages newest first, off passage, no time', () => {
    const items = [
      item({ id: 'a', group: { kind: 'no_time' }, capture_ms: null }),
      item({ id: 'b', group: { kind: 'passage', passage_id: 'p1' }, capture_ms: T0 + 10 * min }),
      item({ id: 'c', group: { kind: 'off_passage' } }),
      item({ id: 'd', group: { kind: 'underway' } }),
      item({ id: 'e', group: { kind: 'passage', passage_id: 'p2' }, capture_ms: p2.start ? Date.parse(p2.start) + min : 0 }),
    ]
    const groups = groupInbox(items, [p1, p2])
    expect(groups.map((g) => g.key)).toEqual(['underway', 'passage:p2', 'passage:p1', 'off_passage', 'no_time'])
    expect(groups.map((g) => g.label)).toEqual(['Underway now', expect.stringContaining('Whitehaven'), expect.stringContaining('Airlie'), 'Not on a passage', 'No time'])
  })

  it('keeps capture order inside a group and puts items with no time by arrival', () => {
    const items = [
      item({ id: 'late', group: { kind: 'passage', passage_id: 'p1' }, capture_ms: T0 + 90 * min }),
      item({ id: 'early', group: { kind: 'passage', passage_id: 'p1' }, capture_ms: T0 + 5 * min }),
      item({ id: 'n2', group: { kind: 'no_time' }, capture_ms: null, created_ms: 20 }),
      item({ id: 'n1', group: { kind: 'no_time' }, capture_ms: null, created_ms: 10 }),
    ]
    const groups = groupInbox(items, [p1])
    expect(groups[0].items.map((i) => i.id)).toEqual(['early', 'late'])
    expect(groups[1].items.map((i) => i.id)).toEqual(['n1', 'n2'])
  })

  it('only offers assigning for groups that can take signs', () => {
    const items = [
      item({ group: { kind: 'underway' } }),
      item({ group: { kind: 'passage', passage_id: 'p1' } }),
      item({ group: { kind: 'off_passage' } }),
      item({ group: { kind: 'no_time' }, capture_ms: null }),
    ]
    const can = Object.fromEntries(groupInbox(items, [p1]).map((g) => [g.key, g.canAssign]))
    expect(can).toEqual({ underway: false, 'passage:p1': true, off_passage: true, no_time: true })
  })

  it('has no empty groups and no groups for no items', () => {
    expect(groupInbox([], [p1])).toEqual([])
  })

  it('leaves out the end of a passage that has no name, and shows just the date when neither has one', () => {
    const date = formatLocalDate(p1.start, 'Australia/Brisbane')
    const labelOf = (p: PassageSummary) =>
      groupInbox([item({ group: { kind: 'passage', passage_id: p.id } })], [p])[0].label
    expect(labelOf({ ...p1, id: 'p1', from_name: '', to_name: 'Barubbra Island' })).toBe(`Barubbra Island · ${date}`)
    expect(labelOf({ ...p1, id: 'p1', from_name: 'Great Keppel Island', to_name: '' })).toBe(`Great Keppel Island · ${date}`)
    expect(labelOf({ ...p1, id: 'p1', from_name: '', to_name: '' })).toBe(date)
    expect(labelOf({ ...p1, id: 'p1', from_name: '', to_name: '' })).not.toContain('--')
  })

  it('names a passage the list no longer holds rather than dropping its photos', () => {
    const groups = groupInbox([item({ group: { kind: 'passage', passage_id: 'gone' } })], [p1])
    expect(groups).toHaveLength(1)
    expect(groups[0].label).toMatch(/passage/i)
  })
})

describe('findDuplicates', () => {
  it('groups near-identical frames taken close together and keeps the sharpest', () => {
    const a = item({ id: 'a', capture_ms: T0, fingerprint: '00ff00ff00ff00ff', sharpness: 50 })
    const b = item({ id: 'b', capture_ms: T0 + 2000, fingerprint: '00ff00ff00ff00fe', sharpness: 90 })
    const c = item({ id: 'c', capture_ms: T0 + 4000, fingerprint: '00ff00ff00ff00ff', sharpness: 70 })
    const other = item({ id: 'd', capture_ms: T0 + 3000, fingerprint: 'ffffffffffffffff', sharpness: 300 })
    const dup = findDuplicates([a, b, c, other])
    expect(dup.get('b')).toEqual({ keeperId: 'b', isKeeper: true, size: 3 })
    expect(dup.get('a')).toEqual({ keeperId: 'b', isKeeper: false, size: 3 })
    expect(dup.get('c')).toEqual({ keeperId: 'b', isKeeper: false, size: 3 })
    expect(dup.has('d')).toBe(false)
  })

  it('does not link the same scene hours apart', () => {
    const a = item({ id: 'a', capture_ms: T0, fingerprint: '00ff00ff00ff00ff' })
    const b = item({ id: 'b', capture_ms: T0 + 3 * 60 * min, fingerprint: '00ff00ff00ff00ff' })
    expect(findDuplicates([a, b]).size).toBe(0)
  })

  it('links through a chain of frames', () => {
    const a = item({ id: 'a', capture_ms: T0, fingerprint: '0000000000000000' })
    const b = item({ id: 'b', capture_ms: T0 + 40_000, fingerprint: '000000000000003f' })
    const c = item({ id: 'c', capture_ms: T0 + 80_000, fingerprint: '0000000000000fff' })
    expect(findDuplicates([a, b, c]).size).toBe(3)
  })

  it('leaves out PDFs, items with no time and items with no fingerprint', () => {
    const base = { fingerprint: '00ff00ff00ff00ff' }
    const pdf = item({ id: 'p', mime: 'application/pdf', fingerprint: '' })
    const noTime = item({ id: 'n', capture_ms: null, ...base })
    const noPrint = item({ id: 'f', fingerprint: '' })
    const a = item({ id: 'a', ...base })
    expect(findDuplicates([pdf, noTime, noPrint, a]).size).toBe(0)
  })

  it('on equal sharpness keeps the earliest', () => {
    const a = item({ id: 'a', capture_ms: T0, fingerprint: '00ff00ff00ff00ff', sharpness: 80 })
    const b = item({ id: 'b', capture_ms: T0 + 1000, fingerprint: '00ff00ff00ff00ff', sharpness: 80 })
    expect(findDuplicates([b, a]).get('a')?.isKeeper).toBe(true)
  })
})

describe('findBlurry', () => {
  it('flags frames far softer than the batch', () => {
    const items = [100, 120, 110, 105, 8].map((s, i) => item({ id: `s${i}`, sharpness: s }))
    expect([...findBlurry(items)]).toEqual(['s4'])
  })

  it('needs a batch to compare against', () => {
    const items = [100, 5].map((s, i) => item({ id: `s${i}`, sharpness: s }))
    expect(findBlurry(items).size).toBe(0)
  })

  it('ignores PDFs and items with no score', () => {
    const items = [
      ...[100, 120, 110, 105].map((s, i) => item({ id: `s${i}`, sharpness: s })),
      item({ id: 'pdf', mime: 'application/pdf', sharpness: 0, fingerprint: '' }),
      item({ id: 'zero', sharpness: 0 }),
    ]
    expect(findBlurry(items).size).toBe(0)
  })
})

describe('placement', () => {
  const p1 = passage('p1', T0, T0 + 120 * min, 'Out to Hook')
  const p2 = passage('p2', T0 + 600 * min, T0 + 700 * min, 'Back')

  it('finds the nearest end of the nearest passage', () => {
    expect(nearestEdge(T0 + 125 * min, [p1, p2])).toEqual({ passageId: 'p1', edge: 'end', tMs: T0 + 120 * min })
    expect(nearestEdge(T0 + 590 * min, [p1, p2])).toEqual({ passageId: 'p2', edge: 'start', tMs: T0 + 600 * min })
    expect(nearestEdge(T0 - 30 * min, [p1, p2])).toEqual({ passageId: 'p1', edge: 'start', tMs: T0 })
  })

  it('has nothing to offer with no passages', () => {
    expect(nearestEdge(T0, [])).toBeNull()
    expect(placementOptions([])).toEqual([])
  })

  it('lists the start and end of every passage, oldest first', () => {
    const opts = placementOptions([p2, p1])
    expect(opts.map((o) => o.tMs)).toEqual([T0, T0 + 120 * min, T0 + 600 * min, T0 + 700 * min])
    expect(opts[0].label).toMatch(/Start of/)
    expect(opts[1].label).toMatch(/End of/)
  })
})
