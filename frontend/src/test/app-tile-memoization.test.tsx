/**
 * Item D end-to-end check: an SSE tick that changes something App reads
 * (gaugeValues) must re-render App, but must NOT re-render a memoized tile
 * whose own props didn't change — proving the stable-callback fix
 * (configureHandlerFor / openRoutesPanel etc., App.tsx) actually holds up
 * once the whole component tree is wired together, not just in isolation.
 *
 * RouteTile and GaugeTile are wrapped (not replaced) below: the wrapper is
 * itself `memo()`'d with the exact props each real tile receives, so it
 * bails out under the identical conditions the real tile's own memo would —
 * the render-count spy inside it only fires when React actually re-renders
 * that subtree, the same signal `<Profiler onRender>` would give, without
 * depending on Profiler's own timing semantics.
 */
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { render, act } from '@testing-library/react'
import { memo, type ComponentProps } from 'react'
import { App } from '../App'

beforeEach(() => {
  // useAuth (real here, not mocked) needs mode:none to clear the shell for
  // rendering; everything else this test doesn't care about stays a no-op
  // failure, same as App.smoke.test.tsx's blanket stub.
  vi.stubGlobal('fetch', vi.fn(async (input: RequestInfo | URL) => {
    const url = typeof input === 'string' ? input : input.toString()
    if (url.includes('/api/auth/mode')) {
      return { ok: true, json: async () => ({ mode: 'none' }) }
    }
    if (url.includes('/api/auth/me')) {
      return { ok: true, json: async () => ({ authenticated: false }) }
    }
    return { ok: false, json: async () => ({}) }
  }))
})

afterEach(() => {
  vi.unstubAllGlobals()
})

// ── the shared SSE connection, mocked so gauge-values ticks can be driven
// directly — use-telemetry-stream.test.ts already covers the transport. ────
const subscribeTelemetryMock = vi.fn()
let gaugeValuesListener: ((raw: string) => void) | null = null

vi.mock('@/hooks/use-telemetry-stream', () => ({
  subscribeTelemetry: (event: string, cb: (raw: string) => void) => {
    subscribeTelemetryMock(event, cb)
    if (event === 'gauge-values') gaugeValuesListener = cb
    return () => {}
  },
  // Real consumers elsewhere in the tree (use-autopilot.ts, vessel-status-bar.tsx
  // via use-vessel-identity.ts) just need these to exist and not throw — their
  // own behavior isn't what this test is checking.
  useTelemetryEvent: () => {},
  useTelemetryStatus: () => 'connected',
  subscribeTelemetryStatus: () => () => {},
}))

function emitGaugeValues(payload: unknown) {
  act(() => {
    gaugeValuesListener?.(JSON.stringify(payload))
  })
}

// ── render-count spies, each wrapping the real tile so its real memo() is
// still what decides whether the spy body runs. ────────────────────────────
const routeTileRenderSpy = vi.fn()
vi.mock('@/components/route-tile', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/components/route-tile')>()
  const Spy = memo(function RouteTileRenderSpy(props: ComponentProps<typeof actual.RouteTile>) {
    routeTileRenderSpy()
    return <actual.RouteTile {...props} />
  })
  return { ...actual, RouteTile: Spy }
})

const gaugeTileRenderSpy = vi.fn()
vi.mock('@/components/gauge-tile', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/components/gauge-tile')>()
  const Spy = memo(function GaugeTileRenderSpy(props: ComponentProps<typeof actual.GaugeTile>) {
    gaugeTileRenderSpy()
    return <actual.GaugeTile {...props} />
  })
  return { ...actual, GaugeTile: Spy }
})

// ── a dashboard page with one route tile (reads App.tsx's now-stable
// openRoutesPanel) and one gauge tile bound to the path the test ticks. ────
vi.mock('@/hooks/use-dashboard-pages', () => ({
  useDashboardPages: () => ({
    pages: [{
      id: 'p1',
      name: 'Page 1',
      widgets: [
        { id: 'route', x: 0, y: 0, w: 4, h: 4 },
        { id: 'gauge:test1', x: 4, y: 0, w: 4, h: 4, gauge: { path: 'a.path', label: 'Test', display: 'numeric', quantity: 'raw', unit: 'raw' } },
      ],
      created_at: '2026-01-01T00:00:00Z',
      updated_at: '2026-01-01T00:00:00Z',
    }],
    loading: false,
    error: null,
    refetch: vi.fn(),
    createPage: vi.fn(),
    updatePage: vi.fn(),
    deletePage: vi.fn(),
    reorderPages: vi.fn(),
    reordering: false,
  }),
}))

