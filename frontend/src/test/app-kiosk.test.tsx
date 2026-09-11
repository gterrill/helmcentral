/**
 * Mounts <App> at /kiosk and at / to cover the wall display end to end
 * (ADR 0089). Preamble copied from app-deep-link.test.tsx (same App tree,
 * same reasons for each hook mock), plus two hoisted-state mocks this suite
 * needs that the deep-link tests don't: use-telemetry-stream (to force
 * "reconnecting" for the signal pill) and use-alarms (to force an active
 * alarm for the alarm pill).
 */
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import { App } from '../App'
import type { DashboardPage } from '@/hooks/use-dashboard-pages'
import type { DashboardLayoutItem } from '@/lib/dashboard-widgets'
import type { ActiveAlarm } from '@/hooks/use-alarms'
import type { LampStripWidgetConfig } from '@/lib/dashboard-widgets'

// A configured ribbon (ADR 0082), so "does it render" tests below are
// actually exercising something: the ribbon is normally null in this test
// file's default fetch stub, which would make its absence at /kiosk trivially
// true rather than a real assertion.
const RIBBON: LampStripWidgetConfig = {
  title: 'Status',
  lamps: [{ path: 'electrical.generator.state', label: 'GEN' }],
  showCheck: false,
}

vi.mock('@/hooks/use-dashboard-ribbon', () => ({
  useDashboardRibbon: () => ({ ribbon: RIBBON, loading: false, error: null, saveRibbon: vi.fn() }),
}))

vi.mock('@/hooks/use-auth', () => ({
  refreshAuthState: vi.fn().mockResolvedValue({ mode: 'none', user: null }),
  useAuth: () => ({
    mode: 'none' as const,
    user: null,
    role: null,
    loading: false,
    error: null,
    login: vi.fn(),
    logout: vi.fn(),
  }),
}))

beforeEach(() => {
  vi.stubGlobal('fetch', vi.fn(async (url: string) => (
    typeof url === 'string' && url.endsWith('/api/health')
      ? { ok: true, json: async () => ({ status: 'ok', version: 'v0.17.0', revision: 'deadbeef' }) }
      : { ok: false, json: async () => ({}) }
  )))
})

vi.mock('@/hooks/use-vessel-state', () => ({
  useVesselState: () => ({
    depth: null, currentDriftKts: null, currentSetDeg: null, navigationState: null, latitude: null, longitude: null,
    headingTrue: null, speedOverGroundKts: null, windSpeedApparentKts: null,
    windAngleApparentDeg: null, windSide: null, windAngleRelativeDeg: null,
    maxGustKts: { '10m': null, '30m': null, '1h': null, '24h': null }, generatorState: null,
    generatorManualStart: false, generatorManualStartTimer: 0,
    generatorRunningByCondition: null, generatorRuntime: null,
    engine0Rpm: null, engine1Rpm: null, gnssQualityIndicator: null, gnssHdop: null,
    gnssValidationState: null, gnssValidationReason: null, gnssCriticalAlert: false
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
  useSolarState: () => ({
    currentW: null,
    todayKWh: null,
    yesterdayKWh: null,
    peakTodayW: null,
    controllers: [],
  }),
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
      temperature_f: -1, condition: '—', high_temp_f: -1, low_temp_f: -1,
      wind_speed_kts: -1, wind_direction: '—', wind_gust_kts: -1, precipitation_pct: -1,
    },
  }),
}))

vi.mock('@/hooks/use-weather-forecast', () => ({
  useWeatherForecast: () => ({
    forecast: [{
      date: 'Jul 9',
      dayName: 'Thursday',
      condition: 'Clear',
      high: 76,
      low: 62,
      windSpeed: 10,
      windGust: 14,
      windDirection: 'NE',
      windSummary: null,
      waveSummary: null,
      precipitationSummary: null,
      precipitation: 5,
      humidityPct: 58,
      visibilityNm: 9.5,
      sunriseTime: null,
      sunsetTime: null,
      moonPhase: null,
      hourlyWind: [],
      hourlyWave: [],
      hourlyPrecip: [],
      hourlyUV: [],
      hourlyCloud: [],
    }],
    loading: false,
    error: null,
    refetch: vi.fn(),
  }),
}))

