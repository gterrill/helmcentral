import { describe, it, expect, afterEach, vi } from 'vitest'
import { renderHook, act } from '@testing-library/react'
import { useFullscreen } from '@/hooks/use-fullscreen'

// Fullscreen API feature detection (MDN): `document.fullscreenEnabled` is the
// signal iPhone Safari fails, which is why the hook (and the header button
// gated on `supported`) has to check it rather than assuming every browser
// has the API.

function stubFullscreenEnabled(enabled: boolean | undefined) {
  Object.defineProperty(document, 'fullscreenEnabled', { value: enabled, configurable: true })
}

function stubFullscreenElement(el: Element | null) {
  Object.defineProperty(document, 'fullscreenElement', { value: el, configurable: true })
}

function fireFullscreenChange(el: Element | null) {
  stubFullscreenElement(el)
  document.dispatchEvent(new Event('fullscreenchange'))
}

describe('useFullscreen', () => {
  afterEach(() => {
    vi.restoreAllMocks()
    stubFullscreenEnabled(undefined)
    stubFullscreenElement(null)
    // @ts-expect-error - test-only cleanup of a property we defined ourselves
    delete document.documentElement.requestFullscreen
    // @ts-expect-error - test-only cleanup of a property we defined ourselves
    delete document.exitFullscreen
  })

  it('reports unsupported when document.fullscreenEnabled is not true', () => {
    stubFullscreenEnabled(false)
    const { result } = renderHook(() => useFullscreen())
    expect(result.current.supported).toBe(false)
  })

  it('reports unsupported when document.fullscreenEnabled is undefined (iPhone Safari)', () => {
    stubFullscreenEnabled(undefined)
    const { result } = renderHook(() => useFullscreen())
    expect(result.current.supported).toBe(false)
  })

  it('reports supported when document.fullscreenEnabled is true', () => {
    stubFullscreenEnabled(true)
    const { result } = renderHook(() => useFullscreen())
    expect(result.current.supported).toBe(true)
  })

  it('starts not fullscreen when document.fullscreenElement is null', () => {
    stubFullscreenEnabled(true)
    stubFullscreenElement(null)
    const { result } = renderHook(() => useFullscreen())
    expect(result.current.isFullscreen).toBe(false)
  })

  // Regression: happy-dom/jsdom don't implement the Fullscreen API at all,
  // so `document.fullscreenElement` reads back `undefined`, not `null`,
  // until a test (or, in a real but non-implementing browser, nothing)
  // defines it. `!== null` alone reports that as fullscreen; this is why
  // the hook uses a truthy check instead.
  it('starts not fullscreen when document.fullscreenElement is undefined (property absent, not merely null)', () => {
    stubFullscreenEnabled(true)
    // @ts-expect-error - test-only: simulate the property never having been
    // defined, distinct from stubFullscreenElement(null) above.
    delete document.fullscreenElement
    const { result } = renderHook(() => useFullscreen())
    expect(result.current.isFullscreen).toBe(false)
  })

  it('starts fullscreen when document.fullscreenElement is already set on mount', () => {
    stubFullscreenEnabled(true)
    stubFullscreenElement(document.documentElement)
    const { result } = renderHook(() => useFullscreen())
    expect(result.current.isFullscreen).toBe(true)
  })

  it('enter() requests fullscreen on the whole document element, not a sub-element', () => {
    stubFullscreenEnabled(true)
    const requestFullscreen = vi.fn().mockResolvedValue(undefined)
    document.documentElement.requestFullscreen = requestFullscreen
    const { result } = renderHook(() => useFullscreen())

    act(() => { result.current.enter() })

    expect(requestFullscreen).toHaveBeenCalledTimes(1)
  })

  it('console.errors on enter() rejection instead of swallowing it', async () => {
    stubFullscreenEnabled(true)
    const error = new Error('denied by user agent')
    document.documentElement.requestFullscreen = vi.fn().mockRejectedValue(error)
    const consoleError = vi.spyOn(console, 'error').mockImplementation(() => {})
    const { result } = renderHook(() => useFullscreen())

    await act(async () => { result.current.enter() })

    expect(consoleError).toHaveBeenCalled()
  })

  it('updates isFullscreen to true after fullscreenchange fires with fullscreenElement set', () => {
    stubFullscreenEnabled(true)
    const { result } = renderHook(() => useFullscreen())
    expect(result.current.isFullscreen).toBe(false)

    act(() => { fireFullscreenChange(document.documentElement) })

    expect(result.current.isFullscreen).toBe(true)
  })

  it('updates isFullscreen back to false after fullscreenchange fires with fullscreenElement null (Esc / browser exit)', () => {
    stubFullscreenEnabled(true)
    stubFullscreenElement(document.documentElement)
    const { result } = renderHook(() => useFullscreen())
    expect(result.current.isFullscreen).toBe(true)

    act(() => { fireFullscreenChange(null) })

    expect(result.current.isFullscreen).toBe(false)
  })

  it('exit() calls document.exitFullscreen() when fullscreenElement is set', () => {
    stubFullscreenEnabled(true)
    stubFullscreenElement(document.documentElement)
    const exitFullscreen = vi.fn().mockResolvedValue(undefined)
    document.exitFullscreen = exitFullscreen
    const { result } = renderHook(() => useFullscreen())

    act(() => { result.current.exit() })

    expect(exitFullscreen).toHaveBeenCalledTimes(1)
  })

  it('exit() is a no-op when fullscreenElement is already null', () => {
    stubFullscreenEnabled(true)
    stubFullscreenElement(null)
    const exitFullscreen = vi.fn()
    document.exitFullscreen = exitFullscreen
    const { result } = renderHook(() => useFullscreen())

    act(() => { result.current.exit() })

    expect(exitFullscreen).not.toHaveBeenCalled()
  })

  it('console.errors on exit() rejection instead of swallowing it', async () => {
    stubFullscreenEnabled(true)
    stubFullscreenElement(document.documentElement)
    const error = new Error('exit failed')
    document.exitFullscreen = vi.fn().mockRejectedValue(error)
    const consoleError = vi.spyOn(console, 'error').mockImplementation(() => {})
    const { result } = renderHook(() => useFullscreen())

    await act(async () => { result.current.exit() })

    expect(consoleError).toHaveBeenCalled()
  })

  it('removes the fullscreenchange listener on unmount', () => {
    stubFullscreenEnabled(true)
    const removeSpy = vi.spyOn(document, 'removeEventListener')
    const { unmount } = renderHook(() => useFullscreen())

    unmount()

    expect(removeSpy).toHaveBeenCalledWith('fullscreenchange', expect.any(Function))
  })
})
