/**
 * Deep-link tests (ADR 0074): mounts <App> at various pathnames and checks
 * that the shell opens directly to the linked panel/section/page, keeps the
 * URL bar in its canonical form, and that ordinary clicks/Back afterwards
 * still push/pop history the same way. Mirrors app-sidebar-navigation.test.tsx's
 * hook-mock preamble (same App tree), except:
 *  - `use-active-dashboard-page` is NOT mocked — this suite exercises its
 *    real localStorage-backed reconciliation against the seeded URL.
 *  - `use-dashboard-pages` is mocked through mutable `vi.hoisted` state
 *    instead of a literal factory, so `pages`/`loading` can vary per test.
 */
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, fireEvent, act, within, waitFor } from '@testing-library/react'
import { App } from '../App'
import { useDocuments, type DocumentRecord } from '@/hooks/use-documents'
import { useDocumentUploads } from '@/hooks/use-document-uploads'
import type { DashboardPage } from '@/hooks/use-dashboard-pages'
import type { DashboardLayoutItem } from '@/lib/dashboard-widgets'

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
// The assistant panel (ADR 0093) is reachable via /mate in this suite -
// answered with a stable "unconfigured" status so the drawer renders its
// zero-state card instead of chasing conversation-list/thread fetches this
// stub doesn't otherwise answer.
beforeEach(() => {
  vi.stubGlobal('fetch', vi.fn(async (url: string) => {
    if (typeof url !== 'string') return { ok: false, json: async () => ({}) }
    if (url.endsWith('/api/health')) {
      return { ok: true, json: async () => ({ status: 'ok', version: 'v0.17.0', revision: 'deadbeef' }) }
    }
    if (url.endsWith('/api/assistant/status')) {
      return { ok: true, json: async () => ({ enabled: false, configured: false, model: '', problem: 'Set up the assistant in Settings → Assistant.' }) }
    }
    return { ok: false, json: async () => ({}) }
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

// ForecastTideSection (rendered as the last card in Forecast) owns its own
// data-fetching via these hooks - mocked here (rather than mocking the
// component itself, since this suite is testing App's real deep-link wiring)
// so the "Forecast" panel is reachable without hitting real fetches.
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

// Mutable so each test can set the page list/loading state it needs before
// rendering — a literal factory (as app-sidebar-navigation.test.tsx uses) is
// fixed at module-eval time and can't vary per test.
const { mockPagesState } = vi.hoisted(() => ({
  mockPagesState: {
    pages: [] as DashboardPage[],
    loading: false,
  },
}))

vi.mock('@/hooks/use-dashboard-pages', () => ({
  useDashboardPages: () => ({
    pages: mockPagesState.pages,
    loading: mockPagesState.loading,
    error: null,
    refetch: vi.fn(),
    createPage: vi.fn(),
    updatePage: vi.fn(),
    deletePage: vi.fn(),
  }),
}))

// Deliberately NOT mocked: @/hooks/use-active-dashboard-page — this suite
// exercises the real hook's localStorage seed/reconciliation against the
// `initialId` App.tsx forwards from the parsed URL.

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
}))

vi.mock('@/hooks/use-server-trails', () => ({
  useServerTrails: () => ({ getSelfTrail: vi.fn(), getAisTrails: vi.fn() }),
}))

vi.mock('@/hooks/use-dark-mode', () => ({
  useDarkMode: () => [false, vi.fn()],
}))

// ADR 0106 F1: DocumentsPanel's own data hooks, mocked the same way
// documents-panel.test.tsx does it (trimmed to what the deep-link test below
// actually needs) - no other existing test in this file ever navigates to
// /documents, so mocking these two hooks file-wide can't affect them.
vi.mock('@/hooks/use-documents')
vi.mock('@/hooks/use-document-uploads')

const mockedUseDocuments = vi.mocked(useDocuments)
const mockedUseDocumentUploads = vi.mocked(useDocumentUploads)

type DocumentsMock = ReturnType<typeof useDocuments>
type UploadsMock = ReturnType<typeof useDocumentUploads>

function makeDocumentsMock(overrides: Partial<DocumentsMock> = {}): DocumentsMock {
  return {
    path: [],
    folders: [],
    documents: [],
    loading: false,
    error: null,
    refresh: vi.fn(),
    tags: [],
    selectedTag: null,
    setSelectedTag: vi.fn(),
    searchResults: null,
    searching: false,
    searchError: null,
    semanticProblem: null,
    search: vi.fn(),
    clearSearch: vi.fn(),
    embeddingsStatus: null,
    createFolder: vi.fn(),
    renameFolder: vi.fn(),
    moveFolder: vi.fn(),
    deleteFolder: vi.fn(),
    patchDocument: vi.fn(),
    deleteDocument: vi.fn(),
    reindexDocument: vi.fn(),
    moveDocuments: vi.fn(),
    ...overrides,
  }
}

function makeUploadsMock(overrides: Partial<UploadsMock> = {}): UploadsMock {
  return {
    items: [],
    add: vi.fn(),
    remove: vi.fn(),
    clear: vi.fn(),
    ready: true,
    error: null,
    ...overrides,
  }
}

function doc(overrides: Partial<DocumentRecord> = {}): DocumentRecord {
  return {
    id: 'doc-1',
    sha256: 'abc123',
    folder_id: null,
    filename: 'manual.pdf',
    title: '',
    notes: '',
    mime: 'application/pdf',
    size_bytes: 123456,
    page_count: 5,
    summary: '',
    status: 'indexed',
    stage: 'done',
    indexed_with: 'local',
    error: '',
    index_model: '',
    index_cost_usd: 0,
    tags: [],
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
    indexed_at: '2026-01-01T00:00:00Z',
    kind: 'file',
    note_type: '',
    note_type_source: '',
    pinned: false,
    sort_index: 0,
    ...overrides,
  }
}

// ── fixtures ──────────────────────────────────────────────────────────────────

const DEPTH_TIDE_WIDGET: DashboardLayoutItem = { id: 'depth-tide', x: 0, y: 0, w: 4, h: 7 }

function page(id: string, name: string, widgets: DashboardLayoutItem[] = []): DashboardPage {
  return { id, name, widgets, created_at: '', updated_at: '' }
}

// ── tests ─────────────────────────────────────────────────────────────────────

describe('App deep links', () => {
  beforeEach(() => {
    vi.spyOn(console, 'error').mockImplementation(() => {})
    globalThis.localStorage?.clear()
    mockPagesState.pages = [page('p1', 'Page 1', [DEPTH_TIDE_WIDGET])]
    mockPagesState.loading = false
  })

  it('opens the Forecast panel directly, keeps the pathname, and titles the tab', async () => {
    window.history.replaceState({}, '', '/forecast')
    render(<App />)

    expect(screen.queryByText('Depth & Tide')).not.toBeInTheDocument()
    expect((await screen.findAllByText(/tide/i)).length).toBeGreaterThan(0)
    expect(window.location.pathname).toBe('/forecast')
    expect(document.title).toBe('Forecast · Helmcentral')
  })

  it('opens the Mate panel directly', async () => {
    window.history.replaceState({}, '', '/mate')
    render(<App />)

    expect(await screen.findByTestId('assistant-drawer')).toBeInTheDocument()
    expect(window.location.pathname).toBe('/mate')
  })

  it('opens a thread deeplink at /mate/<threadId> and keeps the pathname', async () => {
    window.history.replaceState({}, '', '/mate/12345')
    render(<App />)

    expect(await screen.findByTestId('assistant-drawer')).toBeInTheDocument()
    expect(window.location.pathname).toBe('/mate/12345')
  })

  it('treats /assistant as a legacy alias and normalises it to /mate', async () => {
    window.history.replaceState({}, '', '/assistant')
    render(<App />)

    expect(await screen.findByTestId('assistant-drawer')).toBeInTheDocument()
    expect(window.location.pathname).toBe('/mate')
  })

  it('opens the SignalK settings section directly', async () => {
    window.history.replaceState({}, '', '/settings/signalk')
    render(<App />)

    expect(await screen.findByRole('button', { name: 'SignalK' })).toHaveAttribute('aria-current', 'true')
  })

  it('treats an unrecognised path as the dashboard and normalises the bar to /', () => {
    window.history.replaceState({}, '', '/nonsense')
    const lengthBefore = window.history.length
    render(<App />)

    expect(screen.getByText('Depth & Tide')).toBeInTheDocument()
    expect(window.location.pathname).toBe('/')
    expect(window.history.length).toBe(lengthBefore)
  })

  it('pushes a history entry on click and returns to the dashboard on Back', () => {
    window.history.replaceState({}, '', '/')
    render(<App />)

    fireEvent.click(screen.getByRole('button', { name: /forecast/i }))
    expect(window.location.pathname).toBe('/forecast')

    // Simulate the browser's Back button: it lands the document on the
    // previous URL and fires popstate, but does not itself re-run any React
    // effect — jsdom doesn't replay a pushState history stack, so the test
    // reproduces exactly what the popstate handler actually sees.
    window.history.replaceState({}, '', '/')
    act(() => { window.dispatchEvent(new PopStateEvent('popstate')) })

    expect(screen.getByText('Depth & Tide')).toBeInTheDocument()
  })

  it('mounts on a non-first dashboard page named by the URL and remembers it', () => {
    mockPagesState.pages = [page('p1', 'Anchored'), page('p2', 'Underway')]
    globalThis.localStorage?.setItem('dashboard.activePageId', 'p1')
    window.history.replaceState({}, '', '/dashboard/p2')

    render(<App />)

    expect(screen.getByRole('button', { name: 'Underway' })).toHaveAttribute('data-active', 'true')
    expect(globalThis.localStorage?.getItem('dashboard.activePageId')).toBe('p2')
  })

  it('normalises a deep link to the first page down to /', () => {
    mockPagesState.pages = [page('p1', 'Anchored'), page('p2', 'Underway')]
    window.history.replaceState({}, '', '/dashboard/p1')

    render(<App />)

    expect(window.location.pathname).toBe('/')
  })

  it('retains the selected page and replaces its canonical URL when ordering changes', () => {
    const anchored = page('p1', 'Anchored')
    const docked = page('p2', 'Docked')
    mockPagesState.pages = [anchored, docked]
    window.history.replaceState({}, '', '/')
    const { rerender } = render(<App />)
    const historyLength = window.history.length
    mockPagesState.pages = [docked, anchored]
    rerender(<App />)
    expect(screen.getByRole('button', { name: 'Anchored' })).toHaveAttribute('data-active', 'true')
    expect(window.location.pathname).toBe('/dashboard/p1')
    expect(window.history.length).toBe(historyLength)
    mockPagesState.pages = [anchored, docked]
    rerender(<App />)
    expect(window.location.pathname).toBe('/')
    expect(window.history.length).toBe(historyLength)
  })

  it('reconciles an unknown page id to the first page and normalises the bar to /', () => {
    mockPagesState.pages = [page('p1', 'Anchored'), page('p2', 'Underway')]
    window.history.replaceState({}, '', '/dashboard/zzz')

    render(<App />)

    expect(screen.getByRole('button', { name: 'Anchored' })).toHaveAttribute('data-active', 'true')
    expect(window.location.pathname).toBe('/')
  })

  it('pushes /dashboard/<id> for a non-first page and / for the first page from sidebar clicks', () => {
    mockPagesState.pages = [page('p1', 'Anchored'), page('p2', 'Underway')]
    window.history.replaceState({}, '', '/')

    render(<App />)

    fireEvent.click(screen.getByRole('button', { name: 'Underway' }))
    expect(window.location.pathname).toBe('/dashboard/p2')

    fireEvent.click(screen.getByRole('button', { name: 'Anchored' }))
    expect(window.location.pathname).toBe('/')
  })
})

describe('App deep links — Documents', () => {
  beforeEach(() => {
    vi.spyOn(console, 'error').mockImplementation(() => {})
    globalThis.localStorage?.clear()
    mockPagesState.pages = [page('p1', 'Page 1', [DEPTH_TIDE_WIDGET])]
    mockPagesState.loading = false
    mockedUseDocuments.mockReturnValue(makeDocumentsMock({ documents: [doc({ id: 'doc-1', filename: 'manual.pdf' })] }))
    mockedUseDocumentUploads.mockReturnValue(makeUploadsMock())
  })

  // Finding 2 (ADR 0106 F1 review): initialLocation is parsed once, at the
  // very first render of the whole app, and never changes - so without a
  // consumed-once latch, a Mate attachment chip's ?document= link would
  // reopen its document every single time the operator returns to
  // Documents (DocumentsPanel fully unmounts/remounts on every panel
  // switch, per Suspense key={activePanel}), not just the once, for the
  // navigation the link was actually for.
  it("opens a Mate attachment chip's linked document once, not again on a later remount", async () => {
    window.history.replaceState({}, '', '/documents?document=doc-1')
    render(<App />)

    // the viewer (Sheet) should be open, showing manual.pdf - since the
    // filename ALSO appears in the plain folder table underneath, scope the
    // assertion to the dialog itself.
    const dialog = await screen.findByRole('dialog')
    expect(within(dialog).getByText('manual.pdf')).toBeInTheDocument()

    // close it - SheetContent's close button has accessible name "Close".
    fireEvent.click(within(dialog).getByRole('button', { name: 'Close' }))
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())

    // navigate away (unmounts DocumentsPanel - Suspense key={activePanel})
    // and back (remounts it fresh, with the exact same initialLocation)
    fireEvent.click(screen.getByRole('button', { name: /forecast/i }))
    fireEvent.click(screen.getByRole('button', { name: 'Documents' }))

    // must NOT reopen this time
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  })
})