vi.mock('@/hooks/use-tide-settings', () => ({
  useTideSettings: () => ({
    tideProvider: 'bom',
    tideStationId: 'station-1',
    tideStationName: 'Test Harbor',
    loading: false,
    saving: false,
    error: null,
    refetch: vi.fn(),
    saveStation: vi.fn(),
  }),
}))

vi.mock('@/hooks/use-tide-chart', () => ({
  useTideChart: () => ({
    chart: null,
    loading: false,
    error: null,
    isCached: false,
    updatedAt: null,
    ttlSeconds: null,
    refetch: vi.fn(),
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
    loaded: true,
  }),
  publishAppConfigSettings: vi.fn(),
}))

// Mutable so each test can set the page list it needs before rendering.
const { mockPagesState } = vi.hoisted(() => ({
  mockPagesState: {
    pages: [] as DashboardPage[],
    loading: false,
  },
}))

vi.mock('@/hooks/use-dashboard-pages', () => ({
  useDashboardPages: () => ({
    pages: mockPagesState.pages,
    loading: mockPagesState.loading,
    error: null,
    refetch: vi.fn(),
    createPage: vi.fn(),
    updatePage: vi.fn(),
    deletePage: vi.fn(),
  }),
}))

// This suite's own addition to the deep-link preamble: telemetry status
// forced per test, rather than left to the real (never-connects) stub.
const { mockTelemetryStatus } = vi.hoisted(() => ({
  mockTelemetryStatus: { value: 'connected' as 'connected' | 'reconnecting' | 'disconnected' },
}))

vi.mock('@/hooks/use-telemetry-stream', () => ({
  useTelemetryStatus: () => mockTelemetryStatus.value,
  // No-ops: nothing in this suite drives telemetry events, but several
  // hooks (useAutopilot, use-gauge-values) subscribe unconditionally on
  // every render and need a real unsubscribe function back.
  useTelemetryEvent: () => {},
  subscribeTelemetry: () => () => {},
  subscribeTelemetryStatus: () => () => {},
}))

// This suite's own addition too: an active alarm forced per test, rather
// than the real hook's permanently-empty result under the EventSource stub.
const { mockAlarmsState } = vi.hoisted(() => ({
  mockAlarmsState: { alarms: [] as ActiveAlarm[] },
}))

vi.mock('@/hooks/use-alarms', () => ({
  useAlarms: () => ({ alarms: mockAlarmsState.alarms, worst: 'normal', acknowledge: vi.fn(), silence: vi.fn() }),
  collisionAlarmStatesByVessel: () => new Map(),
  findAnchorDragAlarm: () => null,
}))

vi.mock('@/hooks/use-routes', () => ({
  useRoutes: () => ({ routes: [], loading: false, error: null, refetch: vi.fn(), createRoute: vi.fn(), updateRoute: vi.fn(), deleteRoute: vi.fn() }),
}))

vi.mock('@/hooks/use-sat-charts', () => ({
  useSatCharts: () => ({ charts: [], loading: false, error: null, uploadChart: vi.fn(), deleteChart: vi.fn() }),
}))

vi.mock('@/hooks/use-dashboard-route', () => ({
  useDashboardRouteId: () => [null, vi.fn()],
}))

vi.mock('@/hooks/use-route-activation', () => ({
  useRouteActivation: () => ({ status: null, activating: false, deactivating: false, activateError: null, activate: vi.fn(), deactivate: vi.fn() }),
}))

vi.mock('@/hooks/use-anchor-watch-auto-close', () => ({
  useAnchorWatchAutoClose: () => ({ isAutoCloseArmed: false, motoringSecondsElapsed: 0 }),
}))

vi.mock('@/hooks/use-forecast-warnings', () => ({
  useForecastWarnings: () => ({ activeWarning: null }),
  findActiveWindBulletin: () => null,
}))

