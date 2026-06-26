import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { renderHook, act, waitFor } from '@testing-library/react'
import { useSatCharts } from '@/hooks/use-sat-charts'

describe('useSatCharts', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('fetches the chart list on mount', async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      json: async () => ({
        charts: [
          { id: '1', name: 'Reef A', bounds: [150, -25, 151, -24], minzoom: 10, maxzoom: 18, format: 'png', size_bytes: 1000 },
        ],
      }),
    })
    vi.stubGlobal('fetch', fetchMock)

    const { result } = renderHook(() => useSatCharts())

    await waitFor(() => expect(result.current.loading).toBe(false))
    expect(fetchMock).toHaveBeenCalledWith('/api/sat-charts')
    expect(result.current.charts).toHaveLength(1)
    expect(result.current.charts[0].name).toBe('Reef A')
  })

  it('sets an error message when the fetch fails', async () => {
    const fetchMock = vi.fn().mockResolvedValue({ ok: false, status: 500 })
    vi.stubGlobal('fetch', fetchMock)

    const { result } = renderHook(() => useSatCharts())

    await waitFor(() => expect(result.current.loading).toBe(false))
    expect(result.current.error).toContain('500')
    expect(result.current.charts).toHaveLength(0)
  })

  it('uploadChart POSTs FormData and refetches the list', async () => {
    const fetchMock = vi.fn()
      .mockResolvedValueOnce({ ok: true, json: async () => ({ charts: [] }) })
      .mockResolvedValueOnce({
        ok: true,
        json: async () => ({ id: '1', name: 'Reef Chart', bounds: [0, 0, 1, 1], minzoom: 0, maxzoom: 10, format: 'png', size_bytes: 1000 }),
      })
      .mockResolvedValueOnce({
        ok: true,
        json: async () => ({
          charts: [{ id: '1', name: 'Reef Chart', bounds: [0, 0, 1, 1], minzoom: 0, maxzoom: 10, format: 'png', size_bytes: 1000 }],
        }),
      })
    vi.stubGlobal('fetch', fetchMock)

    const { result } = renderHook(() => useSatCharts())
    await waitFor(() => expect(result.current.loading).toBe(false))

    const file = new File(['fake-mbtiles-bytes'], 'reef.mbtiles')
    await act(async () => {
      await result.current.uploadChart(file)
    })

    const uploadCall = fetchMock.mock.calls[1]
    expect(uploadCall[0]).toBe('/api/sat-charts')
    expect(uploadCall[1].method).toBe('POST')
    expect(uploadCall[1].body).toBeInstanceOf(FormData)
    expect(result.current.charts).toHaveLength(1)
    expect(result.current.charts[0].name).toBe('Reef Chart')
  })

  it('uploadChart returns null without refetching when the upload fails', async () => {
    const fetchMock = vi.fn()
      .mockResolvedValueOnce({ ok: true, json: async () => ({ charts: [] }) })
      .mockResolvedValueOnce({ ok: false, status: 400 })
    vi.stubGlobal('fetch', fetchMock)

    const { result } = renderHook(() => useSatCharts())
    await waitFor(() => expect(result.current.loading).toBe(false))

    const file = new File(['not-mbtiles'], 'bad.mbtiles')
    let uploadResult: unknown
    await act(async () => {
      uploadResult = await result.current.uploadChart(file)
    })

    expect(uploadResult).toBeNull()
    expect(fetchMock).toHaveBeenCalledTimes(2)
  })

  it('deleteChart issues a DELETE request and refetches', async () => {
    const fetchMock = vi.fn()
      .mockResolvedValueOnce({
        ok: true,
        json: async () => ({ charts: [{ id: '1', name: 'Reef A', bounds: [0, 0, 1, 1], minzoom: 0, maxzoom: 10, format: 'png', size_bytes: 1000 }] }),
      })
      .mockResolvedValueOnce({ ok: true })
      .mockResolvedValueOnce({ ok: true, json: async () => ({ charts: [] }) })
    vi.stubGlobal('fetch', fetchMock)

    const { result } = renderHook(() => useSatCharts())
    await waitFor(() => expect(result.current.loading).toBe(false))

    await act(async () => {
      await result.current.deleteChart('1')
    })

    expect(fetchMock).toHaveBeenCalledWith('/api/sat-charts/1', expect.objectContaining({ method: 'DELETE' }))
    expect(result.current.charts).toHaveLength(0)
  })
})
