/**
 * ADR 0215 phase 6: the polled queries leave the shell.
 *
 * Every poll here goes through the real query cache: the fetch stub answers
 * each endpoint with a fresh copy of a payload captured off the boat's server
 * (fixtures/live-poll-payloads.ts), and `invalidateQueries` stands in for the
 * poll timer, so a request leaves and lands exactly as a timed poll's does.
 *
 * DashboardBentoGrid is wrapped without memo, so its count is the shell's
 * render count. Tile is wrapped and records its title, so the count per title
 * is that tile's render count.
 */
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { installFakeTelemetryStream } from './helpers/fake-telemetry-stream'
import { render, act } from '@testing-library/react'
import { memo, type ComponentProps } from 'react'
import { App } from '../App'
import { invalidateQueries } from '@/lib/api-query'
import * as polls from './fixtures/live-poll-payloads'
import * as live from './fixtures/live-stream-payloads'

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

/** One poll of `key`, answered with whatever the server now says. */
async function poll(key: string) {
  await act(async () => { await invalidateQueries([key]) })
}

// ── the stream: the real stream and stores over a fake EventSource, so every
// store can be ticked ──────────────────────────────────────────────────────

const stream = installFakeTelemetryStream()
const emit = (event: string, payload: unknown) => stream.emit(event, payload)

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

const gridRenderSpy = vi.fn()
vi.mock('@/components/dashboard-bento-grid', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/components/dashboard-bento-grid')>()
  function GridRenderSpy(props: ComponentProps<typeof actual.DashboardBentoGrid>) {
    gridRenderSpy()
    return <actual.DashboardBentoGrid {...props} />
  }
  return { ...actual, DashboardBentoGrid: GridRenderSpy }
})

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

const TITLES = ['Depth & Tide', 'Today & Now', 'Clock', 'Current Conditions', 'Forecast Conditions', 'Route', 'Battery & Power', 'CZone', 'Gauge A']

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
  // Auth, then the page list, then every tile's first answer.
  for (let i = 0; i < 5; i += 1) await act(async () => { await new Promise((r) => setTimeout(r, 0)) })
  for (const title of TITLES) expect(rendersOf(title), title).toBeGreaterThan(0)
}

// Each poll the shell used to make, keyed by its endpoint.
const POLLED = [
  '/api/weather-forecast', '/api/weather-today', '/api/tide-today', '/api/forecast-warnings',
  '/api/wave-forecast', '/api/upper-air', '/api/routes/active', '/api/depth-trend',
  '/api/czone/switches', '/api/electrical/overnight', '/api/alarm-rules',
  '/api/dashboard-pages', '/api/displays', '/api/dashboard-ribbon',
]

