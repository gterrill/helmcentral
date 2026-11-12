import type { KeyboardEvent } from 'react'
import { describe, expect, it } from 'vitest'
import { isComposing, isSubmitEnter } from '@/lib/keyboard'

function keyEvent(key: string, opts: { isComposing?: boolean, keyCode?: number } = {}) {
  return { key, keyCode: opts.keyCode ?? 0, nativeEvent: { isComposing: opts.isComposing } } as unknown as KeyboardEvent
}

describe('isComposing', () => {
  it('is true while the IME is composing, whatever the key', () => {
    expect(isComposing(keyEvent('Escape', { isComposing: true }))).toBe(true)
    expect(isComposing(keyEvent('ArrowDown', { isComposing: true }))).toBe(true)
  })

  it('is true for keyCode 229 even when isComposing is false', () => {
    expect(isComposing(keyEvent('Enter', { isComposing: false, keyCode: 229 }))).toBe(true)
  })

  it('is false for an ordinary key, including when isComposing is undefined', () => {
    expect(isComposing(keyEvent('Escape', { keyCode: 27 }))).toBe(false)
    expect(isComposing(keyEvent('a'))).toBe(false)
  })
})

describe('isSubmitEnter', () => {
  it('accepts a plain Enter', () => {
    expect(isSubmitEnter(keyEvent('Enter', { keyCode: 13 }))).toBe(true)
  })

  it('ignores the Enter that picks an IME candidate', () => {
    expect(isSubmitEnter(keyEvent('Enter', { isComposing: true }))).toBe(false)
  })

  it('ignores other keys', () => {
    expect(isSubmitEnter(keyEvent('a'))).toBe(false)
    expect(isSubmitEnter(keyEvent('Escape'))).toBe(false)
  })
})
