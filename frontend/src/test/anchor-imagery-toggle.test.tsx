import { beforeEach, describe, expect, it, vi } from 'vitest'
import { fireEvent, render, screen } from '@testing-library/react'
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

vi.mock('@/components/anchor-watch-map', () => ({
  AnchorWatchMap: (props: {
    showImageryLayer?: boolean
    onImageryToggle?: (enabled: boolean) => void
  }) => (
    <div>
      <div data-testid="imagery-state">{props.showImageryLayer ? 'on' : 'off'}</div>
      <button
        aria-label="Toggle satellite imagery"
        onClick={() => props.onImageryToggle?.(!props.showImageryLayer)}
      >
        sat
      </button>
    </div>
  ),
}))

vi.mock('@/hooks/use-vessel-state', async () => (await import('./vessel-state-mock')).vesselStateModule(() => ({
    depth: 3.2,
    currentDriftKts: 0,
    currentSetDeg: 0,
    navigationState: 'at_anchor',
    latitude: -25.2939,
    longitude: 152.9103,
    gnssQualityIndicator: null,
    gnssHdop: null,
    gnssSatellites: null,
    gnssValidationState: null,
    gnssValidationReason: null,
    gnssCriticalAlert: false,
    headingTrue: 265,
    speedOverGroundKts: 0,
    windSpeedApparentKts: 5,
    windAngleApparentDeg: 220,
    windSide: 'port',
    windAngleRelativeDeg: 130,
    maxGustKts: { '10m': 7, '30m': null, '1h': 12, '24h': null },
    generatorState: 'stopped',
    generatorManualStart: false,
    generatorManualStartTimer: 0,
    generatorRunningByCondition: null,
    generatorRuntime: 0,
  })))





vi.mock('@/hooks/use-anchor-watch', async () => (await import('./anchor-watch-mock')).anchorWatchModule(() => ({
    active: true, anchorState: 'set',
    anchorLat: -25.2939,
    anchorLon: 152.9103,
    radiusMeters: 34,
    rodeDeployedM: 0,
    seaState: 'calm',
    seabedType: 'sand',
    distanceMeters: 26,
    bearingDeg: 81,
    planningDepthM: null,
    planningTideHeightFt: null,
    setAnchorHere: vi.fn(),
    updateRadius: vi.fn(),
    updateRodeAndConditions: vi.fn(),
    updatePlanningDepth: vi.fn(),
    clearAnchor: vi.fn(),
  })))

vi.mock('@/hooks/use-server-trails', async () => (await import('./trail-mocks')).trailHooksMock)

vi.mock('@/hooks/use-place-name', () => ({ usePlaceName: () => 'Urangan' }))


vi.mock('@/hooks/use-tide-today', () => ({
  useTideToday: () => ({
    tide: {
      current_tide_height_ft: 0,
      tide_direction: 'Falling',
      high_tide_time: new Date().toISOString(),
      high_tide_height_ft: 2,
      low_tide_time: new Date().toISOString(),
      low_tide_height_ft: 0,
    },
  }),
}))

vi.mock('@/hooks/use-weather-today', () => ({
  useWeatherToday: () => ({
    weather: {
      temperature_f: 68,
      condition: 'Clear',
      high_temp_f: 72,
      low_temp_f: 60,
      wind_speed_kts: 8,
      wind_direction: 'ESE',
      wind_gust_kts: 10,
      precipitation_pct: 0,
    },
  }),
}))

vi.mock('@/hooks/use-weather-forecast', () => ({
  useForecastSunTimes: () => ({ sunriseTime: null, sunsetTime: null }),
  useForecastSun: () => ({ sunriseTime: null, sunsetTime: null, moonPhase: null }),
  useWeatherForecast: () => ({ forecast: [], loading: false, error: null, refetch: vi.fn() }),
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
        { id: 'solar', x: 4, y: 11, w: 4, h: 6 },
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
    }],
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

vi.mock('@/hooks/use-route-activation', () => {
  // One object, so a re-render does not look like a new route status.
  const result = { status: { state: 'inactive', destination: null } }
  return { useRouteActivation: () => result }
})

vi.mock('@/hooks/use-forecast-warnings', () => ({
  useHasActiveWindBulletin: () => false,
  useForecastWarnings: () => ({ activeWarning: null }),
  findActiveWindBulletin: () => null,
}))

vi.mock('@/hooks/use-dark-mode', () => ({
  useDarkMode: () => [false, vi.fn()],
}))

describe('Anchor imagery toggle wiring', () => {
  beforeEach(() => {
    localStorage.clear()
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: false, json: async () => ({}) }))
    vi.spyOn(console, 'error').mockImplementation(() => {})
  })

  it('toggles imagery state through App -> Tile -> Map path', async () => {
    render(<App />)

    // AnchorWatchTile now lazy-loads AnchorWatchMap (kiosk bundle-split
    // follow-up), so the mocked map resolves a beat after the rest of the
    // tree — vi.mock still intercepts the dynamic import the same way it
    // does a static one.
    expect(await screen.findByTestId('imagery-state')).toHaveTextContent('off')

    fireEvent.click(screen.getByLabelText('Toggle satellite imagery'))
    expect(screen.getByTestId('imagery-state')).toHaveTextContent('on')
  })
})
