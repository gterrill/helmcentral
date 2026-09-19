/**
 * Mounts <App> at /display/<slug> and at / to cover wall displays end to end
 * (ADR 0110, superseding ADR 0089). Preamble copied from
 * app-deep-link.test.tsx (same App tree, same reasons for each hook mock),
 * plus the hoisted-state mocks this suite needs that the deep-link tests
 * don't: use-telemetry-stream (to force "reconnecting" for the signal pill),
 * use-alarms (to force an active alarm for the alarm pill), and use-displays
 * (to control which wall displays exist without a real /api/displays fetch).
 */
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, fireEvent, waitFor, within } from '@testing-library/react'
import { App } from '../App'
import type { DashboardPage } from '@/hooks/use-dashboard-pages'
import type { DashboardLayoutItem } from '@/lib/dashboard-widgets'
import type { ActiveAlarm } from '@/hooks/use-alarms'
import { DEFAULT_DWELL_SECONDS } from '@/lib/displays'
import type { LampStripWidgetConfig } from '@/lib/dashboard-widgets'
import type { Display } from '@/lib/displays'
import { registerMateWatch, removeMateWatch } from '@/lib/mate-watch-store'

// A configured ribbon (ADR 0082), so "does it render" tests below are
// actually exercising something: the ribbon is normally null in this test
// file's default fetch stub, which would make its absence at /display/<slug>
// trivially true rather than a real assertion.
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
    ui: { distanceUnits: 'metric', autoCloseAnchorWatchOnEngine: true },
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
const { mockPagesState, mockCreatePage, mockUpdatePage } = vi.hoisted(() => ({
  mockPagesState: {
    pages: [] as DashboardPage[],
    loading: false,
  },
  mockCreatePage: vi.fn(),
  mockUpdatePage: vi.fn(async (id: string, patch: Partial<DashboardPage>) => ({ id, ...patch }) as DashboardPage),
}))

vi.mock('@/hooks/use-dashboard-pages', () => ({
  useDashboardPages: () => ({
    pages: mockPagesState.pages,
    loading: mockPagesState.loading,
    error: null,
    refetch: vi.fn(),
    createPage: mockCreatePage,
    updatePage: mockUpdatePage,
    deletePage: vi.fn(),
    reorderPages: vi.fn(),
    reordering: false,
  }),
}))

// Mutable the same way mockPagesState is - ADR 0110's own wall-display
// record, fetched by App.tsx via useDisplays() rather than pages.
const { mockDisplaysState } = vi.hoisted(() => ({
  mockDisplaysState: {
    displays: [] as Display[],
    loading: false,
  },
}))

