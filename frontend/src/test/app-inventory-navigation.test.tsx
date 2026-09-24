/**
 * Covers the review finding that Open/Full item did nothing outside the
 * Equipment section: App.tsx's onOpenEquipment/onNewEquipment used to set
 * only inventoryEquipmentEditId/inventoryCreatingEquipment, never switching
 * inventorySection to 'equipment' or clearing inventoryBinCode - so
 * InventoryPanel's own showEquipmentEditor check (activeSectionId ===
 * 'equipment' AND an id/creating flag) stayed false and nothing rendered
 * when the operator pressed Open from a bin page or Stocktake's photo grid.
 * inventory-panel.test.tsx couldn't catch this: it renders InventoryPanel in
 * isolation with props already reflecting the right section, and the
 * orchestration that was missing lives entirely in App.tsx. This file
 * mounts the real App tree (mirroring app-documents-navigation-guard.
 * test.tsx's own full-dashboard mock block) so the bug's actual symptom -
 * clicking Open and nothing appearing - can be reproduced and pinned.
 */
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, fireEvent, waitFor, act, within } from '@testing-library/react'
import { App } from '../App'
import type { EquipmentItem, InventoryZone } from '@/hooks/use-inventory'

// bin-quick-add.test.tsx's own comment on why: canvas/createImageBitmap
// aren't real against jsdom, so the downscale step is stubbed to a no-op
// (only the tests below that stage a photo actually exercise it).
vi.mock('@/lib/image-downscale', () => ({
  downscaleImage: vi.fn(async (file: Blob) => file),
  downscaleAll: vi.fn(async (files: File[]) => files.map((file) => ({ file, result: { ok: true, blob: file } }))),
}))

// This test renders the dashboard, not the auth gate - an install with
// auth.mode:none (ADR 0040), the same precondition every other App-level
// test in this suite states explicitly.
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

// ── the rest of App's own top-level hooks, mocked the same way every other
// App-level test file does (app-documents-navigation-guard.test.tsx's own
// block) - none of these are what this file is testing, they just have to
// return something render-safe so <App/> mounts at all. ──────────────────

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
    currentW: null, todayKWh: null, yesterdayKWh: null, peakTodayW: null, controllers: [],
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
      date: 'Jul 9', dayName: 'Thursday', condition: 'Clear', high: 76, low: 62,
      windSpeed: 10, windGust: 14, windDirection: 'NE', windSummary: null, waveSummary: null,
      precipitationSummary: null, precipitation: 5, humidityPct: 58, visibilityNm: 9.5,
      sunriseTime: null, sunsetTime: null, moonPhase: null,
      hourlyWind: [], hourlyWave: [], hourlyPrecip: [], hourlyUV: [], hourlyCloud: [],
    }],
    loading: false, error: null, refetch: vi.fn(),
  }),
}))

vi.mock('@/hooks/use-tide-settings', () => ({
  useTideSettings: () => ({
    tideProvider: 'bom', tideStationId: 'station-1', tideStationName: 'Test Harbor',
    loading: false, saving: false, error: null, refetch: vi.fn(), saveStation: vi.fn(),
  }),
}))

