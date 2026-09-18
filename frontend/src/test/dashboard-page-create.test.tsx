/**
 * App-level coverage for ADR 0107: creating a page names it in place instead
 * of behind a dialog, drops straight into a focused empty name field, and
 * shows a short prompt instead of a blank grid until something is on it. The
 * toolbar that used to be a "Layout Mode" pill above the grid plus a
 * separate control row below it is now one toolbar at the top, in a fixed
 * order.
 *
 * Same harness as app-gauge-group-duplicate.test.tsx: the real
 * useDashboardPages hook, run against a small in-memory backend, driven by
 * the same clicks an operator would make — not a mocked handler's say-so.
 */
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { App } from '../App'
import type { DashboardPage } from '@/hooks/use-dashboard-pages'
import { setViewportWidth } from './viewport'

// ── hook mocks (same harness as app-gauge-group-duplicate.test.tsx) ────────

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

interface PatchCall { id: string; patch: Record<string, unknown> }

let currentPages: DashboardPage[]
let patchCalls: PatchCall[]
let nextPageNumber: number

function seedPages(pages: DashboardPage[]): void {
  currentPages = pages
}

beforeEach(() => {
  setViewportWidth(1440)
  localStorage.clear()
  patchCalls = []
  nextPageNumber = 2
  seedPages([
    { id: 'page-1', name: 'Test Page', widgets: [], created_at: '2026-01-01T00:00:00Z', updated_at: '2026-01-01T00:00:00Z' },
  ])

  vi.stubGlobal('fetch', vi.fn((url: string, init?: RequestInit) => {
    const method = init?.method ?? 'GET'

    if (url === '/api/dashboard-pages' && method === 'GET') {
      return Promise.resolve({ ok: true, json: async () => ({ pages: currentPages }) })
    }

    if (url === '/api/dashboard-pages' && method === 'POST') {
      const body = JSON.parse(String(init?.body ?? '{}')) as { name: string }
      const page: DashboardPage = {
        id: `page-${nextPageNumber++}`,
        name: body.name,
        widgets: [],
        created_at: '2026-01-02T00:00:00Z',
        updated_at: '2026-01-02T00:00:00Z',
      }
      currentPages = [...currentPages, page]
      return Promise.resolve({ ok: true, json: async () => page })
    }

    // App blocks its whole shell behind auth.mode resolving, so these two
    // need a real answer, not the generic ok:false fallback below.
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
      currentPages = currentPages.map((p) => (p.id === patchMatch[1] ? { ...p, ...patch, updated_at: new Date().toISOString() } as DashboardPage : p))
      const updated = currentPages.find((p) => p.id === patchMatch[1])!
      return Promise.resolve({ ok: true, json: async () => updated })
    }

    // Everything else (equipment profiles, signalk paths, manual, auth
    // mode…) — fails gracefully, same fallback App.smoke.test.tsx uses.
    return Promise.resolve({ ok: false, json: async () => ({}) })
  }))

  vi.spyOn(console, 'error').mockImplementation(() => {})
})

async function activePageReady(): Promise<void> {
  await screen.findAllByText('Test Page')
}

async function createNewPage(): Promise<void> {
  fireEvent.click(screen.getByLabelText('Switch dashboard page'))
  fireEvent.click(await screen.findByText(/New Page/))
  // The new page becomes active and layout mode turns on — wait for the
  // toolbar's name field to actually mount before interacting with it.
  await screen.findByLabelText('Page name')
}

