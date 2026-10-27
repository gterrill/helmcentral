/**
 * The Live* wrappers (ADR 0215), each in the file of the tile it feeds. The
 * anchor ones apply the live fix to the watch record: a memoized map below
 * them redraws on a new watch object, so a fix that changes nothing the watch
 * says must hand down the same object. The rest are read through what the
 * wrapper commits, with the clock held still so nothing else commits.
 */
import { act, render, screen } from '@testing-library/react'
import { Profiler, Suspense, type ComponentProps, type ReactNode } from 'react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { installFakeTelemetryStream } from './helpers/fake-telemetry-stream'

import type { AnchorWatchResult } from '@/hooks/use-anchor-watch'
import { setQueryData } from '@/lib/api-query'

// Every request fails, so what the wrappers read is only what a test lands.
const fetchMock = vi.fn(async () => ({ ok: false, status: 500, json: async () => ({}) }))
beforeEach(() => {
  fetchMock.mockClear()
  vi.stubGlobal('fetch', fetchMock)
})
afterEach(() => {
  vi.unstubAllGlobals()
})

function requestedPaths(): string[] {
  return fetchMock.mock.calls.map((call) => String((call as unknown[])[0]))
}

const stream = installFakeTelemetryStream()

// What each anchor wrapper hands its map: the watch at the live fix, one per render.
const atFix: AnchorWatchResult[] = []
vi.mock('@/hooks/use-anchor-map-inputs', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/hooks/use-anchor-map-inputs')>()
  return {
    ...actual,
    useWatchAtFix: ((...args: Parameters<typeof actual.useWatchAtFix>) => {
      const watch = actual.useWatchAtFix(...args)
      atFix.push(watch)
      return watch
    }) as typeof actual.useWatchAtFix,
  }
})
// The maps and the page are lazy chunks that pull in maplibre; the wrappers hand them their props before they load.
vi.mock('@/components/anchor-watch-map', () => ({ AnchorWatchMap: () => null }))
vi.mock('@/components/anchor-watch-drawer', () => ({ AnchorWatchDrawer: () => null }))

// The Nearby map's own chunk pulls in maplibre; the wrapper is what these tests read.
vi.mock('@/components/poi-map-tile-impl', () => ({ default: () => null }))

/**
 * Holds the clock still (the vessel clock ticks every second) and lets mount
 * effects settle, so the only commits counted afterwards are ones a test causes.
 */
function holdClock() {
  beforeEach(() => { vi.useFakeTimers() })
  afterEach(() => { vi.useRealTimers() })
}

async function settle() {
  await act(async () => { await vi.advanceTimersByTimeAsync(0) })
  commits.n = 0
}

/** Counts the commits under it: a wrapper that re-renders, or a view it feeds, adds one. */
const commits = { n: 0 }
function Counted({ children }: { children: ReactNode }) {
  return <Profiler id="counted" onRender={() => { commits.n += 1 }}>{children}</Profiler>
}

vi.mock('@/hooks/use-place-name', () => ({ usePlaceName: () => null }))

const forecastPageProps: Array<{ boat: unknown; boatLongitude: number | null }> = []
vi.mock('@/components/forecast-page', () => ({
  ForecastPage: (props: { boat: unknown; boatLongitude: number | null }) => {
    forecastPageProps.push(props)
    return null
  },
}))

import { LiveAnchorWatchTile } from '@/components/anchor-watch-tile'
import { LiveClockTile } from '@/components/clock-tile'
import { LiveDisplayShell } from '@/components/display-shell'
import { LiveAnchorWatchDrawer, LiveForecastPage } from '@/components/live-drawers'
import { LiveGeneratorTile } from '@/components/generator-tile'
import { LivePoiMapTile } from '@/components/poi-map-tile'

function emitFix(latitude: number, longitude: number) {
  stream.emit('vessel-state', { latitude, longitude, gnss_critical_alert: false })
}

/** A GET /api/anchor-watch answer, landed as a poll would land it. */
function landWatch(overrides: Record<string, unknown> = {}) {
  act(() => {
    setQueryData(['/api/anchor-watch'], { active: false, ...overrides })
  })
}

