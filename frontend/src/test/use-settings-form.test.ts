import { describe, it, expect, vi, afterEach } from 'vitest'
import { renderHook, act, waitFor } from '@testing-library/react'
import { deepMergeSettings, useSettingsForm } from '@/hooks/use-settings-form'
import { useOfflineCoastline } from '@/hooks/use-offline-coastline'
import { useTideSettings } from '@/hooks/use-tide-settings'
import { stubApi, type StubRequest } from '@/test/helpers/stub-api'

describe('useSettingsForm', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('a save reaches a mounted tide settings reader without a reload', async () => {
    stubApi({
      'GET /api/settings': { ui: { tide_provider: 'noaa', tide_station_id: 'A', tide_station_name: 'Alpha' } },
      'POST /api/settings': (req: StubRequest) => req.body,
    })
    const tide = renderHook(() => useTideSettings())
    const form = renderHook(() => useSettingsForm())
    await waitFor(() => expect(tide.result.current.tideProvider).toBe('noaa'))
    await waitFor(() => expect(form.result.current.loading).toBe(false))

    await act(async () => { await form.result.current.save({ ui: { tide_provider: 'bom', tide_station_id: 'B' } }) })
    expect(tide.result.current.tideProvider).toBe('bom')
    expect(tide.result.current.tideStationId).toBe('B')
  })

  it('a save refetches the offline coastline status at once, not at the next poll', async () => {
    let saved = false
    stubApi({
      'GET /api/settings': { offline_coastline: { enabled: false } },
      'POST /api/settings': (req: StubRequest) => {
        saved = true
        return req.body
      },
      'GET /api/offline-coastline': () => ({ enabled: saved, state: saved ? 'idle' : 'off', squares: [] }),
    })
    const coast = renderHook(() => useOfflineCoastline())
    const form = renderHook(() => useSettingsForm())
    await waitFor(() => expect(coast.result.current.status?.state).toBe('off'))
    await waitFor(() => expect(form.result.current.loading).toBe(false))

    await act(async () => { await form.result.current.save({ offline_coastline: { enabled: true } }) })
    await waitFor(() => expect(coast.result.current.status?.state).toBe('idle'), { timeout: 1500 })
  })

  it('reports loaded only when a settings read succeeded', async () => {
    stubApi({ 'GET /api/settings': { signalk: { address: '10.0.0.5', port: 3000 } } })
    const ok = renderHook(() => useSettingsForm())
    await waitFor(() => expect(ok.result.current.loading).toBe(false))
    expect(ok.result.current.loaded).toBe(true)
    ok.unmount()

    vi.stubGlobal('fetch', vi.fn().mockRejectedValue(new Error('backend restarting')))
    const failed = renderHook(() => useSettingsForm())
    await waitFor(() => expect(failed.result.current.loading).toBe(false))
    expect(failed.result.current.loaded).toBe(false)
  })

  describe('when the first read fails', () => {
    afterEach(() => {
      vi.useRealTimers()
    })

    // Reads fail while the backend restarts. Without a retry, `loaded` would
    // stay false until the page was reloaded.
    function failingThenWorking(succeedFromAttempt: number) {
      let attempts = 0
      const fetchMock = vi.fn(async (url: string, init?: RequestInit) => {
        if (init?.method === 'POST') return { ok: true, json: async () => ({ signalk: { address: 'saved.local', port: 3000 } }) }
        if (String(url).endsWith('/api/settings')) {
          attempts += 1
          if (attempts >= succeedFromAttempt) return { ok: true, json: async () => ({ signalk: { address: '10.0.0.5', port: 3000 } }) }
        }
        return { ok: false, json: async () => ({}) }
      })
      vi.stubGlobal('fetch', fetchMock)
      return { fetchMock, attempts: () => attempts }
    }

    it('retries after 5 s, then 15 s, then every 60 s, and stops once loaded', async () => {
      vi.useFakeTimers()
      const { attempts } = failingThenWorking(5)
      const { result } = renderHook(() => useSettingsForm())
      const advance = (ms: number) => act(async () => { await vi.advanceTimersByTimeAsync(ms) })

      await advance(0)
      expect(attempts()).toBe(1)
      expect(result.current.loading).toBe(false)
      expect(result.current.loaded).toBe(false)

      await advance(4_999)
      expect(attempts()).toBe(1)
      await advance(1)
      expect(attempts()).toBe(2)

      await advance(14_999)
      expect(attempts()).toBe(2)
      await advance(1)
      expect(attempts()).toBe(3)

      await advance(59_999)
      expect(attempts()).toBe(3)
      await advance(1)
      expect(attempts()).toBe(4)

      await advance(60_000)
      expect(attempts()).toBe(5)
      expect(result.current.loaded).toBe(true)
      expect(result.current.settings.signalk?.address).toBe('10.0.0.5')

      await advance(300_000)
      expect(attempts()).toBe(5)
    })

    it('stops retrying when unmounted', async () => {
      vi.useFakeTimers()
      const { attempts } = failingThenWorking(99)
      const { unmount } = renderHook(() => useSettingsForm())
      await act(async () => { await vi.advanceTimersByTimeAsync(0) })
      unmount()
      await act(async () => { await vi.advanceTimersByTimeAsync(300_000) })
      expect(attempts()).toBe(1)
    })

    it('counts a successful save as loaded and stops retrying', async () => {
      vi.useFakeTimers()
      const { attempts } = failingThenWorking(99)
      const { result } = renderHook(() => useSettingsForm())
      await act(async () => { await vi.advanceTimersByTimeAsync(0) })
      expect(result.current.loaded).toBe(false)

      await act(async () => { await result.current.save({ signalk: { address: 'saved.local' } }) })
      expect(result.current.loaded).toBe(true)

      await act(async () => { await vi.advanceTimersByTimeAsync(300_000) })
      expect(attempts()).toBe(1)
    })
  })

  it('fetches settings on mount and populates state', async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      json: async () => ({
        signalk: { address: 'localhost', port: 3000 },
        ui: { tide_station_id: 'QLD_TP033' },
      }),
    })
    vi.stubGlobal('fetch', fetchMock)

    const { result } = renderHook(() => useSettingsForm())

    await waitFor(() => expect(result.current.loading).toBe(false))
    expect(result.current.settings.signalk?.address).toBe('localhost')
    expect(result.current.settings.ui?.tide_station_id).toBe('QLD_TP033')
    expect(fetchMock).toHaveBeenCalledWith(expect.stringContaining('/api/settings'), expect.anything())
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
    expect(fetchMock).toHaveBeenCalledWith(expect.stringContaining('/api/settings/signalk'), expect.anything())
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
            tide_station_id: 'QLD_TP033',
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
    expect(body.ui.tide_station_id).toBe('QLD_TP033')
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

  // ADR 0093: the assistant settings block sits alongside `ui` at the top
  // level of SettingsPayload. deepMergeSettings merges one level deep PER
  // top-level key, so a patch that only touches `ui` must leave `assistant`
  // (including its standing notes) untouched rather than wiping it out.
  it('deepMergeSettings preserves assistant.notes when the patch only touches ui', () => {
    const current = {
      ui: { tide_provider: 'bom' },
      assistant: { enabled: true, model: 'anthropic/claude-sonnet-4.5', notes: 'Queenfish on a rising tide.' },
    }

    const merged = deepMergeSettings(current, { ui: { tide_provider: 'noaa' } })

    expect(merged.ui?.tide_provider).toBe('noaa')
    expect(merged.assistant).toEqual(current.assistant)
  })
})
