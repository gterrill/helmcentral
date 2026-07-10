import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { renderHook, act } from '@testing-library/react'
import { useImageryPrefetch } from '@/hooks/use-imagery-prefetch'

describe('useImageryPrefetch', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    vi.clearAllMocks()
  })

  afterEach(() => {
    vi.useRealTimers()
    vi.unstubAllGlobals()
  })

  it('starts a prefetch, polls until complete, and returns the jobId', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce({
        ok: true,
        status: 202,
        json: async () => ({ jobId: 'job-1', totalTiles: 100 }),
      })
      .mockResolvedValueOnce({
        ok: true,
        status: 200,
        json: async () => ({ total: 100, done: 40, complete: false }),
      })
      .mockResolvedValueOnce({
        ok: true,
        status: 200,
        json: async () => ({ total: 100, done: 100, complete: true }),
      })
    vi.stubGlobal('fetch', fetchMock)

    const { result } = renderHook(() => useImageryPrefetch())

    let jobId: string | null = null
    await act(async () => {
      jobId = await result.current.startPrefetch({ west: 150, south: -25, east: 151, north: -24 }, 12, 20)
    })

    expect(jobId).toBe('job-1')
    expect(fetchMock).toHaveBeenNthCalledWith(
      1,
      '/api/world-imagery/prefetch',
      expect.objectContaining({
        method: 'POST',
        body: JSON.stringify({ west: 150, south: -25, east: 151, north: -24, minZoom: 12, maxZoom: 20 }),
      }),
    )
    expect(result.current.prefetching).toBe(true)
    expect(result.current.progress).toEqual({ done: 0, total: 100 })

    await act(async () => {
      await vi.advanceTimersByTimeAsync(1000)
    })
    expect(fetchMock).toHaveBeenNthCalledWith(2, '/api/world-imagery/prefetch/job-1')
    expect(result.current.progress).toEqual({ done: 40, total: 100 })
    expect(result.current.prefetching).toBe(true)

    await act(async () => {
      await vi.advanceTimersByTimeAsync(1000)
    })
    expect(fetchMock).toHaveBeenNthCalledWith(3, '/api/world-imagery/prefetch/job-1')
    expect(result.current.progress).toEqual({ done: 100, total: 100 })
    expect(result.current.prefetching).toBe(false)

    // Polling must have stopped: advancing further doesn't call fetch again.
    fetchMock.mockClear()
    await act(async () => {
      await vi.advanceTimersByTimeAsync(5000)
    })
    expect(fetchMock).not.toHaveBeenCalled()
  })

  it('surfaces the computed tile count as an error and never starts polling when the area is over the cap', async () => {
    const fetchMock = vi.fn().mockResolvedValueOnce({
      ok: false,
      status: 400,
      json: async () => ({ error: 'area too large: 87892 tiles (max 8000)', totalTiles: 87892 }),
    })
    vi.stubGlobal('fetch', fetchMock)

    const { result } = renderHook(() => useImageryPrefetch())

    let jobId: string | null = 'unset'
    await act(async () => {
      jobId = await result.current.startPrefetch({ west: -180, south: -85, east: 180, north: 85 }, 0, 8)
    })

    expect(jobId).toBeNull()
    expect(result.current.prefetching).toBe(false)
    expect(result.current.progress).toBeNull()
    expect(result.current.error).toContain('87892')

    fetchMock.mockClear()
    await act(async () => {
      await vi.advanceTimersByTimeAsync(5000)
    })
    expect(fetchMock).not.toHaveBeenCalled()
  })

  it('stops polling and surfaces an error if a status poll fails', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce({
        ok: true,
        status: 202,
        json: async () => ({ jobId: 'job-2', totalTiles: 10 }),
      })
      .mockResolvedValueOnce({ ok: false, status: 500 })
    vi.stubGlobal('fetch', fetchMock)

    const { result } = renderHook(() => useImageryPrefetch())

    await act(async () => {
      await result.current.startPrefetch({ west: 150, south: -25, east: 151, north: -24 }, 12, 20)
    })
    expect(result.current.prefetching).toBe(true)

    await act(async () => {
      await vi.advanceTimersByTimeAsync(1000)
    })

    expect(result.current.prefetching).toBe(false)
    expect(result.current.error).toBeTruthy()

    fetchMock.mockClear()
    await act(async () => {
      await vi.advanceTimersByTimeAsync(5000)
    })
    expect(fetchMock).not.toHaveBeenCalled()
  })

  it('stops polling on unmount', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce({
        ok: true,
        status: 202,
        json: async () => ({ jobId: 'job-3', totalTiles: 10 }),
      })
    vi.stubGlobal('fetch', fetchMock)

    const { result, unmount } = renderHook(() => useImageryPrefetch())

    await act(async () => {
      await result.current.startPrefetch({ west: 150, south: -25, east: 151, north: -24 }, 12, 20)
    })

    unmount()
    fetchMock.mockClear()

    await act(async () => {
      await vi.advanceTimersByTimeAsync(5000)
    })
    expect(fetchMock).not.toHaveBeenCalled()
  })

  it('handles malformed JSON in the 202 success response gracefully', async () => {
    const fetchMock = vi.fn().mockResolvedValueOnce({
      ok: true,
      status: 202,
      json: async () => {
        throw new SyntaxError('Unexpected end of JSON input')
      },
    })
    vi.stubGlobal('fetch', fetchMock)

    const { result } = renderHook(() => useImageryPrefetch())

    let jobId: string | null = 'unset'
    await act(async () => {
      jobId = await result.current.startPrefetch({ west: 150, south: -25, east: 151, north: -24 }, 12, 20)
    })

    expect(jobId).toBeNull()
    expect(result.current.error).toBeTruthy()
    expect(result.current.prefetching).toBe(false)
    expect(result.current.progress).toBeNull()

    // Verify polling never started
    fetchMock.mockClear()
    await act(async () => {
      await vi.advanceTimersByTimeAsync(5000)
    })
    expect(fetchMock).not.toHaveBeenCalled()
  })

  it('handles malformed JSON in the 400 error response gracefully', async () => {
    const fetchMock = vi.fn().mockResolvedValueOnce({
      ok: false,
      status: 400,
      json: async () => {
        throw new SyntaxError('Unexpected end of JSON input')
      },
    })
    vi.stubGlobal('fetch', fetchMock)

    const { result } = renderHook(() => useImageryPrefetch())

    let jobId: string | null = 'unset'
    await act(async () => {
      jobId = await result.current.startPrefetch({ west: -180, south: -85, east: 180, north: 85 }, 0, 8)
    })

    expect(jobId).toBeNull()
    expect(result.current.error).toBeTruthy()
    expect(result.current.prefetching).toBe(false)
    expect(result.current.progress).toBeNull()

    // Verify polling never started
    fetchMock.mockClear()
    await act(async () => {
      await vi.advanceTimersByTimeAsync(5000)
    })
    expect(fetchMock).not.toHaveBeenCalled()
  })
})
