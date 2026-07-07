import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { renderHook, act, waitFor } from '@testing-library/react'
import { useDashboardLayout } from '@/hooks/use-dashboard-layout'

describe('useDashboardLayout', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('fetches the layout on mount', async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      json: async () => ({ widgets: [{ id: 'wind', x: 0, y: 0, w: 4, h: 8 }] }),
    })
    vi.stubGlobal('fetch', fetchMock)

    const { result } = renderHook(() => useDashboardLayout())

    await waitFor(() => expect(result.current.loading).toBe(false))
    expect(fetchMock).toHaveBeenCalledWith('/api/dashboard-layout')
    expect(result.current.widgets).toHaveLength(1)
    expect(result.current.widgets?.[0].id).toBe('wind')
    expect(result.current.error).toBe(null)
  })

  it('treats an empty widgets array as loaded-but-empty, not an error', async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      json: async () => ({ widgets: [] }),
    })
    vi.stubGlobal('fetch', fetchMock)

    const { result } = renderHook(() => useDashboardLayout())

    await waitFor(() => expect(result.current.loading).toBe(false))
    expect(result.current.error).toBe(null)
    expect(result.current.widgets).toHaveLength(0)
  })

  it('sets an error message when the fetch fails', async () => {
    const fetchMock = vi.fn().mockResolvedValue({ ok: false, status: 500 })
    vi.stubGlobal('fetch', fetchMock)

    const { result } = renderHook(() => useDashboardLayout())

    await waitFor(() => expect(result.current.loading).toBe(false))
    expect(result.current.error).toContain('500')
    expect(result.current.widgets).toBe(null)
  })

  it('saveLayout PUTs the widget list and updates state optimistically', async () => {
    const fetchMock = vi.fn()
      .mockResolvedValueOnce({ ok: true, json: async () => ({ widgets: [] }) })
      .mockResolvedValueOnce({ ok: true, json: async () => ({ widgets: [{ id: 'tanks', x: 0, y: 0, w: 4, h: 4 }], updated_at: '2026-01-01T00:00:00Z' }) })
    vi.stubGlobal('fetch', fetchMock)

    const { result } = renderHook(() => useDashboardLayout())
    await waitFor(() => expect(result.current.loading).toBe(false))

    const newLayout = [{ id: 'tanks', x: 0, y: 0, w: 4, h: 4 }] as const

    await act(async () => {
      await result.current.saveLayout(newLayout as any)
    })

    expect(fetchMock).toHaveBeenCalledWith('/api/dashboard-layout', expect.objectContaining({ method: 'PUT' }))
    expect(fetchMock).toHaveBeenCalledWith('/api/dashboard-layout', expect.objectContaining({
      body: JSON.stringify({ widgets: newLayout }),
    }))
    expect(result.current.widgets).toHaveLength(1)
    expect(result.current.widgets?.[0].id).toBe('tanks')
  })

  it('saveLayout sets saveError on failure without clearing widgets', async () => {
    const fetchMock = vi.fn()
      .mockResolvedValueOnce({ ok: true, json: async () => ({ widgets: [{ id: 'wind', x: 0, y: 0, w: 4, h: 8 }] }) })
      .mockResolvedValueOnce({ ok: false, status: 500 })
    vi.stubGlobal('fetch', fetchMock)

    const { result } = renderHook(() => useDashboardLayout())
    await waitFor(() => expect(result.current.loading).toBe(false))

    const newLayout = [{ id: 'tanks', x: 0, y: 0, w: 4, h: 4 }] as const

    await act(async () => {
      await result.current.saveLayout(newLayout as any)
    })

    expect(result.current.saveError).toBeTruthy()
    expect(typeof result.current.saveError).toBe('string')
    expect(result.current.widgets).not.toBe(null)
    expect(result.current.widgets).toHaveLength(1)
    expect(result.current.widgets?.[0].id).toBe('tanks')
  })
})
