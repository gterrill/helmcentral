import { describe, it, expect } from 'vitest'
import { renderHook, act } from '@testing-library/react'
import { useMinWidth, BREAKPOINTS } from '@/lib/breakpoints'
import { useIsMobile } from '@/hooks/use-mobile'
import { setViewportWidth } from './viewport'

describe('useMinWidth', () => {
  it('is true when the viewport is exactly at the threshold (min-width is inclusive)', () => {
    setViewportWidth(1024)

    const { result } = renderHook(() => useMinWidth(1024))

    expect(result.current).toBe(true)
  })

  it('is false one pixel below the threshold', () => {
    setViewportWidth(1023)

    const { result } = renderHook(() => useMinWidth(1024))

    expect(result.current).toBe(false)
  })

  it('reacts to a viewport transition after mount', () => {
    setViewportWidth(1280)

    const { result } = renderHook(() => useMinWidth(BREAKPOINTS.lg))

    expect(result.current).toBe(true)

    act(() => {
      setViewportWidth(375)
    })

    expect(result.current).toBe(false)
  })
})

describe('useIsMobile', () => {
  it('is false at 768 (the md breakpoint, inclusive of desktop)', () => {
    setViewportWidth(768)

    const { result } = renderHook(() => useIsMobile())

    expect(result.current).toBe(false)
  })

  it('is true at 767, one pixel below md', () => {
    setViewportWidth(767)

    const { result } = renderHook(() => useIsMobile())

    expect(result.current).toBe(true)
  })
})
