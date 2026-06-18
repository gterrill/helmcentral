import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { renderHook, act, waitFor } from '@testing-library/react'
import { useRoutes } from '@/hooks/use-routes'

describe('useRoutes', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('fetches the route list on mount', async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      json: async () => ({ routes: [{ id: '1', name: 'Route A', waypoints: [], created_at: '', updated_at: '' }] }),
    })
    vi.stubGlobal('fetch', fetchMock)

    const { result } = renderHook(() => useRoutes())

    await waitFor(() => expect(result.current.loading).toBe(false))
    expect(fetchMock).toHaveBeenCalledWith('/api/routes')
    expect(result.current.routes).toHaveLength(1)
    expect(result.current.routes[0].name).toBe('Route A')
  })

  it('sets an error message when the fetch fails', async () => {
    const fetchMock = vi.fn().mockResolvedValue({ ok: false, status: 500 })
    vi.stubGlobal('fetch', fetchMock)

    const { result } = renderHook(() => useRoutes())

    await waitFor(() => expect(result.current.loading).toBe(false))
    expect(result.current.error).toContain('500')
    expect(result.current.routes).toHaveLength(0)
  })

  it('createRoute POSTs and refetches the list', async () => {
    const fetchMock = vi.fn()
      .mockResolvedValueOnce({ ok: true, json: async () => ({ routes: [] }) })
      .mockResolvedValueOnce({ ok: true, json: async () => ({ id: '2', name: 'New Route', waypoints: [], created_at: '', updated_at: '' }) })
      .mockResolvedValueOnce({ ok: true, json: async () => ({ routes: [{ id: '2', name: 'New Route', waypoints: [], created_at: '', updated_at: '' }] }) })
    vi.stubGlobal('fetch', fetchMock)

    const { result } = renderHook(() => useRoutes())
    await waitFor(() => expect(result.current.loading).toBe(false))

    await act(async () => {
      await result.current.createRoute('New Route', [{ lat: 1, lon: 2 }])
    })

    expect(fetchMock).toHaveBeenCalledWith('/api/routes', expect.objectContaining({ method: 'POST' }))
    expect(result.current.routes).toHaveLength(1)
    expect(result.current.routes[0].name).toBe('New Route')
  })

  it('deleteRoute issues a DELETE request and refetches', async () => {
    const fetchMock = vi.fn()
      .mockResolvedValueOnce({ ok: true, json: async () => ({ routes: [{ id: '1', name: 'Route A', waypoints: [], created_at: '', updated_at: '' }] }) })
      .mockResolvedValueOnce({ ok: true })
      .mockResolvedValueOnce({ ok: true, json: async () => ({ routes: [] }) })
    vi.stubGlobal('fetch', fetchMock)

    const { result } = renderHook(() => useRoutes())
    await waitFor(() => expect(result.current.loading).toBe(false))

    await act(async () => {
      await result.current.deleteRoute('1')
    })

    expect(fetchMock).toHaveBeenCalledWith('/api/routes/1', expect.objectContaining({ method: 'DELETE' }))
    expect(result.current.routes).toHaveLength(0)
  })
})
