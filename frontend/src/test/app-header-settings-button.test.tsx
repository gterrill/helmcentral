/**
 * The header's "settings for this page" gear: a small number of panels
 * (Alarms, Anchor Watch, Mate, Radar/Mayara) have a matching Settings
 * section, and a shortcut in the header jumps straight there instead of
 * making the operator go back to the sidebar and find it in Settings
 * itself. Every other panel, Dashboard, and Settings itself have no such
 * shortcut (there's nowhere for it to point), so the button is absent.
 * Mirrors app-header-breadcrumb.test.tsx's hook mocking (same App tree).
 */
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, fireEvent, within } from '@testing-library/react'
import { App } from '../App'

// This test renders the dashboard, not the auth gate. State the precondition
// explicitly — an install with auth.mode:none — rather than depending on what
// the real useAuth happens to return when its probe fails (ADR 0040).
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

// ── stub fetch so components that call it don't throw ─────────────────────────
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

// The Logs section (Settings → Logs) opens a real EventSource, which jsdom
// doesn't implement — stubbed the same way settings-logs-section.test.tsx
// stubs it, so mounting Settings on /settings/logs doesn't throw.
vi.mock('@/hooks/use-logs', () => ({
  useLogs: () => ({
    logs: [], isLive: true, setIsLive: vi.fn(), clearLogs: vi.fn(), connected: true, error: null,
  }),
}))

// ── hook mocks (mirrors app-sidebar-navigation.test.tsx) ────────────────────────

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

vi.mock('@/hooks/use-dashboard-pages', () => ({
  useDashboardPages: () => ({
    pages: [{ id: 'p1', name: 'Page 1', widgets: [], created_at: '', updated_at: '' }],
    loading: false,
    error: null,
    refetch: vi.fn(),
    createPage: vi.fn(),
    updatePage: vi.fn(),
    deletePage: vi.fn(),
  }),
}))

vi.mock('@/hooks/use-active-dashboard-page', () => ({
  useActiveDashboardPageId: () => ['p1', vi.fn()],
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

vi.mock('@/hooks/use-server-trails', () => ({
  useServerTrails: () => ({ getSelfTrail: vi.fn(), getAisTrails: vi.fn() }),
}))

vi.mock('@/hooks/use-dark-mode', () => ({
  useDarkMode: () => [false, vi.fn()],
}))

// ── tests ─────────────────────────────────────────────────────────────────────

describe('App header settings-for-this-page button', () => {
  beforeEach(() => {
    vi.spyOn(console, 'error').mockImplementation(() => {})
  })

  it('shows "Alarms settings" on the Alarms panel', () => {
    window.history.replaceState({}, '', '/alarms')
    render(<App />)

    expect(screen.getByRole('button', { name: 'Alarms settings' })).toBeInTheDocument()
  })

  it('shows "Anchor Watch settings" on the Anchor Watch panel', () => {
    window.history.replaceState({}, '', '/anchor-watch')
    render(<App />)

    expect(screen.getByRole('button', { name: 'Anchor Watch settings' })).toBeInTheDocument()
  })

  it('shows "Mate settings" on the Mate panel', () => {
    window.history.replaceState({}, '', '/mate')
    render(<App />)

    expect(screen.getByRole('button', { name: 'Mate settings' })).toBeInTheDocument()
  })

  it('shows "Mayara settings" on the Radar panel', () => {
    window.history.replaceState({}, '', '/radar')
    render(<App />)

    expect(screen.getByRole('button', { name: 'Mayara settings' })).toBeInTheDocument()
  })

  it('hides the button on the Dashboard', () => {
    window.history.replaceState({}, '', '/')
    render(<App />)

    expect(screen.queryByRole('button', { name: 'Alarms settings' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Anchor Watch settings' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Mate settings' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Mayara settings' })).not.toBeInTheDocument()
  })

  it('hides the button on Settings itself', () => {
    window.history.replaceState({}, '', '/settings')
    render(<App />)

    expect(screen.queryByRole('button', { name: 'Alarms settings' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Anchor Watch settings' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Mate settings' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Mayara settings' })).not.toBeInTheDocument()
  })

  it('hides the button on a panel with no matching Settings section (Routes)', () => {
    window.history.replaceState({}, '', '/routes')
    render(<App />)

    expect(screen.queryByRole('button', { name: 'Alarms settings' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Anchor Watch settings' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Mate settings' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Mayara settings' })).not.toBeInTheDocument()
  })

  it('clicking it on Anchor Watch opens Settings with the Anchor Watch section active', async () => {
    window.history.replaceState({}, '', '/anchor-watch')
    render(<App />)

    fireEvent.click(screen.getByRole('button', { name: 'Anchor Watch settings' }))

    // Settings is a lazy chunk — wait for its SectionNav to actually mount.
    // Scoped to the SectionNav landmark, not a bare findByRole: the sidebar
    // has its own "Anchor Watch" panel nav button (always present, no
    // aria-current), which would otherwise satisfy the query before the
    // lazy chunk finishes loading and hide the real assertion behind a
    // false positive.
    const settingsSectionsNav = await screen.findByRole('navigation', { name: 'Settings sections' })
    const anchorWatchSectionButton = within(settingsSectionsNav).getByRole('button', { name: 'Anchor Watch' })
    expect(anchorWatchSectionButton).toHaveAttribute('aria-current', 'true')

    const breadcrumbNav = screen.getByRole('navigation', { name: /breadcrumb/i })
    expect(within(breadcrumbNav).getByText('Settings')).toBeInTheDocument()
    expect(within(breadcrumbNav).getByText('Anchor Watch')).toBeInTheDocument()
  })
})
