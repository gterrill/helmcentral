import { describe, expect, it } from 'vitest'
import { isPlainLeftClick } from '@/lib/plain-click'

const click = (over: Partial<Parameters<typeof isPlainLeftClick>[0]> = {}) =>
  ({ button: 0, metaKey: false, ctrlKey: false, shiftKey: false, altKey: false, ...over })

describe('isPlainLeftClick', () => {
  it('is true for a left click with no modifier', () => {
    expect(isPlainLeftClick(click())).toBe(true)
  })

  it('is false for a middle or right button', () => {
    expect(isPlainLeftClick(click({ button: 1 }))).toBe(false)
    expect(isPlainLeftClick(click({ button: 2 }))).toBe(false)
  })

  it.each(['metaKey', 'ctrlKey', 'shiftKey', 'altKey'] as const)('is false with %s held', (key) => {
    expect(isPlainLeftClick(click({ [key]: true }))).toBe(false)
  })
})