vi.mock('@/hooks/use-server-trails', () => ({
  useServerTrails: () => ({ getSelfTrail: vi.fn(), getAisTrails: vi.fn() }),
}))

// Stored preference is always light in this suite. Hoisted so the dark-theme
// override tests below can assert the real underlying preference is never
// touched by the kiosk override — a plain inline vi.fn() couldn't be
// inspected after the fact.
const { mockToggleDarkMode } = vi.hoisted(() => ({ mockToggleDarkMode: vi.fn() }))

vi.mock('@/hooks/use-dark-mode', () => ({
  useDarkMode: () => [false, mockToggleDarkMode],
}))

// A minimal stand-in for the map-bearing tile the dark-theme override has to
// reach (ADR 0089 phase 2): the real AnchorWatchTile pulls in the map's own
// STYLE_DARK/STYLE_LIGHT switch, which is exactly the consumer a light
// basemap bug would show up in. Mocked down to just the one prop this suite
// cares about, the same pattern anchor-imagery-toggle.test.tsx uses for
// AnchorWatchMap.
vi.mock('@/components/anchor-watch-tile', () => ({
  AnchorWatchTile: (props: { isDarkTheme?: boolean }) => (
    <div data-testid="kiosk-anchor-watch-isdark">{String(props.isDarkTheme)}</div>
  ),
}))

// ── fixtures ──────────────────────────────────────────────────────────────────

const DEPTH_TIDE_WIDGET: DashboardLayoutItem = { id: 'depth-tide', x: 0, y: 0, w: 4, h: 7 }
const WIND_WIDGET: DashboardLayoutItem = { id: 'wind', x: 0, y: 0, w: 4, h: 8 }
const ANCHOR_WATCH_WIDGET: DashboardLayoutItem = { id: 'anchor-watch', x: 0, y: 0, w: 4, h: 7 }

function page(id: string, name: string, widgets: DashboardLayoutItem[], overrides: Partial<DashboardPage> = {}): DashboardPage {
  return { id, name, widgets, created_at: '', updated_at: '', ...overrides }
}

function alarm(overrides: Partial<ActiveAlarm> = {}): ActiveAlarm {
  return {
    rule_id: 'r1', label: 'depth', path: 'environment.depth.belowTransducer', phase: 'active', state: 'alarm',
    value: 1, message: 'shallow', silenced: false, can_silence: true, can_acknowledge: true,
    ...overrides,
  }
}

// ── tests ─────────────────────────────────────────────────────────────────────

