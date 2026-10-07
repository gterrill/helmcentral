import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { renderHook } from '@testing-library/react'
import { StrictMode } from 'react'

const toastMock = vi.hoisted(() => vi.fn())
vi.mock('sonner', () => ({ toast: toastMock }))

import { UPDATED_TO_KEY } from '@/hooks/use-version-reload'
import { useUpdatedToast } from '@/hooks/use-updated-toast'

describe('useUpdatedToast', () => {
  beforeEach(() => {
    toastMock.mockReset()
    sessionStorage.clear()
    vi.spyOn(console, 'warn').mockImplementation(() => {})
  })
  afterEach(() => vi.restoreAllMocks())

  it('does nothing without a marker', () => {
    renderHook(() => useUpdatedToast())
    expect(toastMock).not.toHaveBeenCalled()
  })

  it('announces the new release once, with a release notes action, and clears the marker', () => {
    sessionStorage.setItem(UPDATED_TO_KEY, JSON.stringify({ from: 'v0.41.0', to: 'v0.42.0' }))
    const open = vi.spyOn(window, 'open').mockImplementation(() => null)

    renderHook(() => useUpdatedToast(), { wrapper: StrictMode })

    expect(toastMock).toHaveBeenCalledTimes(1)
    const [message, options] = toastMock.mock.calls[0]
    expect(message).toBe('Helmcentral updated to v0.42.0')
    expect(options.duration).toBeGreaterThanOrEqual(8000)
    expect(options.action.label).toBe('Release notes')
    options.action.onClick()
    expect(open).toHaveBeenCalledWith(
      'https://github.com/gterrill/helmcentral/releases/tag/v0.42.0',
      '_blank',
      'noopener',
    )
    expect(sessionStorage.getItem(UPDATED_TO_KEY)).toBeNull()
  })

  it('offers no action for a non-release build', () => {
    sessionStorage.setItem(UPDATED_TO_KEY, JSON.stringify({ from: 'v0.41.0', to: 'dev' }))
    renderHook(() => useUpdatedToast())
    const [message, options] = toastMock.mock.calls[0]
    expect(message).toBe('Helmcentral updated to dev')
    expect(options.action).toBeUndefined()
  })

  it('ignores and clears a malformed marker', () => {
    sessionStorage.setItem(UPDATED_TO_KEY, 'not json')
    renderHook(() => useUpdatedToast())
    expect(toastMock).not.toHaveBeenCalled()
    expect(sessionStorage.getItem(UPDATED_TO_KEY)).toBeNull()
  })
})