vi.mock('@/hooks/use-tide-chart', () => ({
  useTideChart: () => ({
    chart: null, loading: false, error: null, isCached: false, updatedAt: null, ttlSeconds: null, refetch: vi.fn(),
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
    loading: false, error: null, refetch: vi.fn(), createPage: vi.fn(), updatePage: vi.fn(), deletePage: vi.fn(),
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

// ── inventory fixtures - real use-inventory.ts hooks, driven off a stubbed
// fetch the same way bin-page.test.tsx/stocktake-section.test.tsx/
// equipment-editor.test.tsx already do (no module mock: this file is
// specifically about the real BinPage/StocktakeSection/EquipmentEditor
// wiring through App and InventoryPanel). ─────────────────────────────────

function makeItem(overrides: Partial<EquipmentItem> = {}): EquipmentItem {
  return {
    id: 'eq-1',
    name: 'Spare impeller',
    category: 'general',
    system: 'other',
    manufacturer: '',
    model: '',
    serial: '',
    quantity: 1,
    status: 'stored',
    zone_id: 'z1',
    bin_id: 'b1',
    zone_name: 'Lazarette',
    bin_code: 'LAZ-02',
    location_detail: '',
    install_date: '',
    hour_meter_path: '',
    profile_id: '',
    aliases: [],
    verified_aboard: false,
    notes: '',
    link_count: 0,
    created_at: '',
    updated_at: '',
    photo_ids: [],
    exclusive_photo_ids: [],
    ...overrides,
  }
}

const zones: InventoryZone[] = [
  {
    id: 'z1', name: 'Lazarette', sort_index: 0,
    bins: [{ id: 'b1', zone_id: 'z1', code: 'LAZ-02', name: 'Adhesives', sort_index: 0 }],
  },
]

const item = makeItem()

beforeEach(() => {
  vi.stubGlobal('fetch', vi.fn(async (url: string, init?: RequestInit) => {
    const u = String(url)
    const method = init?.method ?? 'GET'
    if (u.endsWith('/api/health')) {
      return { ok: true, json: async () => ({ status: 'ok', version: 'v0.31.0', revision: 'deadbeef' }) }
    }
    if (u.endsWith('/api/assistant/status')) {
      return { ok: true, json: async () => ({ enabled: false, configured: false, model: '', problem: 'Set up the assistant in Settings → Assistant.' }) }
    }
    if (u.endsWith('/api/inventory/zones')) {
      return { ok: true, json: async () => ({ zones }) }
    }
    if (u.includes('/api/inventory/equipment?bin=') && method === 'GET') {
      return { ok: true, json: async () => ({ items: [item] }) }
    }
    if (u.match(/\/api\/inventory\/equipment\/eq-1$/) && method === 'GET') {
      return { ok: true, json: async () => ({ item, documents: [] }) }
    }
    if (u.match(/\/api\/inventory\/equipment\/eq-1$/) && method === 'PUT') {
      return { ok: true, json: async () => ({ item }) }
    }
    if (u.endsWith('/api/equipment-profiles')) {
      return { ok: true, json: async () => ({ profiles: [], problems: [] }) }
    }
    if (u.endsWith('/api/signalk/paths')) {
      return { ok: true, json: async () => ({ paths: [] }) }
    }
    if (u.endsWith('/api/inventory/equipment') && method === 'POST') {
      const body = JSON.parse(String(init?.body))
      return { ok: true, status: 201, json: async () => ({ item: { id: 'eq-new', photo_ids: [], ...body } }) }
    }
    const photoPost = u.match(/\/api\/inventory\/equipment\/([^/]+)\/photos$/)
    if (photoPost && method === 'POST') {
      return { ok: true, status: 201, json: async () => ({ item: { id: photoPost[1], photo_ids: ['p1'] } }) }
    }
    return { ok: false, json: async () => ({}) }
  }))
  vi.spyOn(URL, 'createObjectURL').mockReturnValue('blob:mock-url')
  vi.spyOn(URL, 'revokeObjectURL').mockImplementation(() => {})
})

describe('App: Open/Full item from outside the Equipment section', () => {
  it('Open from a bin page renders the equipment editor and pushes a new history entry', async () => {
    window.history.replaceState({}, '', '/inventory/bins/LAZ-02')
    const pushSpy = vi.spyOn(window.history, 'pushState')

    render(<App />)

    await screen.findByRole('heading', { name: 'LAZ-02' })
    await waitFor(() => expect(screen.getAllByText('Spare impeller').length).toBeGreaterThan(0))
    fireEvent.click(screen.getByRole('button', { name: 'Open' }))

    // The bug: this used to render nothing at all (InventoryPanel's
    // showEquipmentEditor stayed false because activeSectionId was still
    // 'locations'). Now the Specifications form for eq-1 appears.
    await waitFor(() => expect(screen.getByLabelText('Name')).toHaveValue('Spare impeller'))
    expect(screen.queryByRole('heading', { name: 'LAZ-02' })).not.toBeInTheDocument()

    expect(window.location.pathname).toBe('/inventory/equipment/eq-1')
    expect(pushSpy).toHaveBeenCalled()
  })

  it('Full item from a bin page renders a blank create-mode editor', async () => {
    window.history.replaceState({}, '', '/inventory/bins/LAZ-02')
    render(<App />)

    await screen.findByRole('heading', { name: 'LAZ-02' })
    fireEvent.click(screen.getByRole('button', { name: 'Full item' }))

    await waitFor(() => expect(screen.getByLabelText('Name')).toHaveValue(''))
    expect(screen.getByText('New item')).toBeInTheDocument()
    expect(screen.queryByRole('heading', { name: 'LAZ-02' })).not.toBeInTheDocument()
  })

  it('Open from Stocktake\'s photo grid renders the equipment editor', async () => {
    window.history.replaceState({}, '', '/inventory/stocktake')
    render(<App />)

    const scanField = await screen.findByLabelText('Scan')
    fireEvent.change(scanField, { target: { value: 'https://boat.example/inventory/bins/LAZ-02' } })
    fireEvent.keyDown(scanField, { key: 'Enter' })

    await screen.findByRole('heading', { name: 'LAZ-02' })
    await waitFor(() => expect(screen.getAllByText('Spare impeller').length).toBeGreaterThan(0))

    fireEvent.click(screen.getByRole('button', { name: 'Open' }))

    await waitFor(() => expect(screen.getByLabelText('Name')).toHaveValue('Spare impeller'))
    expect(window.location.pathname).toBe('/inventory/equipment/eq-1')
  })
})

// Release-fixes code-review finding: onOpenEquipment/onNewEquipment used to
// switch straight to the Equipment section with no guard at all - reachable
// from Stocktake's photo grid and the bin page's "Full item" button, both of
// which can hold real work (a stocktake pass's confirmed scans, a staged
// quick-add) that switching sections would silently clear.
describe('App: Open/Full item guards against losing stocktake or quick-add work', () => {
  it('Open mid-stocktake asks first; Stay keeps the confirmed scan; Leave opens the editor', async () => {
    window.history.replaceState({}, '', '/inventory/stocktake')
    render(<App />)

    const scanField = await screen.findByLabelText('Scan')
    fireEvent.change(scanField, { target: { value: 'https://boat.example/inventory/bins/LAZ-02' } })
    fireEvent.keyDown(scanField, { key: 'Enter' })
    await screen.findByRole('heading', { name: 'LAZ-02' })

    fireEvent.change(scanField, { target: { value: 'https://boat.example/inventory/equipment/eq-1' } })
    fireEvent.keyDown(scanField, { key: 'Enter' })
    await screen.findByText('Confirmed')

    fireEvent.click(screen.getByRole('button', { name: 'Open' }))

    await screen.findByText('Leave stocktake?')
    expect(screen.getByText('The scans from this pass will be cleared.')).toBeInTheDocument()
    // Guarded - the editor has not appeared, and the pass is untouched.
    expect(screen.queryByLabelText('Name')).not.toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: 'Stay' }))
    expect(screen.queryByText('Leave stocktake?')).not.toBeInTheDocument()
    expect(screen.getByText('Confirmed')).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: 'Open' }))
    await screen.findByText('Leave stocktake?')
    fireEvent.click(screen.getByRole('button', { name: 'Leave' }))

    await waitFor(() => expect(screen.getByLabelText('Name')).toHaveValue('Spare impeller'))
  })

  it('Full item with a staged quick-add asks first; Stay keeps the draft; Leave opens a blank editor', async () => {
    window.history.replaceState({}, '', '/inventory/bins/LAZ-02')
    render(<App />)

    await screen.findByRole('heading', { name: 'LAZ-02' })
    fireEvent.change(screen.getByLabelText('Name'), { target: { value: 'Gaffer tape' } })

    fireEvent.click(screen.getByRole('button', { name: 'Full item' }))

    await screen.findByText('Leave this bin?')
    expect(screen.getByText('The name and photos you have added will be cleared.')).toBeInTheDocument()
    expect(screen.queryByText('New item')).not.toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: 'Stay' }))
    expect(screen.queryByText('Leave this bin?')).not.toBeInTheDocument()
    expect(screen.getByLabelText('Name')).toHaveValue('Gaffer tape')

    fireEvent.click(screen.getByRole('button', { name: 'Full item' }))
    await screen.findByText('Leave this bin?')
    fireEvent.click(screen.getByRole('button', { name: 'Leave' }))

    await waitFor(() => expect(screen.getByText('New item')).toBeInTheDocument())
    expect(screen.getByLabelText('Name')).toHaveValue('')
  })
})

