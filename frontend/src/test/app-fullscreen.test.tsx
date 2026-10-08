/**
 * Full screen: the header's "Full screen" button (Maximize) requests
 * fullscreen on the whole document, and while fullscreen the sidebar and
 * header disappear in favour of a floating "Exit full screen" button
 * (Minimize) - the touch affordance for a screen with no Esc key. Mirrors
 * the mock set in app-sidebar-navigation.test.tsx, which is the practical
 * existing App-level harness (renders the real App with every data hook
 * mocked, stable fetch stubs for /api/health and the assistant).
 *
 * jsdom has no Fullscreen API at all, so document.fullscreenEnabled,
 * .fullscreenElement, .requestFullscreen and .exitFullscreen are stubbed
 * per MDN's Fullscreen API shape - see use-fullscreen.test.ts for the
 * hook-level coverage of the API itself (feature detection, rejection
 * handling, the fullscreenchange listener).
 */
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { render, screen, fireEvent, act } from '@testing-library/react'
import { App } from '../App'
import type { DashboardPage } from '@/hooks/use-dashboard-pages'

function stubFullscreenEnabled(enabled: boolean | undefined) {
  Object.defineProperty(document, 'fullscreenEnabled', { value: enabled, configurable: true })
}

function stubFullscreenElement(el: Element | null) {
  Object.defineProperty(document, 'fullscreenElement', { value: el, configurable: true })
}

function fireFullscreenChange(el: Element | null) {
  stubFullscreenElement(el)
  act(() => { document.dispatchEvent(new Event('fullscreenchange')) })
}

// A single touch, down then up, past use-swipe-paging's distance/ratio/
// duration thresholds - see use-swipe-paging.test.ts for coverage of the
// thresholds themselves; this file only needs one clean gesture each way.
function swipeLeft(target: Element) {
  fireEvent.pointerDown(target, { pointerType: 'touch', pointerId: 1, clientX: 250, clientY: 100 })
  fireEvent.pointerUp(target, { pointerType: 'touch', pointerId: 1, clientX: 150, clientY: 100 })
}

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
  vi.stubGlobal('fetch', vi.fn(async (url: string) => {
    if (typeof url !== 'string') return { ok: false, json: async () => ({}) }
    if (url.endsWith('/api/health')) {
      return { ok: true, json: async () => ({ status: 'ok', version: 'v0.17.0', revision: 'deadbeef' }) }
    }
    if (url.endsWith('/api/assistant/status')) {
      return { ok: true, json: async () => ({ enabled: false, configured: false, model: '', problem: 'Set up the assistant in Settings → Assistant.' }) }
    }
    if (url.endsWith('/api/assistant/conversations')) {
      return { ok: true, json: async () => ({ conversations: [] }) }
    }
    return { ok: false, json: async () => ({}) }
  }))
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
    currentW: null, todayKWh: null, yesterdayKWh: null, peakTodayW: null, controllers: [],
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

const mockDashboardPages: DashboardPage[] = [{
  id: 'p1',
  name: 'Anchored',
  widgets: [
    { id: 'depth-tide', x: 0, y: 0, w: 4, h: 7 },
  ],
  created_at: '',
  updated_at: '',
}, {
  id: 'p2',
  name: 'Underway',
  widgets: [
    { id: 'wind', x: 0, y: 0, w: 4, h: 7 },
  ],
  created_at: '',
  updated_at: '',
}]