vi.mock('@/hooks/use-displays', () => ({
  useDisplays: () => ({
    displays: mockDisplaysState.displays,
    loading: mockDisplaysState.loading,
    error: null,
    refetch: vi.fn(),
    createDisplay: vi.fn(),
    updateDisplay: vi.fn(),
    deleteDisplay: vi.fn(),
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

vi.mock('@/hooks/use-forecast-warnings', () => ({
  useForecastWarnings: () => ({ activeWarning: null }),
  findActiveWindBulletin: () => null,
}))

vi.mock('@/hooks/use-server-trails', () => ({
  useServerTrails: () => ({ getSelfTrail: vi.fn(), getAisTrails: vi.fn() }),
}))

// Stored preference is always light in this suite. Hoisted so the dark-theme
// override tests below can assert the real underlying preference is never
// touched by the wall override — a plain inline vi.fn() couldn't be
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
// AnchorWatchMap. Doubles as a marker for "did the wall show the OTHER
// display's page" in the cross-display leakage test below.
vi.mock('@/components/anchor-watch-tile', () => ({
  AnchorWatchTile: (props: { isDarkTheme?: boolean }) => (
    <div data-testid="display-anchor-watch-isdark">{String(props.isDarkTheme)}</div>
  ),
}))

beforeEach(() => {
  mockCreatePage.mockReset()
  mockCreatePage.mockResolvedValue(null)
  mockUpdatePage.mockClear()
})

// ── fixtures ──────────────────────────────────────────────────────────────────

const DEPTH_TIDE_WIDGET: DashboardLayoutItem = { id: 'depth-tide', x: 0, y: 0, w: 4, h: 7 }
const WIND_WIDGET: DashboardLayoutItem = { id: 'wind', x: 0, y: 0, w: 4, h: 8 }
const ANCHOR_WATCH_WIDGET: DashboardLayoutItem = { id: 'anchor-watch', x: 0, y: 0, w: 4, h: 7 }

const FLYBRIDGE: Display = {
  id: 'd1', name: 'Flybridge', slug: 'flybridge',
  width: 1920, height: 360, scale: 1, rotate: 0,
  pixel_shift: false, wake_lock: false, created_at: '', updated_at: '',
}
const SALOON: Display = {
  id: 'd2', name: 'Saloon TV', slug: 'saloon-tv',
  width: 1280, height: 720, scale: 1.5, rotate: 0,
  pixel_shift: true, wake_lock: true, created_at: '', updated_at: '',
}

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

describe('App at /display/<slug>', () => {
  beforeEach(() => {
    vi.spyOn(console, 'error').mockImplementation(() => {})
    globalThis.localStorage?.clear()
    mockTelemetryStatus.value = 'connected'
    mockAlarmsState.alarms = []
    mockDisplaysState.displays = [FLYBRIDGE]
    mockDisplaysState.loading = false
    mockPagesState.pages = [
      page('p1', 'Cluster preview', [DEPTH_TIDE_WIDGET], { display_id: 'd1', dwell_seconds: 30 }),
      page('p2', 'Other page', [WIND_WIDGET]),
    ]
    mockPagesState.loading = false
  })

  it('renders the fullscreen wall root with no sidebar or banners, showing the first assigned page', () => {
    window.history.replaceState({}, '', '/display/flybridge')
    render(<App />)

    expect(screen.getByTestId('display-shell-outer')).toBeInTheDocument()
    expect(screen.getByText('Depth & Tide')).toBeInTheDocument()
    expect(screen.queryByText('Dashboard')).not.toBeInTheDocument() // sidebar nav item
    expect(screen.queryByText(/Server connection unavailable/)).not.toBeInTheDocument()
  })

  it("rotates the outer box under the display's own rotate field", () => {
    mockDisplaysState.displays = [{ ...FLYBRIDGE, rotate: 180 }]
    window.history.replaceState({}, '', '/display/flybridge')
    render(<App />)

    const outer = screen.getByTestId('display-shell-outer')
    expect(outer).toHaveAttribute('data-rotate', '180')
    expect(outer.style.transform).toBe('rotate(180deg)')
  })

  it('pins the named page via ?page=, even one not assigned to any display', () => {
    window.history.replaceState({}, '', '/display/flybridge?page=p2')
    render(<App />)

    expect(screen.getByTestId('display-shell-outer')).toBeInTheDocument()
    expect(screen.getByText('Apparent Wind - Course Up')).toBeInTheDocument()
  })

  it('shows the signal pill while telemetry is reconnecting', () => {
    mockTelemetryStatus.value = 'reconnecting'
    window.history.replaceState({}, '', '/display/flybridge')
    render(<App />)

    expect(screen.getByTestId('display-signal-pill')).toHaveTextContent('Reconnecting')
  })

  it('shows the alarm pill while an alarm is active', () => {
    mockAlarmsState.alarms = [alarm()]
    window.history.replaceState({}, '', '/display/flybridge')
    render(<App />)

    expect(screen.getByTestId('display-alarm-pill')).toHaveTextContent('1 alarm')
  })

  it('does not render the pinned indicator ribbon, even when one is configured', () => {
    window.history.replaceState({}, '', '/display/flybridge')
    render(<App />)

    expect(screen.queryByTestId('dashboard-ribbon')).not.toBeInTheDocument()
  })

  it('forces the dark theme regardless of the stored (light) preference, without persisting it', () => {
    mockPagesState.pages = [
      page('p1', 'Cluster preview', [DEPTH_TIDE_WIDGET, ANCHOR_WATCH_WIDGET], { display_id: 'd1', dwell_seconds: 30 }),
    ]
    window.history.replaceState({}, '', '/display/flybridge')
    render(<App />)

    expect(document.documentElement.classList.contains('dark')).toBe(true)
    expect(screen.getByTestId('display-anchor-watch-isdark')).toHaveTextContent('true')
    expect(mockToggleDarkMode).not.toHaveBeenCalled()
    expect(globalThis.localStorage?.getItem('ui.darkMode')).toBeNull()
  })

  // mate-answer-toast plan: the wall display never opens a background
  // GET .../run stream for a watched conversation, even one already
  // registered in the (per-tab, in-memory) watch store - the answer watcher
  // is App.tsx's `enabled: !isDisplay` gate on useMateAnswerWatcher. In real
  // use, a wall tab could never have registered anything itself (no Mate UI
  // is reachable from /display/<slug> at all), but this pins the gate down
  // directly rather than relying on that indirect guarantee.
  it('never opens a background answer-watch stream for the wall display', async () => {
    registerMateWatch('c1', Date.now())
    try {
      const fetchMock = vi.fn(async (url: string) => (
        typeof url === 'string' && url.endsWith('/api/health')
          ? { ok: true, json: async () => ({ status: 'ok', version: 'v0.17.0', revision: 'deadbeef' }) }
          : { ok: false, json: async () => ({}) }
      ))
      vi.stubGlobal('fetch', fetchMock)

      window.history.replaceState({}, '', '/display/flybridge')
      render(<App />)

      await waitFor(() => expect(screen.getByTestId('display-shell-outer')).toBeInTheDocument())
      expect(fetchMock.mock.calls.some(([url]) => String(url).includes('/run'))).toBe(false)
    } finally {
      removeMateWatch('c1')
    }
  })

  it('cycles only the resolved display\'s own pages: a page assigned to another display never shows', async () => {
    mockDisplaysState.displays = [FLYBRIDGE, SALOON]
    mockPagesState.pages = [
      page('p1', 'Engines', [DEPTH_TIDE_WIDGET], { display_id: 'd1', dwell_seconds: 30 }),
      page('p2', 'Anchor', [WIND_WIDGET], { display_id: 'd1', dwell_seconds: 30 }),
      page('p3', 'Saloon Page', [ANCHOR_WATCH_WIDGET], { display_id: 'd2', dwell_seconds: 30 }),
    ]
    window.history.replaceState({}, '', '/display/flybridge')
    render(<App />)

    expect(screen.getByTestId('display-shell-outer')).toBeInTheDocument()
    expect(screen.getByText('Depth & Tide')).toBeInTheDocument()
    expect(screen.queryByTestId('display-anchor-watch-isdark')).not.toBeInTheDocument()

    fireEvent.keyDown(window, { key: 'ArrowRight' })
    expect(screen.getByText('Apparent Wind - Course Up')).toBeInTheDocument()
    expect(screen.queryByTestId('display-anchor-watch-isdark')).not.toBeInTheDocument()

    // Wraps back to the first page (a step landing on index 0 refetches
    // first - use-display-rotation.ts's stepWrap - so this settles async).
    // Still never the other display's page.
    fireEvent.keyDown(window, { key: 'ArrowRight' })
    await waitFor(() => expect(screen.getByText('Depth & Tide')).toBeInTheDocument())
    expect(screen.queryByTestId('display-anchor-watch-isdark')).not.toBeInTheDocument()
  })
})

describe('App at /display (diagnostic states)', () => {
  beforeEach(() => {
    vi.spyOn(console, 'error').mockImplementation(() => {})
    globalThis.localStorage?.clear()
    mockTelemetryStatus.value = 'connected'
    mockAlarmsState.alarms = []
    mockPagesState.pages = []
    mockPagesState.loading = false
    mockDisplaysState.displays = [FLYBRIDGE, SALOON]
    mockDisplaysState.loading = false
  })

  it('shows a quiet loading state while the displays list is still loading, not a feed', () => {
    mockDisplaysState.loading = true
    window.history.replaceState({}, '', '/display/flybridge')
    render(<App />)

    expect(screen.queryByTestId('display-shell-outer')).not.toBeInTheDocument()
    expect(screen.getByText(/loading/i)).toBeInTheDocument()
  })

  it('shows a diagnostic naming every configured display and its URL for a bare /display, never falling back to one of them', () => {
    window.history.replaceState({}, '', '/display')
    render(<App />)

    expect(screen.queryByTestId('display-shell-outer')).not.toBeInTheDocument()
    expect(screen.getByText(/flybridge/i)).toBeInTheDocument()
    expect(screen.getByText(/\/display\/flybridge/)).toBeInTheDocument()
    expect(screen.getByText(/saloon tv/i)).toBeInTheDocument()
    expect(screen.getByText(/\/display\/saloon-tv/)).toBeInTheDocument()
  })

  it('shows the same diagnostic, not a feed, for an unknown slug', () => {
    window.history.replaceState({}, '', '/display/nope')
    render(<App />)

    expect(screen.queryByTestId('display-shell-outer')).not.toBeInTheDocument()
    expect(screen.getByText(/no display is configured at "nope"/i)).toBeInTheDocument()
    expect(screen.getByText(/flybridge/i)).toBeInTheDocument()
  })
})

describe('App at / (dashboard authoring)', () => {
  beforeEach(() => {
    vi.spyOn(console, 'error').mockImplementation(() => {})
    globalThis.localStorage?.clear()
    mockTelemetryStatus.value = 'connected'
    mockAlarmsState.alarms = []
    mockDisplaysState.displays = [FLYBRIDGE]
    mockDisplaysState.loading = false
    mockPagesState.pages = [
      page('p1', 'Wall Engines', [DEPTH_TIDE_WIDGET], { display_id: 'd1', dwell_seconds: 30 }),
      page('p2', 'Other page', [WIND_WIDGET]),
    ]
    mockPagesState.loading = false
  })

  it('lists each display under the sidebar\'s "Wall displays" group, linking to /display/<slug> in a new tab, and opens the management dialog from the group header', () => {
    window.history.replaceState({}, '', '/')
    render(<App />)

    const link = screen.getByRole('link', { name: /flybridge/i })
    expect(link).toHaveAttribute('href', '/display/flybridge')
    expect(link).toHaveAttribute('target', '_blank')

    fireEvent.click(screen.getByRole('button', { name: 'Wall displays' }))
    const dialog = screen.getByRole('dialog')
    expect(within(dialog).getByText('Wall displays')).toBeInTheDocument()
  })

  it('keeps a page assigned to a display out of the Dashboard sub-list, and lists it nested under its display in the sidebar', () => {
    window.history.replaceState({}, '', '/')
    render(<App />)

    expect(screen.getByRole('button', { name: 'Other page' })).toBeInTheDocument()

    const flybridgeLi = screen.getByRole('link', { name: /flybridge/i }).closest('li')
    expect(flybridgeLi).not.toBeNull()
    expect(within(flybridgeLi as HTMLElement).getByText(/wall engines/i)).toBeInTheDocument()
    // Exactly one occurrence in the whole document - proof it isn't ALSO
    // sitting in the Dashboard sub-list somewhere else.
    expect(screen.getAllByText(/wall engines/i)).toHaveLength(1)
  })

  // App.tsx:674/716 used to compute `pages[0]?.id` directly - a wall page at
  // position 0 (server order) opened directly on '/', the opposite of
  // moving wall pages out of the Dashboard list. firstDashboardPageId must
  // skip it.
  it("skips a page assigned to a display when resolving the first page for '/'", () => {
    window.history.replaceState({}, '', '/')
    render(<App />)

    expect(screen.getByText('Apparent Wind - Course Up')).toBeInTheDocument()
    expect(screen.queryByText('Depth & Tide')).not.toBeInTheDocument()
  })

  it('shows the fold guide in edit mode on a page assigned to a display with a measured canvas, not on an unassigned one', () => {
    window.history.replaceState({}, '', '/')
    render(<App />)

    fireEvent.click(screen.getByRole('button', { name: /wall engines/i }))
    fireEvent.click(screen.getByLabelText('Enter edit mode'))
    expect(screen.getByTestId('display-fold')).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: 'Other page' }))
    expect(screen.queryByTestId('display-fold')).not.toBeInTheDocument()
  })

  it('stays on the stored (light) preference outside the display route', () => {
    mockPagesState.pages = [
      page('p1', 'Cluster preview', [DEPTH_TIDE_WIDGET, ANCHOR_WATCH_WIDGET]),
    ]
    window.history.replaceState({}, '', '/')
    render(<App />)

    expect(document.documentElement.classList.contains('dark')).toBe(false)
    expect(screen.getByTestId('display-anchor-watch-isdark')).toHaveTextContent('false')
  })

  it('renders the pinned indicator ribbon outside the display route', () => {
    window.history.replaceState({}, '', '/')
    render(<App />)

    expect(screen.getByTestId('dashboard-ribbon')).toBeInTheDocument()
  })

  it('duplicating a page onto a display copies its widgets/skin/dwell/condition, sets display_id, and drops the hero', async () => {
    mockPagesState.pages = [
      page('p1', 'Wall Engines', [DEPTH_TIDE_WIDGET], { display_id: 'd1', dwell_seconds: 30, hero: 'depth-tide', skin: 'instrument' }),
      page('p2', 'Other page', [WIND_WIDGET]),
    ]
    mockCreatePage.mockResolvedValueOnce(page('p3', 'Untitled page', [DEPTH_TIDE_WIDGET], { display_id: 'd1' }))
    window.history.replaceState({}, '', '/')
    render(<App />)

    fireEvent.click(screen.getByRole('button', { name: /wall engines/i }))
    fireEvent.click(screen.getByLabelText('Enter edit mode'))
    fireEvent.click(screen.getByRole('button', { name: /duplicate to/i }))
    fireEvent.click(screen.getByRole('button', { name: 'Flybridge' }))

    await waitFor(() => expect(mockCreatePage).toHaveBeenCalledTimes(1))
    const [name, init] = mockCreatePage.mock.calls[0]
    expect(name).toBe('Untitled page')
    expect(init).toMatchObject({ display_id: 'd1', skin: 'instrument', dwell_seconds: 30 })
    expect(init.hero).toBeUndefined()
    expect(init.widgets).toEqual([DEPTH_TIDE_WIDGET])
    expect(init.widgets).not.toBe(mockPagesState.pages[0].widgets) // deep-cloned, not the same array
  })

  it('duplicating an ordinary page onto a display gives it a dwell, since the server requires one', async () => {
    // The source is a plain Dashboard page, so it has no dwell to copy.
    // Sending display_id with an undefined dwell_seconds is rejected
    // outright by the backend ("a page on a display requires
    // dwell_seconds"), which made Duplicate-to-display fail for exactly the
    // case it exists to serve: starting a wall version of a normal page.
    mockPagesState.pages = [
      page('p1', 'Engines', [WIND_WIDGET]),
    ]
    mockCreatePage.mockResolvedValueOnce(page('p3', 'Untitled page', [WIND_WIDGET], { display_id: 'd1', dwell_seconds: 30 }))
    window.history.replaceState({}, '', '/')
    render(<App />)

    fireEvent.click(screen.getByLabelText('Enter edit mode'))
    fireEvent.click(screen.getByRole('button', { name: /duplicate to/i }))
    fireEvent.click(screen.getByRole('button', { name: 'Flybridge' }))

    await waitFor(() => expect(mockCreatePage).toHaveBeenCalledTimes(1))
    const [, init] = mockCreatePage.mock.calls[0]
    expect(init.display_id).toBe('d1')
    expect(init.dwell_seconds).toBe(DEFAULT_DWELL_SECONDS)
  })

  it('duplicating a page onto the Dashboard (no display) keeps its hero', async () => {
    mockPagesState.pages = [
      page('p1', 'Source', [WIND_WIDGET], { hero: 'wind' }),
    ]
    mockCreatePage.mockResolvedValueOnce(page('p3', 'Untitled page', [WIND_WIDGET], { hero: 'wind' }))
    window.history.replaceState({}, '', '/')
    render(<App />)

    fireEvent.click(screen.getByRole('button', { name: 'Source' }))
    fireEvent.click(screen.getByLabelText('Enter edit mode'))
    fireEvent.click(screen.getByRole('button', { name: /duplicate to/i }))
    fireEvent.click(screen.getByRole('button', { name: 'Dashboard (no display)' }))

    await waitFor(() => expect(mockCreatePage).toHaveBeenCalledTimes(1))
    const [, init] = mockCreatePage.mock.calls[0]
    expect(init.display_id).toBeUndefined()
    expect(init.hero).toBe('wind')
  })

  it('toasts that the hero was cleared when assigning a page with a hero to a display', async () => {
    mockPagesState.pages = [
      page('p1', 'Engines', [DEPTH_TIDE_WIDGET], { hero: 'depth-tide' }),
    ]
    window.history.replaceState({}, '', '/')
    render(<App />)

    fireEvent.click(screen.getByLabelText('Enter edit mode'))
    fireEvent.change(screen.getByLabelText('Wall display for Engines'), { target: { value: 'd1' } })

    await waitFor(() => expect(mockUpdatePage).toHaveBeenCalled())
    expect(await screen.findByText('Engines is now on Flybridge. Its hero tile was cleared.')).toBeInTheDocument()
  })

  it('does not toast when the page assigned to a display has no hero to clear', async () => {
    mockPagesState.pages = [
      page('p1', 'Engines', [DEPTH_TIDE_WIDGET]),
    ]
    window.history.replaceState({}, '', '/')
    render(<App />)

    fireEvent.click(screen.getByLabelText('Enter edit mode'))
    fireEvent.change(screen.getByLabelText('Wall display for Engines'), { target: { value: 'd1' } })

    await waitFor(() => expect(mockUpdatePage).toHaveBeenCalled())
    expect(screen.queryByText(/hero tile was cleared/)).not.toBeInTheDocument()
  })

  it('does not toast when clearing a display assignment, even on a page that (still) carries a hero', async () => {
    mockPagesState.pages = [
      page('p1', 'Engines', [DEPTH_TIDE_WIDGET], { display_id: 'd1', dwell_seconds: 30, hero: 'depth-tide' }),
    ]
    window.history.replaceState({}, '', '/')
    render(<App />)

    fireEvent.click(screen.getByLabelText('Enter edit mode'))
    fireEvent.change(screen.getByLabelText('Wall display for Engines'), { target: { value: '' } })

    await waitFor(() => expect(mockUpdatePage).toHaveBeenCalled())
    expect(screen.queryByText(/hero tile was cleared/)).not.toBeInTheDocument()
  })
})