describe('App polled queries (ADR 0215 phase 6)', () => {
  for (const key of POLLED) {
    it(`a poll of ${key} answering the same data re-renders neither the shell nor any tile`, async () => {
      await mountApp()
      const grid = gridRenderSpy.mock.calls.length
      const before = counts()

      await poll(key)
      await poll(key)

      expect(gridRenderSpy.mock.calls.length).toBe(grid)
      expect(moved(before)).toEqual([])
    })
  }

  const day0 = polls.WEATHER_FORECAST.days[0]
  const CHANGES: Array<{ key: string; answer: unknown; shows: string[] }> = [
    {
      key: '/api/weather-forecast',
      answer: { ...polls.WEATHER_FORECAST, days: [{ ...day0, high_temp_f: day0.high_temp_f + 2 }, ...polls.WEATHER_FORECAST.days.slice(1)] },
      shows: ['Today & Now', 'Current Conditions', 'Forecast Conditions'],
    },
    {
      key: '/api/weather-today',
      answer: { ...polls.WEATHER_TODAY, temperature_f: 82.4 },
      shows: ['Today & Now', 'Current Conditions'],
    },
    {
      key: '/api/tide-today',
      answer: { ...polls.TIDE_TODAY, current_tide_height_ft: 2.9, datetime: '2026-10-27T07:22:39Z' },
      shows: ['Depth & Tide'],
    },
    {
      key: '/api/wave-forecast',
      answer: { ...polls.WAVE_FORECAST, sea_temperature_f: 81.5 },
      shows: ['Today & Now', 'Forecast Conditions'],
    },
    {
      key: '/api/routes/active',
      // A go-to with no fix yet: the Route tile names it; the Clock has no
      // ETA to show without a fix, so nothing it draws changed.
      answer: { active: false, destination: { lat: -20.25, lon: 149.0, name: 'Butterfly Bay' } },
      shows: ['Route'],
    },
    {
      key: '/api/depth-trend',
      answer: { ...polls.DEPTH_TREND, points: [...polls.DEPTH_TREND.points.slice(1), { ...polls.DEPTH_TREND.points[0], depth_m: 15.1 }] },
      shows: ['Depth & Tide'],
    },
    {
      key: '/api/czone/switches',
      answer: { switches: polls.CZONE_SWITCHES.switches.map((s, i) => (i === 0 ? { ...s, state: s.state === 1 ? 0 : 1 } : s)) },
      shows: ['CZone'],
    },
    {
      key: '/api/electrical/overnight',
      answer: { ...polls.OVERNIGHT, night_rate_percent_per_hour: -1.6 },
      shows: ['Battery & Power'],
    },
    {
      key: '/api/upper-air',
      answer: { ...polls.UPPER_AIR, updated_at: '2026-10-27T13:12:50Z' },
      shows: [],
    },
  ]

  for (const change of CHANGES) {
    it(`a changed poll of ${change.key} re-renders ${change.shows.join(', ') || 'no tile'}, and not the shell`, async () => {
      await mountApp()
      const grid = gridRenderSpy.mock.calls.length
      const before = counts()

      answers[change.key] = change.answer
      await poll(change.key)

      expect(moved(before)).toEqual(change.shows)
      expect(gridRenderSpy.mock.calls.length).toBe(grid)
    })
  }

  it('a forecast warning arriving re-renders the banner, and not the shell or any tile', async () => {
    await mountApp()
    const grid = gridRenderSpy.mock.calls.length
    const banner = bannerRenderSpy.mock.calls.length
    const before = counts()

    answers['/api/forecast-warnings'] = {
      ...polls.FORECAST_WARNINGS,
      bulletins: [{ id: 'b1', title: 'Coastal Waters Warning', issued_at: '2026-10-27T06:00:00Z', details_url: '', category: 'surf', sections: [{ day: 'Monday', warning_type: 'Hazardous Surf Warning' }] }],
    }
    await poll('/api/forecast-warnings')

    expect(bannerRenderSpy.mock.calls.length).toBeGreaterThan(banner)
    expect(moved(before)).toEqual([])
    expect(gridRenderSpy.mock.calls.length).toBe(grid)
  })

  it('with every stream ticking and every poll answering the same data, the shell renders 0 times', async () => {
    await mountApp()
    const grid = gridRenderSpy.mock.calls.length

    for (const step of [0, 1]) {
      emit('gauge-values', { values: { 'a.path': 2 + step }, ages: { 'a.path': 1 } })
      emit('alarms', { alarms: [], worst: 'normal' })
      emit('vessel-state', { latitude: -20.2747 - step / 10000, longitude: 149.0737, depth: 16.2 + step / 10 })
      emit('autopilot', { ...live.AUTOPILOT, target: 5.742 + step / 10 })
      emit('electrical-state', step === 0 ? live.ELECTRICAL : live.ELECTRICAL_NEXT)
      emit('solar-state', { ...live.SOLAR, current_w: 1372.3 + step })
      emit('tanks-state', step === 0 ? live.TANKS : live.TANKS_AGES_ONLY)
      emit('nearby-vessels', step === 0 ? live.NEARBY : live.NEARBY_NEXT)
      emit('radar-targets', live.RADAR)
      // Every poll at once: the empty prefix matches every key.
      await act(async () => { await invalidateQueries([]) })
    }

    expect(gridRenderSpy.mock.calls.length - grid).toBe(0)
  })
})
