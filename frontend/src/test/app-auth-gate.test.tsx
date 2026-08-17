/**
 * App-level auth gating (ADR 0040 §frontend): App.tsx must render the login
 * screen instead of the dashboard exactly when mode === 'signalk' and no
 * user is authenticated, and must render the dashboard completely untouched
 * — no login UI, no behaviour change — when mode === 'none', regardless of
 * whether a "user" happens to be present.
 *
 * Every non-auth hook is mocked the same way App.smoke.test.tsx does it, so
 * this file isolates the one thing it's testing: what App.tsx does with
 * useAuth()'s result. useAuth itself has its own dedicated state-machine
 * tests (use-auth.test.ts) and is not re-tested here.
 */
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen } from '@testing-library/react'
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
  useCZoneSwitches: () => ({ switches: [], loading: false, pending: new Set(), toggleSwitch: vi.fn() }),
}))

vi.mock('@/hooks/use-autopilot', () => ({
  useAutopilot: () => ({
    state: { present: false, engaged: false, state: null, mode: null, target: null, availableActions: [], stale: false },
    pending: new Set(),
    error: null,
    availableModes: [],
    capabilityError: null,
    engage: vi.fn(), disengage: vi.fn(), tack: vi.fn(), gybe: vi.fn(),
    adjustHeading: vi.fn(), setMode: vi.fn(), dodge: vi.fn(), clearDodge: vi.fn(),
  }),
}))

vi.mock('@/hooks/use-depth-trend', () => ({ useDepthTrend: () => ({ points: [], since: 'window' }) }))

vi.mock('@/hooks/use-app-config', () => ({
  useAppConfig: () => ({
    ui: { vesselStateRefreshSeconds: 10, distanceUnits: 'metric', autoCloseAnchorWatchOnEngine: true },
    anchor: {
      bowRollerHeightM: 0, chainSizeMm: 10, chainOnboardM: 50,
      hullType: 'power_cat', windageAreaM2: 10,
    },
    loaded: true,
  }),
  publishAppConfigSettings: vi.fn(),
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

vi.mock('@/hooks/use-auth', () => ({
  useAuth: vi.fn(),
}))

// ── tests ─────────────────────────────────────────────────────────────────────

import { useAuth } from '@/hooks/use-auth'

function mockAuth(overrides: Partial<ReturnType<typeof useAuth>>) {
  vi.mocked(useAuth).mockReturnValue({
    mode: 'none',
    user: null,
    role: null,
    loading: false,
    error: null,
    login: vi.fn(),
    logout: vi.fn(),
    ...overrides,
  })
}

describe('App auth gating', () => {
  beforeEach(() => {
    vi.spyOn(console, 'error').mockImplementation(() => {})
  })

  it('renders the login screen, not the dashboard, when mode is signalk and no one is authenticated', () => {
    mockAuth({ mode: 'signalk', user: null })
    render(<App />)

    expect(screen.getByRole('button', { name: /sign in/i })).toBeInTheDocument()
    expect(screen.queryByText('Dashboard')).not.toBeInTheDocument()
  })

  it('renders the dashboard, not the login screen, once authenticated under mode signalk', () => {
    mockAuth({ mode: 'signalk', user: { username: 'skipper', role: 'admin' }, role: 'admin' })
    render(<App />)

    expect(screen.queryByRole('button', { name: /sign in/i })).not.toBeInTheDocument()
    expect(screen.getAllByText('Dashboard').length).toBeGreaterThan(0)
  })

  it('renders the dashboard exactly as today when mode is none, with no login UI at all', () => {
    mockAuth({ mode: 'none', user: null })
    render(<App />)

    expect(screen.queryByRole('button', { name: /sign in/i })).not.toBeInTheDocument()
    expect(screen.getAllByText('Dashboard').length).toBeGreaterThan(0)
  })

  it('does not render the dashboard when the auth mode could not be determined', () => {
    // A failed /api/auth/mode probe leaves mode null. Falling through to the
    // dashboard would be a fail-OPEN gate: `canWrite`/`canAdmin` are both
    // derived from `mode !== 'signalk'`, so an unknown mode would hand out
    // full admin affordances. Server enforcement still holds, but the UI must
    // not guess — it says it cannot tell, and offers a retry.
    mockAuth({ mode: null, user: null, error: 'Failed to fetch auth mode (500)' })
    render(<App />)

    expect(screen.queryByText('Dashboard')).not.toBeInTheDocument()
    expect(screen.getByText(/could not determine/i)).toBeInTheDocument()
    expect(screen.getByText(/Failed to fetch auth mode \(500\)/)).toBeInTheDocument()
  })

  it('shows nothing conclusive while the auth state is still loading', () => {
    // Rendering the dashboard during the probe would flash the full UI at an
    // unauthenticated visitor before the login screen replaces it.
    mockAuth({ mode: null, user: null, loading: true })
    render(<App />)

    expect(screen.queryByText('Dashboard')).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /sign in/i })).not.toBeInTheDocument()
  })
})
