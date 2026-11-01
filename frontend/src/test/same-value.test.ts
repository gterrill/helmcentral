import { describe, expect, it } from 'vitest'
import { sameSet, sameShallow } from '@/lib/same-value'

describe('sameShallow', () => {
  it('is true for the same keys holding the same values', () => {
    expect(sameShallow({ a: 1, b: null }, { a: 1, b: null })).toBe(true)
  })

  it('is false when a value differs', () => {
    expect(sameShallow({ a: 1 }, { a: 2 })).toBe(false)
  })

  it('is false when the keys differ even if both read undefined', () => {
    expect(sameShallow({ a: undefined } as Record<string, undefined>, { b: undefined } as Record<string, undefined>)).toBe(false)
  })
})

describe('sameSet', () => {
  it('ignores order', () => {
    expect(sameSet(['a', 'b'], ['b', 'a'])).toBe(true)
  })

  it('ignores repeats', () => {
    expect(sameSet(['a', 'a', 'b'], ['b', 'a'])).toBe(true)
  })

  it('is false when the lists differ in size or entries', () => {
    expect(sameSet(['a'], ['a', 'b'])).toBe(false)
    expect(sameSet(['a', 'c'], ['a', 'b'])).toBe(false)
  })
})