const tileProps = {
  isImperial: false,
  isDarkTheme: false,
  showImageryLayer: false,
  onImageryToggle: vi.fn(),
  showRadarEcho: false,
  onRadarEchoToggle: vi.fn(),
  onFullscreen: vi.fn(),
  interactive: true,
}

// The page's own props are config the shell passes through untouched; the wrapper reads none of them.
const drawerProps = {} as ComponentProps<typeof LiveAnchorWatchDrawer>
const wrappers = {
  'LiveAnchorWatchTile': () => <LiveAnchorWatchTile {...tileProps} />,
  'LiveAnchorWatchDrawer': () => <LiveAnchorWatchDrawer {...drawerProps} />,
}

describe.each(Object.entries(wrappers))('%s hands its map the watch at the live fix', (_name, element) => {
  beforeEach(() => { atFix.length = 0 })

  /** The page is a lazy chunk and the wrapper commits with it; let the mocked chunk resolve. */
  async function mount() {
    render(<Suspense fallback={null}>{element()}</Suspense>)
    await act(async () => {})
  }

  it('keeps the watch object while the fix moves and no watch is set', async () => {
    landWatch()
    await mount()
    emitFix(-20.2747, 149.0737)
    const first = atFix[atFix.length - 1]
    const rendered = atFix.length

    emitFix(-20.2748, 149.0738)

    // The wrapper did render again for the new fix, so the identity below is a result, not an absence.
    expect(atFix.length).toBeGreaterThan(rendered)
    expect(atFix[atFix.length - 1]).toBe(first)
  })

  it('hands down a new watch once the distance from the anchor changes', async () => {
    landWatch({ active: true, lat: -20.2747, lon: 149.0737, radius_meters: 30 })
    await mount()
    emitFix(-20.2747, 149.0737)
    const first = atFix[atFix.length - 1]

    emitFix(-20.2749, 149.0737)

    const last = atFix[atFix.length - 1]
    expect(last).not.toBe(first)
    expect(last.distanceMeters).toBeGreaterThan(first.distanceMeters ?? 0)
    expect(last.anchorState).toBe('set')
  })
})

describe('trail polling (ADR 0215 phase 4)', () => {
  it('the Nearby map polls the boat\'s trail while it is on screen, and stops when it leaves', () => {
    vi.useFakeTimers()
    try {
      const { unmount } = render(
        <LivePoiMapTile
          config={{ title: 'Nearby', categories: [], radiusM: 1000, showTrail: true } as never}
          editing={false}
          isDarkTheme={false}
          distanceUnits="metric"
          interactive
        />,
      )
      expect(requestedPaths().filter((path) => path.includes('/api/tracks'))).toHaveLength(1)

      unmount()
      fetchMock.mockClear()
      act(() => { vi.advanceTimersByTime(20_000) })

      expect(requestedPaths().filter((path) => path.includes('/api/tracks'))).toHaveLength(0)
    } finally {
      vi.useRealTimers()
    }
  })

  it('the anchor watch tile polls the swing trail and the AIS trails while it is on screen', () => {
    landWatch()
    render(<LiveAnchorWatchTile {...tileProps} />)

    const paths = requestedPaths()
    expect(paths.some((path) => path.includes('/api/anchor-watch/trails/self'))).toBe(true)
    expect(paths.some((path) => path.includes('/api/tracks'))).toBe(true)
  })
})

describe('LiveClockTile', () => {
  holdClock()

  /** A GET /api/routes/active answer, landed as its poll would land it. */
  function landRoute(answer: Record<string, unknown>) {
    act(() => {
      setQueryData(['/api/routes/active'], answer)
    })
  }

  it('does not redraw on a moving fix while there is no trip to time', async () => {
    render(<Counted><LiveClockTile /></Counted>)
    await settle()
    landRoute({ active: false })
    emitFix(-20.2747, 149.0737)
    const renders = commits.n

    emitFix(-20.2748, 149.0738)
    emitFix(-20.2749, 149.0739)

    expect(commits.n).toBe(renders)
  })

  it('follows the fix while a go-to destination is set', async () => {
    render(<Counted><LiveClockTile /></Counted>)
    await settle()
    // After the mount polls have failed, so the answer landed is the one the tile reads.
    landRoute({ active: false, destination: { name: 'Airlie', lat: -20.27, lon: 148.72 } })
    emitFix(-20.2747, 149.0737)
    const renders = commits.n

    emitFix(-20.2748, 149.0738)

    expect(commits.n).toBeGreaterThan(renders)
  })
})

