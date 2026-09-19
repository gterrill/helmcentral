import { describe, it, expect, afterEach, vi } from 'vitest'
import { renderHook, act, waitFor } from '@testing-library/react'
import { useScreenWakeLock } from '@/hooks/use-screen-wake-lock'

// ADR 0110 §5c. Per the fallback policy (AGENTS.md), a best-effort feature
// has to say when it isn't working rather than silently doing nothing —
// that's why this hook reports a status rather than returning nothing.

function fakeSentinel() {
  return { released: false, release: vi.fn().mockImplementation(async function (this: { released: boolean }) {
    this.released = true
  }) }
}

function stubWakeLock(request: (type: string) => Promise<unknown>) {
  Object.defineProperty(navigator, 'wakeLock', {
    value: { request },
    configurable: true,
  })
}

function setVisibility(state: DocumentVisibilityState) {
  Object.defineProperty(document, 'visibilityState', { value: state, configurable: true })
  document.dispatchEvent(new Event('visibilitychange'))
}

describe('useScreenWakeLock', () => {
  afterEach(() => {
    // @ts-expect-error - test-only cleanup of a property we defined ourselves
    delete navigator.wakeLock
    Object.defineProperty(document, 'visibilityState', { value: 'visible', configurable: true })
    vi.restoreAllMocks()
  })

  it('is "off" when not enabled', () => {
    stubWakeLock(vi.fn().mockResolvedValue(fakeSentinel()))
    const { result } = renderHook(() => useScreenWakeLock(false))
    expect(result.current.status).toBe('off')
  })

  it('is "unsupported" when navigator.wakeLock does not exist', async () => {
    const { result } = renderHook(() => useScreenWakeLock(true))
    await waitFor(() => expect(result.current.status).toBe('unsupported'))
  })

  it('requests the lock and reports "held" on success', async () => {
    const sentinel = fakeSentinel()
    const request = vi.fn().mockResolvedValue(sentinel)
    stubWakeLock(request)

    const { result } = renderHook(() => useScreenWakeLock(true))

    await waitFor(() => expect(result.current.status).toBe('held'))
    expect(request).toHaveBeenCalledWith('screen')
  })

  it('reports "denied" when the request rejects', async () => {
    stubWakeLock(vi.fn().mockRejectedValue(new Error('not allowed')))

    const { result } = renderHook(() => useScreenWakeLock(true))

    await waitFor(() => expect(result.current.status).toBe('denied'))
  })

  it('re-requests the lock when the document becomes visible again', async () => {
    const first = fakeSentinel()
    const second = fakeSentinel()
    const request = vi.fn().mockResolvedValueOnce(first).mockResolvedValueOnce(second)
    stubWakeLock(request)

    const { result } = renderHook(() => useScreenWakeLock(true))
    await waitFor(() => expect(result.current.status).toBe('held'))
    expect(request).toHaveBeenCalledTimes(1)

    // The spec itself drops the lock whenever the tab hides - simulated here
    // by the visibility flip alone, since nothing in this hook needs to be
    // told the sentinel died to know it must re-acquire on return.
    act(() => setVisibility('hidden'))
    expect(request).toHaveBeenCalledTimes(1)

    act(() => setVisibility('visible'))
    await waitFor(() => expect(request).toHaveBeenCalledTimes(2))
    expect(result.current.status).toBe('held')
  })

  it('releases the lock on unmount', async () => {
    const sentinel = fakeSentinel()
    stubWakeLock(vi.fn().mockResolvedValue(sentinel))

    const { result, unmount } = renderHook(() => useScreenWakeLock(true))
    await waitFor(() => expect(result.current.status).toBe('held'))

    unmount()
    expect(sentinel.release).toHaveBeenCalled()
  })

  it('releases the lock and reports "off" when enabled goes false', async () => {
    const sentinel = fakeSentinel()
    stubWakeLock(vi.fn().mockResolvedValue(sentinel))

    const { result, rerender } = renderHook(({ enabled }) => useScreenWakeLock(enabled), {
      initialProps: { enabled: true },
    })
    await waitFor(() => expect(result.current.status).toBe('held'))

    rerender({ enabled: false })

    expect(sentinel.release).toHaveBeenCalled()
    expect(result.current.status).toBe('off')
  })
})
