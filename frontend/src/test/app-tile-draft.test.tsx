/**
 * App-level coverage for the tile draft wiring (docs/adr/0260): each dialog
 * is given its own kind's draft, Save writes the tile and closes the dialog,
 * Cancel writes nothing, and a failed write keeps the dialog open.
 *
 * Embed and Nearby map dialogs call onSave and then onOpenChange(false)
 * themselves, so the sequence is checked here, through the real App and a
 * one-page in-memory backend, not just in the hook.
 */
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { landAnchorWatch } from './helpers/anchor-watch-poll'
import { installFakeTelemetryStream } from './helpers/fake-telemetry-stream'

import { App } from '../App'
import type { DashboardPage } from '@/hooks/use-dashboard-pages'
import type { DashboardLayoutItem } from '@/lib/dashboard-widgets'
import { setViewportWidth } from './viewport'

// ── fetch so components that call it don't throw ──────────────────────────
// Real useDashboardPages hook, talking to a one-page in-memory backend below.

// ── hook mocks (same harness as App.smoke.test.tsx) ────────────────────────





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
      temperature_f: -1, condition: '—',
      wind_speed_kts: -1, wind_direction: '—', wind_gust_kts: -1, precipitation_pct: -1,
      provider: '', cached: false, updated_at: '', ttl_seconds: 0,
    },
  }),
}))

vi.mock('@/hooks/use-weather-forecast', () => ({
  useForecastSunTimes: () => ({ sunriseTime: null, sunsetTime: null }),
  useForecastSun: () => ({ sunriseTime: null, sunsetTime: null, moonPhase: null }),
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

vi.mock('@/hooks/use-route-activation', () => {
  // One object, so a re-render does not look like a new route status.
  const result = { status: { state: 'inactive', destination: null } }
  return { useRouteActivation: () => result }
})

vi.mock('@/hooks/use-forecast-warnings', () => ({
  useHasActiveWindBulletin: () => false,
  useForecastWarnings: () => ({ activeWarning: null }),
  findActiveWindBulletin: () => null,
}))

vi.mock('@/hooks/use-server-trails', async () => (await import('./trail-mocks')).trailHooksMock)

vi.mock('@/hooks/use-dark-mode', () => ({
  useDarkMode: () => [false, vi.fn()],
}))

// ── a one-page in-memory backend for /api/dashboard-pages ─────────────────
// Real useDashboardPages hook runs against this, so PATCH bodies below are
// exactly what App would send a real server — not a mocked handler's say-so.

interface PatchCall { id: string; patch: Record<string, unknown> }

let serverPage: DashboardPage
let patchCalls: PatchCall[]
let failPatches = false

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
  failPatches = false
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
      if (failPatches) return Promise.resolve({ ok: false, status: 500, json: async () => ({ error: 'boom' }), text: async () => 'boom' })
      serverPage = { ...serverPage, ...patch, updated_at: new Date().toISOString() } as DashboardPage
      return Promise.resolve({ ok: true, json: async () => serverPage })
    }

    // Everything else (equipment profiles, signalk paths, help, auth
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

function widgetsFromLastPatch(): DashboardLayoutItem[] {
  const last = patchCalls.at(-1)
  return (last?.patch.widgets as DashboardLayoutItem[] | undefined) ?? []
}

// The stream is up and has said nothing yet: every reading is unknown.
installFakeTelemetryStream({ seeds: { 'vessel-state': {} } })
// The first GET /api/anchor-watch has answered: no watch is set.
beforeEach(() => { landAnchorWatch() })


const existingGauge: DashboardLayoutItem = {
  id: 'gauge:existing1', x: 0, y: 0, w: 3, h: 6,
  gauge: { path: 'navigation.speedOverGround', label: 'SOG', display: 'numeric', quantity: 'raw', unit: 'raw' },
}

async function openFromAddTile(name: RegExp): Promise<void> {
  fireEvent.click(screen.getByRole('button', { name: /add tile/i }))
  fireEvent.click(await screen.findByRole('menuitem', { name }))
}

async function openEmbedDialog(): Promise<void> {
  render(<App />)
  await activePageReady()
  enterEditMode()
  await openFromAddTile(/^embed/i)
  await screen.findByLabelText('URL')
}

describe('the Embed dialog through App', () => {
  it('saves one new tile below the page and closes the dialog', async () => {
    seedPage([existingGauge])
    await openEmbedDialog()
    fireEvent.change(screen.getByLabelText('URL'), { target: { value: 'https://example.com/d' } })
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))

    await waitFor(() => expect(patchCalls).toHaveLength(1))
    const widgets = widgetsFromLastPatch()
    expect(widgets).toHaveLength(2)
    expect(widgets[1]).toMatchObject({ x: 0, y: 6, embed: { url: 'https://example.com/d' } })
    await waitFor(() => expect(screen.queryByLabelText('URL')).toBeNull())
    expect(patchCalls).toHaveLength(1)
  })

  it('writes nothing on Cancel and closes the dialog', async () => {
    await openEmbedDialog()
    fireEvent.click(screen.getByRole('button', { name: 'Cancel' }))
    await waitFor(() => expect(screen.queryByLabelText('URL')).toBeNull())
    expect(patchCalls).toHaveLength(0)
  })

  it('stays open with its text when the write fails, and saves on a retry', async () => {
    await openEmbedDialog()
    fireEvent.change(screen.getByLabelText('URL'), { target: { value: 'https://example.com/d' } })
    failPatches = true
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))
    await waitFor(() => expect(patchCalls).toHaveLength(1))
    // Give the answer time to land: the dialog must still be there.
    await new Promise((r) => setTimeout(r, 50))
    expect(screen.getByLabelText('URL')).toHaveValue('https://example.com/d')

    failPatches = false
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))
    await waitFor(() => expect(patchCalls).toHaveLength(2))
    await waitFor(() => expect(screen.queryByLabelText('URL')).toBeNull())
    expect(widgetsFromLastPatch()).toHaveLength(1)
  })

  it('does not open for a tile of another kind', async () => {
    seedPage([existingGauge])
    render(<App />)
    await activePageReady()
    enterEditMode()
    fireEvent.click(await screen.findByRole('button', { name: /^configure sog/i }))
    expect(screen.queryByLabelText('URL')).toBeNull()
  })
})

describe('the Nearby map dialog through App', () => {
  it('saves one new tile with its own config and closes the dialog', async () => {
    render(<App />)
    await activePageReady()
    enterEditMode()
    await openFromAddTile(/^nearby map/i)
    fireEvent.click(await screen.findByRole('button', { name: 'Save' }))

    await waitFor(() => expect(patchCalls).toHaveLength(1))
    const [tile] = widgetsFromLastPatch()
    expect(tile.id.startsWith('poi-map:')).toBe(true)
    expect(tile).toMatchObject({ x: 0, y: 0, w: 12, h: 7, poiMap: { rangeNm: 5, layout: 'split' } })
    await waitFor(() => expect(screen.queryByRole('button', { name: 'Save' })).toBeNull())
  })

  it('writes nothing on Cancel', async () => {
    render(<App />)
    await activePageReady()
    enterEditMode()
    await openFromAddTile(/^nearby map/i)
    fireEvent.click(await screen.findByRole('button', { name: 'Cancel' }))
    await waitFor(() => expect(screen.queryByRole('button', { name: 'Cancel' })).toBeNull())
    expect(patchCalls).toHaveLength(0)
  })
})