// Release-fixes /code-review medium finding: Open/Full item route through
// requestWithinInventory (the describe block above), but three other exits
// out of a bin page holding quick-add work did not - the bin page's own
// Back button, leaving Inventory entirely (sidebar/another panel), and
// browser Back/Forward. Each discarded a staged name/photo with no prompt.
describe('App: three more exits guarded against losing stocktake or quick-add work', () => {
  // The bin page's own back button and InventoryNav's section tabs share
  // the accessible name "Locations"/"Equipment" with each other (the tab)
  // and the page's own Back - scope to the bin page's own container so
  // `getByRole` resolves to exactly one.
  const binPageContainer = () => {
    const heading = screen.getByRole('heading', { name: 'LAZ-02' })
    const container = heading.closest('.mx-auto')
    if (!container) throw new Error('bin page container not found')
    return container as HTMLElement
  }

  it("the bin page's own Back button asks first when quick-add holds work; Stay keeps it, Leave performs it", async () => {
    window.history.replaceState({}, '', '/inventory/bins/LAZ-02')
    render(<App />)
    await screen.findByRole('heading', { name: 'LAZ-02' })
    fireEvent.change(screen.getByLabelText('Name'), { target: { value: 'Gaffer tape' } })

    fireEvent.click(within(binPageContainer()).getByRole('button', { name: 'Locations' }))
    await screen.findByText('Leave this bin?')
    expect(screen.getByText('The name and photos you have added will be cleared.')).toBeInTheDocument()
    // The dialog is modal - the rest of the tree (the bin page underneath)
    // is marked inert, so getByRole (which excludes inert content) can't see
    // it; getByText still can (same technique app-settings-navigation-guard.
    // test.tsx's own guarded-Back tests use).
    expect(screen.getByText('LAZ-02')).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: 'Stay' }))
    expect(screen.queryByText('Leave this bin?')).not.toBeInTheDocument()
    expect(screen.getByLabelText('Name')).toHaveValue('Gaffer tape')

    fireEvent.click(within(binPageContainer()).getByRole('button', { name: 'Locations' }))
    await screen.findByText('Leave this bin?')
    fireEvent.click(screen.getByRole('button', { name: 'Leave' }))

    await waitFor(() => expect(screen.queryByRole('heading', { name: 'LAZ-02' })).not.toBeInTheDocument())
  })

  it("the bin page's own Back button goes straight through with no work staged", async () => {
    window.history.replaceState({}, '', '/inventory/bins/LAZ-02')
    render(<App />)
    await screen.findByRole('heading', { name: 'LAZ-02' })

    fireEvent.click(within(binPageContainer()).getByRole('button', { name: 'Locations' }))

    await waitFor(() => expect(screen.queryByRole('heading', { name: 'LAZ-02' })).not.toBeInTheDocument())
    expect(screen.queryByText('Leave this bin?')).not.toBeInTheDocument()
  })

  it('leaving Inventory from the sidebar asks first when the bin page holds quick-add work; Stay keeps it, Leave performs it', async () => {
    window.history.replaceState({}, '', '/inventory/bins/LAZ-02')
    render(<App />)
    await screen.findByRole('heading', { name: 'LAZ-02' })
    fireEvent.change(screen.getByLabelText('Name'), { target: { value: 'Gaffer tape' } })

    fireEvent.click(screen.getByRole('button', { name: 'Dashboard' }))
    await screen.findByText('Leave this bin?')
    // Same inert-tree reasoning as the Back-button test above.
    expect(screen.getByText('LAZ-02')).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: 'Stay' }))
    expect(screen.queryByText('Leave this bin?')).not.toBeInTheDocument()
    expect(screen.getByLabelText('Name')).toHaveValue('Gaffer tape')

    fireEvent.click(screen.getByRole('button', { name: 'Dashboard' }))
    await screen.findByText('Leave this bin?')
    fireEvent.click(screen.getByRole('button', { name: 'Leave' }))

    await waitFor(() => expect(screen.queryByRole('heading', { name: 'LAZ-02' })).not.toBeInTheDocument())
  })

  it('leaving Inventory from the sidebar goes straight through with no work staged', async () => {
    window.history.replaceState({}, '', '/inventory/bins/LAZ-02')
    render(<App />)
    await screen.findByRole('heading', { name: 'LAZ-02' })

    fireEvent.click(screen.getByRole('button', { name: 'Dashboard' }))

    await waitFor(() => expect(screen.queryByRole('heading', { name: 'LAZ-02' })).not.toBeInTheDocument())
    expect(screen.queryByText('Leave this bin?')).not.toBeInTheDocument()
  })

  it('browser Back off a bin page with quick-add work asks first and re-pushes the bin URL; Leave completes the Back', async () => {
    window.history.replaceState({}, '', '/inventory/bins/LAZ-02')
    render(<App />)
    await screen.findByRole('heading', { name: 'LAZ-02' })
    fireEvent.change(screen.getByLabelText('Name'), { target: { value: 'Gaffer tape' } })

    // Simulate the browser's own Back: it lands the document on the
    // previous URL and fires popstate, but does not itself re-run any React
    // effect - jsdom doesn't replay a pushState history stack, so this
    // reproduces exactly what the popstate handler actually sees (same
    // technique app-settings-navigation-guard.test.tsx's own Back tests use).
    window.history.replaceState({}, '', '/inventory/locations')
    act(() => { window.dispatchEvent(new PopStateEvent('popstate')) })

    expect(screen.getByText('Leave this bin?')).toBeInTheDocument()
    expect(window.location.pathname).toBe('/inventory/bins/LAZ-02')
    expect(screen.getByText('LAZ-02')).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: 'Leave' }))
    expect(window.location.pathname).toBe('/inventory/locations')
  })

  it('browser Back off Stocktake with a confirmed scan asks first and re-pushes /inventory/stocktake', async () => {
    window.history.replaceState({}, '', '/inventory/stocktake')
    render(<App />)

    const scanField = await screen.findByLabelText('Scan')
    fireEvent.change(scanField, { target: { value: 'https://boat.example/inventory/bins/LAZ-02' } })
    fireEvent.keyDown(scanField, { key: 'Enter' })
    await screen.findByRole('heading', { name: 'LAZ-02' })

    fireEvent.change(scanField, { target: { value: 'https://boat.example/inventory/equipment/eq-1' } })
    fireEvent.keyDown(scanField, { key: 'Enter' })
    await screen.findByText('Confirmed')

    window.history.replaceState({}, '', '/inventory/locations')
    act(() => { window.dispatchEvent(new PopStateEvent('popstate')) })

    expect(screen.getByText('Leave stocktake?')).toBeInTheDocument()
    expect(window.location.pathname).toBe('/inventory/stocktake')
    expect(screen.getByText('Confirmed')).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: 'Leave' }))
    expect(window.location.pathname).toBe('/inventory/locations')
  })

  it('browser Back with no work goes straight through', async () => {
    window.history.replaceState({}, '', '/inventory/bins/LAZ-02')
    render(<App />)
    await screen.findByRole('heading', { name: 'LAZ-02' })

    window.history.replaceState({}, '', '/inventory/locations')
    act(() => { window.dispatchEvent(new PopStateEvent('popstate')) })

    expect(window.location.pathname).toBe('/inventory/locations')
    expect(screen.queryByText('Leave this bin?')).not.toBeInTheDocument()
  })

  // requestWithinInventory already covered the section-tab switch before
  // this round (onSectionChange, App.tsx) - pinned here as a regression
  // check alongside the three exits that didn't.
  it("switching Inventory's section tab still asks first when quick-add holds work", async () => {
    window.history.replaceState({}, '', '/inventory/bins/LAZ-02')
    render(<App />)
    await screen.findByRole('heading', { name: 'LAZ-02' })
    fireEvent.change(screen.getByLabelText('Name'), { target: { value: 'Gaffer tape' } })

    fireEvent.click(screen.getByRole('button', { name: 'Equipment' }))
    await screen.findByText('Leave this bin?')

    fireEvent.click(screen.getByRole('button', { name: 'Stay' }))
    expect(screen.getByLabelText('Name')).toHaveValue('Gaffer tape')
  })
})

