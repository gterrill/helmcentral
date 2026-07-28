/**
 * Covers App.tsx's navigation guard: while the Settings panel is open and
 * dirty, clicking away via the sidebar should intercept the navigation with
 * a confirmation AlertDialog (Cancel / Discard / Save and Continue) instead
 * of navigating immediately. Mirrors app-sidebar-navigation.test.tsx's hook
 * mocking (same App tree), plus mocks of the two settings-only hooks
 * (`use-settings-form` / `use-secrets-status`) so the Settings panel can be
 * reached and its dirty state driven directly from the test.
 */
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { App } from '../App'
import { SECRET_KEYS, type SecretKey } from '@/hooks/use-secrets-status'

// ── stub fetch so components that call it don't throw ─────────────────────────
beforeEach(() => {
  vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
    ok: false,
    json: async () => ({}),
  }))
})

// ── hook mocks (mirrors app-sidebar-navigation.test.tsx) ───────────────────────

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
    pages: [],
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

// ── settings-only hook mocks: give the test direct control over "dirty" ────────

const saveMock = vi.fn(async (patch: unknown) => {
  void patch
  return {}
})
const saveTouchedKeysMock = vi.fn(async (keys: SecretKey[]) => {
  void keys
  return {}
})

vi.mock('@/hooks/use-settings-form', async () => {
  const actual = await vi.importActual<typeof import('@/hooks/use-settings-form')>('@/hooks/use-settings-form')
  return {
    ...actual,
    useSettingsForm: () => ({
      settings: {},
      setSettings: vi.fn(),
      loading: false,
      saving: false,
      error: null,
      save: saveMock,
    }),
  }
})

const emptyTouched = () =>
  Object.fromEntries(SECRET_KEYS.map((key) => [key, false])) as Record<SecretKey, boolean>
let mockTouched: Record<SecretKey, boolean> = emptyTouched()

vi.mock('@/hooks/use-secrets-status', async () => {
  const actual = await vi.importActual<typeof import('@/hooks/use-secrets-status')>('@/hooks/use-secrets-status')
  return {
    ...actual,
    useSecretsStatus: () => ({
      status: Object.fromEntries(actual.SECRET_KEYS.map((key: SecretKey) => [key, false])),
      values: Object.fromEntries(actual.SECRET_KEYS.map((key: SecretKey) => [key, ''])),
      touched: mockTouched,
      loading: false,
      error: null,
      setFieldValue: vi.fn(),
      saveTouchedKeys: saveTouchedKeysMock,
      clearKey: vi.fn(),
    }),
  }
})

// ── tests ─────────────────────────────────────────────────────────────────────

/** Navigates to Settings and makes it dirty (touching a secret field's status). */
function navigateToDirtySettings() {
  fireEvent.click(screen.getByRole('button', { name: 'Settings' }))
  fireEvent.click(screen.getByRole('button', { name: 'Vessel' }))
  fireEvent.change(screen.getByLabelText('Vessel prefix'), { target: { value: 'S/V Test' } })
}

describe('App navigation guard on dirty Settings', () => {
  beforeEach(() => {
    mockTouched = emptyTouched()
    saveMock.mockReset().mockResolvedValue({})
    saveTouchedKeysMock.mockReset().mockResolvedValue({})
    vi.spyOn(console, 'error').mockImplementation(() => {})
  })

  it('opens the confirmation dialog instead of navigating immediately when leaving a dirty Settings page', async () => {
    render(<App />)
    navigateToDirtySettings()

    await waitFor(() => {
      expect(screen.getByLabelText('Vessel prefix')).toHaveValue('S/V Test')
    })

    fireEvent.click(screen.getByRole('button', { name: 'Forecast' }))

    // Still on Settings — the Forecast panel content did not take over.
    expect(screen.getByLabelText('Vessel prefix')).toBeInTheDocument()
    expect(screen.getByText('Unsaved changes')).toBeInTheDocument()
  })

  it('Cancel closes the dialog and stays on Settings', async () => {
    render(<App />)
    navigateToDirtySettings()
    fireEvent.click(screen.getByRole('button', { name: 'Forecast' }))

    expect(screen.getByText('Unsaved changes')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Cancel' }))

    await waitFor(() => {
      expect(screen.queryByText('Unsaved changes')).not.toBeInTheDocument()
    })
    expect(screen.getByLabelText('Vessel prefix')).toBeInTheDocument()
  })

  it('Discard navigates away without saving', async () => {
    render(<App />)
    navigateToDirtySettings()
    fireEvent.click(screen.getByRole('button', { name: 'Forecast' }))

    fireEvent.click(screen.getByRole('button', { name: 'Discard' }))

    await waitFor(() => {
      expect(screen.queryByLabelText('Vessel prefix')).not.toBeInTheDocument()
    })
    expect(saveMock).not.toHaveBeenCalled()
  })

  it('Save and Continue saves then navigates on success', async () => {
    render(<App />)
    navigateToDirtySettings()
    fireEvent.click(screen.getByRole('button', { name: 'Forecast' }))

    fireEvent.click(screen.getByRole('button', { name: /save and continue/i }))

    await waitFor(() => {
      expect(saveMock).toHaveBeenCalledTimes(1)
    })
    await waitFor(() => {
      expect(screen.queryByLabelText('Vessel prefix')).not.toBeInTheDocument()
    })
    expect(screen.queryByText('Unsaved changes')).not.toBeInTheDocument()
  })

  it('Save and Continue does NOT navigate away if the save fails', async () => {
    saveMock.mockRejectedValueOnce(new Error('boom'))
    render(<App />)
    navigateToDirtySettings()
    fireEvent.click(screen.getByRole('button', { name: 'Forecast' }))

    fireEvent.click(screen.getByRole('button', { name: /save and continue/i }))

    await waitFor(() => {
      expect(saveMock).toHaveBeenCalledTimes(1)
    })

    // Still on Settings — save failed, so navigation must not have happened.
    expect(screen.getByLabelText('Vessel prefix')).toBeInTheDocument()
  })
})
