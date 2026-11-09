import { describe, expect, it } from 'vitest'

import { shallowEqual, shallowEqualArray } from '@/lib/shallow-equal'

describe('shallowEqual', () => {
  it('compares keys and values one level deep', () => {
    expect(shallowEqual({ a: 1, b: 'x' }, { b: 'x', a: 1 })).toBe(true)
    expect(shallowEqual({ a: 1 }, { a: 2 })).toBe(false)
    expect(shallowEqual({ a: 1 }, { a: 1, b: 2 })).toBe(false)
    expect(shallowEqual({ a: 1, b: undefined }, { a: 1, c: undefined })).toBe(false)
    expect(shallowEqual({ a: {} }, { a: {} })).toBe(false)
  })
})

describe('shallowEqualArray', () => {
  it('is true for the same flat objects in the same order', () => {
    expect(shallowEqualArray([{ a: 1 }, { a: 2 }], [{ a: 1 }, { a: 2 }])).toBe(true)
    expect(shallowEqualArray([], [])).toBe(true)
  })
  it('is false when the length, an order or a value differs', () => {
    expect(shallowEqualArray([{ a: 1 }], [])).toBe(false)
    expect(shallowEqualArray([{ a: 1 }, { a: 2 }], [{ a: 2 }, { a: 1 }])).toBe(false)
    expect(shallowEqualArray([{ a: 1 }], [{ a: 3 }])).toBe(false)
  })
})
