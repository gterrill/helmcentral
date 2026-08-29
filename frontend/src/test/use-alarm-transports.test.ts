/**
 * The load path used to drop a non-ok response on the floor: config stayed
 * at emptyTransportConfig() with `error` null, which reads as "loaded, and
 * nothing is configured" and is indistinguishable from that being the truth.
 * The fallback policy rules that out — a failed upstream read is surfaced,
 * not smoothed over — and the settings page now leans on the distinction,
 * since it refuses to save a draft that was never seeded from the server.
 */
import { describe, it, expect, vi, afterEach } from 'vitest'
import { act, renderHook, waitFor } from '@testing-library/react'

import { useAlarmTransports, type AlarmTransportConfig } from '@/hooks/use-alarm-transports'

const CONFIG: Partial<AlarmTransportConfig> = {
  ntfy: { enabled: true, server: 'https://ntfy.sh', topic: 'pikorua-alarms' },
}

interface Route { ok: boolean; status?: number; body?: unknown }

/** Routes are keyed "METHOD /path" so a GET and a POST to the same path can differ. */
function stubFetch(routes: Record<string, Route>) {
  const fetchMock = vi.fn(async (url: string, init?: { method?: string }) => {
    const method = (init?.method ?? 'GET').toUpperCase()
    const key = Object.keys(routes).find((candidate) => {
      const [routeMethod, path] = candidate.split(' ')
      return routeMethod === method && url.includes(path)
    })
    if (!key) throw new Error(`unstubbed ${method} ${url}`)
    const route = routes[key]
    return { ok: route.ok, status: route.status ?? (route.ok ? 200 : 500), json: async () => route.body ?? {} }
  })
  vi.stubGlobal('fetch', fetchMock)
  return fetchMock
}

const OK_SECRETS: Route = { ok: true, body: { NTFY_TOKEN: true } }

describe('useAlarmTransports load', () => {
  afterEach(() => { vi.unstubAllGlobals() })

  it('reports loaded with the server config on success', async () => {
    stubFetch({ 'GET /api/alarm-transports': { ok: true, body: CONFIG }, 'GET /api/settings/secrets': OK_SECRETS })

    const { result } = renderHook(() => useAlarmTransports())

    await waitFor(() => expect(result.current.loading).toBe(false))
    expect(result.current.loaded).toBe(true)
    expect(result.current.error).toBeNull()
    expect(result.current.config.ntfy.topic).toBe('pikorua-alarms')
    // Absent keys still come back filled in from the empty config.
    expect(result.current.config.watchdog).toEqual({ stream_silence_seconds: 0, heartbeat_minutes: 0 })
    expect(result.current.secretsPresent).toEqual({ NTFY_TOKEN: true })
  })

  it('surfaces a non-ok config response rather than passing off an empty config as loaded', async () => {
    stubFetch({
      'GET /api/alarm-transports': { ok: false, status: 500 },
      'GET /api/settings/secrets': OK_SECRETS,
    })

    const { result } = renderHook(() => useAlarmTransports())

    await waitFor(() => expect(result.current.loading).toBe(false))
    expect(result.current.error).toMatch(/500/)
    expect(result.current.loaded).toBe(false)
  })

  it('surfaces a non-ok secrets response too', async () => {
    stubFetch({
      'GET /api/alarm-transports': { ok: true, body: CONFIG },
      'GET /api/settings/secrets': { ok: false, status: 403 },
    })

    const { result } = renderHook(() => useAlarmTransports())

    await waitFor(() => expect(result.current.loading).toBe(false))
    expect(result.current.error).toMatch(/403/)
    expect(result.current.loaded).toBe(false)
  })

  it('surfaces a transport-level failure', async () => {
    vi.stubGlobal('fetch', vi.fn().mockRejectedValue(new Error('network down')))

    const { result } = renderHook(() => useAlarmTransports())

    await waitFor(() => expect(result.current.loading).toBe(false))
    expect(result.current.error).toBe('network down')
    expect(result.current.loaded).toBe(false)
  })

  /**
   * `loaded` answers "is there a server copy under this draft", which is not
   * the same question as "did the last request work". A refresh that fails
   * after a good load leaves the last good config in place, so the settings
   * page can still save against it.
   */
  it('keeps loaded and the last good config when a later refresh fails', async () => {
    const routes: Record<string, Route> = {
      'GET /api/alarm-transports': { ok: true, body: CONFIG },
      'GET /api/settings/secrets': OK_SECRETS,
      'POST /api/alarm-transports': { ok: true },
    }
    stubFetch(routes)

    const { result } = renderHook(() => useAlarmTransports())
    await waitFor(() => expect(result.current.loaded).toBe(true))

    // The write lands; it is the refetch behind it that fails.
    routes['GET /api/alarm-transports'] = { ok: false, status: 502 }
    await act(async () => { await result.current.save(result.current.config, {}) })

    await waitFor(() => expect(result.current.error).toMatch(/502/))
    expect(result.current.loaded).toBe(true)
    expect(result.current.config.ntfy.topic).toBe('pikorua-alarms')
  })
})
