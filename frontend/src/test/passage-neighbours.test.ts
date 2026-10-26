import { describe, expect, it } from 'vitest'

import { passageNeighbours, passagesInFilter } from '@/lib/passage-neighbours'
import type { PassageSummary } from '@/lib/passages'

function p(id: string, start: string, cruise: string | null = null): PassageSummary {
  return { id, start, end: start, cruise_id: cruise } as PassageSummary
}

// Newest first, as the list arrives.
const LIST = [
  p('d', '2026-04-04T00:00:00Z', 'c1'),
  p('c', '2026-03-03T00:00:00Z'),
  p('b', '2026-02-02T00:00:00Z', 'c1'),
  p('a', '2026-01-01T00:00:00Z'),
]

describe('passagesInFilter', () => {
  it('keeps everything for all, the unfiled for unfiled, one cruise for a cruise', () => {
    expect(passagesInFilter(LIST, { kind: 'all' }).map((x) => x.id)).toEqual(['d', 'c', 'b', 'a'])
    expect(passagesInFilter(LIST, { kind: 'unfiled' }).map((x) => x.id)).toEqual(['c', 'a'])
    expect(passagesInFilter(LIST, { kind: 'cruise', id: 'c1' }).map((x) => x.id)).toEqual(['d', 'b'])
  })
})

describe('passageNeighbours', () => {
  it('previous is earlier and next is later', () => {
    const n = passageNeighbours(LIST, 'b', { kind: 'all' })
    expect(n.previous?.id).toBe('a')
    expect(n.next?.id).toBe('c')
  })

  it('returns null at each end', () => {
    expect(passageNeighbours(LIST, 'a', { kind: 'all' }).previous).toBeNull()
    expect(passageNeighbours(LIST, 'd', { kind: 'all' }).next).toBeNull()
  })

  it('walks only the passages in the filter', () => {
    const n = passageNeighbours(LIST, 'b', { kind: 'cruise', id: 'c1' })
    expect(n.previous).toBeNull()
    expect(n.next?.id).toBe('d')
    expect(passageNeighbours(LIST, 'c', { kind: 'unfiled' }).previous?.id).toBe('a')
  })

  it('orders by start time whatever order the list arrives in', () => {
    const shuffled = [LIST[2], LIST[0], LIST[3], LIST[1]]
    const n = passageNeighbours(shuffled, 'c', { kind: 'all' })
    expect(n.previous?.id).toBe('b')
    expect(n.next?.id).toBe('d')
  })

  it('has no neighbours when the passage is not in the filter', () => {
    const n = passageNeighbours(LIST, 'a', { kind: 'cruise', id: 'c1' })
    expect(n).toEqual({ previous: null, next: null })
  })
})
