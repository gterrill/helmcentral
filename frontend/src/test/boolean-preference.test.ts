import { beforeEach, describe, expect, it } from 'vitest'

import { createBooleanPreference } from '@/lib/boolean-preference'

beforeEach(() => localStorage.clear())

describe('createBooleanPreference', () => {
  it('uses the default until a choice is saved, then remembers it under its key', () => {
    const on = createBooleanPreference('t.on', true)
    const off = createBooleanPreference('t.off', false)
    expect(on.load()).toBe(true)
    expect(off.load()).toBe(false)
    on.save(false)
    expect(localStorage.getItem('t.on')).toBe('0')
    expect(on.load()).toBe(false)
    expect(off.load()).toBe(false)
  })
})