describe('App at /kiosk', () => {
  beforeEach(() => {
    vi.spyOn(console, 'error').mockImplementation(() => {})
    globalThis.localStorage?.clear()
    mockTelemetryStatus.value = 'connected'
    mockAlarmsState.alarms = []
    mockPagesState.pages = [
      page('p1', 'Cluster preview', [DEPTH_TIDE_WIDGET], { kiosk: true, kiosk_seconds: 30 }),
      page('p2', 'Other page', [WIND_WIDGET]),
    ]
    mockPagesState.loading = false
  })

  it('renders the fullscreen kiosk root with no sidebar or banners, showing the first flagged page', () => {
    window.history.replaceState({}, '', '/kiosk')
    render(<App />)

    expect(screen.getByTestId('kiosk-root')).toBeInTheDocument()
    expect(screen.getByText('Depth & Tide')).toBeInTheDocument()
    expect(screen.queryByText('Dashboard')).not.toBeInTheDocument() // sidebar nav item
    expect(screen.queryByText(/Server connection unavailable/)).not.toBeInTheDocument()
  })

  it('rotates the root under ?rotate=180', () => {
    window.history.replaceState({}, '', '/kiosk?rotate=180')
    render(<App />)

    const root = screen.getByTestId('kiosk-root')
    expect(root).toHaveAttribute('data-rotate', '180')
    expect(root.style.transform).toBe('rotate(180deg)')
  })

  it('pins the named page via ?page=, even one not flagged for kiosk', () => {
    window.history.replaceState({}, '', '/kiosk?page=p2')
    render(<App />)

    expect(screen.getByTestId('kiosk-root')).toBeInTheDocument()
    expect(screen.getByText('Apparent Wind - Course Up')).toBeInTheDocument()
  })

  it('shows the signal pill while telemetry is reconnecting', () => {
    mockTelemetryStatus.value = 'reconnecting'
    window.history.replaceState({}, '', '/kiosk')
    render(<App />)

    expect(screen.getByTestId('kiosk-signal-pill')).toHaveTextContent('Reconnecting')
  })

  it('shows the alarm pill while an alarm is active', () => {
    mockAlarmsState.alarms = [alarm()]
    window.history.replaceState({}, '', '/kiosk')
    render(<App />)

    expect(screen.getByTestId('kiosk-alarm-pill')).toHaveTextContent('1 alarm')
  })

  it('does not render the pinned indicator ribbon, even when one is configured', () => {
    window.history.replaceState({}, '', '/kiosk')
    render(<App />)

    expect(screen.queryByTestId('dashboard-ribbon')).not.toBeInTheDocument()
  })

  it('forces the dark theme regardless of the stored (light) preference, without persisting it', () => {
    mockPagesState.pages = [
      page('p1', 'Cluster preview', [DEPTH_TIDE_WIDGET, ANCHOR_WATCH_WIDGET], { kiosk: true, kiosk_seconds: 30 }),
    ]
    window.history.replaceState({}, '', '/kiosk')
    render(<App />)

    expect(document.documentElement.classList.contains('dark')).toBe(true)
    expect(screen.getByTestId('kiosk-anchor-watch-isdark')).toHaveTextContent('true')
    expect(mockToggleDarkMode).not.toHaveBeenCalled()
    expect(globalThis.localStorage?.getItem('ui.darkMode')).toBeNull()
  })
})

describe('App at / (kiosk authoring)', () => {
  beforeEach(() => {
    vi.spyOn(console, 'error').mockImplementation(() => {})
    globalThis.localStorage?.clear()
    mockTelemetryStatus.value = 'connected'
    mockAlarmsState.alarms = []
    mockPagesState.pages = [
      page('p1', 'Cluster preview', [DEPTH_TIDE_WIDGET], { kiosk: true, kiosk_seconds: 30 }),
      page('p2', 'Other page', [WIND_WIDGET]),
    ]
    mockPagesState.loading = false
  })

  it('lists a "Wall display" sidebar link to /kiosk that opens in a new tab', () => {
    window.history.replaceState({}, '', '/')
    render(<App />)

    const link = screen.getByRole('link', { name: /wall display/i })
    expect(link).toHaveAttribute('href', '/kiosk')
    expect(link).toHaveAttribute('target', '_blank')
  })

  it('shows the fold guide in edit mode on a page flagged for kiosk, not on an unflagged one', () => {
    window.history.replaceState({}, '', '/')
    render(<App />)

    // Active page (first, "Cluster preview") is kiosk-flagged.
    fireEvent.click(screen.getByLabelText('Enter edit mode'))
    expect(screen.getByTestId('kiosk-fold')).toBeInTheDocument()

    // Switch to the unflagged page via the sidebar sub-list.
    fireEvent.click(screen.getByRole('button', { name: 'Other page' }))
    expect(screen.queryByTestId('kiosk-fold')).not.toBeInTheDocument()
  })

  it('stays on the stored (light) preference outside the kiosk route', () => {
    mockPagesState.pages = [
      page('p1', 'Cluster preview', [DEPTH_TIDE_WIDGET, ANCHOR_WATCH_WIDGET], { kiosk: true, kiosk_seconds: 30 }),
    ]
    window.history.replaceState({}, '', '/')
    render(<App />)

    expect(document.documentElement.classList.contains('dark')).toBe(false)
    expect(screen.getByTestId('kiosk-anchor-watch-isdark')).toHaveTextContent('false')
  })

  it('renders the pinned indicator ribbon outside the kiosk route', () => {
    window.history.replaceState({}, '', '/')
    render(<App />)

    expect(screen.getByTestId('dashboard-ribbon')).toBeInTheDocument()
  })
})
