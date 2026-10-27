import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { installFakeTelemetryStream } from './helpers/fake-telemetry-stream'
import { render, act, fireEvent, screen } from '@testing-library/react'
import { memo, type ComponentProps, type ReactNode } from 'react'
import { App } from '../App'
import { setQueryData } from '@/lib/api-query'
import * as polls from './fixtures/live-poll-payloads'

// ── the server ───────────────────────────────────────────────────────────────

const PAGE_WIDGETS = [
  { id: 'depth-tide', x: 0, y: 0, w: 4, h: 4 },
  { id: 'today-now', x: 4, y: 0, w: 4, h: 4 },
  { id: 'clock', x: 8, y: 0, w: 4, h: 4 },
  { id: 'current-conditions', x: 0, y: 4, w: 4, h: 6 },
  { id: 'forecast-conditions', x: 4, y: 4, w: 4, h: 6 },
  { id: 'route', x: 8, y: 4, w: 4, h: 4 },
  { id: 'battery-power', x: 0, y: 10, w: 4, h: 6 },
  { id: 'czone-switches', x: 4, y: 10, w: 4, h: 6 },
  { id: 'gauge:a', x: 8, y: 10, w: 4, h: 4, gauge: { path: 'a.path', label: 'Gauge A', display: 'numeric', quantity: 'raw', unit: 'raw' } },
  { id: 'gauge:b', x: 0, y: 16, w: 4, h: 4, gauge: { path: 'b.path', label: 'Gauge B', display: 'numeric', quantity: 'raw', unit: 'raw' } },
]

const PAGES = {
  pages: [{ id: 'p1', name: 'Page 1', widgets: PAGE_WIDGETS, created_at: '2026-01-01T00:00:00Z', updated_at: '2026-01-01T00:00:00Z' }],
}

const BASE_ANSWERS: Record<string, unknown> = {
  '/api/auth/mode': { mode: 'none' },
  '/api/auth/me': { authenticated: false },
  '/api/weather-forecast': polls.WEATHER_FORECAST,
  '/api/weather-today': polls.WEATHER_TODAY,
  '/api/tide-today': polls.TIDE_TODAY,
  '/api/forecast-warnings': polls.FORECAST_WARNINGS,
  '/api/wave-forecast': polls.WAVE_FORECAST,
  '/api/upper-air': polls.UPPER_AIR,
  '/api/routes/active': polls.ROUTE_ACTIVE,
  '/api/depth-trend': polls.DEPTH_TREND,
  '/api/czone/switches': polls.CZONE_SWITCHES,
  '/api/electrical/overnight': polls.OVERNIGHT,
  '/api/alarm-rules': polls.ALARM_RULES,
  '/api/dashboard-pages': PAGES,
  '/api/displays': polls.DISPLAYS,
  '/api/dashboard-ribbon': polls.RIBBON,
  '/api/settings': polls.SETTINGS,
  '/api/anchor-watch': { active: false },
  '/api/inventory/equipment': { items: [] },
  '/api/equipment-profiles': { profiles: [], problems: [] },
  '/api/vessel/discovery': { connected: false, groups: [] },
  '/api/setup/status': { steps: [] },
}
let answers: Record<string, unknown> = BASE_ANSWERS

beforeEach(() => {
  answers = { ...BASE_ANSWERS }
  vi.stubGlobal('fetch', vi.fn(async (input: RequestInfo | URL) => {
    const url = new URL(typeof input === 'string' ? input : input.toString(), 'http://stub.invalid')
    if (url.pathname in answers) {
      // A fresh copy on every request: equal by value, never the same object.
      const body = structuredClone(answers[url.pathname])
      return { ok: true, status: 200, json: async () => body }
    }
    return { ok: false, status: 404, json: async () => ({}) }
  }))
})

afterEach(() => {
  vi.unstubAllGlobals()
})

/** The page list as a write's answer lands it: the server's whole page, every widget a new object. */
function landPages(page: Record<string, unknown>) {
  act(() => {
    setQueryData(['/api/dashboard-pages'], { pages: [structuredClone(page)] })
  })
}

// ── the stream: the real stream and stores over a fake EventSource, so every
// store can be ticked ──────────────────────────────────────────────────────

installFakeTelemetryStream()


// ── render-count spies ───────────────────────────────────────────────────────

const tileTitleSpy = vi.fn()
vi.mock('@/components/ui/tile', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/components/ui/tile')>()
  function TileTitleSpy(props: ComponentProps<typeof actual.Tile>) {
    tileTitleSpy(props.title)
    return <actual.Tile {...props} />
  }
  return { ...actual, Tile: TileTitleSpy }
})

