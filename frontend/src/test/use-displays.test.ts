import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { renderHook, act, waitFor } from '@testing-library/react'
import { toast } from 'sonner'
import { useDisplays } from '@/hooks/use-displays'

vi.mock('sonner', () => ({ toast: { error: vi.fn() } }))

// Modelled directly on use-dashboard-pages.test.ts (ADR 0110 §6: "The
// displays hook" is built the same way, for the same reasons).

const flybridge = {
  id: 'd1', name: 'Flybridge', slug: 'flybridge',
  width: 1920, height: 360, scale: 1, rotate: 180 as const,
  pixel_shift: false, wake_lock: false,
  created_at: '', updated_at: '',
}

describe('useDisplays', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('fetches the display list on mount', async () => {
    const fetchMock = vi.fn().mockResolvedValue({ ok: true, json: async () => ({ displays: [flybridge] }) })
    vi.stubGlobal('fetch', fetchMock)

    const { result } = renderHook(() => useDisplays())

    await waitFor(() => expect(result.current.loading).toBe(false))
    expect(fetchMock).toHaveBeenCalledWith('/api/displays')
    expect(result.current.displays).toEqual([flybridge])
    expect(result.current.error).toBeNull()
  })

  it('sets an error message when the initial fetch fails', async () => {
    const fetchMock = vi.fn().mockResolvedValue({ ok: false, status: 500 })
    vi.stubGlobal('fetch', fetchMock)

    const { result } = renderHook(() => useDisplays())

    await waitFor(() => expect(result.current.loading).toBe(false))
    expect(result.current.error).toContain('500')
    expect(result.current.displays).toHaveLength(0)
  })

  it('tolerates a missing displays array in the response', async () => {
    const fetchMock = vi.fn().mockResolvedValue({ ok: true, json: async () => ({}) })
    vi.stubGlobal('fetch', fetchMock)

    const { result } = renderHook(() => useDisplays())

    await waitFor(() => expect(result.current.loading).toBe(false))
    expect(result.current.displays).toEqual([])
  })

  it('createDisplay POSTs and appends the new display', async () => {
    const fetchMock = vi.fn()
      .mockResolvedValueOnce({ ok: true, json: async () => ({ displays: [] }) })
      .mockResolvedValueOnce({ ok: true, json: async () => flybridge })
    vi.stubGlobal('fetch', fetchMock)

    const { result } = renderHook(() => useDisplays())
    await waitFor(() => expect(result.current.loading).toBe(false))

    let created: unknown
    await act(async () => {
      created = await result.current.createDisplay({ name: 'Flybridge' })
    })

    expect(fetchMock).toHaveBeenLastCalledWith('/api/displays', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ name: 'Flybridge' }),
    })
    expect(created).toEqual(flybridge)
    expect(result.current.displays).toEqual([flybridge])
  })

  it('toasts and returns null when createDisplay fails, without setting the hook error', async () => {
    const fetchMock = vi.fn()
      .mockResolvedValueOnce({ ok: true, json: async () => ({ displays: [] }) })
      .mockResolvedValueOnce({ ok: false, status: 400, json: async () => ({ error: 'slug already in use' }) })
    vi.stubGlobal('fetch', fetchMock)

    const { result } = renderHook(() => useDisplays())
    await waitFor(() => expect(result.current.loading).toBe(false))

    let created: unknown
    await act(async () => {
      created = await result.current.createDisplay({ name: 'Flybridge', slug: 'flybridge' })
    })

    expect(created).toBeNull()
    expect(result.current.error).toBeNull()
    expect(toast.error).toHaveBeenCalledWith('Could not create display', expect.objectContaining({ description: 'slug already in use' }))
  })

  it('updateDisplay PATCHes and patches the display in place', async () => {
    const updated = { ...flybridge, scale: 1.5 }
    const fetchMock = vi.fn()
      .mockResolvedValueOnce({ ok: true, json: async () => ({ displays: [flybridge] }) })
      .mockResolvedValueOnce({ ok: true, json: async () => updated })
    vi.stubGlobal('fetch', fetchMock)

    const { result } = renderHook(() => useDisplays())
    await waitFor(() => expect(result.current.loading).toBe(false))

    await act(async () => {
      await result.current.updateDisplay('d1', { scale: 1.5 })
    })

    expect(fetchMock).toHaveBeenLastCalledWith('/api/displays/d1', {
      method: 'PATCH',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ scale: 1.5 }),
    })
    expect(result.current.displays).toEqual([updated])
  })

  it('toasts and returns null when updateDisplay fails, without setting the hook error (reserved for the initial load)', async () => {
    const fetchMock = vi.fn()
      .mockResolvedValueOnce({ ok: true, json: async () => ({ displays: [flybridge] }) })
      .mockResolvedValueOnce({ ok: false, status: 400, json: async () => ({ error: 'width and height must both be zero or both in range' }) })
    vi.stubGlobal('fetch', fetchMock)

    const { result } = renderHook(() => useDisplays())
    await waitFor(() => expect(result.current.loading).toBe(false))

    let updated: unknown
    await act(async () => {
      updated = await result.current.updateDisplay('d1', { width: 0 })
    })

    expect(updated).toBeNull()
    expect(result.current.error).toBeNull()
    expect(result.current.displays).toEqual([flybridge])
    expect(toast.error).toHaveBeenCalledWith('Could not save display', expect.objectContaining({
      description: 'width and height must both be zero or both in range',
    }))
  })

  it('deleteDisplay issues a DELETE, removes the display locally, and returns the released page ids', async () => {
    const fetchMock = vi.fn()
      .mockResolvedValueOnce({ ok: true, json: async () => ({ displays: [flybridge] }) })
      .mockResolvedValueOnce({ ok: true, json: async () => ({ status: 'deleted', released_page_ids: ['p1', 'p2'] }) })
    vi.stubGlobal('fetch', fetchMock)

    const { result } = renderHook(() => useDisplays())
    await waitFor(() => expect(result.current.loading).toBe(false))

    let released: unknown
    await act(async () => {
      released = await result.current.deleteDisplay('d1')
    })

    expect(fetchMock).toHaveBeenLastCalledWith('/api/displays/d1', { method: 'DELETE' })
    expect(released).toEqual(['p1', 'p2'])
    expect(result.current.displays).toEqual([])
  })

  it('toasts and returns null when deleteDisplay fails, leaving the display in place', async () => {
    const fetchMock = vi.fn()
      .mockResolvedValueOnce({ ok: true, json: async () => ({ displays: [flybridge] }) })
      .mockResolvedValueOnce({ ok: false, status: 500 })
    vi.stubGlobal('fetch', fetchMock)

    const { result } = renderHook(() => useDisplays())
    await waitFor(() => expect(result.current.loading).toBe(false))

    let released: unknown
    await act(async () => {
      released = await result.current.deleteDisplay('d1')
    })

    expect(released).toBeNull()
    expect(result.current.displays).toEqual([flybridge])
    expect(toast.error).toHaveBeenCalledWith('Could not delete display', expect.objectContaining({ description: expect.any(String) }))
  })

  it('refetch re-fetches the list', async () => {
    const fetchMock = vi.fn()
      .mockResolvedValueOnce({ ok: true, json: async () => ({ displays: [] }) })
      .mockResolvedValueOnce({ ok: true, json: async () => ({ displays: [flybridge] }) })
    vi.stubGlobal('fetch', fetchMock)

    const { result } = renderHook(() => useDisplays())
    await waitFor(() => expect(result.current.loading).toBe(false))
    expect(result.current.displays).toEqual([])

    await act(async () => {
      await result.current.refetch()
    })

    expect(fetchMock).toHaveBeenCalledTimes(2)
    expect(result.current.displays).toEqual([flybridge])
  })
})
