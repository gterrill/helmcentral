import { renderHook, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { usePlaceName } from '@/hooks/use-place-name'

const REFRESH_SECONDS = 60

interface Fix {
  latitude: number | null
  longitude: number | null
}

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
  it('does not refetch when the GPS fix moves', async () => {
    const { rerender } = renderHook(
      ({ latitude, longitude }) => usePlaceName(latitude, longitude, REFRESH_SECONDS),
      { initialProps: { latitude: -25.2986, longitude: 152.9021 } },
    )

    expect(fetchMock).toHaveBeenCalledTimes(1)

    // A moving vessel produces a fresh GPS fix several times a second. None of
    // these should trigger a request: the endpoint takes no lat/lon, the
    // backend reads position server-side.
    rerender({ latitude: -25.2987, longitude: 152.9022 })
    rerender({ latitude: -25.2988, longitude: 152.9023 })
    rerender({ latitude: -25.2989, longitude: 152.9024 })
    rerender({ latitude: -25.299, longitude: 152.9025 })

    expect(fetchMock).toHaveBeenCalledTimes(1)
  })

  it('polls on the refresh interval', async () => {
    renderHook(() => usePlaceName(-25.2986, 152.9021, REFRESH_SECONDS))

    expect(fetchMock).toHaveBeenCalledTimes(1)

    await vi.advanceTimersByTimeAsync(REFRESH_SECONDS * 1000)
    expect(fetchMock).toHaveBeenCalledTimes(2)

    await vi.advanceTimersByTimeAsync(REFRESH_SECONDS * 1000)
    expect(fetchMock).toHaveBeenCalledTimes(3)
  })

  it('waits for a fix before fetching, then fetches once when one arrives', async () => {
    const noFix: Fix = { latitude: null, longitude: null }
    const { rerender } = renderHook(
      ({ latitude, longitude }: Fix) => usePlaceName(latitude, longitude, REFRESH_SECONDS),
      { initialProps: noFix },
    )

    expect(fetchMock).not.toHaveBeenCalled()

    rerender({ latitude: -25.2986, longitude: 152.9021 })
    expect(fetchMock).toHaveBeenCalledTimes(1)
  })

  it('exposes the resolved place name', async () => {
    // Real timers: testing-library's waitFor does not drive vitest's fake clock.
    vi.useRealTimers()
    const { result } = renderHook(() => usePlaceName(-25.2986, 152.9021, REFRESH_SECONDS))

    await waitFor(() => expect(result.current).toBe('Urangan'))
  })
})
