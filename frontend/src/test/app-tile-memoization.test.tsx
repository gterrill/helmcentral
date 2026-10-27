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
let alarmsListener: ((raw: string) => void) | null = null
let vesselStateListener: ((raw: string) => void) | null = null

vi.mock('@/hooks/use-telemetry-stream', () => ({
  subscribeTelemetry: (event: string, cb: (raw: string) => void) => {
    subscribeTelemetryMock(event, cb)
    if (event === 'gauge-values') gaugeValuesListener = cb
    if (event === 'alarms') alarmsListener = cb
    if (event === 'vessel-state') vesselStateListener = cb
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

function emitAlarms(payload: unknown) {
  act(() => {
    alarmsListener?.(JSON.stringify(payload))
  })
}

function emitVesselState(payload: unknown) {
  act(() => {
    vesselStateListener?.(JSON.stringify(payload))
  })
}

// One vessel-state event from the boat's own server (GET /api/vessel-state,
// 2026-10-27, at anchor), so the ticks below move fields the way the stream
// moves them.
const LIVE_VESSEL_STATE = {
  current_drift_impact_kts: 0.1, current_drift_kts: 0.1, current_set_deg: 56.62344246937747,
  datetime: '2026-10-27T01:58:19Z', depth: 16.27, depth_last_update_age_s: 2.6, draft_m: 1.2,
  engines_running: null, generator_manual_start: false, generator_manual_start_timer: 0,
  generator_running_by_condition: 'stopped', generator_runtime: 0, generator_state: 'stopped',
  gnss_critical_alert: false, gnss_hdop: 0.58, gnss_quality_indicator: 2, gnss_satellites: 12,
  gnss_validation_reason: '', gnss_validation_state: 'trusted', heading_true: 329.2946244655377,
  latitude: -20.274677459176406, length_overall_m: 17.9, longitude: 149.07369155347328,
  max_gust_kts: { '10m': 6.1, '1h': 11, '24h': 16.4, '30m': 6.1 },
  max_gust_true_kts: { '10m': 6.1, '1h': 8.9, '24h': 15.7, '30m': 6.1 },
  name: 'Pikorua', position_last_update_age_s: 0, source: 'signalk', speed_over_ground_kts: 0.1,
  status: 'motoring', timezone: 'Etc/GMT-10', vessel_prefix: 'M/V',
  wind_angle_apparent_deg: 22.998525896551243, wind_angle_relative_deg: 22.998525896551243,
  wind_angle_true_deg: 22.998525896551243, wind_angle_true_relative_deg: 22.998525896551243,
  wind_direction_true_deg: 352.60477746092664, wind_last_update_age_s: 2.1, wind_side: 'starboard',
  wind_side_true: 'starboard', wind_speed_apparent_kts: 5.09287128, wind_speed_true_kts: 5.09287128,
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

// AlarmBanner draws no Tile, so it is wrapped like RouteTile: memo() with the
// real props, so the spy runs only when the real banner would.
const bannerRenderSpy = vi.fn()
vi.mock('@/components/alarm-banner', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/components/alarm-banner')>()
  const Spy = memo(function AlarmBannerRenderSpy(props: ComponentProps<typeof actual.AlarmBanner>) {
    bannerRenderSpy()
    return <actual.AlarmBanner {...props} />
  })
  return { ...actual, AlarmBanner: Spy }
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
        // Alarm consumers: the lamp strip reads the whole list and the worst
        // state, the Nearby Vessels tile only the collision alarms.
        { id: 'lamps:l', x: 0, y: 4, w: 4, h: 2, lamps: { title: 'Lamps', lamps: [{ path: 'c.path', label: 'C' }], showCheck: true } },
        { id: 'nearby-vessels', x: 4, y: 4, w: 4, h: 4 },
        // Vessel-state consumers: wind reads the wind fields, position the fix.
        { id: 'wind', x: 8, y: 4, w: 4, h: 4 },
        { id: 'position', x: 0, y: 8, w: 4, h: 4 },
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
// use-gauge-values and use-alarms are deliberately NOT mocked: it needs to run for real so
// the emits above flow through its actual (now-shared, now-memoized) store. ─

// use-vessel-state is not mocked either: the vessel-state ticks below run
// through its real store.

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
  anchorWatchAt: (watch: unknown) => watch,
  useAnchorWatch: () => ({
    active: false, anchorState: 'none', anchorLat: null, anchorLon: null, radiusMeters: 0,
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
  forecastWarningDetailsUrl: () => null,
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

  // The server rounds ages to 0.1 s, so most of them move on every tick. An
  // age that moves without going stale changes nothing either gauge shows.
  it('re-renders no gauge when ages move only within the fresh band', async () => {
    await mountApp()
    const a = rendersOf('Gauge A')
    const b = rendersOf('Gauge B')

    emitGaugeValues({ values: { 'a.path': 1, 'b.path': 1 }, ages: { 'a.path': 0.3, 'b.path': 0.7 } })
    emitGaugeValues({ values: { 'a.path': 1, 'b.path': 1 }, ages: { 'a.path': 0.7, 'b.path': 1.1 } })

    expect(rendersOf('Gauge A')).toBe(a)
    expect(rendersOf('Gauge B')).toBe(b)
  })

  it('re-renders a gauge whose age crosses the stale threshold', async () => {
    await mountApp()
    const a = rendersOf('Gauge A')
    const b = rendersOf('Gauge B')

    emitGaugeValues({ values: { 'a.path': 1, 'b.path': 1 }, ages: { 'a.path': 1, 'b.path': 121 } })

    expect(rendersOf('Gauge B')).toBeGreaterThan(b)
    expect(rendersOf('Gauge A')).toBe(a)
  })
})

// ── alarms (ADR 0215 phase 2) ────────────────────────────────────────────────

function houseBankAlarm(overrides: Record<string, unknown> = {}) {
  return {
    rule_id: 'house-bank-low', label: 'House Bank Low', path: 'electrical.batteries.house.capacity.stateOfCharge',
    phase: 'active', state: 'alarm', value: 0.18, message: 'House bank 18%',
    silenced: false, can_silence: false, can_acknowledge: true,
    ...overrides,
  }
}

describe('App alarm surfaces (ADR 0215 phase 2)', () => {
  it('re-renders no alarm consumer when the same alarms payload arrives twice', async () => {
    await mountApp()
    emitAlarms({ alarms: [houseBankAlarm()], worst: 'alarm' })
    const banner = bannerRenderSpy.mock.calls.length
    const lamps = rendersOf('Lamps')
    const nearby = rendersOf('Nearby Vessels')
    expect(banner).toBeGreaterThan(0)
    expect(lamps).toBeGreaterThan(0)
    expect(nearby).toBeGreaterThan(0)

    // The server sends the list every two seconds whether or not it changed.
    emitAlarms({ alarms: [houseBankAlarm()], worst: 'alarm' })
    emitAlarms({ alarms: [houseBankAlarm()], worst: 'alarm' })

    expect(bannerRenderSpy.mock.calls.length).toBe(banner)
    expect(rendersOf('Lamps')).toBe(lamps)
    expect(rendersOf('Nearby Vessels')).toBe(nearby)
  })

  it('does not re-render the shell on an alarms tick', async () => {
    await mountApp()
    const grid = gridRenderSpy.mock.calls.length

    emitAlarms({ alarms: [houseBankAlarm()], worst: 'alarm' })
    emitAlarms({ alarms: [houseBankAlarm({ phase: 'acknowledged' })], worst: 'alarm' })
    emitAlarms({ alarms: [], worst: 'normal' })

    expect(gridRenderSpy.mock.calls.length).toBe(grid)
  })

  it('re-renders the banner and the lamp strip when an alarm changes', async () => {
    await mountApp()
    emitAlarms({ alarms: [houseBankAlarm()], worst: 'alarm' })
    const banner = bannerRenderSpy.mock.calls.length
    const lamps = rendersOf('Lamps')
    const nearby = rendersOf('Nearby Vessels')

    emitAlarms({ alarms: [houseBankAlarm({ phase: 'acknowledged', acked_at: '2026-10-27T02:00:00Z' })], worst: 'alarm' })

    expect(bannerRenderSpy.mock.calls.length).toBeGreaterThan(banner)
    expect(rendersOf('Lamps')).toBeGreaterThan(lamps)
    // Not a collision alarm, so nothing the Nearby Vessels tile shows changed.
    expect(rendersOf('Nearby Vessels')).toBe(nearby)
  })
})

// ── vessel state (ADR 0215 phase 3) ──────────────────────────────────────────

describe('App vessel-state tiles (ADR 0215 phase 3)', () => {
  async function mountWithFix() {
    await mountApp()
    emitVesselState(LIVE_VESSEL_STATE)
    expect(rendersOf('Wind')).toBeGreaterThan(0)
    expect(rendersOf('Position')).toBeGreaterThan(0)
  }

  it('re-renders Position, and not Wind, when only the latitude changes', async () => {
    await mountWithFix()
    const wind = rendersOf('Wind')
    const position = rendersOf('Position')

    emitVesselState({ ...LIVE_VESSEL_STATE, latitude: -20.2747 })

    expect(rendersOf('Position')).toBeGreaterThan(position)
    expect(rendersOf('Wind')).toBe(wind)
  })

  // The server rounds ages to 0.1 s, so they move on almost every tick. Within
  // the fresh band nothing shows them, so nothing should redraw.
  it('re-renders nothing when only ages move within the fresh band', async () => {
    await mountWithFix()
    const wind = rendersOf('Wind')
    const position = rendersOf('Position')
    const route = routeTileRenderSpy.mock.calls.length
    const grid = gridRenderSpy.mock.calls.length

    emitVesselState({ ...LIVE_VESSEL_STATE, datetime: '2026-10-27T01:58:20Z', depth_last_update_age_s: 3.6, position_last_update_age_s: 0.4, wind_last_update_age_s: 0.3 })
    emitVesselState({ ...LIVE_VESSEL_STATE, datetime: '2026-10-27T01:58:21Z', depth_last_update_age_s: 4.6, position_last_update_age_s: 1.1, wind_last_update_age_s: 1.3 })

    expect(rendersOf('Wind')).toBe(wind)
    expect(rendersOf('Position')).toBe(position)
    expect(routeTileRenderSpy.mock.calls.length).toBe(route)
    expect(gridRenderSpy.mock.calls.length).toBe(grid)
  })

  it('re-renders Wind when its age crosses into stale', async () => {
    await mountWithFix()
    const wind = rendersOf('Wind')
    const position = rendersOf('Position')

    emitVesselState({ ...LIVE_VESSEL_STATE, wind_last_update_age_s: 121 })

    expect(rendersOf('Wind')).toBeGreaterThan(wind)
    expect(rendersOf('Position')).toBe(position)
  })

  it('does not re-render the shell grid on a vessel-state tick', async () => {
    await mountWithFix()
    const grid = gridRenderSpy.mock.calls.length

    emitVesselState({ ...LIVE_VESSEL_STATE, latitude: -20.2747, longitude: 149.0737, heading_true: 330.1, wind_speed_apparent_kts: 6.2, depth: 16.1 })
    emitVesselState({ ...LIVE_VESSEL_STATE, latitude: -20.2748, longitude: 149.0738, heading_true: 331.4, wind_speed_apparent_kts: 5.7, depth: 16.0 })

    expect(gridRenderSpy.mock.calls.length).toBe(grid)
  })
})
