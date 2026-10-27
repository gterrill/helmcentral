/**
 * Item D end-to-end check, now that tiles subscribe to their own live data
 * (ADR 0215): a gauge-values tick must not re-render the shell at all, and
 * must re-render only the gauge whose own reading changed.
 *
 * RouteTile is wrapped (not replaced) below: the wrapper is itself `memo()`'d
 * with the exact props the real tile receives, so it bails out under the
 * identical conditions the real tile's own memo would.
 *
 * A gauge reads the store itself, so a wrapper around GaugeTile would never
 * see a store-driven render. Tile is wrapped instead and records the title it
 * is drawn with: every render of a gauge draws its Tile again, so the count
 * per title is that gauge's render count. DashboardBentoGrid is wrapped
 * without memo, so its count is the shell's.
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

const tileTitleSpy = vi.fn()
vi.mock('@/components/ui/tile', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/components/ui/tile')>()
  function TileTitleSpy(props: ComponentProps<typeof actual.Tile>) {
    tileTitleSpy(props.title)
    return <actual.Tile {...props} />
  }
  return { ...actual, Tile: TileTitleSpy }
})

const gridRenderSpy = vi.fn()
vi.mock('@/components/dashboard-bento-grid', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/components/dashboard-bento-grid')>()
  function GridRenderSpy(props: ComponentProps<typeof actual.DashboardBentoGrid>) {
    gridRenderSpy()
    return <actual.DashboardBentoGrid {...props} />
  }
  return { ...actual, DashboardBentoGrid: GridRenderSpy }
})

function rendersOf(title: string): number {
  return tileTitleSpy.mock.calls.filter(([t]) => t === title).length
}

// ── a dashboard page with one route tile and two gauges, on paths a and b. ────
vi.mock('@/hooks/use-dashboard-pages', () => ({
  useDashboardPages: () => ({
    pages: [{
      id: 'p1',
      name: 'Page 1',
      widgets: [
        { id: 'route', x: 0, y: 0, w: 4, h: 4 },
        { id: 'gauge:a', x: 4, y: 0, w: 4, h: 4, gauge: { path: 'a.path', label: 'Gauge A', display: 'numeric', quantity: 'raw', unit: 'raw' } },
        { id: 'gauge:b', x: 8, y: 0, w: 4, h: 4, gauge: { path: 'b.path', label: 'Gauge B', display: 'numeric', quantity: 'raw', unit: 'raw' } },
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
    enginesRunning: null, gnssQualityIndicator: null, gnssHdop: null,
    gnssValidationState: null, gnssValidationReason: null, gnssCriticalAlert: false,
  }),
}))

vi.mock('@/hooks/use-electrical-state', () => ({
  useElectricalState: () => ({
    batterySocPercent: null, batteryCapacityAh: null, chargingCurrentA: null,
    chargingPowerW: null, solarOutputW: null, acOutputW: null, dc12vPowerW: null,
    dc12vCurrentA: null, voltageReadoutV: null, voltageReadoutLabel: null, acLoadsW: null,
    generatorRealPowerW: null, generatorConfigured: true, batteryRatePercentPerHour: null, timeToGoHours: null,
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

vi.mock('@/hooks/use-route-activation', () => {
  // One object, so a re-render does not look like a new route status.
  const result = { status: { state: 'inactive', destination: null } }
  return { useRouteActivation: () => result }
})

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

async function mountApp() {
  render(<App />)
  // Auth resolution (useAuth, real here) is async even under mode:none —
  // let it settle before the dashboard (and its widgets) mounts.
  await act(async () => { await Promise.resolve() })
  expect(subscribeTelemetryMock).toHaveBeenCalledWith('gauge-values', expect.any(Function))
  expect(rendersOf('Gauge A')).toBeGreaterThan(0)
  expect(rendersOf('Gauge B')).toBeGreaterThan(0)
  // A first reading for both gauges, so the ticks below change one thing each.
  emitGaugeValues({ values: { 'a.path': 1, 'b.path': 1 }, ages: { 'a.path': 1, 'b.path': 1 } })
}

describe('App tile memoization (item D)', () => {
  it('does not re-render the shell, or a memoized tile, on a gauge-values tick', async () => {
    await mountApp()
    const gridRenders = gridRenderSpy.mock.calls.length
    const routeRenders = routeTileRenderSpy.mock.calls.length
    const gaugeARenders = rendersOf('Gauge A')
    expect(gridRenders).toBeGreaterThan(0)
    expect(routeRenders).toBeGreaterThan(0)

    emitGaugeValues({ values: { 'a.path': 2, 'b.path': 1 }, ages: { 'a.path': 1, 'b.path': 1 } })
    emitGaugeValues({ values: { 'a.path': 3, 'b.path': 1 }, ages: { 'a.path': 1, 'b.path': 1 } })

    // The tick reached the gauge bound to the path that changed.
    expect(rendersOf('Gauge A')).toBeGreaterThan(gaugeARenders)
    // The shell holds no gauge values, so it did not render, and nor did the
    // grid it renders or the route tile below it.
    expect(gridRenderSpy.mock.calls.length).toBe(gridRenders)
    expect(routeTileRenderSpy.mock.calls.length).toBe(routeRenders)
  })

  it('re-renders gauge A, and not gauge B, when only a changes', async () => {
    await mountApp()
    const a = rendersOf('Gauge A')
    const b = rendersOf('Gauge B')

    emitGaugeValues({ values: { 'a.path': 2, 'b.path': 1 }, ages: { 'a.path': 1, 'b.path': 1 } })

    expect(rendersOf('Gauge A')).toBeGreaterThan(a)
    expect(rendersOf('Gauge B')).toBe(b)
  })

  it('does not re-render gauge A when only the age behind b changes', async () => {
    await mountApp()
    const a = rendersOf('Gauge A')
    const b = rendersOf('Gauge B')

    emitGaugeValues({ values: { 'a.path': 1, 'b.path': 1 }, ages: { 'a.path': 1, 'b.path': 2 } })

    expect(rendersOf('Gauge B')).toBeGreaterThan(b)
    expect(rendersOf('Gauge A')).toBe(a)
  })
})
