import { describe, expect, it } from 'vitest'

import { formatListSort, neighbours, orderRows, parseListSort, patchList } from '@/lib/list-order'

describe('parseListSort / formatListSort', () => {
  it('reads a column id as ascending and a leading dash as descending', () => {
    expect(parseListSort('name')).toEqual({ id: 'name', desc: false })
    expect(parseListSort('-added')).toEqual({ id: 'added', desc: true })
  })

  it('reads nothing usable as no sort', () => {
    expect(parseListSort(null)).toBeNull()
    expect(parseListSort('')).toBeNull()
    expect(parseListSort('-')).toBeNull()
    expect(parseListSort('na me')).toBeNull()
  })

  it('round-trips', () => {
    for (const raw of ['name', '-name', 'last-run', '-size']) {
      expect(formatListSort(parseListSort(raw)!)).toBe(raw)
    }
  })
})

interface Row { id: string; name: string; n?: number }
const ACCESSORS = { name: (r: Row) => r.name, n: (r: Row) => r.n }
const ROWS: Row[] = [
  { id: 'a', name: 'pump 10', n: 3 },
  { id: 'b', name: 'Pump 2', n: undefined },
  { id: 'c', name: 'anchor', n: 1 },
  { id: 'd', name: 'Anchor', n: 1 },
]

describe('orderRows', () => {
  it('keeps the given order with no sort, and never mutates the input', () => {
    const copy = [...ROWS]
    expect(orderRows(ROWS, null, ACCESSORS).map((r) => r.id)).toEqual(['a', 'b', 'c', 'd'])
    expect(ROWS).toEqual(copy)
  })

  it('sorts text without regard to case, with numbers in their natural order', () => {
    expect(orderRows(ROWS, { id: 'name', desc: false }, ACCESSORS).map((r) => r.id)).toEqual(['c', 'd', 'b', 'a'])
    expect(orderRows(ROWS, { id: 'name', desc: true }, ACCESSORS).map((r) => r.id)).toEqual(['a', 'b', 'c', 'd'])
  })

  it('leaves equal values in their given order, both ways', () => {
    expect(orderRows(ROWS, { id: 'n', desc: false }, ACCESSORS).map((r) => r.id)).toEqual(['c', 'd', 'a', 'b'])
    expect(orderRows(ROWS, { id: 'n', desc: true }, ACCESSORS).map((r) => r.id)).toEqual(['a', 'c', 'd', 'b'])
  })

  it('treats a column it does not know as no sort', () => {
    expect(orderRows(ROWS, { id: 'nope', desc: false }, ACCESSORS).map((r) => r.id)).toEqual(['a', 'b', 'c', 'd'])
  })
})

describe('neighbours', () => {
  const list = [{ id: 'a' }, { id: 'b' }, { id: 'c' }]

  it('gives the record either side', () => {
    expect(neighbours(list, 'b')).toEqual({ previous: { id: 'a' }, next: { id: 'c' } })
  })

  it('gives null at each end', () => {
    expect(neighbours(list, 'a')).toEqual({ previous: null, next: { id: 'b' } })
    expect(neighbours(list, 'c')).toEqual({ previous: { id: 'b' }, next: null })
  })

  it('gives null both ways when the record is not in the list', () => {
    expect(neighbours(list, 'z')).toEqual({ previous: null, next: null })
    expect(neighbours([], 'a')).toEqual({ previous: null, next: null })
  })

  it('finds a record by a key other than id', () => {
    expect(neighbours([{ code: 'x' }, { code: 'y' }], 'x', (r) => r.code).next).toEqual({ code: 'y' })
  })
})

describe('patchList', () => {
  it('sets a field and leaves the rest', () => {
    expect(patchList({ q: 'a' } as { q?: string; zone?: string }, { zone: 'z1' })).toEqual({ q: 'a', zone: 'z1' })
  })

  it('removes a field patched to nothing, so a default state is an empty object', () => {
    expect(patchList({ q: 'a', zone: 'z1' } as { q?: string; zone?: string }, { q: '', zone: undefined })).toEqual({})
  })
})