// The shell's renders: an unmemoized wrapper around the grid. The grid's own
// renders: every call it makes to renderWidget, which it makes for each widget
// each time it renders. Its memo, if it has one, is kept.
const gridRenderSpy = vi.fn()
const renderWidgetSpy = vi.fn()
vi.mock('@/components/dashboard-bento-grid', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/components/dashboard-bento-grid')>()
  type Props = ComponentProps<typeof actual.DashboardBentoGrid>
  const real = actual.DashboardBentoGrid as unknown as { type?: (props: Props) => ReactNode; compare?: (a: Props, b: Props) => boolean } & ((props: Props) => ReactNode)
  const body = real.type ?? real
  function CountedGrid(props: Props) {
    return body({ ...props, renderWidget: (w) => { renderWidgetSpy(w.id); return props.renderWidget(w) } })
  }
  const Grid = real.type ? memo(CountedGrid, real.compare ?? undefined) : CountedGrid
  function GridRenderSpy(props: Props) {
    gridRenderSpy()
    return <Grid {...props} />
  }
  return { ...actual, DashboardBentoGrid: GridRenderSpy }
})

vi.mock('@/components/mate-sheet', () => ({ MateSheet: () => null }))

// The banner draws no Tile: wrapped in memo() with the real props, so the spy
// runs only when the real banner would.
const bannerRenderSpy = vi.fn()
vi.mock('@/components/alarm-banner', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/components/alarm-banner')>()
  const Spy = memo(function AlarmBannerRenderSpy(props: ComponentProps<typeof actual.AlarmBanner>) {
    bannerRenderSpy()
    return <actual.AlarmBanner {...props} />
  })
  return { ...actual, AlarmBanner: Spy }
})

vi.mock('@/hooks/use-app-config', () => ({
  useAppConfig: () => ({
    ui: { vesselStateRefreshSeconds: 10, distanceUnits: 'metric', autoCloseAnchorWatchOnEngine: true },
    anchor: {
      bowRollerHeightM: 0, chainSizeMm: 10, chainOnboardM: 50,
      hullType: 'power_cat', scopeMethod: 'ratio', windageAreaM2: 10,
      gpsFromBowM: 0, loaM: 0,
    },
    boatModel: null,
    loaded: true,
  }),
  publishAppConfigSettings: vi.fn(),
}))

vi.mock('@/hooks/use-dark-mode', () => ({
  useDarkMode: () => [false, vi.fn()],
}))

const TITLES = ['Depth & Tide', 'Today & Now', 'Clock', 'Current Conditions', 'Forecast Conditions', 'Route', 'Battery & Power', 'CZone', 'Gauge A', 'Gauge B']

function rendersOf(title: string): number {
  return tileTitleSpy.mock.calls.filter(([t]) => t === title).length
}

function counts(): Record<string, number> {
  return Object.fromEntries(TITLES.map((title) => [title, rendersOf(title)]))
}

/** The tiles whose render count moved since `before`. */
function moved(before: Record<string, number>): string[] {
  const now = counts()
  return TITLES.filter((title) => now[title] !== before[title])
}

async function mountApp() {
  render(<App />)
  for (let i = 0; i < 5; i += 1) await act(async () => { await new Promise((r) => setTimeout(r, 0)) })
  for (const title of TITLES) expect(rendersOf(title), title).toBeGreaterThan(0)
}

const PAGE = PAGES.pages[0]

describe('App WidgetHost and grid (ADR 0215 phase 7)', () => {
  it('opening the Mate sheet re-renders the shell, but no tile, and the grid asks for none', async () => {
    await mountApp()
    const shell = gridRenderSpy.mock.calls.length
    const asked = renderWidgetSpy.mock.calls.length
    const before = counts()

    fireEvent.click(screen.getByRole('button', { name: 'Ask Mate' }))
    await act(async () => { await new Promise((r) => setTimeout(r, 0)) })

    expect(gridRenderSpy.mock.calls.length).toBeGreaterThan(shell)
    expect(renderWidgetSpy.mock.calls.length).toBe(asked)
    expect(moved(before)).toEqual([])
  })

  it('renaming the page re-renders no tile, and the grid asks for none', async () => {
    await mountApp()
    const asked = renderWidgetSpy.mock.calls.length
    const before = counts()

    landPages({ ...PAGE, name: 'Anchored', updated_at: '2026-10-27T08:00:00Z' })

    expect(renderWidgetSpy.mock.calls.length).toBe(asked)
    expect(moved(before)).toEqual([])
  })

  it('changing one widget\'s config re-renders only that tile', async () => {
    await mountApp()
    const before = counts()

    landPages({
      ...PAGE,
      widgets: PAGE.widgets.map((w) => (w.id === 'gauge:b' && w.gauge ? { ...w, gauge: { ...w.gauge, display: 'bar' } } : w)),
    })

    expect(moved(before)).toEqual(['Gauge B'])
  })

  it('toggling layout editing re-renders the tiles', async () => {
    await mountApp()
    const before = counts()

    fireEvent.click(screen.getByRole('button', { name: 'Enter edit mode' }))

    expect(rendersOf('Gauge A')).toBeGreaterThan(before['Gauge A'])
    expect(rendersOf('Gauge B')).toBeGreaterThan(before['Gauge B'])
  })
})