// ── the rest of App's hook surface — same shape as App.smoke.test.tsx.
// use-gauge-values is deliberately NOT mocked: it needs to run for real so
// the emits above flow through its actual (now-shared, now-memoized) store. ─

vi.mock('@/hooks/use-vessel-state', () => ({
  useVesselState: () => ({
    depth: null, currentDriftKts: null, currentSetDeg: null, navigationState: null, latitude: null, longitude: null,
    headingTrue: null, speedOverGroundKts: null, windSpeedApparentKts: null,
    windAngleApparentDeg: null, windSide: null, windAngleRelativeDeg: null,
    maxGustKts: { '10m': null, '30m': null, '1h': null, '24h': null }, generatorState: null,
    generatorManualStart: false, generatorManualStartTimer: 0,
    generatorRunningByCondition: null, generatorRuntime: null,
    engine0Rpm: null, engine1Rpm: null, gnssQualityIndicator: null, gnssHdop: null,
    gnssValidationState: null, gnssValidationReason: null, gnssCriticalAlert: false,
  }),
}))

vi.mock('@/hooks/use-electrical-state', () => ({
  useElectricalState: () => ({
    batterySocPercent: null, batteryCapacityAh: null, chargingCurrentA: null,
    chargingPowerW: null, solarOutputW: null, acOutputW: null, dc12vPowerW: null,
    dc12vCurrentA: null, dc24vVoltageV: null, acLoadsW: null,
    generatorRealPowerW: null, batteryRatePercentPerHour: null, timeToGoHours: null,
    charger0CurrentA: null, charger0AcIn1CurrentA: null,
    charger0ChargingMode: null, charger0Error: null,
  }),
}))

vi.mock('@/hooks/use-solar-state', () => ({
  useSolarState: () => ({ currentW: null, todayKWh: null, yesterdayKWh: null, peakTodayW: null, controllers: [] }),
}))

vi.mock('@/hooks/use-nearby-vessels', () => ({
  useNearbyVessels: () => ({ vessels: [], loading: false }),
}))

vi.mock('@/hooks/use-radar-targets', () => ({
  useRadarTargets: () => ({ targets: [], radars: [], source: 'disabled', loading: false }),
}))

vi.mock('@/hooks/use-anchor-watch', () => ({
  useAnchorWatch: () => ({
    anchorState: 'none', anchorLat: null, anchorLon: null, radiusMeters: 0,
    rodeDeployedM: 0, seaState: 'calm', seabedType: 'sand',
    distanceMeters: null, bearingDeg: null,
    planningDepthM: null, planningTideHeightFt: null,
    setAnchorHere: vi.fn(), updateRadius: vi.fn(),
    updateRodeAndConditions: vi.fn(), updatePlanningDepth: vi.fn(),
    clearAnchor: vi.fn(),
  }),
}))

vi.mock('@/hooks/use-place-name', () => ({ usePlaceName: () => null }))

vi.mock('@/hooks/use-tanks-state', () => ({
  useTanksState: () => ({ tanks: [], loading: false }),
}))

vi.mock('@/hooks/use-tide-today', () => ({
  useTideToday: () => ({
    tide: {
      current_tide_height_ft: -1, tide_direction: '—',
      high_tide_time: new Date(0).toISOString(), high_tide_height_ft: -1,
      low_tide_time: new Date(0).toISOString(), low_tide_height_ft: -1,
    },
  }),
}))

vi.mock('@/hooks/use-weather-today', () => ({
  useWeatherToday: () => ({
    weather: {
      temperature_f: -1, condition: '—',
      wind_speed_kts: -1, wind_direction: '—', wind_gust_kts: -1, precipitation_pct: -1,
      provider: '', cached: false, updated_at: '', ttl_seconds: 0,
    },
  }),
}))

vi.mock('@/hooks/use-weather-forecast', () => ({
  useWeatherForecast: () => ({ forecast: [], loading: false, error: null, provider: null, refetch: vi.fn() }),
}))

