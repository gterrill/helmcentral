import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { renderHook, act, waitFor } from '@testing-library/react'
import { toast } from 'sonner'
import { useDashboardRibbon } from '@/hooks/use-dashboard-ribbon'
import type { LampStripWidgetConfig } from '@/lib/dashboard-widgets'

vi.mock('sonner', () => ({ toast: { error: vi.fn() } }))

const sampleRibbon: LampStripWidgetConfig = {
  title: 'Indicators',
  lamps: [{ path: 'electrical.generator.state', label: 'GEN' }],
  showCheck: true,
}

describe('useDashboardRibbon', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('fetches the ribbon on mount', async () => {
    const fetchMock = vi.fn().mockResolvedValue({ ok: true, json: async () => ({ ribbon: sampleRibbon }) })
    vi.stubGlobal('fetch', fetchMock)

    const { result } = renderHook(() => useDashboardRibbon())

    await waitFor(() => expect(result.current.loading).toBe(false))
    expect(fetchMock).toHaveBeenCalledWith('/api/dashboard-ribbon')
    expect(result.current.ribbon).toEqual(sampleRibbon)
    expect(result.current.error).toBeNull()
  })

  it('reports null when nothing is pinned', async () => {
    const fetchMock = vi.fn().mockResolvedValue({ ok: true, json: async () => ({ ribbon: null }) })
    vi.stubGlobal('fetch', fetchMock)

    const { result } = renderHook(() => useDashboardRibbon())

    await waitFor(() => expect(result.current.loading).toBe(false))
    expect(result.current.ribbon).toBeNull()
  })

  it('sets an error message when the initial fetch fails', async () => {
    const fetchMock = vi.fn().mockResolvedValue({ ok: false, status: 500 })
    vi.stubGlobal('fetch', fetchMock)

    const { result } = renderHook(() => useDashboardRibbon())

    await waitFor(() => expect(result.current.loading).toBe(false))
    expect(result.current.error).toContain('500')
    expect(result.current.ribbon).toBeNull()
  })

  it('saveRibbon PUTs the config and adopts the server response', async () => {
    const fetchMock = vi.fn()
      .mockResolvedValueOnce({ ok: true, json: async () => ({ ribbon: null }) })
      .mockResolvedValueOnce({ ok: true, json: async () => ({ ribbon: sampleRibbon }) })
    vi.stubGlobal('fetch', fetchMock)

    const { result } = renderHook(() => useDashboardRibbon())
    await waitFor(() => expect(result.current.loading).toBe(false))

    let saved: boolean | undefined
    await act(async () => {
      saved = await result.current.saveRibbon(sampleRibbon)
    })

    expect(saved).toBe(true)
    expect(fetchMock).toHaveBeenLastCalledWith('/api/dashboard-ribbon', {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ ribbon: sampleRibbon }),
    })
    expect(result.current.ribbon).toEqual(sampleRibbon)
  })

  it('saveRibbon(null) clears the ribbon', async () => {
    const fetchMock = vi.fn()
      .mockResolvedValueOnce({ ok: true, json: async () => ({ ribbon: sampleRibbon }) })
      .mockResolvedValueOnce({ ok: true, json: async () => ({ ribbon: null }) })
    vi.stubGlobal('fetch', fetchMock)

    const { result } = renderHook(() => useDashboardRibbon())
    await waitFor(() => expect(result.current.loading).toBe(false))
    expect(result.current.ribbon).toEqual(sampleRibbon)

    let saved: boolean | undefined
    await act(async () => {
      saved = await result.current.saveRibbon(null)
    })

    expect(saved).toBe(true)
    expect(fetchMock).toHaveBeenLastCalledWith('/api/dashboard-ribbon', {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ ribbon: null }),
    })
    expect(result.current.ribbon).toBeNull()
  })

  it('toasts the server error message and returns false when saveRibbon is rejected', async () => {
    const fetchMock = vi.fn()
      .mockResolvedValueOnce({ ok: true, json: async () => ({ ribbon: null }) })
      .mockResolvedValueOnce({
        ok: false,
        status: 400,
        json: async () => ({ error: 'lamp strip needs at least one lamp or the check indicator: ribbon' }),
      })
    vi.stubGlobal('fetch', fetchMock)

    const { result } = renderHook(() => useDashboardRibbon())
    await waitFor(() => expect(result.current.loading).toBe(false))

    let saved: boolean | undefined
    await act(async () => {
      saved = await result.current.saveRibbon({ title: 'Status', lamps: [] })
    })

    expect(saved).toBe(false)
    expect(toast.error).toHaveBeenCalledWith(
      'Could not save the indicator ribbon',
      expect.objectContaining({ description: expect.stringContaining('lamp strip needs at least one lamp') }),
    )
    // A rejected save must not adopt the bad config into state.
    expect(result.current.ribbon).toBeNull()
  })

  it('toasts a status-based message without throwing when the failure body is not JSON', async () => {
    const fetchMock = vi.fn()
      .mockResolvedValueOnce({ ok: true, json: async () => ({ ribbon: null }) })
      .mockResolvedValueOnce({
        ok: false,
        status: 500,
        json: async () => { throw new SyntaxError('Unexpected token < in JSON') },
      })
    vi.stubGlobal('fetch', fetchMock)

    const { result } = renderHook(() => useDashboardRibbon())
    await waitFor(() => expect(result.current.loading).toBe(false))

    await expect(act(async () => {
      await result.current.saveRibbon(sampleRibbon)
    })).resolves.not.toThrow()

    expect(toast.error).toHaveBeenCalledWith(
      'Could not save the indicator ribbon',
      expect.objectContaining({ description: expect.stringContaining('500') }),
    )
  })
})
