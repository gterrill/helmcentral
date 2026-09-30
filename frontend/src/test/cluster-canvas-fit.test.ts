import { describe, expect, test } from 'vitest'

import { MAX_FIT_SCALE, fitMinHeight, fitScale } from '@/lib/cluster-canvas'

describe('fitScale', () => {
  test('is limited by width when the box is tall', () => {
    expect(fitScale({ availW: 260, availH: 900, designW: 520, designH: 228 })).toBeCloseTo(0.5)
  })

  test('is limited by height when the box is wide', () => {
    expect(fitScale({ availW: 900, availH: 228, designW: 520, designH: 228 })).toBeCloseTo(1)
    expect(fitScale({ availW: 900, availH: 114, designW: 520, designH: 228 })).toBeCloseTo(0.5)
  })

  test('grows past design size but never past the cap', () => {
    expect(fitScale({ availW: 780, availH: 342, designW: 520, designH: 228 })).toBeCloseTo(1.5)
    expect(fitScale({ availW: 5000, availH: 5000, designW: 520, designH: 228 })).toBe(MAX_FIT_SCALE)
  })

  test('with no usable height, falls back to width only and never grows', () => {
    expect(fitScale({ availW: 260, availH: 0, designW: 520, designH: 228 })).toBeCloseTo(0.5)
    expect(fitScale({ availW: 1000, availH: 0, designW: 520, designH: 228 })).toBe(1)
    expect(fitScale({ availW: 1000, availH: undefined, designW: 520, designH: 228 })).toBe(1)
  })

  test('a zero width gives a zero scale rather than NaN', () => {
    expect(fitScale({ availW: 0, availH: 300, designW: 520, designH: 228 })).toBe(0)
  })
})

describe('fitMinHeight', () => {
  test('is the design height scaled down to the width, never up', () => {
    expect(fitMinHeight({ availW: 260, designW: 520, designH: 228 })).toBeCloseTo(114)
    expect(fitMinHeight({ availW: 1000, designW: 520, designH: 228 })).toBe(228)
  })
})
