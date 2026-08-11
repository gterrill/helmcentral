import { describe, it, expect, vi, afterEach } from 'vitest'
import { renderHook, waitFor } from '@testing-library/react'
import { apiBaseUrl } from '@/config/api'
import { useSecretsStatus } from '@/hooks/use-secrets-status'
import { useSignalKDiscovery } from '@/hooks/use-signalk-discovery'

// The shipped artifacts — the release binary with the frontend go:embed-ed into
// it, and the Docker image — always serve the SPA and the API from the same
// origin, on whatever port the operator chose via PORT. A base URL that pins
// port 8080 breaks every install that isn't on 8080 and every reverse-proxied
// one, so same-origin relative paths are the only correct default. Dev keeps
// working through vite.config.ts's /api proxy.
describe('API base URL', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('defaults to same-origin so requests are relative', () => {
    expect(apiBaseUrl).toBe('')
  })

  it('issues relative /api requests rather than pinning a port', async () => {
    const fetchMock = vi.fn(async () => ({
      ok: true,
      json: async () => ({}),
    }))
    vi.stubGlobal('fetch', fetchMock)

    const { result } = renderHook(() => useSecretsStatus())
    await waitFor(() => expect(result.current.loading).toBe(false))

    expect(fetchMock).toHaveBeenCalled()
    for (const [url] of fetchMock.mock.calls as unknown as [string][]) {
      expect(url).toMatch(/^\/api\//)
    }
  })

  it('posts SignalK discovery to a relative path', async () => {
    const fetchMock = vi.fn(async () => ({
      ok: true,
      json: async () => ({ servers: [], scanned_subnet: '192.168.1.0/24' }),
    }))
    vi.stubGlobal('fetch', fetchMock)

    const { result } = renderHook(() => useSignalKDiscovery())
    await result.current.discover()

    const [url] = fetchMock.mock.calls[0] as unknown as [string]
    expect(url).toBe('/api/signalk/discover')
  })
})
