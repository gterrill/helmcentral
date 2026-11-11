import { act, renderHook } from '@testing-library/react'
import { beforeEach, describe, expect, it } from 'vitest'

import { useLocalStorageId } from '@/hooks/use-local-storage-id'

describe('useLocalStorageId', () => {
  beforeEach(() => localStorage.clear())

  it('remembers the id between mounts', () => {
    const first = renderHook(() => useLocalStorageId('k'))
    expect(first.result.current[0]).toBeNull()
    act(() => first.result.current[1]('f1'))
    expect(first.result.current[0]).toBe('f1')
    first.unmount()
    expect(renderHook(() => useLocalStorageId('k')).result.current[0]).toBe('f1')
  })

  it('works without storage: the id holds for the session and nothing throws', () => {
    const original = Object.getOwnPropertyDescriptor(window, 'localStorage')!
    Object.defineProperty(window, 'localStorage', { get: () => { throw new Error('blocked') }, configurable: true })
    try {
      const { result } = renderHook(() => useLocalStorageId('k', 'deep-link'))
      expect(result.current[0]).toBe('deep-link')
      act(() => result.current[1]('f2'))
      expect(result.current[0]).toBe('f2')
    } finally {
      Object.defineProperty(window, 'localStorage', original)
    }
  })
})
