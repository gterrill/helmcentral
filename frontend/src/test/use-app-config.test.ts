import { describe, it, expect, vi, afterEach, beforeEach } from 'vitest'
import { renderHook, act, waitFor } from '@testing-library/react'

// Units and the anchor geometry are operator settings the backend already
// stores, but for a long time nothing read them back: every shipped artifact
// used the compiled defaults, so an operator who chose imperial still saw
// metric. These cover the runtime path that closes that gap.

const settingsResponse = (body: unknown) =>
  vi.fn(async () => ({ ok: true, json: async () => body }))

async function loadModule() {
  vi.resetModules()
  return import('@/hooks/use-app-config')
}

describe('useAppConfig', () => {
  beforeEach(() => {
    vi.resetModules()
  })

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('starts from the compiled defaults before settings arrive', async () => {
    vi.stubGlobal('fetch', vi.fn(() => new Promise(() => {})))
    const { useAppConfig } = await loadModule()

    const { result } = renderHook(() => useAppConfig())

    expect(result.current.ui.distanceUnits).toBe('metric')
    expect(result.current.anchor.chainOnboardM).toBe(150)
  })

  it('applies units from the settings endpoint', async () => {
    vi.stubGlobal('fetch', settingsResponse({ units: 'imperial' }))
    const { useAppConfig } = await loadModule()

    const { result } = renderHook(() => useAppConfig())

    await waitFor(() => expect(result.current.ui.distanceUnits).toBe('imperial'))
  })

  it('applies the anchor block and refresh interval from the settings endpoint', async () => {
    vi.stubGlobal('fetch', settingsResponse({
      ui: { vessel_state_refresh_seconds: 30 },
      anchor: {
        bow_roller_height_m: 2.4,
        chain_size_mm: 10,
        chain_onboard_m: 80,
        hull_type: 'sail_mono',
        windage_area_m2: 22,
      },
    }))
    const { useAppConfig } = await loadModule()

    const { result } = renderHook(() => useAppConfig())

    await waitFor(() => expect(result.current.anchor.hullType).toBe('sail_mono'))
    expect(result.current.anchor.bowRollerHeightM).toBe(2.4)
    expect(result.current.anchor.chainSizeMm).toBe(10)
    expect(result.current.anchor.chainOnboardM).toBe(80)
    expect(result.current.anchor.windageAreaM2).toBe(22)
    expect(result.current.ui.vesselStateRefreshSeconds).toBe(30)
  })

  it('ignores malformed values rather than rendering nonsense', async () => {
    vi.stubGlobal('fetch', settingsResponse({
      units: 'furlongs',
      ui: { vessel_state_refresh_seconds: -5 },
      anchor: { hull_type: 'hovercraft', chain_onboard_m: 0 },
    }))
    const { useAppConfig } = await loadModule()

    const { result } = renderHook(() => useAppConfig())
    await act(async () => { await Promise.resolve() })

    expect(result.current.ui.distanceUnits).toBe('metric')
    expect(result.current.ui.vesselStateRefreshSeconds).toBe(10)
    expect(result.current.anchor.hullType).toBe('power_cat')
    expect(result.current.anchor.chainOnboardM).toBe(150)
  })

  it('keeps the defaults when the settings endpoint is unreachable', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => { throw new Error('offline') }))
    const { useAppConfig } = await loadModule()

    const { result } = renderHook(() => useAppConfig())
    await act(async () => { await Promise.resolve() })

    expect(result.current.ui.distanceUnits).toBe('metric')
    expect(result.current.anchor.chainOnboardM).toBe(150)
  })

  it('fetches once no matter how many components consume it', async () => {
    const fetchMock = settingsResponse({ units: 'imperial' })
    vi.stubGlobal('fetch', fetchMock)
    const { useAppConfig } = await loadModule()

    const a = renderHook(() => useAppConfig())
    const b = renderHook(() => useAppConfig())

    await waitFor(() => expect(a.result.current.ui.distanceUnits).toBe('imperial'))
    await waitFor(() => expect(b.result.current.ui.distanceUnits).toBe('imperial'))
    expect(fetchMock).toHaveBeenCalledTimes(1)
  })

  // Saving in Settings must take effect immediately. The POST response is the
  // authoritative new payload, so it is published straight to consumers rather
  // than triggering another GET or waiting for a reload.
  it('updates live consumers when saved settings are published', async () => {
    vi.stubGlobal('fetch', settingsResponse({ units: 'metric' }))
    const { useAppConfig, publishAppConfigSettings } = await loadModule()

    const { result } = renderHook(() => useAppConfig())
    await waitFor(() => expect(result.current.ui.distanceUnits).toBe('metric'))

    act(() => {
      publishAppConfigSettings({ units: 'imperial', anchor: { chain_onboard_m: 90 } })
    })

    expect(result.current.ui.distanceUnits).toBe('imperial')
    expect(result.current.anchor.chainOnboardM).toBe(90)
  })
})
