import { renderHook, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { usePlaceName } from '@/hooks/use-place-name'

const REFRESH_SECONDS = 60

function placeNameResponse(name: string) {
  return new Response(JSON.stringify({ name }), { status: 200 })
}

let fetchMock: ReturnType<typeof vi.fn>

beforeEach(() => {
  vi.useFakeTimers()
  fetchMock = vi.fn(async () => placeNameResponse('Urangan'))
  vi.stubGlobal('fetch', fetchMock)
})

afterEach(() => {
  vi.useRealTimers()
  vi.unstubAllGlobals()
  vi.restoreAllMocks()
})

describe('usePlaceName', () => {
  it('does not refetch on a re-render while the fix holds', async () => {
    const { rerender } = renderHook(
      ({ hasFix }) => usePlaceName(hasFix, REFRESH_SECONDS),
      { initialProps: { hasFix: true } },
    )

    expect(fetchMock).toHaveBeenCalledTimes(1)

    // A moving vessel produces a fresh GPS fix several times a second. None of
    // these should trigger a request: the endpoint takes no lat/lon, the
    // backend reads position server-side.
    rerender({ hasFix: true })
    rerender({ hasFix: true })
    rerender({ hasFix: true })

    expect(fetchMock).toHaveBeenCalledTimes(1)
  })

  it('polls on the refresh interval', async () => {
    renderHook(() => usePlaceName(true, REFRESH_SECONDS))

    expect(fetchMock).toHaveBeenCalledTimes(1)

    await vi.advanceTimersByTimeAsync(REFRESH_SECONDS * 1000)
    expect(fetchMock).toHaveBeenCalledTimes(2)

    await vi.advanceTimersByTimeAsync(REFRESH_SECONDS * 1000)
    expect(fetchMock).toHaveBeenCalledTimes(3)
  })

  it('waits for a fix before fetching, then fetches once when one arrives', async () => {
    const { rerender } = renderHook(
      ({ hasFix }: { hasFix: boolean }) => usePlaceName(hasFix, REFRESH_SECONDS),
      { initialProps: { hasFix: false } },
    )

    expect(fetchMock).not.toHaveBeenCalled()

    rerender({ hasFix: true })
    expect(fetchMock).toHaveBeenCalledTimes(1)
  })

  it('exposes the resolved place name', async () => {
    // Real timers: testing-library's waitFor does not drive vitest's fake clock.
    vi.useRealTimers()
    const { result } = renderHook(() => usePlaceName(true, REFRESH_SECONDS))

    await waitFor(() => expect(result.current).toBe('Urangan'))
  })
})
