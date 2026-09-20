/**
 * Covers App.tsx's navigation guard as extended for a dirty Details page
 * (ADR 0115 §2, review finding): while a document's Details page is open and
 * holds an unsaved title/notes/tags edit, leaving it - a sidebar click, the
 * page's own breadcrumb Back, or the browser's Back button - should
 * intercept the navigation with the same confirmation AlertDialog (Cancel /
 * Discard / Save and Continue) the dirty-Settings guard already uses, rather
 * than silently discarding the draft. Mirrors
 * app-settings-navigation-guard.test.tsx's hook-mocking shape (same App
 * tree), swapping the Settings-only hook mocks for the Documents ones
 * app-deep-link.test.tsx already uses to reach the Documents panel.
 */
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, fireEvent, waitFor, act } from '@testing-library/react'
import { App } from '../App'
import { useDocument, useDocuments, type DocumentRecord } from '@/hooks/use-documents'
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
    dryRunEmbeddingsBackfill: vi.fn(),
    startEmbeddingsBackfill: vi.fn(),
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
    ...overrides,
  }
}

// ── tests ─────────────────────────────────────────────────────────────────────

/**
 * Navigates to Documents, opens "receipt.pdf"'s Details page via the row
 * menu, and dirties its title field. Async throughout - DocumentsPanel and
 * DocumentDetailsPage are both lazy chunks (React.lazy), so each step's
 * target doesn't exist in the DOM until its own chunk resolves.
 */
async function navigateToDirtyDocumentDetails() {
  fireEvent.click(screen.getByRole('button', { name: 'Documents' }))
  fireEvent.click(await screen.findByRole('button', { name: /actions for receipt\.pdf/i }))
  fireEvent.click(screen.getByRole('menuitem', { name: /details/i }))
  await screen.findByLabelText('Title')
  fireEvent.change(screen.getByLabelText('Title'), { target: { value: 'Impeller kit v2' } })
}