// Release-fixes /code-review medium finding: pendingNavigationKind used to
// be recomputed on every render from live inventoryDirty/inventoryHasWork -
// correct while nothing async is running behind the modal dialog, wrong the
// moment something is. A quick-add Save kicked off just before the guard
// opened keeps running underneath the (inert) dialog, and its success clears
// inventoryHasWork - which used to flip the dialog from Leave/Stay to the
// "Unsaved changes"/Save and Continue form for a page that was never dirty
// in that sense, and whose ref has nothing mounted to save.
describe('App: the guard dialog freezes its wording at the moment it opens', () => {
  it('a quick-add Save finishing underneath the dialog does not swap Leave/Stay for Save and Continue', async () => {
    window.history.replaceState({}, '', '/inventory/bins/LAZ-02')
    let resolveCreate: (() => void) | null = null
    vi.stubGlobal('fetch', vi.fn(async (url: string, init?: RequestInit) => {
      const u = String(url)
      const method = init?.method ?? 'GET'
      if (u.endsWith('/api/health')) {
        return { ok: true, json: async () => ({ status: 'ok', version: 'v0.31.0', revision: 'deadbeef' }) }
      }
      if (u.endsWith('/api/assistant/status')) {
        return { ok: true, json: async () => ({ enabled: false, configured: false, model: '', problem: '' }) }
      }
      if (u.endsWith('/api/inventory/zones')) {
        return { ok: true, json: async () => ({ zones }) }
      }
      if (u.includes('/api/inventory/equipment?bin=') && method === 'GET') {
        return { ok: true, json: async () => ({ items: [item] }) }
      }
      if (u.endsWith('/api/equipment-profiles')) {
        return { ok: true, json: async () => ({ profiles: [], problems: [] }) }
      }
      if (u.endsWith('/api/signalk/paths')) {
        return { ok: true, json: async () => ({ paths: [] }) }
      }
      if (u.endsWith('/api/inventory/equipment') && method === 'POST') {
        // Deliberately left pending - resolved explicitly below, once the
        // guard dialog is already open, to reproduce the save completing
        // underneath it rather than before it opens.
        return new Promise((resolve) => {
          resolveCreate = () => resolve({ ok: true, status: 201, json: async () => ({ item: { id: 'eq-new', photo_ids: [] } }) })
        })
      }
      return { ok: false, json: async () => ({}) }
    }))

    render(<App />)
    await screen.findByRole('heading', { name: 'LAZ-02' })
    fireEvent.change(screen.getByLabelText('Name'), { target: { value: 'Gaffer tape' } })
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))
    // The create POST above is deliberately stuck pending - hasWork is
    // still true (name/photos only clear once it resolves).

    const heading = screen.getByRole('heading', { name: 'LAZ-02' })
    const container = heading.closest('.mx-auto') as HTMLElement
    fireEvent.click(within(container).getByRole('button', { name: 'Locations' }))

    await screen.findByText('Leave this bin?')
    expect(screen.getByText('The name and photos you have added will be cleared.')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /save and continue/i })).not.toBeInTheDocument()
    expect(screen.queryByText('Unsaved changes')).not.toBeInTheDocument()

    // Let the save resolve now, with the (modal, inert) dialog already open.
    await act(async () => {
      resolveCreate?.()
      await Promise.resolve()
      await Promise.resolve()
    })

    // Still the same Leave/Stay form - not recomputed off the now-false
    // hasWork into the "Unsaved changes"/Save and Continue shape.
    expect(screen.getByText('Leave this bin?')).toBeInTheDocument()
    expect(screen.getByText('The name and photos you have added will be cleared.')).toBeInTheDocument()
    expect(screen.queryByText('Unsaved changes')).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /save and continue/i })).not.toBeInTheDocument()
  })
})

