import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { act, renderHook } from '@testing-library/react'

import {
  VERSION_ATTENTION_INTERVAL_MS,
  VERSION_CHECK_INTERVAL_MS,
  useVersionReload,
  type VersionReloadOptions,
} from '@/hooks/use-version-reload'

function health(version: string, revision: string) {
  return { ok: true, status: 200, json: async () => ({ version, revision }) }
}

describe('useVersionReload', () => {
  let clock = 0
  const now = () => clock
  const reload = vi.fn()

  beforeEach(() => {
    clock = 1_000_000
    reload.mockReset()
    vi.spyOn(console, 'warn').mockImplementation(() => {})
    vi.spyOn(console, 'error').mockImplementation(() => {})
  })
  afterEach(() => {
    vi.unstubAllGlobals()
    vi.restoreAllMocks()
  })

  const flush = async () => { for (let i = 0; i < 10; i++) await Promise.resolve() }
  const settle = () => act(flush)
  const focus = () => act(async () => { window.dispatchEvent(new Event('focus')); await flush() })
  const becomeVisible = () => act(async () => { document.dispatchEvent(new Event('visibilitychange')); await flush() })
  const mount = (options: VersionReloadOptions = {}) => renderHook(() => useVersionReload({ now, reload, ...options }))

  it('takes the first successful read as the build this page is running', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(health('v1', 'aaa')))
    const { result } = mount()
    await settle()
    expect(result.current.current).toEqual({ version: 'v1', revision: 'aaa' })
    expect(result.current.available).toBeNull()
  })

  it('on focus checks again once a minute has passed, and not before', async () => {
    const fetchMock = vi.fn().mockResolvedValue(health('v1', 'aaa'))
    vi.stubGlobal('fetch', fetchMock)
    mount()
    await settle()
    expect(fetchMock).toHaveBeenCalledTimes(1)

    clock += VERSION_ATTENTION_INTERVAL_MS - 1
    await focus()
    await becomeVisible()
    expect(fetchMock).toHaveBeenCalledTimes(1)

    clock += 1
    await focus()
    expect(fetchMock).toHaveBeenCalledTimes(2)
    await becomeVisible()
    expect(fetchMock).toHaveBeenCalledTimes(2)
  })

  it('does not wait an hour: a page shown for ten minutes has already been checked', async () => {
    const fetchMock = vi.fn().mockResolvedValue(health('v1', 'aaa'))
    vi.stubGlobal('fetch', fetchMock)
    mount()
    await settle()
    clock += 10 * 60 * 1000
    await becomeVisible()
    expect(fetchMock).toHaveBeenCalledTimes(2)
  })

  it('reports a changed revision or version as available and never reloads an attended screen', async () => {
    const fetchMock = vi.fn().mockResolvedValueOnce(health('v1', 'aaa')).mockResolvedValueOnce(health('v1', 'bbb')).mockResolvedValueOnce(health('v2', 'bbb'))
    vi.stubGlobal('fetch', fetchMock)
    const { result } = mount({ hasUnsavedWork: () => false, unattended: () => false })
    await settle()

    clock += VERSION_ATTENTION_INTERVAL_MS
    await focus()
    expect(result.current.available).toEqual({ version: 'v1', revision: 'bbb' })

    clock += VERSION_ATTENTION_INTERVAL_MS
    await focus()
    expect(result.current.available).toEqual({ version: 'v2', revision: 'bbb' })
    expect(reload).not.toHaveBeenCalled()
  })

  it('an unchanged build is not an update', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(health('v1', 'aaa')))
    const { result } = mount()
    await settle()
    clock += VERSION_ATTENTION_INTERVAL_MS
    await focus()
    expect(result.current.available).toBeNull()
    expect(reload).not.toHaveBeenCalled()
  })

  it('a failed, non-ok or version-less read is logged and never counts as an update', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(health('v1', 'aaa'))
      .mockRejectedValueOnce(new Error('offline'))
      .mockResolvedValueOnce({ ok: false, status: 503, json: async () => ({}) })
      .mockResolvedValueOnce({ ok: true, status: 200, json: async () => ({}) })
    vi.stubGlobal('fetch', fetchMock)
    const { result } = mount({ unattended: () => true })
    await settle()
    for (let i = 0; i < 3; i++) {
      clock += VERSION_ATTENTION_INTERVAL_MS
      await focus()
    }
    expect(fetchMock).toHaveBeenCalledTimes(4)
    expect(result.current.available).toBeNull()
    expect(reload).not.toHaveBeenCalled()
    expect(console.warn).toHaveBeenCalledTimes(3)
  })

  it('a failed first read leaves the next successful one as the baseline, not an update', async () => {
    const fetchMock = vi.fn().mockRejectedValueOnce(new Error('offline')).mockResolvedValueOnce(health('v2', 'bbb'))
    vi.stubGlobal('fetch', fetchMock)
    const { result } = mount({ unattended: () => true })
    await settle()
    expect(result.current.current).toBeNull()

    clock += VERSION_ATTENTION_INTERVAL_MS
    await focus()
    expect(result.current.current).toEqual({ version: 'v2', revision: 'bbb' })
    expect(result.current.available).toBeNull()
    expect(reload).not.toHaveBeenCalled()
  })

  it('an unattended screen reloads as soon as an update is seen', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValueOnce(health('v1', 'aaa')).mockResolvedValueOnce(health('v1', 'bbb')))
    mount({ unattended: () => true, hasUnsavedWork: () => false })
    await settle()
    clock += VERSION_ATTENTION_INTERVAL_MS
    await focus()
    expect(reload).toHaveBeenCalledTimes(1)
  })

  it('an unattended screen holds the reload while work is unsaved, and goes on the next check once it is not', async () => {
    const fetchMock = vi.fn().mockResolvedValueOnce(health('v1', 'aaa')).mockResolvedValue(health('v2', 'bbb'))
    vi.stubGlobal('fetch', fetchMock)
    let unsaved = true
    mount({ unattended: () => true, hasUnsavedWork: () => unsaved })
    await settle()
    clock += VERSION_ATTENTION_INTERVAL_MS
    await focus()
    expect(reload).not.toHaveBeenCalled()

    unsaved = false
    clock += VERSION_ATTENTION_INTERVAL_MS
    await focus()
    expect(reload).toHaveBeenCalledTimes(1)
  })

  describe('five-minute timer', () => {
    beforeEach(() => vi.useFakeTimers())
    afterEach(() => vi.useRealTimers())

    const tick = (ms: number) =>
      act(async () => {
        clock += ms
        await vi.advanceTimersByTimeAsync(ms)
      })

    it('checks on the timer while the page is visible, with no focus events', async () => {
      const fetchMock = vi.fn().mockResolvedValueOnce(health('v1', 'aaa')).mockResolvedValueOnce(health('v1', 'bbb'))
      vi.stubGlobal('fetch', fetchMock)
      const { result } = mount()
      await tick(0)
      expect(fetchMock).toHaveBeenCalledTimes(1)

      await tick(VERSION_CHECK_INTERVAL_MS)
      expect(fetchMock).toHaveBeenCalledTimes(2)
      expect(result.current.available).toEqual({ version: 'v1', revision: 'bbb' })
    })

    it('does not check on the timer while the page is hidden', async () => {
      const fetchMock = vi.fn().mockResolvedValue(health('v1', 'aaa'))
      vi.stubGlobal('fetch', fetchMock)
      mount()
      await tick(0)
      const spy = vi.spyOn(document, 'visibilityState', 'get').mockReturnValue('hidden')
      await tick(VERSION_CHECK_INTERVAL_MS)
      expect(fetchMock).toHaveBeenCalledTimes(1)
      spy.mockRestore()
    })

    it('stops the timer on unmount', async () => {
      const fetchMock = vi.fn().mockResolvedValue(health('v1', 'aaa'))
      vi.stubGlobal('fetch', fetchMock)
      const { unmount } = mount()
      await tick(0)
      unmount()
      await tick(VERSION_CHECK_INTERVAL_MS * 2)
      expect(fetchMock).toHaveBeenCalledTimes(1)
    })
  })

  describe('the build stamped into the bundle', () => {
    const stamped = { version: 'v1', revision: 'aaa' }

    it('is the running build from the start, without waiting for a health read', () => {
      vi.stubGlobal('fetch', vi.fn(() => new Promise(() => {})))
      const { result } = mount({ bundle: stamped })
      expect(result.current.current).toEqual(stamped)
    })

    it('is the baseline: a first read that differs is already an update', async () => {
      vi.stubGlobal('fetch', vi.fn().mockResolvedValue(health('v2', 'bbb')))
      const { result } = mount({ bundle: stamped })
      await settle()
      expect(result.current.current).toEqual(stamped)
      expect(result.current.available).toEqual({ version: 'v2', revision: 'bbb' })
    })

    it('a first read that matches is no update', async () => {
      vi.stubGlobal('fetch', vi.fn().mockResolvedValue(health('v1', 'aaa')))
      const { result } = mount({ bundle: stamped })
      await settle()
      expect(result.current.available).toBeNull()
    })

    it('an unstamped (dev) bundle falls back to the first read as the baseline', async () => {
      vi.stubGlobal('fetch', vi.fn().mockResolvedValue(health('v9', 'zzz')))
      const { result } = mount({ bundle: { version: 'dev', revision: 'unknown' } })
      expect(result.current.current).toBeNull()
      await settle()
      expect(result.current.current).toEqual({ version: 'v9', revision: 'zzz' })
      expect(result.current.available).toBeNull()
    })

    it('reports a failed read while there is no build to show', async () => {
      vi.stubGlobal('fetch', vi.fn().mockRejectedValue(new Error('offline')))
      const { result } = mount({ bundle: { version: 'dev', revision: 'unknown' } })
      await settle()
      expect(result.current.readError).toBe('offline')
    })
  })

  describe('a held reload on an unattended screen', () => {
    it('is retried on focus without another health read, once the throttle would have blocked it', async () => {
      const fetchMock = vi.fn().mockResolvedValueOnce(health('v1', 'aaa')).mockResolvedValue(health('v2', 'bbb'))
      vi.stubGlobal('fetch', fetchMock)
      let unsaved = true
      mount({ unattended: () => true, hasUnsavedWork: () => unsaved })
      await settle()
      clock += VERSION_ATTENTION_INTERVAL_MS
      await focus()
      expect(reload).not.toHaveBeenCalled()
      expect(fetchMock).toHaveBeenCalledTimes(2)

      unsaved = false
      clock += 1000
      await focus()
      expect(reload).toHaveBeenCalledTimes(1)
      expect(fetchMock).toHaveBeenCalledTimes(2)
    })

    it('is retried when asked, as unsaved work clears', async () => {
      vi.stubGlobal('fetch', vi.fn().mockResolvedValueOnce(health('v1', 'aaa')).mockResolvedValue(health('v2', 'bbb')))
      let unsaved = true
      const { result } = mount({ unattended: () => true, hasUnsavedWork: () => unsaved })
      await settle()
      clock += VERSION_ATTENTION_INTERVAL_MS
      await focus()
      expect(reload).not.toHaveBeenCalled()

      unsaved = false
      act(() => result.current.retryHeldReload())
      expect(reload).toHaveBeenCalledTimes(1)
    })

    it('does nothing when no update is known', async () => {
      vi.stubGlobal('fetch', vi.fn().mockResolvedValue(health('v1', 'aaa')))
      const { result } = mount({ unattended: () => true })
      await settle()
      act(() => result.current.retryHeldReload())
      expect(reload).not.toHaveBeenCalled()
    })
  })

  describe('a lazy screen that fails to load', () => {
    const preloadError = () => {
      const event = new Event('vite:preloadError', { cancelable: true })
      act(() => { window.dispatchEvent(event) })
      return event
    }

    it('after a deploy: says the screen needs the update, logs the failure and leaves the event alone', async () => {
      vi.stubGlobal('fetch', vi.fn().mockResolvedValueOnce(health('v1', 'aaa')).mockResolvedValueOnce(health('v2', 'bbb')))
      const { result } = mount({ unattended: () => false })
      await settle()

      const event = preloadError()
      await settle()
      // Left alone: with preventDefault Vite resolves the import to undefined and the lazy wrapper throws a TypeError instead.
      expect(event.defaultPrevented).toBe(false)
      expect(console.error).toHaveBeenCalled()
      expect(result.current.chunkFailure).toBe('needs-update')
      expect(result.current.available).toEqual({ version: 'v2', revision: 'bbb' })
      expect(reload).not.toHaveBeenCalled()
    })

    it('with the same build: reports a connection problem and does not reload', async () => {
      vi.stubGlobal('fetch', vi.fn().mockResolvedValue(health('v1', 'aaa')))
      const { result } = mount()
      await settle()
      preloadError()
      await settle()
      expect(result.current.chunkFailure).toBe('connection')
      expect(reload).not.toHaveBeenCalled()
    })

    it('when the re-check itself fails: reports a connection problem', async () => {
      vi.stubGlobal('fetch', vi.fn().mockResolvedValueOnce(health('v1', 'aaa')).mockRejectedValueOnce(new Error('offline')))
      const { result } = mount()
      await settle()
      preloadError()
      await settle()
      expect(result.current.chunkFailure).toBe('connection')
    })

    it('a failure reported by an error boundary takes the same path', async () => {
      vi.stubGlobal('fetch', vi.fn().mockResolvedValueOnce(health('v1', 'aaa')).mockResolvedValueOnce(health('v2', 'bbb')))
      const { result } = mount()
      await settle()
      act(() => result.current.reportChunkFailure(new Error('Failed to fetch dynamically imported module')))
      await settle()
      expect(result.current.chunkFailure).toBe('needs-update')
    })

    it('once cleared, a second failure shows again', async () => {
      vi.stubGlobal('fetch', vi.fn().mockResolvedValue(health('v1', 'aaa')))
      const { result } = mount()
      await settle()
      preloadError()
      await settle()
      expect(result.current.chunkFailure).toBe('connection')
      act(() => result.current.clearChunkFailure())
      expect(result.current.chunkFailure).toBeNull()
      preloadError()
      await settle()
      expect(result.current.chunkFailure).toBe('connection')
    })

    it('stops listening on unmount', async () => {
      vi.stubGlobal('fetch', vi.fn().mockResolvedValue(health('v1', 'aaa')))
      const { unmount } = mount()
      await settle()
      unmount()
      preloadError()
      expect(console.error).not.toHaveBeenCalled()
    })
  })
})
