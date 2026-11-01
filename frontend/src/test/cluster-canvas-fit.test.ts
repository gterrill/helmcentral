import { describe, expect, test } from 'vitest'

import { MAX_FIT_SCALE, fitBasisHeight, fitScale, pickDesign } from '@/lib/cluster-canvas'

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

describe('fitBasisHeight', () => {
  test('is the design height scaled down to the width, never up', () => {
    expect(fitBasisHeight({ availW: 260, designW: 520, designH: 228 })).toBeCloseTo(114)
    expect(fitBasisHeight({ availW: 1000, designW: 520, designH: 228 })).toBe(228)
  })
})

describe('pickDesign', () => {
  const tall = { key: 'tall', width: 800, height: 540, stretch: true }
  const wide = { key: 'wide', width: 1000, height: 420, stretch: true }

  test('keeps the first design while it fits at least as well', () => {
    expect(pickDesign([tall, wide], { availW: 500, availH: 400 }).design.key).toBe('tall')
    // Equal scale: the first stays.
    expect(pickDesign([tall, wide], { availW: 0, availH: 0 }).design.key).toBe('tall')
  })

  test('takes the wide design when the box is short and wide', () => {
    const pick = pickDesign([tall, wide], { availW: 743, availH: 310 })
    expect(pick.design.key).toBe('wide')
    expect(pick.scale).toBeCloseTo(0.7381, 3)
  })

  test('stretches a height-limited canvas to the box width, never narrower than its design', () => {
    const pick = pickDesign([wide], { availW: 1200, availH: 210 })
    expect(pick.scale).toBeCloseTo(0.5)
    expect(pick.width).toBeCloseTo(2400)
    expect(pickDesign([wide], { availW: 300, availH: 900 }).width).toBe(1000)
  })

  test('when both designs hit the scale cap, the one that needs less stretch wins', () => {
    const pick = pickDesign([tall, wide], { availW: 4000, availH: 1500 })
    expect(pick.scale).toBe(MAX_FIT_SCALE)
    // tall: 2000/800 = 2.5x its width; wide: 2000/1000 = 2x.
    expect(pick.design.key).toBe('wide')
    expect(pick.width).toBeCloseTo(2000)
  })

  test('a design that does not stretch keeps its own width', () => {
    expect(pickDesign([{ key: 'ring', width: 400, height: 200 }], { availW: 1600, availH: 200 }).width).toBe(400)
  })
})
