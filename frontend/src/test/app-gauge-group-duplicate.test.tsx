/**
 * App-level coverage for the gauge group Duplicate button (docs/adr/0102).
 *
 * Duplicate is Save As, not Save And Copy: change the values in the dialog,
 * then click Duplicate instead of Save, and the edits land on a new tile
 * while the source is left exactly as it was when the dialog opened. For a
 * brand-new draft that was never saved, that means Duplicate produces the
 * one tile carrying the edits, not a saved draft plus a separate copy.
 *
 * gauge-group-config-dialog.test.tsx only proves the onDuplicate callback
 * fires with the right config — it stops at the dialog boundary and never
 * touches App's handler, which is where this placement and persistence
 * logic actually lives. These tests drive the real App component (real
 * useDashboardPages hook, fetch stubbed as a tiny in-memory backend) through
 * the same clicks an operator would make.
 */
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { App } from '../App'
import type { DashboardPage } from '@/hooks/use-dashboard-pages'
import { DASHBOARD_WIDGET_DEFAULT_SIZE, type DashboardLayoutItem } from '@/lib/dashboard-widgets'
import { setViewportWidth } from './viewport'

// ── fetch so components that call it don't throw ──────────────────────────
// Real useDashboardPages hook, talking to a one-page in-memory backend below.

// ── hook mocks (same harness as App.smoke.test.tsx) ────────────────────────

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
      temperature_f: -1, condition: '—',
      wind_speed_kts: -1, wind_direction: '—', wind_gust_kts: -1, precipitation_pct: -1,
      provider: '', cached: false, updated_at: '', ttl_seconds: 0,
    },
  }),
}))

vi.mock('@/hooks/use-weather-forecast', () => ({
  useWeatherForecast: () => ({ forecast: [], loading: false, error: null, provider: null, refetch: vi.fn() }),
}))