describe('LiveDisplayShell', () => {
  holdClock()

  // The wall's pill shows how many alarms and how bad, not their readings.
  it('does not redraw the frame when only an alarm\'s value moves', async () => {
    const alarm = (value: number) => ({
      rule_id: 'baro', label: 'Barometer falling', path: 'helmcentral.environment.pressureRate', op: 'below', unit: 'Pa/s',
      value, state: 'warn', phase: 'active', message: 'Barometer falling', silenced: false, can_silence: false, can_acknowledge: true,
    })
    const display = {
      id: 'wall', name: 'Wall', slug: 'wall', width: 0, height: 0, scale: 0, rotate: 0 as const, pixel_shift: false, wake_lock: false,
      created_at: '', updated_at: '',
    }
    render(<Counted><LiveDisplayShell display={display}>{null}</LiveDisplayShell></Counted>)
    await settle()
    stream.emit('alarms', { alarms: [alarm(-0.03256)], worst: 'warn' })
    const renders = commits.n

    stream.emit('alarms', { alarms: [alarm(-0.03251)], worst: 'warn' })

    expect(commits.n).toBe(renders)
  })
})

describe('LiveGeneratorTile', () => {
  holdClock()

  const electrical = (soc: number) => ({ battery_soc_percent: soc, battery_capacity_ah: 1000, charging_current_a: 100, generator_configured: true })

  const emit = (event: string, payload: unknown) => stream.emit(event, payload)

  it('shows no battery outside a timed run, and follows Battery at Finish during one', async () => {
    render(<Counted><LiveGeneratorTile readOnly={false} /></Counted>)
    await settle()
    emit('electrical-state', electrical(70))
    expect(screen.queryByText('Battery at Finish')).not.toBeInTheDocument()
    const idle = commits.n
    emit('electrical-state', electrical(71))
    expect(commits.n).toBe(idle)

    // An hour's timed run at +10 %/h: 71 % now lands at 81 % at the finish.
    emit('vessel-state', { generator_manual_start: true, generator_manual_start_timer: 3600, generator_state: 'running' })
    expect(screen.getByText('Battery at Finish').nextElementSibling).toHaveTextContent('81%')
    const running = commits.n

    // 71.2 % still rounds to 81 % at the finish: nothing the tile shows moved.
    emit('electrical-state', electrical(71.2))
    expect(commits.n).toBe(running)

    emit('electrical-state', electrical(72))
    expect(screen.getByText('Battery at Finish').nextElementSibling).toHaveTextContent('82%')
  })
})

describe('LiveForecastPage', () => {
  // The page compares the boat's clock with a planned stay's by longitude, so
  // a fix re-renders it. The boat's forecast did not change, so the page must
  // be handed the same forecast props.
  it('hands the page the same forecast props when only the longitude moves', async () => {
    forecastPageProps.length = 0
    render(<Suspense fallback={null}><LiveForecastPage canWrite unit="metric" /></Suspense>)
    await act(async () => { await new Promise((r) => setTimeout(r, 0)) })
    emitFix(-20.2747, 149.0737)
    const before = forecastPageProps.length
    const boat = forecastPageProps[before - 1].boat

    emitFix(-20.2747, 149.0738)

    expect(forecastPageProps.length).toBeGreaterThan(before)
    expect(forecastPageProps[forecastPageProps.length - 1].boatLongitude).toBe(149.0738)
    expect(forecastPageProps[forecastPageProps.length - 1].boat).toBe(boat)
  })
})