// Release-fixes /code-review medium finding: a partial-failure save clears
// the quick-add form's own name/photo fields, but failedUploads/savedItemId
// still hold a photo nothing has actually sent - bin-quick-add.test.tsx
// pins that BinQuickAdd itself now reports this as hasWork; this is the
// guard's own end of it, through the real App/InventoryPanel/BinPage wiring.
describe('App: a photo still queued for Retry guards leaving the bin', () => {
  it('Open on another item after a partial-failure save asks first, naming the queued photo', async () => {
    window.history.replaceState({}, '', '/inventory/bins/LAZ-02')
    vi.stubGlobal('fetch', vi.fn(async (url: string, init?: RequestInit) => {
      const u = String(url)
      const method = init?.method ?? 'GET'
      if (u.endsWith('/api/health')) {
        return { ok: true, json: async () => ({ status: 'ok', version: 'v0.31.0', revision: 'deadbeef' }) }
      }
      if (u.endsWith('/api/assistant/status')) {
        return { ok: true, json: async () => ({ enabled: false, configured: false, model: '', problem: '' }) }
      }
      if (u.endsWith('/api/inventory/zones')) {
        return { ok: true, json: async () => ({ zones }) }
      }
      if (u.includes('/api/inventory/equipment?bin=') && method === 'GET') {
        return { ok: true, json: async () => ({ items: [item] }) }
      }
      if (u.match(/\/api\/inventory\/equipment\/eq-1$/) && method === 'GET') {
        return { ok: true, json: async () => ({ item, documents: [] }) }
      }
      if (u.endsWith('/api/equipment-profiles')) {
        return { ok: true, json: async () => ({ profiles: [], problems: [] }) }
      }
      if (u.endsWith('/api/signalk/paths')) {
        return { ok: true, json: async () => ({ paths: [] }) }
      }
      if (u.endsWith('/api/inventory/equipment') && method === 'POST') {
        return { ok: true, status: 201, json: async () => ({ item: { id: 'eq-new', photo_ids: [] } }) }
      }
      if (u.endsWith('/api/inventory/equipment/eq-new/photos') && method === 'POST') {
        return { ok: false, status: 500, json: async () => ({ error: 'upload failed: a.jpg' }) }
      }
      return { ok: false, json: async () => ({}) }
    }))

    render(<App />)
    await screen.findByRole('heading', { name: 'LAZ-02' })

    const file = new File(['a'], 'a.jpg', { type: 'image/jpeg' })
    fireEvent.change(screen.getByLabelText('Take photo'), { target: { files: [file] } })
    await waitFor(() => expect(screen.getByText('Remove')).toBeInTheDocument())
    fireEvent.change(screen.getByLabelText('Name'), { target: { value: 'Gaffer tape' } })
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))

    await screen.findByText("Saved Gaffer tape, but 1 photo didn't upload: upload failed: a.jpg")
    // The form is cleared - Open must still ask, off the retry queue alone.
    expect(screen.getByLabelText('Name')).toHaveValue('')

    fireEvent.click(screen.getByRole('button', { name: 'Open' }))

    await screen.findByText('Leave this bin?')
    expect(screen.getByText("1 photo for Gaffer tape hasn't uploaded yet.")).toBeInTheDocument()
    expect(screen.queryByLabelText('Name', { selector: 'input' })).not.toBeNull()

    fireEvent.click(screen.getByRole('button', { name: 'Stay' }))
    expect(screen.queryByText('Leave this bin?')).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Retry' })).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: 'Open' }))
    await screen.findByText('Leave this bin?')
    fireEvent.click(screen.getByRole('button', { name: 'Leave' }))

    await waitFor(() => expect(screen.getByLabelText('Name')).toHaveValue('Spare impeller'))
  })
})
