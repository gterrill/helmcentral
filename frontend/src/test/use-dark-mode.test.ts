import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { renderHook, act } from '@testing-library/react'
import { useDarkMode } from '@/hooks/use-dark-mode'

const DARK_MODE_KEY = 'ui.darkMode'

function stubMatchMedia(prefersDark: boolean) {
  vi.stubGlobal(
    'matchMedia',
    vi.fn().mockImplementation((query: string) => ({
      matches: prefersDark && query === '(prefers-color-scheme: dark)',
      media: query,
    })),
  )
}

describe('useDarkMode', () => {
  beforeEach(() => {
    localStorage.clear()
    document.documentElement.classList.remove('dark')
    stubMatchMedia(false)
  })

  afterEach(() => {
    document.documentElement.classList.remove('dark')
    vi.unstubAllGlobals()
  })

  it('defaults to light mode when localStorage is empty and system prefers light', () => {
    const { result } = renderHook(() => useDarkMode())

    expect(result.current[0]).toBe(false)
    expect(document.documentElement.classList.contains('dark')).toBe(false)
  })

  it('defaults to dark mode when localStorage is empty and system prefers dark', () => {
    stubMatchMedia(true)

    const { result } = renderHook(() => useDarkMode())

    expect(result.current[0]).toBe(true)
    expect(document.documentElement.classList.contains('dark')).toBe(true)
  })

  it('restores dark=true from localStorage on init', () => {
    localStorage.setItem(DARK_MODE_KEY, 'true')

    const { result } = renderHook(() => useDarkMode())

    expect(result.current[0]).toBe(true)
    expect(document.documentElement.classList.contains('dark')).toBe(true)
  })

  it('restores dark=false from localStorage even when system prefers dark', () => {
    localStorage.setItem(DARK_MODE_KEY, 'false')
    stubMatchMedia(true)

    const { result } = renderHook(() => useDarkMode())

    expect(result.current[0]).toBe(false)
    expect(document.documentElement.classList.contains('dark')).toBe(false)
  })

  it('toggle adds dark class to <html> and persists to localStorage', () => {
    const { result } = renderHook(() => useDarkMode())

    act(() => { result.current[1]() })

    expect(result.current[0]).toBe(true)
    expect(document.documentElement.classList.contains('dark')).toBe(true)
    expect(localStorage.getItem(DARK_MODE_KEY)).toBe('true')
  })

  it('toggle removes dark class from <html> and persists to localStorage', () => {
    localStorage.setItem(DARK_MODE_KEY, 'true')

    const { result } = renderHook(() => useDarkMode())
    expect(result.current[0]).toBe(true)

    act(() => { result.current[1]() })

    expect(result.current[0]).toBe(false)
    expect(document.documentElement.classList.contains('dark')).toBe(false)
    expect(localStorage.getItem(DARK_MODE_KEY)).toBe('false')
  })

  it('toggling twice returns to the original state', () => {
    const { result } = renderHook(() => useDarkMode())

    act(() => { result.current[1]() })
    act(() => { result.current[1]() })

    expect(result.current[0]).toBe(false)
    expect(document.documentElement.classList.contains('dark')).toBe(false)
  })
})
