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
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { App } from '../App'
import type { EquipmentItem, InventoryZone } from '@/hooks/use-inventory'

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
    return { ok: false, json: async () => ({}) }
  }))
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
