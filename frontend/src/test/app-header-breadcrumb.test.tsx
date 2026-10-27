/**
 * The header breadcrumb used to render "Dashboard > <panel label>" for
 * every panel, which is wrong: panels are not children of Dashboard, they
 * are siblings of it in the sidebar. This covers the fix: the root crumb is
 * the panel's own label, Settings and Inventory (the two panels with their
 * own in-page SectionNav) additionally show their active section as a
 * second crumb, and the Dashboard view itself (activePanel === null) is
 * unchanged. Mirrors app-sidebar-navigation.test.tsx's hook mocking (same
 * App tree).
 */
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { landAnchorWatch } from './helpers/anchor-watch-poll'
import { installFakeTelemetryStream } from './helpers/fake-telemetry-stream'
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





vi.mock('@/hooks/use-place-name', () => ({ usePlaceName: () => null }))


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
  useForecastSunTimes: () => ({ sunriseTime: null, sunsetTime: null }),
  useForecastSun: () => ({ sunriseTime: null, sunsetTime: null, moonPhase: null }),
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

vi.mock('@/hooks/use-route-activation', () => {
  // One object, so a re-render does not look like a new route status.
  const result = { status: { state: 'inactive', destination: null } }
  return { useRouteActivation: () => result }
})

vi.mock('@/hooks/use-forecast-warnings', () => ({
  useHasActiveWindBulletin: () => false,
  useForecastWarnings: () => ({ activeWarning: null }),
  findActiveWindBulletin: () => null,
  forecastWarningDetailsUrl: () => null,
}))

vi.mock('@/hooks/use-server-trails', async () => (await import('./trail-mocks')).trailHooksMock)

vi.mock('@/hooks/use-dark-mode', () => ({
  useDarkMode: () => [false, vi.fn()],
}))

// ── tests ─────────────────────────────────────────────────────────────────────

function breadcrumbNav() {
  return screen.getByRole('navigation', { name: /breadcrumb/i })
}

// The stream is up and has said nothing yet: every reading is unknown.
installFakeTelemetryStream({ seeds: { 'vessel-state': {} } })
// The first GET /api/anchor-watch has answered: no watch is set.
beforeEach(() => { landAnchorWatch() })

describe('App header breadcrumb', () => {
  beforeEach(() => {
    vi.spyOn(console, 'error').mockImplementation(() => {})
  })

  it('shows the Dashboard root unchanged when no panel is open', () => {
    window.history.replaceState({}, '', '/')
    render(<App />)

    const nav = breadcrumbNav()
    expect(within(nav).getByText('Dashboard')).toBeInTheDocument()
  })

  it('on /settings/logs shows Settings and Logs, and no Dashboard crumb', async () => {
    window.history.replaceState({}, '', '/settings/logs')
    render(<App />)

    // Settings is a lazy chunk — wait for its section nav to actually mount.
    await screen.findByRole('button', { name: 'Logs' })

    const nav = breadcrumbNav()
    expect(within(nav).getByText('Settings')).toBeInTheDocument()
    expect(within(nav).getByText('Logs')).toBeInTheDocument()
    expect(within(nav).queryByText('Dashboard')).not.toBeInTheDocument()
  })

  it('on /inventory shows Inventory and Equipment, and no Dashboard crumb', async () => {
    window.history.replaceState({}, '', '/inventory')
    render(<App />)

    // Inventory is a lazy chunk — wait for its section nav to actually mount.
    await screen.findByRole('button', { name: 'Equipment' })

    const nav = breadcrumbNav()
    expect(within(nav).getByText('Inventory')).toBeInTheDocument()
    expect(within(nav).getByText('Equipment')).toBeInTheDocument()
    expect(within(nav).queryByText('Dashboard')).not.toBeInTheDocument()
  })

  it('shows a single crumb with the panel label for a panel with no section nav, and no Dashboard crumb', async () => {
    window.history.replaceState({}, '', '/')
    render(<App />)

    fireEvent.click(screen.getByRole('button', { name: /forecast/i }))
    await screen.findAllByText(/tide/i)

    const nav = breadcrumbNav()
    expect(within(nav).getByText('Forecast')).toBeInTheDocument()
    expect(within(nav).queryByText('Dashboard')).not.toBeInTheDocument()
  })
})
