import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { renderHook, act, waitFor } from '@testing-library/react'
import { toast } from 'sonner'
import { useDashboardPages } from '@/hooks/use-dashboard-pages'

vi.mock('sonner', () => ({ toast: { error: vi.fn() } }))

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

  it('createPage POSTs and appends the new page', async () => {
    const fetchMock = vi.fn()
      .mockResolvedValueOnce({ ok: true, json: async () => ({ pages: [] }) })
      .mockResolvedValueOnce({ ok: true, json: async () => ({ id: '2', name: 'New Page', widgets: [], created_at: '', updated_at: '' }) })
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

  it('updatePage PATCHes and patches the page in place', async () => {
    const fetchMock = vi.fn()
      .mockResolvedValueOnce({ ok: true, json: async () => ({ pages: [{ id: '1', name: 'Page A', widgets: [], created_at: '', updated_at: '' }] }) })
      .mockResolvedValueOnce({ ok: true, json: async () => ({ id: '1', name: 'Renamed', widgets: [], created_at: '', updated_at: '' }) })
    vi.stubGlobal('fetch', fetchMock)

    const { result } = renderHook(() => useDashboardPages())
    await waitFor(() => expect(result.current.loading).toBe(false))

    await act(async () => {
      await result.current.updatePage('1', { name: 'Renamed' })
    })

    expect(fetchMock).toHaveBeenCalledWith('/api/dashboard-pages/1', expect.objectContaining({ method: 'PATCH' }))
    expect(result.current.pages[0].name).toBe('Renamed')
  })

  it('deletePage issues a DELETE request and removes the page locally', async () => {
    const fetchMock = vi.fn()
      .mockResolvedValueOnce({ ok: true, json: async () => ({ pages: [{ id: '1', name: 'Page A', widgets: [], created_at: '', updated_at: '' }] }) })
      .mockResolvedValueOnce({ ok: true })
    vi.stubGlobal('fetch', fetchMock)

    const { result } = renderHook(() => useDashboardPages())
    await waitFor(() => expect(result.current.loading).toBe(false))

    await act(async () => {
      await result.current.deletePage('1')
    })

    expect(fetchMock).toHaveBeenCalledWith('/api/dashboard-pages/1', expect.objectContaining({ method: 'DELETE' }))
    expect(result.current.pages).toHaveLength(0)
  })

  it('toasts the server error message when a layout save (updatePage) is rejected, instead of failing silently', async () => {
    const fetchMock = vi.fn()
      .mockResolvedValueOnce({ ok: true, json: async () => ({ pages: [{ id: '1', name: 'Page A', widgets: [], created_at: '', updated_at: '' }] }) })
      .mockResolvedValueOnce({
        ok: false,
        status: 400,
        json: async () => ({ error: 'embed widget requires embed config: embed:abc' }),
      })
    vi.stubGlobal('fetch', fetchMock)

    const { result } = renderHook(() => useDashboardPages())
    await waitFor(() => expect(result.current.loading).toBe(false))

    await act(async () => {
      await result.current.updatePage('1', { widgets: [] })
    })

    expect(toast.error).toHaveBeenCalledWith(
      'Could not save dashboard',
      expect.objectContaining({ description: expect.stringContaining('embed widget requires embed config: embed:abc') }),
    )
  })

  it('does not set the hook error state on a failed updatePage, so the dashboard is not blanked by !pagesError gating', async () => {
    const fetchMock = vi.fn()
      .mockResolvedValueOnce({ ok: true, json: async () => ({ pages: [{ id: '1', name: 'Page A', widgets: [], created_at: '', updated_at: '' }] }) })
      .mockResolvedValueOnce({
        ok: false,
        status: 400,
        json: async () => ({ error: 'embed widget requires embed config: embed:abc' }),
      })
    vi.stubGlobal('fetch', fetchMock)

    const { result } = renderHook(() => useDashboardPages())
    await waitFor(() => expect(result.current.loading).toBe(false))

    await act(async () => {
      await result.current.updatePage('1', { widgets: [] })
    })

    expect(result.current.error).toBeNull()
  })

  it('still resolves a failed updatePage to null', async () => {
    const fetchMock = vi.fn()
      .mockResolvedValueOnce({ ok: true, json: async () => ({ pages: [{ id: '1', name: 'Page A', widgets: [], created_at: '', updated_at: '' }] }) })
      .mockResolvedValueOnce({
        ok: false,
        status: 400,
        json: async () => ({ error: 'embed widget requires embed config: embed:abc' }),
      })
    vi.stubGlobal('fetch', fetchMock)

    const { result } = renderHook(() => useDashboardPages())
    await waitFor(() => expect(result.current.loading).toBe(false))

    let returned: unknown
    await act(async () => {
      returned = await result.current.updatePage('1', { widgets: [] })
    })

    expect(returned).toBeNull()
  })

  it('does not toast on a successful updatePage', async () => {
    const fetchMock = vi.fn()
      .mockResolvedValueOnce({ ok: true, json: async () => ({ pages: [{ id: '1', name: 'Page A', widgets: [], created_at: '', updated_at: '' }] }) })
      .mockResolvedValueOnce({ ok: true, json: async () => ({ id: '1', name: 'Renamed', widgets: [], created_at: '', updated_at: '' }) })
    vi.stubGlobal('fetch', fetchMock)

    const { result } = renderHook(() => useDashboardPages())
    await waitFor(() => expect(result.current.loading).toBe(false))

    await act(async () => {
      await result.current.updatePage('1', { name: 'Renamed' })
    })

    expect(toast.error).not.toHaveBeenCalled()
  })

  it('toasts and still returns false when deletePage fails', async () => {
    const fetchMock = vi.fn()
      .mockResolvedValueOnce({ ok: true, json: async () => ({ pages: [{ id: '1', name: 'Page A', widgets: [], created_at: '', updated_at: '' }] }) })
      .mockResolvedValueOnce({
        ok: false,
        status: 400,
        json: async () => ({ error: 'page is the last remaining page' }),
      })
    vi.stubGlobal('fetch', fetchMock)

    const { result } = renderHook(() => useDashboardPages())
    await waitFor(() => expect(result.current.loading).toBe(false))

    let returned: unknown
    await act(async () => {
      returned = await result.current.deletePage('1')
    })

    expect(returned).toBe(false)
    expect(toast.error).toHaveBeenCalledWith(
      'Could not delete page',
      expect.objectContaining({ description: expect.stringContaining('page is the last remaining page') }),
    )
  })

  it('toasts a status-based message without throwing when the failure body is not JSON', async () => {
    const fetchMock = vi.fn()
      .mockResolvedValueOnce({ ok: true, json: async () => ({ pages: [{ id: '1', name: 'Page A', widgets: [], created_at: '', updated_at: '' }] }) })
      .mockResolvedValueOnce({
        ok: false,
        status: 500,
        json: async () => { throw new SyntaxError('Unexpected token < in JSON') },
      })
    vi.stubGlobal('fetch', fetchMock)

    const { result } = renderHook(() => useDashboardPages())
    await waitFor(() => expect(result.current.loading).toBe(false))

    await expect(act(async () => {
      await result.current.updatePage('1', { widgets: [] })
    })).resolves.not.toThrow()

    expect(toast.error).toHaveBeenCalledWith(
      'Could not save dashboard',
      expect.objectContaining({ description: expect.stringContaining('500') }),
    )
  })
})
