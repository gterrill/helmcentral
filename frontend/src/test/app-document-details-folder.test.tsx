/**
 * Covers how the document Details page's folder crumbs and Move change the
 * Documents listing App.tsx returns to: a folder change drops the manual
 * section left over from the old folder, and a Move rewrites the current
 * history entry rather than pushing one Back would land on with the
 * document's old folder. Harness copied from
 * app-documents-navigation-guard.test.tsx.
 */
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { App } from '../App'
import { useDocument, useDocuments, useFolderPath, type DocumentRecord } from '@/hooks/use-documents'
import { useDocumentUploads } from '@/hooks/use-document-uploads'

// This test renders the dashboard, not the auth gate. State the precondition
// explicitly - an install with auth.mode:none - rather than depending on what
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
  mockedUseFolderPath.mockReturnValue({ path: [], error: null })
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
  forecastWarningDetailsUrl: () => null,
}))

vi.mock('@/hooks/use-server-trails', () => ({
  useServerTrails: () => ({ getSelfTrail: vi.fn(), getAisTrails: vi.fn() }),
}))

vi.mock('@/hooks/use-dark-mode', () => ({
  useDarkMode: () => [false, vi.fn()],
}))

// ── Documents hooks: mocked the same way documents-panel.test.tsx and
// document-details-page.test.tsx do it - use-documents.test.ts already
// covers the real fetch/PATCH wiring, so this file is only about App's own
// navigation guard reacting to whatever these hooks report. ────────────────

vi.mock('@/hooks/use-documents')
vi.mock('@/hooks/use-document-uploads')

const mockedUseDocuments = vi.mocked(useDocuments)
const mockedUseDocument = vi.mocked(useDocument)
const mockedUseFolderPath = vi.mocked(useFolderPath)
const mockedUseDocumentUploads = vi.mocked(useDocumentUploads)

type DocumentsMock = ReturnType<typeof useDocuments>
type DocumentMock = ReturnType<typeof useDocument>
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

function makeDocumentMock(overrides: Partial<DocumentMock> = {}): DocumentMock {
  return {
    document: null,
    loading: false,
    error: null,
    refresh: vi.fn(),
    patch: vi.fn(),
    move: vi.fn(),
    ...overrides,
  }
}

function makeUploadsMock(overrides: Partial<UploadsMock> = {}): UploadsMock {
  return {
    items: [],
    add: vi.fn(),
    remove: vi.fn(),
    removeByDocumentIds: vi.fn(),
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
    filename: 'receipt.pdf',
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

// ── tests ─────────────────────────────────────────────────────────────────────

// The picker itself is covered by move-to-folder-dialog's own tests; here it
// only needs to hand back a destination.
vi.mock('@/components/documents/move-to-folder-dialog', () => ({
  MoveToFolderDialog: ({ open, onPick }: { open: boolean; onPick: (id: string | null) => void }) =>
    open ? <button onClick={() => onPick('f-b')}>Pick folder B</button> : null,
}))

/** Opens receipt.pdf's Details page from folder A's listing, with a manual
 * section from folder A still selected in App state. */
async function openDetailsFromFolderA() {
  window.history.replaceState({}, '', '/documents?folder=f-a&section=sec-a')
  render(<App />)
  fireEvent.click(await screen.findByRole('button', { name: /actions for receipt\.pdf/i }, { timeout: 5000 }))
  fireEvent.click(await screen.findByRole('menuitem', { name: /details/i }))
  await screen.findByLabelText('Title', {}, { timeout: 5000 })
}

describe('Document Details folder changes', () => {
  beforeEach(() => {
    vi.spyOn(console, 'error').mockImplementation(() => {})
    mockedUseDocuments.mockReturnValue(makeDocumentsMock({ documents: [doc()] }))
    mockedUseDocumentUploads.mockReturnValue(makeUploadsMock())
  })

  it('a folder crumb opens that folder without the old folder\'s manual section', async () => {
    mockedUseDocument.mockReturnValue(makeDocumentMock({ document: doc({ folder_id: 'f-b' }) }))
    mockedUseFolderPath.mockReturnValue({ path: [{ id: 'f-b', name: 'Boat', parent_id: null }], error: null })
    await openDetailsFromFolderA()
    fireEvent.click(screen.getByRole('link', { name: 'Boat' }))

    await waitFor(() => {
      expect(screen.queryByLabelText('Title')).not.toBeInTheDocument()
    })
    const params = new URLSearchParams(window.location.search)
    expect(params.get('folder')).toBe('f-b')
    expect(params.get('section')).toBeNull()
  })

  it('a Move replaces the history entry and drops the old folder\'s manual section', async () => {
    const move = vi.fn(async () => {})
    mockedUseDocument.mockReturnValue(makeDocumentMock({ document: doc({ folder_id: 'f-a' }), move }))
    await openDetailsFromFolderA()
    const pushState = vi.spyOn(window.history, 'pushState')
    fireEvent.click(screen.getByRole('button', { name: 'Move' }))
    fireEvent.click(await screen.findByRole('button', { name: 'Pick folder B' }))

    await waitFor(() => {
      expect(new URLSearchParams(window.location.search).get('folder')).toBe('f-b')
    })
    expect(move).toHaveBeenCalledWith('f-b')
    expect(new URLSearchParams(window.location.search).get('section')).toBeNull()
    expect(window.location.pathname).toBe('/documents/doc-1')
    expect(pushState).not.toHaveBeenCalled()

    // Back from the moved document goes to the folder A listing it was
    // opened from, not to a Details entry still claiming folder A.
    window.history.back()
    await waitFor(() => {
      expect(screen.queryByLabelText('Title')).not.toBeInTheDocument()
    })
  })
})
