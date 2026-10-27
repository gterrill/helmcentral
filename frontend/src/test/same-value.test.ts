import { describe, expect, it } from 'vitest'
import { sameShallow } from '@/lib/same-value'

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
