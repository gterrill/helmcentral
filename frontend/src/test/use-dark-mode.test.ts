import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { renderHook, act } from '@testing-library/react'
import { useDarkMode } from '@/hooks/use-dark-mode'

const DARK_MODE_KEY = 'ui.darkMode'
const AUTO_LAST_KEY = 'ui.darkMode.autoLast'

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

  it('cycling from night then on to day removes the dark class and persists false', () => {
    localStorage.setItem(DARK_MODE_KEY, 'true')

    const { result } = renderHook(() => useDarkMode())
    expect(result.current[0]).toBe(true)

    act(() => { result.current[1]() }) // night -> auto, no sun data so it holds night
    expect(result.current[0]).toBe(true)
    act(() => { result.current[1]() }) // auto -> day

    expect(result.current[0]).toBe(false)
    expect(document.documentElement.classList.contains('dark')).toBe(false)
    expect(localStorage.getItem(DARK_MODE_KEY)).toBe('false')
  })

  it('three presses return to the original state', () => {
    const { result } = renderHook(() => useDarkMode())

    act(() => { result.current[1]() })
    act(() => { result.current[1]() })
    act(() => { result.current[1]() })

    expect(result.current[0]).toBe(false)
    expect(document.documentElement.classList.contains('dark')).toBe(false)
  })

  // ── Auto ─────────────────────────────────────────────────────────────────

  const TZ = 'Australia/Brisbane'
  const sun = { sunriseTime: '6:12AM', sunsetTime: '5:48PM', timeZone: TZ }
  // Brisbane local hh:mm as a UTC instant (boat is UTC+10).
  const at = (hh: number, mm: number) => new Date(Date.UTC(2026, 9, 2, hh - 10, mm))

  describe('auto mode', () => {
    beforeEach(() => { vi.useFakeTimers() })
    afterEach(() => { vi.useRealTimers() })

    it('cycles Day, Night, Auto, Day and persists each value', () => {
      vi.setSystemTime(at(12, 0))
      localStorage.setItem(DARK_MODE_KEY, 'false')
      const { result } = renderHook(() => useDarkMode(sun))
      expect(result.current[2].mode).toBe('day')

      act(() => { result.current[1]() })
      expect(result.current[2].mode).toBe('night')
      expect(localStorage.getItem(DARK_MODE_KEY)).toBe('true')

      act(() => { result.current[1]() })
      expect(result.current[2].mode).toBe('auto')
      expect(localStorage.getItem(DARK_MODE_KEY)).toBe('auto')

      act(() => { result.current[1]() })
      expect(result.current[2].mode).toBe('day')
      expect(localStorage.getItem(DARK_MODE_KEY)).toBe('false')
    })

    it('from an unset key cycles on from whatever the system preference showed', () => {
      stubMatchMedia(true)
      const { result } = renderHook(() => useDarkMode(sun))
      expect(result.current[0]).toBe(true)
      act(() => { result.current[1]() })
      expect(result.current[2].mode).toBe('auto')
    })

    it('restores auto from localStorage and resolves night before sunrise', () => {
      vi.setSystemTime(at(5, 0))
      localStorage.setItem(DARK_MODE_KEY, 'auto')
      const { result } = renderHook(() => useDarkMode(sun))
      expect(result.current[0]).toBe(true)
      expect(result.current[2]).toEqual({ mode: 'auto', autoUntil: '6:12 AM' })
      expect(document.documentElement.classList.contains('dark')).toBe(true)
    })

    it('resolves day between sunrise and sunset', () => {
      vi.setSystemTime(at(12, 0))
      localStorage.setItem(DARK_MODE_KEY, 'auto')
      const { result } = renderHook(() => useDarkMode(sun))
      expect(result.current[0]).toBe(false)
      expect(result.current[2].autoUntil).toBe('5:48 PM')
    })

    it('flips on its own when sunset passes while open', () => {
      vi.setSystemTime(at(17, 47))
      localStorage.setItem(DARK_MODE_KEY, 'auto')
      const { result } = renderHook(() => useDarkMode(sun))
      expect(result.current[0]).toBe(false)

      act(() => { vi.advanceTimersByTime(60_000) })
      expect(result.current[0]).toBe(true)
    })

    it('writes the resolved theme to storage so a reload can keep it', () => {
      vi.setSystemTime(at(22, 0))
      localStorage.setItem(DARK_MODE_KEY, 'auto')
      renderHook(() => useDarkMode(sun))
      expect(localStorage.getItem(AUTO_LAST_KEY)).toBe('true')

      vi.setSystemTime(at(12, 0))
      localStorage.setItem(DARK_MODE_KEY, 'auto')
      renderHook(() => useDarkMode(sun))
      expect(localStorage.getItem(AUTO_LAST_KEY)).toBe('false')
    })

    it('reloads in Auto with no sun times and keeps the last resolved night over a light system', () => {
      localStorage.setItem(DARK_MODE_KEY, 'auto')
      localStorage.setItem(AUTO_LAST_KEY, 'true')
      stubMatchMedia(false)
      const { result } = renderHook(() => useDarkMode({ sunriseTime: null, sunsetTime: null, timeZone: TZ }))
      expect(result.current[0]).toBe(true)
      expect(result.current[2].autoUntil).toBeNull()
      expect(document.documentElement.classList.contains('dark')).toBe(true)
    })

    it('reloads in Auto with last resolved day over a dark system', () => {
      localStorage.setItem(DARK_MODE_KEY, 'auto')
      localStorage.setItem(AUTO_LAST_KEY, 'false')
      stubMatchMedia(true)
      const { result } = renderHook(() => useDarkMode())
      expect(result.current[0]).toBe(false)
    })

    it('falls back to the system preference only when Auto has never resolved', () => {
      localStorage.setItem(DARK_MODE_KEY, 'auto')
      stubMatchMedia(true)
      const { result } = renderHook(() => useDarkMode())
      expect(result.current[0]).toBe(true)
    })

    it('keeps the current theme and reports it cannot decide when the times are missing', () => {
      vi.setSystemTime(at(12, 0))
      localStorage.setItem(DARK_MODE_KEY, 'true')
      const { result, rerender } = renderHook((p) => useDarkMode(p), {
        initialProps: { ...sun, sunriseTime: null, sunsetTime: null } as typeof sun | { sunriseTime: null; sunsetTime: null; timeZone: string },
      })
      expect(result.current[0]).toBe(true)

      act(() => { result.current[1]() }) // night -> auto
      expect(result.current[2]).toEqual({ mode: 'auto', autoUntil: null })
      expect(result.current[0]).toBe(true)

      // Data arrives: Auto now decides (noon is day).
      rerender(sun)
      expect(result.current[0]).toBe(false)
      expect(result.current[2].autoUntil).toBe('5:48 PM')

      // And if it goes away again, the last decision holds.
      rerender({ sunriseTime: null, sunsetTime: null, timeZone: TZ })
      expect(result.current[0]).toBe(false)
      expect(result.current[2].autoUntil).toBeNull()
    })
  })
})
