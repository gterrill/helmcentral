import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

import { describe, it, expect, vi, afterEach } from 'vitest'
import { renderHook, waitFor } from '@testing-library/react'

// Phase 0 fixture: the real /capabilities response captured from the boat's
// mayara instance (backend/testdata/mayara/README.md), same fixture
// radar-echo-legend.test.ts parses directly. Read fresh here too, via
// node:fs from import.meta.url, rather than inlined -- an inlined copy is
// exactly how this integration has drifted from the wire before (the plan's
// Phase 0 section lists four such cases).
const fixtureDir = resolve(dirname(fileURLToPath(import.meta.url)), '../../../backend/testdata/mayara')

function loadCapabilitiesFixture(): unknown {
  const raw = readFileSync(resolve(fixtureDir, 'capabilities-fur6424A.json'), 'utf-8')
  return JSON.parse(raw)
}

async function loadModule() {
  vi.resetModules()
  return import('@/hooks/use-radar-capabilities')
}

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('useRadarCapabilities', () => {
  it('parses the real captured capabilities into capabilities plus a 256-entry palette', async () => {
    const fixture = loadCapabilitiesFixture()
    const fetchMock = vi.fn(async () => ({ ok: true, status: 200, json: async () => fixture }))
    vi.stubGlobal('fetch', fetchMock)

    const { useRadarCapabilities } = await loadModule()
    const { result } = renderHook(() => useRadarCapabilities('fur6424A'))

    await waitFor(() => expect(result.current.loading).toBe(false))

    expect(result.current.error).toBeNull()
    expect(result.current.capabilities).not.toBeNull()
    // Verbatim from the payload -- 8192, not the 4096 the wire actually
    // carries. See this hook's own doc comment for why that's correct here.
    expect(result.current.capabilities?.spokesPerRevolution).toBe(8192)
    expect(result.current.palette).not.toBeNull()
    expect(result.current.palette?.length).toBe(256)

    const [url] = fetchMock.mock.calls[0] as unknown as [string]
    expect(url).toBe('/api/radar/capabilities?radar=fur6424A')
  })

  it('yields null capabilities and a set error on a 502 carrying an error key -- never an empty legend', async () => {
    const fetchMock = vi.fn(async () => ({
      ok: false,
      status: 502,
      json: async () => ({ error: 'mayara capabilities endpoint returned status 502' }),
    }))
    vi.stubGlobal('fetch', fetchMock)

    const { useRadarCapabilities } = await loadModule()
    const { result } = renderHook(() => useRadarCapabilities('fur6424A'))

    await waitFor(() => expect(result.current.loading).toBe(false))

    expect(result.current.capabilities).toBeNull()
    expect(result.current.palette).toBeNull()
    expect(result.current.error).toBe('mayara capabilities endpoint returned status 502')
  })

  it('yields null rather than a partial legend for a payload missing its legend', async () => {
    const fetchMock = vi.fn(async () => ({
      ok: true,
      status: 200,
      json: async () => ({ spokesPerRevolution: 8192, maxSpokeLength: 1024 }),
    }))
    vi.stubGlobal('fetch', fetchMock)

    const { useRadarCapabilities } = await loadModule()
    const { result } = renderHook(() => useRadarCapabilities('fur6424A'))

    await waitFor(() => expect(result.current.loading).toBe(false))

    expect(result.current.capabilities).toBeNull()
    expect(result.current.palette).toBeNull()
    expect(result.current.error).not.toBeNull()
  })

  it('yields null rather than a partial legend for a payload whose legend.pixels is malformed', async () => {
    const fixture = loadCapabilitiesFixture() as Record<string, unknown>
    const legend = fixture.legend as Record<string, unknown>
    const malformed = { ...fixture, legend: { ...legend, pixels: 'not-an-array' } }
    const fetchMock = vi.fn(async () => ({ ok: true, status: 200, json: async () => malformed }))
    vi.stubGlobal('fetch', fetchMock)

    const { useRadarCapabilities } = await loadModule()
    const { result } = renderHook(() => useRadarCapabilities('fur6424A'))

    await waitFor(() => expect(result.current.loading).toBe(false))

    expect(result.current.capabilities).toBeNull()
    expect(result.current.palette).toBeNull()
    expect(result.current.error).not.toBeNull()
  })

  it('does not refetch on a second mount for the same radar id', async () => {
    const fixture = loadCapabilitiesFixture()
    const fetchMock = vi.fn(async () => ({ ok: true, status: 200, json: async () => fixture }))
    vi.stubGlobal('fetch', fetchMock)

    const { useRadarCapabilities } = await loadModule()

    const first = renderHook(() => useRadarCapabilities('fur6424A'))
    await waitFor(() => expect(first.result.current.loading).toBe(false))

    const second = renderHook(() => useRadarCapabilities('fur6424A'))
    await waitFor(() => expect(second.result.current.loading).toBe(false))

    expect(second.result.current.capabilities?.spokesPerRevolution).toBe(8192)
    expect(fetchMock).toHaveBeenCalledTimes(1)
  })

  it('does not fetch when radarId is null', async () => {
    const fetchMock = vi.fn(async () => ({ ok: true, status: 200, json: async () => ({}) }))
    vi.stubGlobal('fetch', fetchMock)

    const { useRadarCapabilities } = await loadModule()
    const { result } = renderHook(() => useRadarCapabilities(null))

    expect(result.current.loading).toBe(false)
    expect(result.current.capabilities).toBeNull()
    expect(result.current.error).toBeNull()
    expect(fetchMock).not.toHaveBeenCalled()
  })
})
