/**
 * Sidebar navigation test: mounts <App> with all hooks mocked (same pattern
 * as App.smoke.test.tsx) and asserts that the shadcn Sidebar drives panel
 * navigation in place of the old BottomDrawer:
 *  - the dashboard grid is visible by default (Sidebar's "Dashboard" nav item present)
 *  - clicking the "Forecast" sidebar nav button swaps the content region to the
 *    forecast panel (which now also hosts the Tide card, since the standalone
 *    "Tides" panel was folded into Forecast)
 *  - clicking "Dashboard" again returns to the grid view
 */
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import { App } from '../App'

// ── stub fetch so components that call it don't throw ─────────────────────────
beforeEach(() => {
  vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
    ok: false,
    json: async () => ({}),
  }))
})

// ── hook mocks (mirrors App.smoke.test.tsx) ────────────────────────────────────

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

vi.mock('@/hooks/use-anchor-watch', () => ({
  useAnchorWatch: () => ({
    anchorState: 'none', anchorLat: null, anchorLon: null, radiusMeters: 0,
    rodeDeployedM: 0, seaState: 'calm', seabedType: 'sand',
    distanceMeters: null, bearingDeg: null, suggestSet: false,
    setAnchorHere: vi.fn(), updateRadius: vi.fn(),
    updateRodeAndConditions: vi.fn(), clearAnchor: vi.fn(),
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

// ForecastTideSection (rendered as the last card in Forecast) owns its own
// data-fetching via these hooks - mocked here (rather than mocking the
// component itself, since this suite is testing App's real sidebar/panel
// wiring) so the "Forecast" panel is reachable without hitting real fetches.
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

vi.mock('@/config/app-config', () => ({
  appConfig: { boat: { name: 'Test Vessel', model: 'Test' } },
  FORECAST_REFRESH_SECONDS: 600,
  getUiConfig: () => ({ vesselStateRefreshSeconds: 10, distanceUnits: 'metric' }),
  getAnchorConfig: () => ({
    bowRollerHeightM: 0, chainSizeMm: 10, chainOnboardM: 50,
    hullType: 'power_cat', windageAreaM2: 10,
  }),
}))

vi.mock('@/hooks/use-dashboard-pages', () => ({
  useDashboardPages: () => ({
    pages: [{
      id: 'p1',
      name: 'Anchored',
      widgets: [
        { id: 'vessel', x: 0, y: 0, w: 12, h: 3 },
        { id: 'wind', x: 0, y: 3, w: 4, h: 8 },
        { id: 'depth-tide', x: 0, y: 11, w: 4, h: 7 },
        { id: 'position', x: 0, y: 18, w: 4, h: 5 },
        { id: 'today-now', x: 0, y: 23, w: 4, h: 5 },
        { id: 'anchor-watch', x: 4, y: 3, w: 4, h: 8 },
        { id: 'rode-scope', x: 4, y: 11, w: 4, h: 6 },
        { id: 'tanks', x: 4, y: 17, w: 4, h: 4 },
        { id: 'route', x: 4, y: 21, w: 4, h: 4 },
        { id: 'nearby-vessels', x: 4, y: 25, w: 4, h: 5 },
        { id: 'battery-power', x: 8, y: 3, w: 4, h: 14 },
        { id: 'alternator', x: 8, y: 17, w: 4, h: 6 },
        { id: 'generator', x: 8, y: 23, w: 4, h: 5 },
        { id: 'czone-switches', x: 8, y: 28, w: 4, h: 6 },
      ],
      created_at: '',
      updated_at: '',
    }, {
      id: 'p2',
      name: 'Underway',
      widgets: [],
      created_at: '',
      updated_at: '',
    }],
    loading: false,
    error: null,
    refetch: vi.fn(),
    createPage: vi.fn(),
    updatePage: vi.fn(),
    deletePage: vi.fn(),
  }),
}))

const mockSetActivePageId = vi.fn()

vi.mock('@/hooks/use-active-dashboard-page', () => ({
  useActiveDashboardPageId: () => ['p1', mockSetActivePageId],
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

vi.mock('@/hooks/use-dark-mode', () => ({
  useDarkMode: () => [false, vi.fn()],
}))

// ── tests ─────────────────────────────────────────────────────────────────────

describe('App sidebar navigation', () => {
  beforeEach(() => {
    vi.spyOn(console, 'error').mockImplementation(() => {})
  })

  it('shows the dashboard by default, navigates to a panel via the sidebar, and back', () => {
    render(<App />)

    // Dashboard grid visible by default — "Depth & Tide" tile is part of the grid.
    expect(screen.getByText('Depth & Tide')).toBeInTheDocument()

    // Sidebar nav has a "Dashboard" item, active by default. It's the first
    // "Dashboard"-labelled button (a "Back to dashboard" button also appears
    // once a panel is open, so disambiguate by taking the nav item).
    const dashboardNavButton = screen.getAllByRole('button', { name: /dashboard/i })[0]
    expect(dashboardNavButton).toBeInTheDocument()

    // The standalone "Tides" panel/nav entry was removed — the tide chart
    // now lives inside Forecast instead.
    expect(screen.queryByRole('button', { name: /^tides$/i })).not.toBeInTheDocument()

    // Click "Forecast" in the sidebar nav — the Tide chart now lives inside
    // this panel (as its last card) rather than in its own standalone panel.
    const forecastNavButton = screen.getByRole('button', { name: /forecast/i })
    fireEvent.click(forecastNavButton)

    // The dashboard grid tile content should no longer be present; the
    // forecast panel's Tide card should be.
    expect(screen.queryByText('Depth & Tide')).not.toBeInTheDocument()
    expect(screen.getAllByText(/tide/i).length).toBeGreaterThan(0)

    // Navigate back to the dashboard via the sidebar nav item.
    fireEvent.click(screen.getAllByRole('button', { name: /dashboard/i })[0])
    expect(screen.getByText('Depth & Tide')).toBeInTheDocument()
  })

  it('opens the Forecast panel when the "Depth & Tide" dashboard tile is clicked', () => {
    render(<App />)

    fireEvent.click(screen.getByText('Depth & Tide'))

    // DepthTideTile's onOpen now routes to 'forecast', not the removed 'tides' panel.
    expect(screen.queryByText('Depth & Tide')).not.toBeInTheDocument()
    expect(screen.getAllByText(/tide/i).length).toBeGreaterThan(0)
  })

  it('lists dashboard pages as sidebar sub-items and navigates between them', () => {
    render(<App />)

    // Both pages appear as sub-items under the "Dashboard" nav item.
    const anchoredSubItem = screen.getByRole('button', { name: 'Anchored' })
    const underwaySubItem = screen.getByRole('button', { name: 'Underway' })
    expect(anchoredSubItem).toBeInTheDocument()
    expect(underwaySubItem).toBeInTheDocument()

    // The active page ('p1' / "Anchored") is reflected via active styling.
    expect(anchoredSubItem).toHaveAttribute('data-active', 'true')
    expect(underwaySubItem).toHaveAttribute('data-active', 'false')

    // Clicking "Underway" sets the active page id to 'p2'.
    fireEvent.click(underwaySubItem)
    expect(mockSetActivePageId).toHaveBeenCalledWith('p2')
  })
})