describe('App navigation guard on a dirty Documents Details page', () => {
  const patchMock = vi.fn(async (patch: unknown) => {
    void patch
    return doc({ title: 'Impeller kit v2' })
  })

  beforeEach(() => {
    patchMock.mockReset().mockResolvedValue(doc({ title: 'Impeller kit v2' }))
    vi.spyOn(console, 'error').mockImplementation(() => {})
    mockedUseDocuments.mockReturnValue(makeDocumentsMock({ documents: [doc()] }))
    mockedUseDocument.mockReturnValue(makeDocumentMock({ document: doc(), patch: patchMock }))
    mockedUseDocumentUploads.mockReturnValue(makeUploadsMock())
  })

  it('opens the confirmation dialog instead of navigating immediately when leaving a dirty Details page', async () => {
    render(<App />)
    await navigateToDirtyDocumentDetails()

    fireEvent.click(screen.getByRole('button', { name: 'Forecast' }))

    // Still on the Details page - the Forecast panel content did not take over.
    expect(screen.getByLabelText('Title')).toBeInTheDocument()
    expect(screen.getByText('Unsaved changes')).toBeInTheDocument()
    expect(screen.getByText(/unsaved changes on the details page/i)).toBeInTheDocument()
  })

  it('Cancel closes the dialog and stays on the Details page', async () => {
    render(<App />)
    await navigateToDirtyDocumentDetails()
    fireEvent.click(screen.getByRole('button', { name: 'Forecast' }))

    expect(screen.getByText('Unsaved changes')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Cancel' }))

    await waitFor(() => {
      expect(screen.queryByText('Unsaved changes')).not.toBeInTheDocument()
    })
    expect(screen.getByLabelText('Title')).toBeInTheDocument()
  })

  it('Discard navigates away without saving', async () => {
    render(<App />)
    await navigateToDirtyDocumentDetails()
    fireEvent.click(screen.getByRole('button', { name: 'Forecast' }))

    fireEvent.click(screen.getByRole('button', { name: 'Discard' }))

    await waitFor(() => {
      expect(screen.queryByLabelText('Title')).not.toBeInTheDocument()
    })
    expect(patchMock).not.toHaveBeenCalled()
  })

  it('Save and Continue saves then navigates on success', async () => {
    render(<App />)
    await navigateToDirtyDocumentDetails()
    fireEvent.click(screen.getByRole('button', { name: 'Forecast' }))

    fireEvent.click(screen.getByRole('button', { name: /save and continue/i }))

    await waitFor(() => {
      expect(patchMock).toHaveBeenCalledTimes(1)
    })
    expect(patchMock).toHaveBeenCalledWith({ title: 'Impeller kit v2', notes: '', tags: [] })
    await waitFor(() => {
      expect(screen.queryByLabelText('Title')).not.toBeInTheDocument()
    })
    expect(screen.queryByText('Unsaved changes')).not.toBeInTheDocument()
  })

  it('Save and Continue does NOT navigate away if the save fails', async () => {
    patchMock.mockRejectedValueOnce(new Error('title already used'))
    render(<App />)
    await navigateToDirtyDocumentDetails()
    fireEvent.click(screen.getByRole('button', { name: 'Forecast' }))

    fireEvent.click(screen.getByRole('button', { name: /save and continue/i }))

    await waitFor(() => {
      expect(patchMock).toHaveBeenCalledTimes(1)
    })

    // Still on the Details page - save failed, so navigation must not have happened.
    expect(screen.getByLabelText('Title')).toBeInTheDocument()
    expect(screen.getByText('title already used')).toBeInTheDocument()
  })

  // ADR 0115 §2 review finding: the page's own breadcrumb Back
  // (onBack) stays on the 'documents' panel - only documentsEditId changes -
  // so it can't be caught by the same targetPanel check every other call
  // site above relies on. It needs its own path through the guard.
  it('the page\'s own breadcrumb Back also asks before leaving a dirty Details page', async () => {
    render(<App />)
    await navigateToDirtyDocumentDetails()

    // Two things share the accessible name "Documents" here: App's own top
    // breadcrumb ("current page", rendered as a non-interactive
    // aria-disabled span) and DocumentDetailsPage's own breadcrumb link
    // (onBack) - only the latter is an actual <a>.
    const documentsLink = screen.getAllByRole('link', { name: 'Documents' }).find((el) => el.tagName === 'A')
    expect(documentsLink).toBeDefined()
    fireEvent.click(documentsLink!)

    expect(screen.getByLabelText('Title')).toBeInTheDocument()
    expect(screen.getByText('Unsaved changes')).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: 'Discard' }))

    await waitFor(() => {
      expect(screen.queryByLabelText('Title')).not.toBeInTheDocument()
    })
  })

  // Mirrors app-settings-navigation-guard.test.tsx's own Back/Forward
  // coverage: Back is the browser's native button, not the page's own
  // breadcrumb link above - it moves window.location on its own and only
  // fires popstate, so the guard has to intercept it there instead.
  it('Back on a dirty Details page opens the confirmation dialog and re-pushes its own URL', async () => {
    window.history.replaceState({}, '', '/documents/doc-1')
    render(<App />)

    await screen.findByLabelText('Title')
    fireEvent.change(screen.getByLabelText('Title'), { target: { value: 'Impeller kit v2' } })

    window.history.replaceState({}, '', '/documents')
    act(() => { window.dispatchEvent(new PopStateEvent('popstate')) })

    expect(screen.getByText('Unsaved changes')).toBeInTheDocument()
    expect(window.location.pathname).toBe('/documents/doc-1')
    // The modal dialog marks the rest of the tree inert, so this reads the
    // still-mounted Details page back with getByDisplayValue (unfiltered)
    // rather than a role-based query.
    expect(screen.getByDisplayValue('Impeller kit v2')).toBeInTheDocument()
  })

  it('Discard on that guarded Back navigates to the listing and the bar goes to /documents', async () => {
    window.history.replaceState({}, '', '/documents/doc-1')
    render(<App />)

    await screen.findByLabelText('Title')
    fireEvent.change(screen.getByLabelText('Title'), { target: { value: 'Impeller kit v2' } })

    window.history.replaceState({}, '', '/documents')
    act(() => { window.dispatchEvent(new PopStateEvent('popstate')) })
    fireEvent.click(screen.getByRole('button', { name: 'Discard' }))

    expect(window.location.pathname).toBe('/documents')
  })
})