vi.mock('@/hooks/use-wave-forecast', () => ({
  useWaveForecast: () => ({
    days: [], seaTemperatureF: null, provider: null, loading: false, error: null, refetch: vi.fn(),
  }),
}))

vi.mock('@/hooks/use-czone-switches', () => ({
  useCZoneSwitches: () => ({ switches: [], loading: false, pending: {}, toggleSwitch: vi.fn() }),
}))

vi.mock('@/hooks/use-depth-trend', () => ({ useDepthTrend: () => ({ points: [], since: 'window' }) }))

vi.mock('@/hooks/use-app-config', () => ({
  useAppConfig: () => ({
    ui: { vesselStateRefreshSeconds: 10, distanceUnits: 'metric', autoCloseAnchorWatchOnEngine: true },
    anchor: {
      bowRollerHeightM: 0, chainSizeMm: 10, chainOnboardM: 50,
      hullType: 'power_cat', scopeMethod: 'ratio', windageAreaM2: 10,
      gpsFromBowM: 0, loaM: 0,
    },
    boatModel: null,
    loaded: true,
  }),
  publishAppConfigSettings: vi.fn(),
}))

// A fixed reference, not a fresh `[]` literal per call: the real useRoutes()
// holds `routes` in useState, stable across renders that don't change it —
// a naive mock returning a new array every call would make RouteTile's
// `routes` prop "change" every render regardless of this test's fix, which
// isn't what the real hook does and would make this test prove nothing.
const STABLE_EMPTY_ROUTES: unknown[] = []
vi.mock('@/hooks/use-routes', () => ({
  useRoutes: () => ({ routes: STABLE_EMPTY_ROUTES, loading: false, error: null, refetch: vi.fn(), createRoute: vi.fn(), updateRoute: vi.fn(), deleteRoute: vi.fn() }),
}))

vi.mock('@/hooks/use-dashboard-route', () => ({
  useDashboardRouteId: () => [null, vi.fn()],
}))

vi.mock('@/hooks/use-route-activation', () => ({
  useRouteActivation: () => ({ status: null, activating: false, deactivating: false, activateError: null, activate: vi.fn(), deactivate: vi.fn() }),
}))

vi.mock('@/hooks/use-forecast-warnings', () => ({
  useForecastWarnings: () => ({ activeWarning: null }),
  findActiveWindBulletin: () => null,
}))

vi.mock('@/hooks/use-server-trails', () => ({
  useServerTrails: () => ({ getSelfTrail: vi.fn(), getAisTrails: vi.fn() }),
}))

vi.mock('@/hooks/use-dark-mode', () => ({
  useDarkMode: () => [false, vi.fn()],
}))

describe('App tile memoization (item D)', () => {
  it('re-renders App on a gauge-values tick but not a memoized tile whose own props are unaffected', async () => {
    render(<App />)
    // Auth resolution (useAuth, real here) is async even under mode:none —
    // let it settle before the dashboard (and its widgets) mounts.
    await act(async () => { await Promise.resolve() })

    expect(subscribeTelemetryMock).toHaveBeenCalledWith('gauge-values', expect.any(Function))
    const routeRendersAfterMount = routeTileRenderSpy.mock.calls.length
    const gaugeRendersAfterMount = gaugeTileRenderSpy.mock.calls.length
    expect(routeRendersAfterMount).toBeGreaterThan(0)
    expect(gaugeRendersAfterMount).toBeGreaterThan(0)

    // Two ticks with different values for the path GaugeTile is bound to:
    // gaugeValues' object identity legitimately changes both times, which
    // must re-render GaugeTile (it reads gaugeValues[widget.gauge.path]) —
    // proving the emits actually propagate through App. RouteTile reads
    // none of gaugeValues; its own props (routes, dashboardRouteId, onOpen,
    // speedKts) are all unchanged, so its memo should bail out both times.
    emitGaugeValues({ values: { 'a.path': 1 }, ages: {} })
    emitGaugeValues({ values: { 'a.path': 2 }, ages: {} })

    expect(gaugeTileRenderSpy.mock.calls.length).toBeGreaterThan(gaugeRendersAfterMount)
    expect(routeTileRenderSpy.mock.calls.length).toBe(routeRendersAfterMount)
  })
})
