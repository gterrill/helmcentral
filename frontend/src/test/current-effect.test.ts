import { describe, expect, test } from 'vitest'

import { classifyCurrentEffect } from '@/lib/current-effect'

describe('classifyCurrentEffect', () => {
  test.each([
    [0.4, 'helping'],
    [0.05, 'helping'],
    [-0.06, 'hindering'],
    [-1.8, 'hindering'],
    [0.04, 'none'],
    [-0.04, 'none'],
    [0, 'none'],
    [null, null],
  ])('%s is %s', (impact, expected) => {
    expect(classifyCurrentEffect(impact)).toBe(expected)
  })
})
