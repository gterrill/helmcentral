import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { renderHook, act, waitFor } from '@testing-library/react'
import { useDashboardPages } from '@/hooks/use-dashboard-pages'

describe('useDashboardPages', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('fetches the page list on mount', async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      json: async () => ({ pages: [{ id: '1', name: 'Page A', widgets: [], created_at: '', updated_at: '' }] }),
    })
    vi.stubGlobal('fetch', fetchMock)

    const { result } = renderHook(() => useDashboardPages())

    await waitFor(() => expect(result.current.loading).toBe(false))
    expect(fetchMock).toHaveBeenCalledWith('/api/dashboard-pages')
    expect(result.current.pages).toHaveLength(1)
    expect(result.current.pages[0].name).toBe('Page A')
  })

  it('sets an error message when the fetch fails', async () => {
    const fetchMock = vi.fn().mockResolvedValue({ ok: false, status: 500 })
    vi.stubGlobal('fetch', fetchMock)

    const { result } = renderHook(() => useDashboardPages())

    await waitFor(() => expect(result.current.loading).toBe(false))
    expect(result.current.error).toContain('500')
    expect(result.current.pages).toHaveLength(0)
  })

  it('createPage POSTs and refetches the list', async () => {
    const fetchMock = vi.fn()
      .mockResolvedValueOnce({ ok: true, json: async () => ({ pages: [] }) })
      .mockResolvedValueOnce({ ok: true, json: async () => ({ id: '2', name: 'New Page', widgets: [], created_at: '', updated_at: '' }) })
      .mockResolvedValueOnce({ ok: true, json: async () => ({ pages: [{ id: '2', name: 'New Page', widgets: [], created_at: '', updated_at: '' }] }) })
    vi.stubGlobal('fetch', fetchMock)

    const { result } = renderHook(() => useDashboardPages())
    await waitFor(() => expect(result.current.loading).toBe(false))

    await act(async () => {
      await result.current.createPage('New Page', [])
    })

    expect(fetchMock).toHaveBeenCalledWith('/api/dashboard-pages', expect.objectContaining({ method: 'POST' }))
    expect(result.current.pages).toHaveLength(1)
    expect(result.current.pages[0].name).toBe('New Page')
  })

  it('updatePage PATCHes and refetches the list', async () => {
    const fetchMock = vi.fn()
      .mockResolvedValueOnce({ ok: true, json: async () => ({ pages: [{ id: '1', name: 'Page A', widgets: [], created_at: '', updated_at: '' }] }) })
      .mockResolvedValueOnce({ ok: true, json: async () => ({ id: '1', name: 'Renamed', widgets: [], created_at: '', updated_at: '' }) })
      .mockResolvedValueOnce({ ok: true, json: async () => ({ pages: [{ id: '1', name: 'Renamed', widgets: [], created_at: '', updated_at: '' }] }) })
    vi.stubGlobal('fetch', fetchMock)

    const { result } = renderHook(() => useDashboardPages())
    await waitFor(() => expect(result.current.loading).toBe(false))

    await act(async () => {
      await result.current.updatePage('1', { name: 'Renamed' })
    })

    expect(fetchMock).toHaveBeenCalledWith('/api/dashboard-pages/1', expect.objectContaining({ method: 'PATCH' }))
    expect(result.current.pages[0].name).toBe('Renamed')
  })

  it('deletePage issues a DELETE request and refetches', async () => {
    const fetchMock = vi.fn()
      .mockResolvedValueOnce({ ok: true, json: async () => ({ pages: [{ id: '1', name: 'Page A', widgets: [], created_at: '', updated_at: '' }] }) })
      .mockResolvedValueOnce({ ok: true })
      .mockResolvedValueOnce({ ok: true, json: async () => ({ pages: [] }) })
    vi.stubGlobal('fetch', fetchMock)

    const { result } = renderHook(() => useDashboardPages())
    await waitFor(() => expect(result.current.loading).toBe(false))

    await act(async () => {
      await result.current.deletePage('1')
    })

    expect(fetchMock).toHaveBeenCalledWith('/api/dashboard-pages/1', expect.objectContaining({ method: 'DELETE' }))
    expect(result.current.pages).toHaveLength(0)
  })
})
