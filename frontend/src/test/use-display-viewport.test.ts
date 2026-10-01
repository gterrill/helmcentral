import { act, renderHook } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, test, vi } from 'vitest'
import { useDisplayViewport, VIEWPORT_REPORT_DEBOUNCE_MS } from '@/hooks/use-display-viewport'

function setViewport(w: number, h: number) {
  Object.defineProperty(window, 'innerWidth', { value: w, configurable: true })
  Object.defineProperty(window, 'innerHeight', { value: h, configurable: true })
}

describe('useDisplayViewport', () => {
  const fetchMock = vi.fn()

  beforeEach(() => {
    vi.useFakeTimers()
    fetchMock.mockReset()
    fetchMock.mockResolvedValue({ ok: true })
    vi.stubGlobal('fetch', fetchMock)
    setViewport(1536, 856)
  })
  afterEach(() => {
    vi.useRealTimers()
    vi.unstubAllGlobals()
  })

  test('reports the window size on mount', () => {
    renderHook(() => useDisplayViewport('d1'))
    expect(fetchMock).toHaveBeenCalledTimes(1)
    const [url, init] = fetchMock.mock.calls[0]
    expect(url).toBe('/api/displays/d1/viewport')
    expect(init.method).toBe('PUT')
    expect(JSON.parse(init.body)).toMatchObject({ w: 1536, h: 856 })
  })

  test('debounces resizes into one report and returns the live size', () => {
    const { result } = renderHook(() => useDisplayViewport('d1'))
    fetchMock.mockClear()
    act(() => { setViewport(1280, 720); window.dispatchEvent(new Event('resize')) })
    act(() => { setViewport(1000, 700); window.dispatchEvent(new Event('resize')) })
    expect(fetchMock).not.toHaveBeenCalled()
    expect(result.current).toEqual({ w: 1000, h: 700 })
    act(() => { vi.advanceTimersByTime(VIEWPORT_REPORT_DEBOUNCE_MS) })
    expect(fetchMock).toHaveBeenCalledTimes(1)
    expect(JSON.parse(fetchMock.mock.calls[0][1].body)).toMatchObject({ w: 1000, h: 700 })
  })

  test('does not report an unchanged size', () => {
    renderHook(() => useDisplayViewport('d1'))
    fetchMock.mockClear()
    act(() => { window.dispatchEvent(new Event('resize')); vi.advanceTimersByTime(VIEWPORT_REPORT_DEBOUNCE_MS) })
    expect(fetchMock).not.toHaveBeenCalled()
  })

  test('logs a failed report instead of hiding it', async () => {
    const err = vi.spyOn(console, 'error').mockImplementation(() => {})
    fetchMock.mockResolvedValue({ ok: false, status: 403 })
    renderHook(() => useDisplayViewport('d1'))
    await act(async () => { await Promise.resolve(); await Promise.resolve() })
    expect(err).toHaveBeenCalled()
    err.mockRestore()
  })
})
