import { beforeEach, describe, expect, it, vi } from 'vitest'
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { App } from '../App'
import { installFakeTelemetryStream } from './helpers/fake-telemetry-stream'
import { landAnchorWatch } from './helpers/anchor-watch-poll'

// The drawer now mounts a map at every anchor state (Phase F), including
// 'none' — this test's vessel position is a real fix, so without this stub
// it would try to mount the real maplibre-gl map in jsdom, which the map's
// own dedicated tests handle separately (see anchor-imagery-toggle.test.tsx
// ~lines 22-37 for the same pattern).
vi.mock('@/components/anchor-watch-map', () => ({
  AnchorWatchMap: () => <div data-testid="anchor-watch-map" />,
}))

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


// The boat is at a real fix and its engines are running (the stream seeds it);
// the watch record is what GET /api/anchor-watch last said.
const stream = installFakeTelemetryStream()
const fetchMock = vi.fn()
const ANCHOR = { lat: -36.8485, lon: 174.7633 }
beforeEach(() => {
  vi.clearAllMocks()
  // The first GET /api/anchor-watch stays unanswered: a test is "loaded" only
  // once it lands a watch with landWatch, and "before the first answer" is the
  // state of a page whose poll has not come back, not of one whose poll failed.
  // Writes fail, as the app's other tests have it; only their requests matter.
  fetchMock.mockImplementation((url: string, init?: RequestInit) => (
    String(url).endsWith('/api/anchor-watch') && (init?.method ?? 'GET') === 'GET'
      ? new Promise(() => {})
      : Promise.resolve({ ok: false, json: async () => ({}) })
  ))
  vi.stubGlobal('fetch', fetchMock)
  stream.seed('vessel-state', { latitude: ANCHOR.lat, longitude: ANCHOR.lon, gnss_critical_alert: false, engines_running: true })
})

/** A GET /api/anchor-watch answer: no watch, or one set where the boat is. */
function landWatch(state: 'none' | 'set', extra: Record<string, unknown> = {}) {
  landAnchorWatch(state === 'set' ? { active: true, ...ANCHOR, radius_meters: 20, ...extra } : { active: false, ...extra })
}

/** The writes the app sent to /api/anchor-watch, as [method, body]. */
function writes(method: string) {
  return fetchMock.mock.calls
    .filter(([url, init]) => String(url).endsWith('/api/anchor-watch') && (init as RequestInit | undefined)?.method === method)
    .map(([, init]) => JSON.parse(String((init as RequestInit).body ?? 'null')))
}

vi.mock('@/hooks/use-place-name', () => ({ usePlaceName: () => null }))


vi.mock('@/hooks/use-tide-today', () => ({
  useTideToday: () => ({
    tide: {
      current_tide_height_ft: -1,
      tide_direction: '—',
      high_tide_time: new Date(0).toISOString(),
      high_tide_height_ft: -1,
      low_tide_time: new Date(0).toISOString(),
      low_tide_height_ft: -1,
    },
  }),
}))

vi.mock('@/hooks/use-weather-today', () => ({
  useWeatherToday: () => ({
    weather: {
      temperature_f: -1,
      condition: '—',
      high_temp_f: -1,
      low_temp_f: -1,
      wind_speed_kts: -1,
      wind_direction: '—',
      wind_gust_kts: -1,
      precipitation_pct: -1,
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

describe('Anchor watch drawer drop button', () => {
  // The seeded vessel state carries no depth, so this is the "no sounder at
  // drop" case (ADR 0063) — the drop still goes out, with the planning depth
  // and tide as the server's -1 "not read" rather than silently omitted.
  it('drops anchor from drawer when no watch is active, capturing null depth/tide when there is no sounder reading', async () => {
    landWatch('none')
    render(<App />)

    fireEvent.click(screen.getByRole('button', { name: 'Anchor Watch' }))
    const dropButtons = await screen.findAllByRole('button', { name: 'Drop' })
    fireEvent.click(dropButtons[dropButtons.length - 1])

    await waitFor(() => expect(writes('POST')).toEqual([
      { lat: ANCHOR.lat, lon: ANCHOR.lon, apply_bow_offset: true, planning_depth_m: -1, planning_tide_height_ft: -1 },
    ]))
  })

  it('does not show a drawer drop button when anchor watch is active', () => {
    landWatch('set')
    render(<App />)

    fireEvent.click(screen.getByRole('button', { name: 'Anchor Watch' }))

    expect(screen.queryByRole('button', { name: 'Drop' })).toBeNull()
  })

  it('raises the anchor from the drawer through the confirm dialog when a watch is active', async () => {
    landWatch('set')
    render(<App />)

    fireEvent.click(screen.getByRole('button', { name: 'Anchor Watch' }))
    const raiseButtons = await screen.findAllByRole('button', { name: 'Raise' })
    fireEvent.click(raiseButtons[raiseButtons.length - 1])

    const dialog = screen.getByRole('alertdialog')
    fireEvent.click(within(dialog).getByRole('button', { name: 'Raise' }))

    await waitFor(() => expect(writes('DELETE')).toHaveLength(1))
  })
})

// ADR 0099: the auto-raise decision moved server-side. Clients learn about
// it through last_auto_raise on the same GET /api/anchor-watch poll every
// client already makes (useAnchorWatch), and each shows the toast once per
// event per client by comparing against what it last saw.
describe('Anchor watch auto-raise toast', () => {
  it('shows the toast once a new last_auto_raise arrives after mount', async () => {
    landWatch('none')
    render(<App />)

    expect(screen.queryByText(/raised automatically/i)).not.toBeInTheDocument()

    landWatch('none', { last_auto_raise: { at: '2026-09-15T00:00:00Z', reason: 'engines_running' } })

    await waitFor(() => expect(screen.getByText(/raised automatically/i)).toBeInTheDocument())
  })

  // The real sequence on a reload: the shell mounts before GET
  // /api/anchor-watch answers, so the first record it sees is the unloaded one
  // with no auto-raise. An auto-raise from before the page loaded arrives with
  // the first answer and is history, not news.
  it('does not toast for an old last_auto_raise that arrives with the first answer, and does for a new one', async () => {
    // No answer yet: the record is the unloaded one.
    render(<App />)

    landWatch('none', { last_auto_raise: { at: '2026-09-15T00:00:00Z', reason: 'engines_running' } })
    await waitFor(() => expect(screen.getByRole('status')).toBeInTheDocument())
    expect(screen.queryByText(/raised automatically/i)).not.toBeInTheDocument()

    landWatch('none', { last_auto_raise: { at: '2026-09-16T00:00:00Z', reason: 'engines_running' } })
    await waitFor(() => expect(screen.getByText(/raised automatically/i)).toBeInTheDocument())
  })

  it('does not toast for a last_auto_raise already present at mount', async () => {
    landWatch('none', { last_auto_raise: { at: '2026-09-15T00:00:00Z', reason: 'engines_running' } })
    render(<App />)

    // Give the establishing effect a tick, then confirm no toast fired for
    // an event that predates this page load.
    await waitFor(() => expect(screen.getByRole('status')).toBeInTheDocument())
    expect(screen.queryByText(/raised automatically/i)).not.toBeInTheDocument()
  })
})
