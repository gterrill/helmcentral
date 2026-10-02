import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { act, renderHook } from '@testing-library/react'

import { VERSION_CHECK_INTERVAL_MS, useVersionReload } from '@/hooks/use-version-reload'

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
  })
  afterEach(() => {
    vi.unstubAllGlobals()
    vi.restoreAllMocks()
  })

  const settle = () => act(async () => { for (let i = 0; i < 10; i++) await Promise.resolve() })
  const focus = () => act(async () => { window.dispatchEvent(new Event('focus')); for (let i = 0; i < 10; i++) await Promise.resolve() })
  const becomeVisible = () => act(async () => { document.dispatchEvent(new Event('visibilitychange')); for (let i = 0; i < 10; i++) await Promise.resolve() })

  it('reads the build on load and does not check again before an hour has passed', async () => {
    const fetchMock = vi.fn().mockResolvedValue(health('v1', 'aaa'))
    vi.stubGlobal('fetch', fetchMock)
    renderHook(() => useVersionReload({ now, reload }))
    await settle()
    expect(fetchMock).toHaveBeenCalledTimes(1)

    clock += VERSION_CHECK_INTERVAL_MS - 1
    await focus()
    await becomeVisible()
    expect(fetchMock).toHaveBeenCalledTimes(1)
  })

  it('checks again after an hour on focus, and then not again within the next hour', async () => {
    const fetchMock = vi.fn().mockResolvedValue(health('v1', 'aaa'))
    vi.stubGlobal('fetch', fetchMock)
    renderHook(() => useVersionReload({ now, reload }))
    await settle()

    clock += VERSION_CHECK_INTERVAL_MS
    await focus()
    expect(fetchMock).toHaveBeenCalledTimes(2)
    await focus()
    expect(fetchMock).toHaveBeenCalledTimes(2)
  })

  it('checks after an hour on visibilitychange to visible', async () => {
    const fetchMock = vi.fn().mockResolvedValue(health('v1', 'aaa'))
    vi.stubGlobal('fetch', fetchMock)
    renderHook(() => useVersionReload({ now, reload }))
    await settle()

    clock += VERSION_CHECK_INTERVAL_MS
    await becomeVisible()
    expect(fetchMock).toHaveBeenCalledTimes(2)
  })

  it('reloads when the revision changed', async () => {
    const fetchMock = vi.fn().mockResolvedValueOnce(health('v1', 'aaa')).mockResolvedValueOnce(health('v1', 'bbb'))
    vi.stubGlobal('fetch', fetchMock)
    renderHook(() => useVersionReload({ now, reload }))
    await settle()

    clock += VERSION_CHECK_INTERVAL_MS
    await focus()
    expect(reload).toHaveBeenCalledTimes(1)
  })

  it('reloads when the version changed', async () => {
    const fetchMock = vi.fn().mockResolvedValueOnce(health('v1', 'aaa')).mockResolvedValueOnce(health('v2', 'aaa'))
    vi.stubGlobal('fetch', fetchMock)
    renderHook(() => useVersionReload({ now, reload }))
    await settle()

    clock += VERSION_CHECK_INTERVAL_MS
    await focus()
    expect(reload).toHaveBeenCalledTimes(1)
  })

  it('does not reload when the build is unchanged', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(health('v1', 'aaa')))
    renderHook(() => useVersionReload({ now, reload }))
    await settle()

    clock += VERSION_CHECK_INTERVAL_MS
    await focus()
    expect(reload).not.toHaveBeenCalled()
  })

  it('does not reload, and logs, when the check fails; it waits for the next eligible return', async () => {
    const fetchMock = vi.fn().mockResolvedValueOnce(health('v1', 'aaa')).mockRejectedValueOnce(new Error('offline'))
    vi.stubGlobal('fetch', fetchMock)
    renderHook(() => useVersionReload({ now, reload }))
    await settle()

    clock += VERSION_CHECK_INTERVAL_MS
    await focus()
    expect(reload).not.toHaveBeenCalled()
    expect(console.warn).toHaveBeenCalled()

    // Not retried early.
    clock += 1000
    await focus()
    expect(fetchMock).toHaveBeenCalledTimes(2)
  })

  it('does not reload on a non-ok or version-less health response', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(health('v1', 'aaa'))
      .mockResolvedValueOnce({ ok: false, status: 503, json: async () => ({}) })
      .mockResolvedValueOnce({ ok: true, status: 200, json: async () => ({}) })
    vi.stubGlobal('fetch', fetchMock)
    renderHook(() => useVersionReload({ now, reload }))
    await settle()

    clock += VERSION_CHECK_INTERVAL_MS
    await focus()
    clock += VERSION_CHECK_INTERVAL_MS
    await focus()
    expect(fetchMock).toHaveBeenCalledTimes(3)
    expect(reload).not.toHaveBeenCalled()
  })

  it('a failed initial read followed by a successful one sets the baseline without reloading', async () => {
    const fetchMock = vi
      .fn()
      .mockRejectedValueOnce(new Error('offline'))
      .mockResolvedValueOnce(health('v2', 'bbb'))
      .mockResolvedValueOnce(health('v3', 'ccc'))
    vi.stubGlobal('fetch', fetchMock)
    renderHook(() => useVersionReload({ now, reload }))
    await settle()

    clock += VERSION_CHECK_INTERVAL_MS
    await focus()
    expect(reload).not.toHaveBeenCalled()

    clock += VERSION_CHECK_INTERVAL_MS
    await focus()
    expect(reload).toHaveBeenCalledTimes(1)
  })

  it('holds the reload while there is unsaved work, then reloads on the next return without another fetch', async () => {
    const fetchMock = vi.fn().mockResolvedValueOnce(health('v1', 'aaa')).mockResolvedValueOnce(health('v1', 'bbb'))
    vi.stubGlobal('fetch', fetchMock)
    let unsaved = true
    renderHook(() => useVersionReload({ now, reload, hasUnsavedWork: () => unsaved }))
    await settle()

    clock += VERSION_CHECK_INTERVAL_MS
    await focus()
    expect(reload).not.toHaveBeenCalled()

    unsaved = false
    await focus()
    expect(reload).toHaveBeenCalledTimes(1)
    expect(fetchMock).toHaveBeenCalledTimes(2)
  })
})
