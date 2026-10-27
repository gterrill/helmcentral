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
import { landAnchorWatch } from './helpers/anchor-watch-poll'
import { installFakeTelemetryStream } from './helpers/fake-telemetry-stream'
import { settingsFormMock } from '@/test/helpers/settings-form-mock'
import { render, screen, fireEvent, waitFor, act, within } from '@testing-library/react'
import { App } from '../App'
import { SECRET_KEYS, type SecretKey } from '@/hooks/use-secrets-status'

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
  vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
    ok: false,
    json: async () => ({}),
  }))
})

// ── hook mocks (mirrors app-sidebar-navigation.test.tsx) ───────────────────────





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

// One page, id 'p1' — matching the constant `useActiveDashboardPageId` mock
// below, so the deep-link tests' page id resolves as the *first* page
// (canonical `/`) rather than a `/dashboard/p1` this suite has no interest
// in exercising.
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
    useSettingsForm: () => settingsFormMock(saveMock),
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
      resetTouched: vi.fn(),
    }),
  }
})

// The Settings page mounts AlarmTransportsProvider for the whole page (so an
// Alarms-section edit counts toward the page's dirty signal), which means the
// real hook would fire its /api/alarm-transports GET into jsdom. Stub it the
// same way useSettingsForm/useSecretsStatus are stubbed above. `config` is
// built once so its identity is stable across renders.
vi.mock('@/hooks/use-alarm-transports', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/hooks/use-alarm-transports')>()
  const config = actual.emptyTransportConfig()
  return {
    ...actual,
    useAlarmTransports: () => ({
      config, secretsPresent: {}, loading: false, loaded: true, error: null,
      save: vi.fn().mockResolvedValue(undefined), test: vi.fn().mockResolvedValue(undefined),
      testResults: null, testing: false,
    }),
  }
})

// ── tests ─────────────────────────────────────────────────────────────────────

/**
 * Navigates to Settings and makes it dirty (touching a secret field's
 * status). Async because SettingsPage is a lazy chunk (React.lazy) — the
 * 'Vessel' nav button doesn't exist until that chunk resolves, so it has to
 * be awaited with findBy* rather than queried synchronously.
 */
async function navigateToDirtySettings() {
  fireEvent.click(screen.getByRole('button', { name: 'Settings' }))
  fireEvent.click(await screen.findByRole('button', { name: 'Vessel' }))
  fireEvent.change(screen.getByLabelText('Vessel prefix'), { target: { value: 'S/V Test' } })
}

// The stream is up and has said nothing yet: every reading is unknown.
installFakeTelemetryStream({ seeds: { 'vessel-state': {} } })
// The first GET /api/anchor-watch has answered: no watch is set.
beforeEach(() => { landAnchorWatch() })

describe('App navigation guard on dirty Settings', () => {
  beforeEach(() => {
    mockTouched = emptyTouched()
    saveMock.mockReset().mockResolvedValue({})
    saveTouchedKeysMock.mockReset().mockResolvedValue({})
    vi.spyOn(console, 'error').mockImplementation(() => {})
  })

  it('opens the confirmation dialog instead of navigating immediately when leaving a dirty Settings page', async () => {
    render(<App />)
    await navigateToDirtySettings()

    await waitFor(() => {
      expect(screen.getByLabelText('Vessel prefix')).toHaveValue('S/V Test')
    })

    fireEvent.click(screen.getByRole('button', { name: 'Forecast' }))

    // Still on Settings — the Forecast panel content did not take over.
    expect(screen.getByLabelText('Vessel prefix')).toBeInTheDocument()
    expect(within(screen.getByRole('alertdialog')).getByText('Unsaved changes')).toBeInTheDocument()
  })

  // ADR 0074: Back/Forward go through the same guard as a sidebar click.
  // These three dirty Settings via `mockTouched` (a touched secret field —
  // the same technique settings-page.test.tsx's own touched-secret case
  // uses) rather than via navigateToDirtySettings()'s section switch:
  // switching to another section is itself a navigation that correctly
  // pushes its own `/settings/<id>` entry (per the sync effect), which would
  // leave these assertions unable to tell "the guard re-pushed /settings"
  // apart from "the section switch already put us there". Staying on
  // General keeps the bar's before/after states unambiguous.
  it('Back on a dirty Settings page reached via a deep link opens the confirmation dialog and re-pushes /settings', async () => {
    mockTouched = { ...emptyTouched(), SIGNALK_USERNAME: true }
    window.history.replaceState({}, '', '/settings')
    render(<App />)

    // Settings is a lazy chunk — wait for it to actually mount and report
    // dirty before simulating Back, or the popstate handler below would see
    // settingsDirty still at its initial `false`. The header breadcrumb also
    // says "General" (as its section crumb) the moment settingsSection's
    // initial state resolves, well before the lazy chunk mounts, so this
    // has to be the SectionNav's own button, not bare text.
    await screen.findByRole('button', { name: 'General' })

    // Simulate the browser's Back button: it lands the document on the
    // previous URL and fires popstate, but does not itself re-run any React
    // effect — jsdom doesn't replay a pushState history stack, so this
    // reproduces exactly what the popstate handler actually sees.
    window.history.replaceState({}, '', '/')
    act(() => { window.dispatchEvent(new PopStateEvent('popstate')) })

    expect(within(screen.getByRole('alertdialog')).getByText('Unsaved changes')).toBeInTheDocument()
    expect(window.location.pathname).toBe('/settings')
    // Settings itself is still the panel on screen, not the dashboard — the
    // modal dialog marks the rest of the tree inert, so this reads it back
    // with getByLabelText (unfiltered) rather than getByRole (which excludes
    // inert content). Distance units is General's own field, unlike
    // "General" itself, which the breadcrumb's section crumb also says.
    expect(screen.getByLabelText('Distance units')).toBeInTheDocument()
  })
})
