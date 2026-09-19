/**
 * ADR 0095: the in-app manual's three entry points, mounted in App.tsx - the
 * header's contextual `?`, the sidebar's Help item, and (elsewhere,
 * settings-page.test.tsx) Settings' own Manual button. Preamble copied from
 * app-mate-voice.test.tsx (same App tree, same reasons for each hook mock,
 * same stubFetch router-with-catch-all shape), trimmed of the voice-specific
 * pieces this suite doesn't touch.
 */
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { App } from '../App'

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

vi.mock('@/hooks/use-app-config', () => ({
  useAppConfig: () => ({
    ui: { distanceUnits: 'metric', autoCloseAnchorWatchOnEngine: true },
    anchor: {
      bowRollerHeightM: 0, chainSizeMm: 10, chainOnboardM: 50,
      hullType: 'power_cat', scopeMethod: 'ratio', windageAreaM2: 10,
      gpsFromBowM: 0, loaM: 0,
    },
    assistant: { voiceInput: false, readAloud: false, wakeWord: false },
    loaded: true,
  }),
  publishAppConfigSettings: vi.fn(),
}))

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
      current_tide_height_ft: -1, tide_direction: 'n/a',
      high_tide_time: new Date(0).toISOString(), high_tide_height_ft: -1,
      low_tide_time: new Date(0).toISOString(), low_tide_height_ft: -1,
    },
  }),
}))

vi.mock('@/hooks/use-weather-today', () => ({
  useWeatherToday: () => ({
    weather: {
      temperature_f: -1, condition: 'n/a',
      wind_speed_kts: -1, wind_direction: 'n/a', wind_gust_kts: -1, precipitation_pct: -1,
      provider: '', cached: false, updated_at: '', ttl_seconds: 0,
    },
  }),
}))

vi.mock('@/hooks/use-weather-forecast', () => ({
  useWeatherForecast: () => ({ forecast: [], loading: false, error: null, provider: null, refetch: vi.fn() }),
}))

vi.mock('@/hooks/use-wave-forecast', () => ({
  useWaveForecast: () => ({ days: [], seaTemperatureF: null, provider: null, loading: false, error: null, refetch: vi.fn() }),
}))

vi.mock('@/hooks/use-czone-switches', () => ({
  useCZoneSwitches: () => ({ switches: [], loading: false, pending: {}, toggleSwitch: vi.fn() }),
}))

vi.mock('@/hooks/use-depth-trend', () => ({ useDepthTrend: () => ({ points: [], since: 'window' }) }))

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
}))

vi.mock('@/hooks/use-server-trails', () => ({
  useServerTrails: () => ({ getSelfTrail: vi.fn(), getAisTrails: vi.fn() }),
}))

vi.mock('@/hooks/use-dark-mode', () => ({
  useDarkMode: () => [false, vi.fn()],
}))

vi.mock('@/hooks/use-alarms', () => ({
  useAlarms: () => ({ alarms: [], worst: 'normal', acknowledge: vi.fn(), silence: vi.fn() }),
  collisionAlarmStatesByVessel: () => new Map(),
}))

function stubFetch() {
  const fetchMock = vi.fn(async (url: string) => {
    if (typeof url !== 'string') return { ok: false, json: async () => ({}) }
    if (url.endsWith('/api/health')) {
      return { ok: true, json: async () => ({ status: 'ok', version: 'v0.20.0', revision: 'deadbeef' }) }
    }
    if (url.endsWith('/api/assistant/status')) {
      return { ok: true, json: async () => ({ enabled: false, configured: false, model: '' }) }
    }
    if (url.endsWith('/api/assistant/conversations')) {
      return { ok: true, json: async () => ({ conversations: [] }) }
    }
    if (url.includes('/api/manual/')) {
      // Never resolved: these tests only assert *that* the sheet fetched
      // the right id, not how it renders once loaded (manual-sheet.test.tsx
      // already covers every load state on its own).
      return new Promise(() => {})
    }
    // Everything else (dashboard pages, routes, tanks, ...) reports "not
    // found" - the hooks behind them tolerate that (App.smoke.test.tsx pins
    // this), and nothing this file asserts on depends on their data.
    return { ok: false, json: async () => ({}) }
  })
  vi.stubGlobal('fetch', fetchMock)
  return fetchMock
}

function manualFetchCalls(fetchMock: ReturnType<typeof stubFetch>): string[] {
  return fetchMock.mock.calls
    .map(([url]) => url as string)
    .filter((url) => url.includes('/api/manual/'))
}

describe('the in-app manual (ADR 0095)', () => {
  beforeEach(() => {
    vi.spyOn(console, 'error').mockImplementation(() => {})
    window.history.replaceState({}, '', '/')
  })

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('the header ? opens the manual for the current screen - the dashboard, then Forecast after navigating there', async () => {
    const fetchMock = stubFetch()
    render(<App />)

    fireEvent.click(screen.getByRole('button', { name: 'Open help' }))
    // ManualSheet is lazy-loaded (mounted only on this first open), so the
    // fetch its mount effect fires isn't necessarily on screen yet.
    await waitFor(() => expect(manualFetchCalls(fetchMock)).toContain('/api/manual/features/dashboard'))

    // Close the sheet (Escape) and move to Forecast before asking again -
    // the sheet only re-reads its target when it (re)opens.
    fireEvent.keyDown(document, { key: 'Escape' })
    fireEvent.click(screen.getByRole('button', { name: 'Forecast' }))
    fireEvent.click(screen.getByRole('button', { name: 'Open help' }))

    await waitFor(() => expect(manualFetchCalls(fetchMock)).toContain('/api/manual/features/forecast'))
  })

  it('carries the title "Help for this screen" and the CircleHelp icon', () => {
    stubFetch()
    render(<App />)

    const button = screen.getByRole('button', { name: 'Open help' })
    expect(button).toHaveAttribute('title', 'Help for this screen')
    expect(button.querySelector('svg')).toHaveClass('lucide-circle-help')
  })

  it('the sidebar Help item opens the contents page, and is never marked active', async () => {
    const fetchMock = stubFetch()
    render(<App />)

    const manualNavButton = screen.getByRole('button', { name: 'Help' })
    fireEvent.click(manualNavButton)

    await waitFor(() => expect(manualFetchCalls(fetchMock)).toContain('/api/manual/index'))
    expect(manualNavButton).toHaveAttribute('data-active', 'false')
  })

  it('/display/<slug> shows neither the header ? nor a sidebar Help item', () => {
    stubFetch()
    window.history.replaceState({}, '', '/display/flybridge')
    render(<App />)

    expect(screen.queryByRole('button', { name: 'Open help' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Help' })).not.toBeInTheDocument()
  })
})