vi.mock('@/hooks/use-dashboard-pages', () => ({
  useDashboardPages: () => ({
    pages: mockDashboardPages,
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

// A mutable fixture (mirroring mockDashboardPages above) so the swipe-paging
// tests can start on a page other than the first without a second vi.mock -
// reset in this file's own beforeEach so it never leaks between tests.
let mockActivePageId: string | null = 'p1'
const mockSetActivePageId = vi.fn()

vi.mock('@/hooks/use-active-dashboard-page', () => ({
  useActiveDashboardPageId: () => [mockActivePageId, mockSetActivePageId],
}))

vi.mock('@/hooks/use-routes', () => ({
  useRoutes: () => ({ routes: [], loading: false, error: null, refetch: vi.fn(), createRoute: vi.fn(), updateRoute: vi.fn(), deleteRoute: vi.fn() }),
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
  forecastWarningDetailsUrl: () => null,
}))

// One live, acknowledgeable alarm - enough to drive the AlarmBanner (see
// app-mate-voice.test.tsx's own mock for the same hook) so its own
// right-aligned View/Acknowledge buttons are actually on screen for the
// exit-button overlap test below. collisionAlarmStatesByVessel is
// re-exported from this module and imported alongside useAlarms in
// App.tsx, so it has to be mocked here too.
const mockAlarm = {
  rule_id: 'helmcentral:test-alarm',
  label: 'Test alarm',
  path: 'notifications.test',
  phase: 'active' as const,
  state: 'alarm' as const,
  value: 1,
  message: 'Test alarm firing',
  silenced: false,
  can_silence: false,
  can_acknowledge: true,
}

vi.mock('@/hooks/use-alarms', () => ({
  useAlarms: () => ({ alarms: [mockAlarm], worst: 'alarm', acknowledge: vi.fn(), silence: vi.fn() }),
  collisionAlarmStatesByVessel: () => new Map(),
}))

vi.mock('@/hooks/use-server-trails', () => ({
  useServerTrails: () => ({ getSelfTrail: vi.fn(), getAisTrails: vi.fn() }),
}))

vi.mock('@/hooks/use-dark-mode', () => ({
  useDarkMode: () => [false, vi.fn()],
}))

const { mockWakeLock, mockServedOverHttps } = vi.hoisted(() => ({
  mockWakeLock: vi.fn(() => ({ status: 'off' as const })),
  mockServedOverHttps: vi.fn(() => true),
}))
vi.mock('@/hooks/use-screen-wake-lock', () => ({
  useScreenWakeLock: mockWakeLock,
  servedOverHttps: mockServedOverHttps,
}))

describe('App full screen', () => {
  beforeEach(() => {
    vi.spyOn(console, 'error').mockImplementation(() => {})
    // happy-dom (this project's test environment) implements none of the
    // Fullscreen API, so document.fullscreenElement reads back `undefined`
    // rather than `null` until a test defines it - set the real not-in-
    // fullscreen value explicitly rather than relying on that default.
    stubFullscreenElement(null)
    mockActivePageId = 'p1'
    mockSetActivePageId.mockClear()
    mockWakeLock.mockClear()
    mockServedOverHttps.mockReturnValue(true)
  })

  afterEach(() => {
    stubFullscreenEnabled(undefined)
    stubFullscreenElement(null)
    // @ts-expect-error - test-only cleanup of a property we defined ourselves
    delete document.documentElement.requestFullscreen
    // @ts-expect-error - test-only cleanup of a property we defined ourselves
    delete document.exitFullscreen
  })

  it('shows the "Full screen" header button when the Fullscreen API is available', () => {
    stubFullscreenEnabled(true)
    render(<App />)

    expect(screen.getByRole('button', { name: 'Full screen' })).toBeInTheDocument()
  })

  it('keeps the screen awake while full screen over HTTPS', () => {
    stubFullscreenEnabled(true)
    render(<App />)
    expect(mockWakeLock).toHaveBeenLastCalledWith(false)

    fireFullscreenChange(document.documentElement)
    expect(mockWakeLock).toHaveBeenLastCalledWith(true)

    fireFullscreenChange(null)
    expect(mockWakeLock).toHaveBeenLastCalledWith(false)
  })

  it('does not ask for a wake lock over plain HTTP', () => {
    mockServedOverHttps.mockReturnValue(false)
    stubFullscreenEnabled(true)
    render(<App />)

    fireFullscreenChange(document.documentElement)
    expect(mockWakeLock).toHaveBeenLastCalledWith(false)
  })

  it('has no "Full screen" button when the Fullscreen API is unavailable (iPhone Safari)', () => {
    stubFullscreenEnabled(undefined)
    render(<App />)

    expect(screen.queryByRole('button', { name: 'Full screen' })).not.toBeInTheDocument()
  })

  it('clicking "Full screen" requests fullscreen on the document', () => {
    stubFullscreenEnabled(true)
    const requestFullscreen = vi.fn().mockResolvedValue(undefined)
    document.documentElement.requestFullscreen = requestFullscreen
    render(<App />)

    fireEvent.click(screen.getByRole('button', { name: 'Full screen' }))

    expect(requestFullscreen).toHaveBeenCalledTimes(1)
  })

  it('hides the sidebar and header, and shows the floating exit button, once fullscreenchange reports the document element', () => {
    stubFullscreenEnabled(true)
    render(<App />)

    // Sanity check on the starting (non-fullscreen) chrome.
    expect(screen.getByRole('button', { name: 'Full screen' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Dashboard' })).toBeInTheDocument()

    fireFullscreenChange(document.documentElement)

    expect(screen.queryByRole('button', { name: 'Full screen' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Dashboard' })).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Exit full screen' })).toBeInTheDocument()
    // The grid itself stays on screen - full screen only removes the chrome.
    expect(screen.getByText('Depth & Tide')).toBeInTheDocument()
  })

  // [P1 finding] The floating exit button sits top-right, the same corner
  // the live alarm banner's own View/Acknowledge buttons land in once the
  // header is gone (AlarmBanner's action row is always the trailing,
  // right-aligned child of its row). Unpadded, a tap meant for "View" landed
  // on the exit button instead, so the banner stack reserves the button's
  // width on its right while full screen. happy-dom does no real CSS layout,
  // so this can't assert actual pixel overlap; it checks the Tailwind
  // classes directly instead, the same way test/helpers/z-ladder.tsx checks
  // z-index classes without a real layout engine.
  it('floats top-right with the live alarm banner padded clear of it', () => {
    stubFullscreenEnabled(true)
    render(<App />)

    fireFullscreenChange(document.documentElement)

    // Sanity: the banner (with its own right-aligned actions) is actually
    // showing in this state.
    expect(screen.getByRole('button', { name: 'View' })).toBeInTheDocument()

    const exitButton = screen.getByRole('button', { name: 'Exit full screen' })
    expect(exitButton.className).toMatch(/\bfixed\b/)
    expect(exitButton.className).toMatch(/\btop-2\b/)
    expect(exitButton.className).toMatch(/\bright-2\b/)
    expect(screen.getByTestId('alarm-banner-stack').className).toMatch(/\bpr-12\b/)
  })

  it('clicking "Exit full screen" calls document.exitFullscreen()', () => {
    stubFullscreenEnabled(true)
    const exitFullscreen = vi.fn().mockResolvedValue(undefined)
    document.exitFullscreen = exitFullscreen
    render(<App />)

    fireFullscreenChange(document.documentElement)
    fireEvent.click(screen.getByRole('button', { name: 'Exit full screen' }))

    expect(exitFullscreen).toHaveBeenCalledTimes(1)
  })

  it('brings the header and sidebar back (and drops the floating exit button) if the operator navigates to a panel while fullscreen', () => {
    stubFullscreenEnabled(true)
    render(<App />)

    fireFullscreenChange(document.documentElement)
    expect(screen.queryByRole('button', { name: 'Dashboard' })).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Exit full screen' })).toBeInTheDocument()

    // Same navigation path app-deep-link.test.tsx uses for a URL-driven
    // panel change (e.g. tapping an alarm notification) - jsdom doesn't
    // replay a pushState history stack, so replaceState + a manual
    // popstate is how that suite drives the same handler.
    act(() => {
      window.history.replaceState({}, '', '/alarms')
      window.dispatchEvent(new PopStateEvent('popstate'))
    })

    // Chrome is back - fullscreen itself was never exited, only the
    // dashboard-only hiding gate (activePanel !== null now).
    expect(screen.getByRole('button', { name: 'Dashboard' })).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Exit full screen' })).not.toBeInTheDocument()
  })

  it('a touch swipe left on the dashboard switches to the next page and shows its name and position, while full screen', () => {
    stubFullscreenEnabled(true)
    render(<App />)
    fireFullscreenChange(document.documentElement)

    swipeLeft(screen.getByTestId('dashboard-swipe-surface'))

    expect(mockSetActivePageId).toHaveBeenCalledWith('p2')
    // The sidebar and page switcher (which would also say "Underway", as a
    // page-list entry) are both gone while full screen - this is the toast.
    expect(screen.getByText('Underway')).toBeInTheDocument()
    expect(screen.getByText('2 / 2')).toBeInTheDocument()
  })

  it('a touch swipe left does nothing at the last page (no wrap)', () => {
    mockActivePageId = 'p2'
    stubFullscreenEnabled(true)
    render(<App />)
    fireFullscreenChange(document.documentElement)

    swipeLeft(screen.getByTestId('dashboard-swipe-surface'))

    expect(mockSetActivePageId).not.toHaveBeenCalled()
    expect(screen.queryByText('2 / 2')).not.toBeInTheDocument()
  })

  it('a touch swipe does nothing when not full screen', () => {
    stubFullscreenEnabled(true)
    render(<App />)
    // Deliberately not calling fireFullscreenChange - still the ordinary,
    // chrome-on dashboard.

    swipeLeft(screen.getByTestId('dashboard-swipe-surface'))

    expect(mockSetActivePageId).not.toHaveBeenCalled()
    expect(screen.queryByText('2 / 2')).not.toBeInTheDocument()
  })

  // A wall-display page opened from the sidebar is not in the dashboard page
  // list; a swipe must not jump to some unrelated position in that list.
  it('a touch swipe does nothing when the page on screen is not in the dashboard page list', () => {
    mockActivePageId = 'wall-page'
    stubFullscreenEnabled(true)
    render(<App />)
    fireFullscreenChange(document.documentElement)

    swipeLeft(screen.getByTestId('dashboard-swipe-surface'))

    expect(mockSetActivePageId).not.toHaveBeenCalled()
  })
})
