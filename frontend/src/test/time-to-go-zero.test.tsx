import { describe, it, vi, beforeEach, expect } from 'vitest'
import { landAnchorWatch } from './helpers/anchor-watch-poll'
import { installFakeTelemetryStream } from './helpers/fake-telemetry-stream'
import * as live from './fixtures/live-stream-payloads'
import { render, screen, within } from '@testing-library/react'
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

// The house bank is full (100 %), 1440 Ah. The stream carries the figures and
// the store works out the rate and time to go from them.
const stream = installFakeTelemetryStream()
const bank = { ...live.ELECTRICAL, battery_soc_percent: 100, battery_capacity_ah: 1440 }

beforeEach(() => {
  vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
    ok: false,
    json: async () => ({}),
  }))
  vi.spyOn(console, 'error').mockImplementation(() => {})
  // Full and charging: time until full computes to zero.
  stream.seed('electrical-state', { ...bank, charging_current_a: 73.8 })
})

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

// The first GET /api/anchor-watch has answered: no watch is set.
beforeEach(() => { landAnchorWatch() })

describe('Battery tile time to go', () => {
  it('shows an em dash when time to go computes to zero', () => {
    render(<App />)

    // "Time Remaining" no longer exists as its own labelled sub-card: the
    // direction and the formatted hours now live in the right-aligned block
    // of the STATE card, so scope through the tile's heading instead.
    const heading = screen.getByText('Battery & Power')
    const card = heading.closest('[data-slot="card"]') as HTMLElement
    expect(card).not.toBeNull()
    const timeBlock = card.querySelector('.text-right') as HTMLElement
    expect(timeBlock).not.toBeNull()
    expect(timeBlock).toHaveTextContent('—')
    // The seed arrived: the tile shows the bank's 100 % charge, not an unknown.
    expect(card).toHaveTextContent('100')
  })

  it('shows rounded weeks for very large remaining time', () => {
    // 0.7 A out of 1440 Ah at 100 % is 2057 hours to empty.
    stream.seed('electrical-state', { ...bank, charging_current_a: -0.7 })

    render(<App />)

    const heading = screen.getByText('Battery & Power')
    const card = heading.closest('[data-slot="card"]') as HTMLElement
    expect(card).not.toBeNull()
    const timeBlock = card.querySelector('.text-right') as HTMLElement
    expect(timeBlock).not.toBeNull()
    expect(within(timeBlock).getByText('12w')).toBeInTheDocument()
    expect(timeBlock).toHaveTextContent('To empty')
  })
})
