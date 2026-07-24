import { describe, it, expect, vi, afterEach } from 'vitest'
import { renderHook, act, waitFor } from '@testing-library/react'
import { useSettingsForm } from '@/hooks/use-settings-form'

describe('useSettingsForm', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('fetches settings on mount and populates state', async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      json: async () => ({
        signalk: { address: 'localhost', port: 3000 },
        ui: { tank_labels: { 'fuel.2': 'Port Fuel' } },
      }),
    })
    vi.stubGlobal('fetch', fetchMock)

    const { result } = renderHook(() => useSettingsForm())

    await waitFor(() => expect(result.current.loading).toBe(false))
    expect(result.current.settings.signalk?.address).toBe('localhost')
    expect(result.current.settings.ui?.tank_labels).toEqual({ 'fuel.2': 'Port Fuel' })
    expect(fetchMock).toHaveBeenCalledWith(expect.stringContaining('/api/settings'))
  })

  it('falls back to GET /api/settings/signalk when GET /api/settings fails', async () => {
    const fetchMock = vi.fn(async (url: string) => {
      if (url.includes('/api/settings/signalk')) {
        return { ok: true, json: async () => ({ address: 'boat.local', port: 3001 }) }
      }
      if (url.includes('/api/settings')) {
        return { ok: false, json: async () => ({}) }
      }
      return { ok: false, json: async () => ({}) }
    })
    vi.stubGlobal('fetch', fetchMock)

    const { result } = renderHook(() => useSettingsForm())

    await waitFor(() => expect(result.current.loading).toBe(false))
    expect(result.current.settings.signalk).toEqual({ address: 'boat.local', port: 3001 })
    expect(fetchMock).toHaveBeenCalledWith(expect.stringContaining('/api/settings/signalk'))
  })

  it('save(patch) deep-merges into current state and POSTs the FULL merged object', async () => {
    const fetchMock = vi.fn(async (_url: string, options?: { method?: string; body?: string }) => {
      if (options?.method === 'POST') {
        return { ok: true, json: async () => JSON.parse(options.body ?? '{}') }
      }
      return {
        ok: true,
        json: async () => ({
          ui: {
            tank_labels: { 'fuel.2': 'Port Fuel' },
            forecast_refresh_seconds: 3600,
          },
        }),
      }
    })
    vi.stubGlobal('fetch', fetchMock)

    const { result } = renderHook(() => useSettingsForm())
    await waitFor(() => expect(result.current.loading).toBe(false))

    await act(async () => {
      await result.current.save({ ui: { tide_provider: 'bom' } })
    })

    const postCall = fetchMock.mock.calls.find(([, options]) => options?.method === 'POST')
    expect(postCall).toBeTruthy()
    const body = JSON.parse((postCall![1] as { body: string }).body)

    // The patch's field made it into the POST body...
    expect(body.ui.tide_provider).toBe('bom')
    // ...and fields already in state that were NOT part of the patch were
    // NOT dropped — the POST body is the full merged object, not a bare
    // patch.
    expect(body.ui.tank_labels).toEqual({ 'fuel.2': 'Port Fuel' })
    expect(body.ui.forecast_refresh_seconds).toBe(3600)
  })

  it('surfaces a save failure via the error field', async () => {
    const fetchMock = vi.fn(async (_url: string, options?: { method?: string }) => {
      if (options?.method === 'POST') {
        return { ok: false, json: async () => ({ error: 'Database connection failed' }) }
      }
      return { ok: true, json: async () => ({}) }
    })
    vi.stubGlobal('fetch', fetchMock)

    const { result } = renderHook(() => useSettingsForm())
    await waitFor(() => expect(result.current.loading).toBe(false))

    await act(async () => {
      await result.current.save({ ui: { tide_provider: 'bom' } }).catch(() => {})
    })

    expect(result.current.error).toBe('Database connection failed')
  })
})