describe('creating a dashboard page', () => {
  it('names the new page in place: no dialog, an empty focused field with a placeholder', async () => {
    render(<App />)
    await activePageReady()
    await createNewPage()

    const nameField = screen.getByLabelText('Page name') as HTMLInputElement
    expect(nameField).toHaveValue('')
    expect(nameField).toHaveAttribute('placeholder', 'Page name')
    expect(nameField).toHaveFocus()
    // No dialog was involved in getting here.
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  })

  it('Enter with a typed name sends a PATCH; blurring an empty field sends nothing', async () => {
    render(<App />)
    await activePageReady()
    await createNewPage()

    const nameField = screen.getByLabelText('Page name') as HTMLInputElement
    fireEvent.change(nameField, { target: { value: 'Anchored' } })
    fireEvent.keyDown(nameField, { key: 'Enter', code: 'Enter' })

    await waitFor(() => {
      const namePatch = patchCalls.find((c) => 'name' in c.patch)
      expect(namePatch?.patch.name).toBe('Anchored')
    })

    // A second, freshly created page left blank and merely blurred must
    // never reach the server — an empty name would otherwise draw the
    // backend's 400 (backend/dashboard_pages.go). createNewPage() itself
    // only POSTs; no PATCH should follow from the blur below.
    await createNewPage()
    const patchCountBeforeBlur = patchCalls.length
    fireEvent.blur(screen.getByLabelText('Page name'))
    await new Promise((resolve) => setTimeout(resolve, 0))
    expect(patchCalls).toHaveLength(patchCountBeforeBlur)
  })

  it('an empty blur ends the naming session showing "Untitled page", not a blank field', async () => {
    render(<App />)
    await activePageReady()
    await createNewPage()

    const nameField = screen.getByLabelText('Page name') as HTMLInputElement
    fireEvent.blur(nameField)

    await waitFor(() => {
      expect(nameField).toHaveValue('Untitled page')
    })
    expect(patchCalls.some((c) => 'name' in c.patch)).toBe(false)
  })

  it('Enter commits once; a blur right after does not send a duplicate PATCH', async () => {
    render(<App />)
    await activePageReady()
    await createNewPage()

    const nameField = screen.getByLabelText('Page name') as HTMLInputElement
    fireEvent.change(nameField, { target: { value: 'Anchored' } })
    fireEvent.keyDown(nameField, { key: 'Enter', code: 'Enter' })
    // Enter itself blurs the field (so the one commit runs from onBlur); this
    // extra blur is the one that used to fire a second PATCH before the
    // page prop had caught up to the new name.
    fireEvent.blur(nameField)

    await waitFor(() => {
      const namePatches = patchCalls.filter((c) => 'name' in c.patch)
      expect(namePatches).toHaveLength(1)
    })
    await new Promise((resolve) => setTimeout(resolve, 0))
    const namePatches = patchCalls.filter((c) => 'name' in c.patch)
    expect(namePatches).toHaveLength(1)
    expect(namePatches[0]?.patch.name).toBe('Anchored')
  })

  it('shows the empty-page prompt instead of a blank grid', async () => {
    render(<App />)
    await activePageReady()
    await createNewPage()

    expect(screen.getByText(/create your personalized page/i)).toBeInTheDocument()
    expect(screen.getByText(/use the add widget button to get started/i)).toBeInTheDocument()
  })

  it('lays the toolbar out in order — name field, Add Widget, Ribbon, Skin, Hero, Kiosk — above the page content', async () => {
    render(<App />)
    await activePageReady()
    await createNewPage()

    const toolbar = screen.getByTestId('layout-toolbar')
    const nameField = screen.getByLabelText('Page name')
    const addWidget = screen.getByRole('button', { name: /add widget/i })
    const ribbon = screen.getByRole('button', { name: /^ribbon$/i })
    const skin = screen.getByLabelText(/skin for/i)
    const hero = screen.getByLabelText(/hero widget for/i)
    const kiosk = screen.getByLabelText(/^kiosk for/i)

    const positions = [nameField, addWidget, ribbon, skin, hero, kiosk]
    for (let i = 0; i < positions.length - 1; i += 1) {
      // DOCUMENT_POSITION_FOLLOWING (4): positions[i] comes before positions[i + 1].
      expect(positions[i].compareDocumentPosition(positions[i + 1]) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
    }

    // The toolbar sits above the page's own content (here, the empty-page
    // prompt standing in for the grid).
    const prompt = screen.getByText(/create your personalized page/i)
    expect(toolbar.compareDocumentPosition(prompt) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
  })

  // App.tsx used to clear namingPageId only from PageTitleField's own
  // onDone (an Enter/blur that saved, or an Escape). Switching pages
  // without touching the field at all skipped that entirely, so the flag
  // stuck to the page that created it.
  it('does not leave a stale naming flag on a page you switch away from without naming it', async () => {
    render(<App />)
    await activePageReady()
    await createNewPage()

    // Switch to the original page without ever touching the new page's
    // name field. Scoped to the popover (role="dialog"): the sidebar lists
    // the same page names as its own nav buttons, so an unscoped query is
    // ambiguous.
    fireEvent.click(screen.getByLabelText('Switch dashboard page'))
    fireEvent.click(within(await screen.findByRole('dialog')).getByRole('button', { name: 'Test Page' }))

    // Switch back to the page just created.
    fireEvent.click(screen.getByLabelText('Switch dashboard page'))
    fireEvent.click(within(await screen.findByRole('dialog')).getByRole('button', { name: 'Untitled page' }))

    // Its name field must show the page's real, saved name - not sit blank
    // and stealing focus for a naming session that already ended when we
    // switched away the first time.
    const nameField = await screen.findByLabelText('Page name') as HTMLInputElement
    expect(nameField).toHaveValue('Untitled page')
    expect(nameField).not.toHaveFocus()
  })
})

describe('loading the dashboard', () => {
  it('does not flash the empty-page prompt before /api/dashboard-pages has resolved', async () => {
    let resolveGet!: (value: { ok: boolean; json: () => Promise<unknown> }) => void
    vi.stubGlobal('fetch', vi.fn((url: string, init?: RequestInit) => {
      const method = init?.method ?? 'GET'
      if (url === '/api/dashboard-pages' && method === 'GET') {
        return new Promise((resolve) => { resolveGet = resolve })
      }
      if (url === '/api/auth/mode' && method === 'GET') {
        return Promise.resolve({ ok: true, json: async () => ({ mode: 'none' }) })
      }
      if (url === '/api/auth/me' && method === 'GET') {
        return Promise.resolve({ ok: true, json: async () => ({ authenticated: false }) })
      }
      return Promise.resolve({ ok: false, json: async () => ({}) })
    }))

    render(<App />)

    // The initial GET is still in flight, so there is no active page yet -
    // that has to read as "still loading", not as "this page is empty".
    expect(screen.queryByText(/nothing on this page yet/i)).not.toBeInTheDocument()

    resolveGet({ ok: true, json: async () => ({ pages: currentPages }) })
    await activePageReady()
    expect(screen.getByText(/press edit to add widgets/i)).toBeInTheDocument()
  })
})
