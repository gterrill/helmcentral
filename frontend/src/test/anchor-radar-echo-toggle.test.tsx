import { beforeEach, describe, expect, it, vi } from 'vitest'
import { installFakeTelemetryStream } from './helpers/fake-telemetry-stream'
import { landAnchorWatch } from './helpers/anchor-watch-poll'
import { fireEvent, render, screen } from '@testing-library/react'
import { App } from '../App'

// Direct sibling of anchor-imagery-toggle.test.tsx, same shape: tile and
// drawer both mount AnchorWatchMap, and both must share the one piece of
// state App.tsx owns, persisted to its own localStorage key.

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
    showRadarEcho?: boolean
    onRadarEchoToggle?: (enabled: boolean) => void
  }) => (
    <div>
      <div data-testid="radar-echo-state">{props.showRadarEcho ? 'on' : 'off'}</div>
      <button
        aria-label="Toggle radar echo overlay"
        onClick={() => props.onRadarEchoToggle?.(!props.showRadarEcho)}
      >
        radar echo
      </button>
    </div>
  ),
}))

const stream = installFakeTelemetryStream()
beforeEach(() => {
  stream.seed('vessel-state', {
    depth: 3.2, current_drift_kts: 0, current_set_deg: 0, status: 'at_anchor',
    latitude: -25.2939, longitude: 152.9103, gnss_critical_alert: false,
    heading_true: 265, speed_over_ground_kts: 0,
    wind_speed_apparent_kts: 5, wind_angle_apparent_deg: 220, wind_side: 'port', wind_angle_relative_deg: 130,
    max_gust_kts: { '10m': 7, '1h': 12 },
    generator_state: 'stopped', generator_manual_start: false, generator_manual_start_timer: 0, generator_runtime: 0,
  })
  landAnchorWatch({ active: true, lat: -25.2939, lon: 152.9103, radius_meters: 34 })
})

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

describe('Radar echo toggle wiring', () => {
  beforeEach(() => {
    localStorage.clear()
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: false, json: async () => ({}) }))
    vi.spyOn(console, 'error').mockImplementation(() => {})
  })

  it('toggles radar echo state through App -> Tile -> Map path', async () => {
    render(<App />)

    // AnchorWatchTile now lazy-loads AnchorWatchMap (kiosk bundle-split
    // follow-up), so the mocked map resolves a beat after the rest of the
    // tree — vi.mock still intercepts the dynamic import the same way it
    // does a static one.
    expect(await screen.findByTestId('radar-echo-state')).toHaveTextContent('off')

    fireEvent.click(screen.getByLabelText('Toggle radar echo overlay'))
    expect(screen.getByTestId('radar-echo-state')).toHaveTextContent('on')
  })

  it('shares one state between the tile and the fullscreen drawer, and persists it', async () => {
    render(<App />)

    fireEvent.click(screen.getByLabelText('Toggle radar echo overlay'))
    expect(screen.getByTestId('radar-echo-state')).toHaveTextContent('on')
    expect(localStorage.getItem('anchorWatch.radarEcho.enabled')).toBe('true')

    // Navigate from the dashboard tile to the fullscreen anchor-watch panel
    // (the drawer) via the sidebar — a completely different mounted
    // AnchorWatchMap instance, sharing state only through App.tsx. The
    // drawer is a lazy chunk, so its content isn't there synchronously.
    fireEvent.click(screen.getByRole('button', { name: /anchor watch/i }))

    expect(await screen.findByTestId('radar-echo-state')).toHaveTextContent('on')

    // Toggling off from the drawer's map must flow back through the same
    // App-owned state, not a copy local to the drawer.
    fireEvent.click(screen.getByLabelText('Toggle radar echo overlay'))
    expect(screen.getByTestId('radar-echo-state')).toHaveTextContent('off')
    expect(localStorage.getItem('anchorWatch.radarEcho.enabled')).toBe('false')
  })

  it('reads the persisted key back on mount', () => {
    localStorage.setItem('anchorWatch.radarEcho.enabled', 'true')

    render(<App />)

    expect(screen.getByTestId('radar-echo-state')).toHaveTextContent('on')
  })
})
