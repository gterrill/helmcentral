/**
 * App-level coverage for the gauge group Duplicate button (docs/adr/0102).
 *
 * Commit 3f7986b moved Duplicate from the grid chrome into the gauge group
 * config dialog and introduced two bugs in App.tsx's handleDuplicateGaugeGroup:
 *
 *   Bug A: duplicating a brand-new (never-saved) draft wrote only the copy.
 *   handleAddGaugeGroup sets gaugeGroupDraft locally without adding it to
 *   effectiveWidgets, so "[...effectiveWidgets, copy]" silently dropped the
 *   draft the operator had just built.
 *
 *   Bug B: duplicating an already-saved group applied the operator's edits
 *   only to the copy. The original stayed in effectiveWidgets under its old
 *   config, so a Celsius-vs-raw mismatch fixed on screen was still present
 *   in what actually got saved for the source tile.
 *
 * gauge-group-config-dialog.test.tsx only proves the onDuplicate callback
 * fires with the right config — it stops at the dialog boundary and never
 * touches App's handler, which is where both bugs actually live. That's why
 * neither one was caught. These tests drive the real App component (real
 * useDashboardPages hook, fetch stubbed as a tiny in-memory backend) through
 * the same clicks an operator would make.
 */
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { App } from '../App'
import type { DashboardPage } from '@/hooks/use-dashboard-pages'
import type { DashboardLayoutItem } from '@/lib/dashboard-widgets'
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

async function openGaugeGroupDialogFromAddWidget(): Promise<void> {
  fireEvent.click(screen.getByRole('button', { name: /add widget/i }))
  fireEvent.click(await screen.findByRole('button', { name: /^gauge group/i }))
}

function widgetsFromLastPatch(): DashboardLayoutItem[] {
  const last = patchCalls.at(-1)
  return (last?.patch.widgets as DashboardLayoutItem[] | undefined) ?? []
}

describe('duplicating a gauge group from App', () => {
  it('persists both the new draft and its copy, not just the copy (bug A)', async () => {
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

    // Buggy handler wrote only the copy: length 1, draft gone entirely.
    expect(widgets).toHaveLength(2)
    expect(widgets.every((w) => w.gaugeGroup?.title === 'Port')).toBe(true)
    expect(widgets.every((w) => w.gaugeGroup?.gauges[0]?.path === 'propulsion.port.revolutions')).toBe(true)
    expect(new Set(widgets.map((w) => w.id)).size).toBe(2)
  })

  it('offsets the copy so it does not land on top of the freshly saved draft', async () => {
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
    const [a, b] = widgetsFromLastPatch()
    expect(a).toBeDefined()
    expect(b).toBeDefined()
    expect(a.x === b.x && a.y === b.y).toBe(false)
  })

  it('saves the edit to the original and adds a copy, for an already-saved group (bug B)', async () => {
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
    // Buggy handler left the original's title as 'Port' — the edit only
    // ever reached the copy.
    expect(original?.gaugeGroup?.title).toBe('Starboard')

    const copy = widgets.find((w) => w.id !== 'gauge-group:existing1')
    expect(copy?.gaugeGroup?.title).toBe('Starboard')
    expect(copy && original && (copy.x !== original.x || copy.y !== original.y)).toBe(true)
  })
})
