import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { renderHook } from '@testing-library/react'
import { StrictMode } from 'react'

const toastMock = vi.hoisted(() => vi.fn())
vi.mock('sonner', () => ({ toast: toastMock }))

import { useUpdatedToast } from '@/hooks/use-updated-toast'
import { SEEN_VERSION_KEY } from '@/lib/seen-version'

const build = (version: string, revision = 'aaa') => ({ version, revision })

describe('useUpdatedToast', () => {
  beforeEach(() => {
    toastMock.mockReset()
    localStorage.clear()
    vi.spyOn(console, 'warn').mockImplementation(() => {})
  })
  afterEach(() => vi.restoreAllMocks())

  it('waits for the first health read', () => {
    localStorage.setItem(SEEN_VERSION_KEY, 'v0.41.0')
    renderHook(() => useUpdatedToast(null))
    expect(toastMock).not.toHaveBeenCalled()
  })

  it('stays silent the first time this browser loads, and remembers the version', () => {
    renderHook(() => useUpdatedToast(build('v0.42.0')))
    expect(toastMock).not.toHaveBeenCalled()
    expect(localStorage.getItem(SEEN_VERSION_KEY)).toBe('v0.42.0')
  })

  it('announces a load on a different version than the browser last showed, once, with release notes', () => {
    localStorage.setItem(SEEN_VERSION_KEY, 'v0.41.0')
    const open = vi.spyOn(window, 'open').mockImplementation(() => null)

    renderHook(() => useUpdatedToast(build('v0.42.0')), { wrapper: StrictMode })

    expect(toastMock).toHaveBeenCalledTimes(1)
    const [message, options] = toastMock.mock.calls[0]
    expect(message).toBe('Helmcentral updated to v0.42.0')
    expect(options.duration).toBeGreaterThanOrEqual(8000)
    expect(options.action.label).toBe('Release notes')
    options.action.onClick()
    expect(open).toHaveBeenCalledWith('https://github.com/gterrill/helmcentral/releases/tag/v0.42.0', '_blank', 'noopener')
    expect(localStorage.getItem(SEEN_VERSION_KEY)).toBe('v0.42.0')
  })

  it('does not announce the same version again on the next load', () => {
    localStorage.setItem(SEEN_VERSION_KEY, 'v0.41.0')
    const first = renderHook(() => useUpdatedToast(build('v0.42.0')))
    first.unmount()
    renderHook(() => useUpdatedToast(build('v0.42.0')))
    expect(toastMock).toHaveBeenCalledTimes(1)
  })

  it('a revision-only change does not announce', () => {
    localStorage.setItem(SEEN_VERSION_KEY, 'v0.42.0')
    renderHook(() => useUpdatedToast(build('v0.42.0', 'bbb')))
    expect(toastMock).not.toHaveBeenCalled()
  })

  it('a dev build never announces, and is remembered', () => {
    localStorage.setItem(SEEN_VERSION_KEY, 'v0.41.0')
    renderHook(() => useUpdatedToast(build('dev', 'unknown')))
    expect(toastMock).not.toHaveBeenCalled()
    expect(localStorage.getItem(SEEN_VERSION_KEY)).toBe('dev')
  })

  it('offers no action for a version that is not a release tag', () => {
    localStorage.setItem(SEEN_VERSION_KEY, 'v0.41.0')
    renderHook(() => useUpdatedToast(build('v0.42.0-3-gabc')))
    const [message, options] = toastMock.mock.calls[0]
    expect(message).toBe('Helmcentral updated to v0.42.0-3-gabc')
    expect(options.action).toBeUndefined()
  })

  it('logs and skips the message when storage cannot be used', () => {
    vi.stubGlobal('localStorage', { getItem: () => { throw new Error('blocked') }, setItem: () => { throw new Error('blocked') } })
    try {
      renderHook(() => useUpdatedToast(build('v0.42.0')))
      expect(toastMock).not.toHaveBeenCalled()
      expect(console.warn).toHaveBeenCalled()
    } finally {
      vi.unstubAllGlobals()
    }
  })
})
