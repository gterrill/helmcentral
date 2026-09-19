/**
 * ADR 0110 §5b. The key table here is a GUESS pending the Phase 0 probe run
 * on the actual C5 hardware (docs/adr/0110 §7) — what a Magic Remote emits
 * in webOS's browser isn't knowable from a dev machine. Once that capture
 * exists, only DISPLAY_REMOTE_KEYS (use-display-remote.ts) and this file's
 * expectations should need to change; the hook's own dispatch logic (match
 * key/code/keyCode against the table, preventDefault, call the matching
 * action) shouldn't.
 */
import { describe, it, expect, vi, afterEach } from 'vitest'
import { renderHook } from '@testing-library/react'
import { useDisplayRemote } from '@/hooks/use-display-remote'

function fire(init: Partial<KeyboardEventInit> & { keyCode?: number }): KeyboardEvent {
  const event = new KeyboardEvent('keydown', { key: init.key, code: init.code, cancelable: true })
  if (init.keyCode !== undefined) {
    Object.defineProperty(event, 'keyCode', { value: init.keyCode })
  }
  window.dispatchEvent(event)
  return event
}

describe('useDisplayRemote', () => {
  afterEach(() => {
    vi.restoreAllMocks()
  })

  function actions() {
    return {
      next: vi.fn(),
      previous: vi.fn(),
      togglePause: vi.fn(),
      resume: vi.fn(),
    }
  }

  it('calls next() on ArrowRight, PageDown and MediaTrackNext, preventing default', () => {
    const a = actions()
    renderHook(() => useDisplayRemote(a))

    for (const key of ['ArrowRight', 'PageDown', 'MediaTrackNext']) {
      a.next.mockClear()
      const event = fire({ key })
      expect(a.next).toHaveBeenCalledTimes(1)
      expect(event.defaultPrevented).toBe(true)
    }
  })

  it('calls previous() on ArrowLeft, PageUp and MediaTrackPrevious, preventing default', () => {
    const a = actions()
    renderHook(() => useDisplayRemote(a))

    for (const key of ['ArrowLeft', 'PageUp', 'MediaTrackPrevious']) {
      a.previous.mockClear()
      const event = fire({ key })
      expect(a.previous).toHaveBeenCalledTimes(1)
      expect(event.defaultPrevented).toBe(true)
    }
  })

  it('calls togglePause() on Enter, Space and MediaPlayPause, preventing default (so Space never scrolls)', () => {
    const a = actions()
    renderHook(() => useDisplayRemote(a))

    for (const key of ['Enter', ' ', 'MediaPlayPause']) {
      a.togglePause.mockClear()
      const event = fire({ key, code: key === ' ' ? 'Space' : undefined })
      expect(a.togglePause).toHaveBeenCalledTimes(1)
      expect(event.defaultPrevented).toBe(true)
    }
  })

  it('calls resume() on Escape and the webOS Back key (keyCode 461), preventing default', () => {
    const a = actions()
    renderHook(() => useDisplayRemote(a))

    a.resume.mockClear()
    let event = fire({ key: 'Escape' })
    expect(a.resume).toHaveBeenCalledTimes(1)
    expect(event.defaultPrevented).toBe(true)

    a.resume.mockClear()
    // webOS's Back key reports a non-standard `key` value in practice; the
    // hook must not rely on `key` alone for this one and has to fall back to
    // the raw keyCode alias.
    event = fire({ key: 'Unidentified', keyCode: 461 })
    expect(a.resume).toHaveBeenCalledTimes(1)
    expect(event.defaultPrevented).toBe(true)
  })

  it('does nothing, and does not preventDefault, on an unhandled key', () => {
    const a = actions()
    renderHook(() => useDisplayRemote(a))

    const event = fire({ key: 'a' })
    expect(a.next).not.toHaveBeenCalled()
    expect(a.previous).not.toHaveBeenCalled()
    expect(a.togglePause).not.toHaveBeenCalled()
    expect(a.resume).not.toHaveBeenCalled()
    expect(event.defaultPrevented).toBe(false)
  })

  it('is always on - no enabled flag to pass', () => {
    // Arrow keys and Space do nothing on a wall today and the ODROID has no
    // input device at all, so there is nothing to gate: the hook's options
    // type carries only the four action callbacks.
    const a = actions()
    expect(() => renderHook(() => useDisplayRemote(a))).not.toThrow()
  })

  it('removes its listener on unmount', () => {
    const a = actions()
    const { unmount } = renderHook(() => useDisplayRemote(a))
    unmount()

    fire({ key: 'ArrowRight' })
    expect(a.next).not.toHaveBeenCalled()
  })
})