vi.mock('@/hooks/use-wave-forecast', () => ({
  useWaveForecast: () => ({
    days: [], seaTemperatureF: null, provider: null, loading: false, error: null, refetch: vi.fn(),
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

vi.mock('@/hooks/use-routes', () => ({
  useRoutes: () => ({ routes: [], loading: false, error: null, refetch: vi.fn(), createRoute: vi.fn(), updateRoute: vi.fn(), deleteRoute: vi.fn() }),
}))

vi.mock('@/hooks/use-sat-charts', () => ({
  useSatCharts: () => ({ charts: [], loading: false, error: null, uploadChart: vi.fn(), deleteChart: vi.fn() }),
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

// ── a one-page in-memory backend for /api/dashboard-pages ─────────────────
// Real useDashboardPages hook runs against this, so PATCH bodies below are
// exactly what App would send a real server — not a mocked handler's say-so.

interface PatchCall { id: string; patch: Record<string, unknown> }

let serverPage: DashboardPage
let patchCalls: PatchCall[]

function seedPage(widgets: DashboardLayoutItem[]): void {
  serverPage = {
    id: 'page-1',
    name: 'Test Page',
    widgets,
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
  }
}

beforeEach(() => {
  setViewportWidth(1440)
  localStorage.clear()
  patchCalls = []
  seedPage([])

  vi.stubGlobal('fetch', vi.fn((url: string, init?: RequestInit) => {
    const method = init?.method ?? 'GET'

    if (url === '/api/dashboard-pages' && method === 'GET') {
      return Promise.resolve({ ok: true, json: async () => ({ pages: [serverPage] }) })
    }

    // App blocks its whole shell behind auth.mode resolving (App.tsx: "Could
    // not determine whether this Helmcentral requires a sign-in" otherwise),
    // so these two need a real answer, not the generic ok:false fallback.
    if (url === '/api/auth/mode' && method === 'GET') {
      return Promise.resolve({ ok: true, json: async () => ({ mode: 'none' }) })
    }
    if (url === '/api/auth/me' && method === 'GET') {
      return Promise.resolve({ ok: true, json: async () => ({ authenticated: false }) })
    }

    const patchMatch = /^\/api\/dashboard-pages\/([^/]+)$/.exec(url)
    if (patchMatch && method === 'PATCH') {
      const patch = JSON.parse(String(init?.body ?? '{}')) as Record<string, unknown>
      patchCalls.push({ id: patchMatch[1], patch })
      serverPage = { ...serverPage, ...patch, updated_at: new Date().toISOString() } as DashboardPage
      return Promise.resolve({ ok: true, json: async () => serverPage })
    }

    // Everything else (equipment profiles, signalk paths, manual, auth
    // mode…) — fails gracefully, same fallback App.smoke.test.tsx uses.
    return Promise.resolve({ ok: false, json: async () => ({}) })
  }))

  vi.spyOn(console, 'error').mockImplementation(() => {})
})

// ── drive real UI, same clicks an operator makes ───────────────────────────

/**
 * Waits until App has resolved its one page as the active page. "Test Page"
 * shows up twice once it has (the sidebar's page list and the header page
 * switcher), so this waits for at least one rather than a unique match.
 */
async function activePageReady(): Promise<void> {
  await screen.findAllByText('Test Page')
}

function enterEditMode(): void {
  fireEvent.click(screen.getByRole('button', { name: /enter edit mode/i }))
}

// Add Tile is a grouped DropdownMenu now (ADR 0107), not the plain
// Popover-of-buttons it used to be — its entries are `menuitem`s, not
// `button`s.
async function openGaugeGroupDialogFromAddWidget(): Promise<void> {
  fireEvent.click(screen.getByRole('button', { name: /add tile/i }))
  fireEvent.click(await screen.findByRole('menuitem', { name: /^gauge group/i }))
}

/** Same picker, for a built-in widget rather than a multi-instance draft. */
async function addBuiltinWidgetFromAddWidget(label: string | RegExp): Promise<void> {
  fireEvent.click(screen.getByRole('button', { name: /add tile/i }))
  fireEvent.click(await screen.findByRole('menuitem', { name: label }))
}

function widgetsFromLastPatch(): DashboardLayoutItem[] {
  const last = patchCalls.at(-1)
  return (last?.patch.widgets as DashboardLayoutItem[] | undefined) ?? []
}

describe('duplicating a gauge group from App', () => {
  it('yields exactly one widget carrying the edits, for a brand-new draft that was never saved', async () => {
    render(<App />)
    await activePageReady()
    enterEditMode()
    await openGaugeGroupDialogFromAddWidget()

    fireEvent.change(screen.getByLabelText('Title'), { target: { value: 'Port' } })
    fireEvent.change(screen.getAllByLabelText('SignalK path')[0], {
      target: { value: 'propulsion.port.revolutions' },
    })

    fireEvent.click(screen.getByRole('button', { name: /duplicate this tile/i }))

    await waitFor(() => expect(patchCalls.length).toBeGreaterThan(0))
    const widgets = widgetsFromLastPatch()

    // The draft never joined the widget list on its own account, so
    // Duplicate has only the copy to write: one tile, not two.
    expect(widgets).toHaveLength(1)
    expect(widgets[0].gaugeGroup?.title).toBe('Port')
    expect(widgets[0].gaugeGroup?.gauges[0]?.path).toBe('propulsion.port.revolutions')
  })

  it('leaves an already-saved original untouched in the PATCH body and adds one new widget carrying the edits', async () => {
    seedPage([
      {
        id: 'gauge-group:existing1',
        x: 0, y: 0, w: 4, h: 7,
        gaugeGroup: {
          title: 'Port',
          gauges: [
            { path: 'propulsion.port.revolutions', label: 'RPM', display: 'numeric', quantity: 'frequency', unit: 'rpm' },
          ],
        },
      },
    ])

    render(<App />)
    await activePageReady()
    enterEditMode()

    fireEvent.click(await screen.findByRole('button', { name: 'Configure Port' }))
    fireEvent.change(screen.getByLabelText('Title'), { target: { value: 'Starboard' } })
    fireEvent.click(screen.getByRole('button', { name: /duplicate this tile/i }))

    await waitFor(() => expect(patchCalls.length).toBeGreaterThan(0))
    const widgets = widgetsFromLastPatch()

    expect(widgets).toHaveLength(2)
    const original = widgets.find((w) => w.id === 'gauge-group:existing1')
    // Save As: the edit lands on the copy only. The original keeps the
    // config it had when the dialog opened.
    expect(original?.gaugeGroup?.title).toBe('Port')

    const copy = widgets.find((w) => w.id !== 'gauge-group:existing1')
    expect(copy?.gaugeGroup?.title).toBe('Starboard')
  })

  it("does not sit the copy at the original's coordinates", async () => {
    seedPage([
      {
        id: 'gauge-group:existing1',
        x: 0, y: 0, w: 4, h: 7,
        gaugeGroup: {
          title: 'Port',
          gauges: [
            { path: 'propulsion.port.revolutions', label: 'RPM', display: 'numeric', quantity: 'frequency', unit: 'rpm' },
          ],
        },
      },
    ])

    render(<App />)
    await activePageReady()
    enterEditMode()

    fireEvent.click(await screen.findByRole('button', { name: 'Configure Port' }))
    fireEvent.click(screen.getByRole('button', { name: /duplicate this tile/i }))

    await waitFor(() => expect(patchCalls.length).toBeGreaterThan(0))
    const widgets = widgetsFromLastPatch()
    const original = widgets.find((w) => w.id === 'gauge-group:existing1')
    const copy = widgets.find((w) => w.id !== 'gauge-group:existing1')

    expect(original).toBeDefined()
    expect(copy).toBeDefined()
    expect(copy!.x === original!.x && copy!.y === original!.y).toBe(false)
    // Below everything else on the page, same as any other freshly placed
    // widget, rather than stacked directly on its source.
    expect(copy!.y).toBe(original!.y + original!.h)
  })
})

/**
 * Regression coverage for the bug ADR 0107 fixes: every built-in widget used
 * to land at a hard-coded {w:4, h:6} regardless of what it actually shows,
 * which cut Battery & Power's Shore line off the bottom of the tile before
 * an operator ever touched a resize handle.
 */
describe('adding a built-in widget from App', () => {
  it("sends Battery & Power's own default height, not the old hard-coded 6", async () => {
    render(<App />)
    await activePageReady()
    enterEditMode()

    await addBuiltinWidgetFromAddWidget(/^battery & power/i)

    await waitFor(() => expect(patchCalls.length).toBeGreaterThan(0))
    const widgets = widgetsFromLastPatch()
    const batteryPower = widgets.find((w) => w.id === 'battery-power')

    expect(batteryPower).toBeDefined()
    expect(batteryPower!.h).toBe(DASHBOARD_WIDGET_DEFAULT_SIZE['battery-power'].h)
    expect(batteryPower!.h).toBeGreaterThan(6)
    expect(batteryPower!.w).toBe(DASHBOARD_WIDGET_DEFAULT_SIZE['battery-power'].w)
  })
})
