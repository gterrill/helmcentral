/**
 * The wrappers in live-surfaces.tsx that apply the live fix to the anchor
 * watch record (ADR 0215). A memoized map below them redraws on a new watch
 * object, so a fix that changes nothing the watch says must hand down the
 * same object.
 */
import { act, render } from '@testing-library/react'
import { Suspense } from 'react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

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

const listeners = new Map<string, (raw: string) => void>()
vi.mock('@/hooks/use-telemetry-stream', () => ({
  subscribeTelemetry: (event: string, cb: (raw: string) => void) => {
    listeners.set(event, cb)
    return () => {}
  },
}))

const tileWatches: AnchorWatchResult[] = []
vi.mock('@/components/anchor-watch-tile', () => ({
  AnchorWatchTile: ({ watch }: { watch: AnchorWatchResult }) => {
    tileWatches.push(watch)
    return null
  },
}))

const clockRenders = vi.fn()
vi.mock('@/components/clock-tile', () => ({
  ClockTile: () => {
    clockRenders()
    return null
  },
}))

const poiMapRenders = vi.fn()
vi.mock('@/components/poi-map-tile', () => ({
  PoiMapTile: () => {
    poiMapRenders()
    return null
  },
}))

const displayShellRenders = vi.fn()
vi.mock('@/components/display-shell', () => ({
  DisplayShell: () => {
    displayShellRenders()
    return null
  },
}))

const generatorProps: Array<{ batterySocPercent: number | null; batteryRatePercentPerHour: number | null }> = []
vi.mock('@/components/generator-tile', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/components/generator-tile')>()),
  GeneratorTile: (props: { batterySocPercent: number | null; batteryRatePercentPerHour: number | null }) => {
    generatorProps.push(props)
    return null
  },
}))

vi.mock('@/hooks/use-place-name', () => ({ usePlaceName: () => null }))

const forecastPageProps: Array<{ boat: unknown; boatLongitude: number | null }> = []
vi.mock('@/components/forecast-page', () => ({
  ForecastPage: (props: { boat: unknown; boatLongitude: number | null }) => {
    forecastPageProps.push(props)
    return null
  },
}))

import { LiveAnchorWatchTile, LiveClockTile, LiveDisplayShell, LiveForecastPage, LiveGeneratorTile, LivePoiMapTile } from '@/components/live-surfaces'

function emitFix(latitude: number, longitude: number) {
  act(() => {
    listeners.get('vessel-state')?.(JSON.stringify({ latitude, longitude, gnss_critical_alert: false }))
  })
}

/** A GET /api/anchor-watch answer, landed as a poll would land it. */
function landWatch(overrides: Record<string, unknown> = {}) {
  act(() => {
    setQueryData(['/api/anchor-watch'], { active: false, ...overrides })
  })
}

function renderTile() {
  return render(
    <LiveAnchorWatchTile
      isImperial={false}
      isDarkTheme={false}
      showImageryLayer={false}
      onImageryToggle={vi.fn()}
      showRadarEcho={false}
      onRadarEchoToggle={vi.fn()}
      onFullscreen={vi.fn()}
      interactive
    />,
  )
}

describe('LiveAnchorWatchTile', () => {
  it('keeps the watch object while the fix moves and no watch is set', () => {
    tileWatches.length = 0
    landWatch()
    renderTile()
    emitFix(-20.2747, 149.0737)
    const first = tileWatches[tileWatches.length - 1]

    emitFix(-20.2748, 149.0738)

    expect(tileWatches.length).toBeGreaterThan(1)
    expect(tileWatches[tileWatches.length - 1]).toBe(first)
  })

  it('hands down a new watch once the distance from the anchor changes', () => {
    tileWatches.length = 0
    landWatch({ active: true, lat: -20.2747, lon: 149.0737, radius_meters: 30 })
    renderTile()
    emitFix(-20.2747, 149.0737)
    const first = tileWatches[tileWatches.length - 1]

    emitFix(-20.2749, 149.0737)

    const last = tileWatches[tileWatches.length - 1]
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
    renderTile()

    const paths = requestedPaths()
    expect(paths.some((path) => path.includes('/api/anchor-watch/trails/self'))).toBe(true)
    expect(paths.some((path) => path.includes('/api/tracks'))).toBe(true)
  })
})

describe('LiveClockTile', () => {
  /** A GET /api/routes/active answer, landed as its poll would land it. */
  function landRoute(answer: Record<string, unknown>) {
    act(() => {
      setQueryData(['/api/routes/active'], answer)
    })
  }

  it('does not redraw on a moving fix while there is no trip to time', () => {
    landRoute({ active: false })
    render(<LiveClockTile />)
    emitFix(-20.2747, 149.0737)
    const renders = clockRenders.mock.calls.length

    emitFix(-20.2748, 149.0738)
    emitFix(-20.2749, 149.0739)

    expect(clockRenders.mock.calls.length).toBe(renders)
  })

  it('follows the fix while a go-to destination is set', () => {
    landRoute({ active: false, destination: { name: 'Airlie', lat: -20.27, lon: 148.72 } })
    render(<LiveClockTile />)
    emitFix(-20.2747, 149.0737)
    const renders = clockRenders.mock.calls.length

    emitFix(-20.2748, 149.0738)

    expect(clockRenders.mock.calls.length).toBeGreaterThan(renders)
  })
})

describe('LiveDisplayShell', () => {
  // The wall's pill shows how many alarms and how bad, not their readings.
  it('does not redraw the frame when only an alarm\'s value moves', () => {
    const alarm = (value: number) => ({
      rule_id: 'baro', label: 'Barometer falling', path: 'helmcentral.environment.pressureRate', op: 'below', unit: 'Pa/s',
      value, state: 'warn', phase: 'active', message: 'Barometer falling', silenced: false, can_silence: false, can_acknowledge: true,
    })
    render(<LiveDisplayShell display={{} as never}>{null}</LiveDisplayShell>)
    act(() => { listeners.get('alarms')?.(JSON.stringify({ alarms: [alarm(-0.03256)], worst: 'warn' })) })
    const renders = displayShellRenders.mock.calls.length

    act(() => { listeners.get('alarms')?.(JSON.stringify({ alarms: [alarm(-0.03251)], worst: 'warn' })) })

    expect(displayShellRenders.mock.calls.length).toBe(renders)
  })
})

describe('LiveGeneratorTile', () => {
  const electrical = (soc: number) => ({ battery_soc_percent: soc, battery_capacity_ah: 1000, charging_current_a: 100, generator_configured: true })

  function emit(event: string, payload: unknown) {
    act(() => { listeners.get(event)?.(JSON.stringify(payload)) })
  }

  it('hands down no battery outside a timed run, and follows Battery at Finish during one', () => {
    generatorProps.length = 0
    render(<LiveGeneratorTile readOnly={false} />)
    emit('electrical-state', electrical(70))
    expect(generatorProps[generatorProps.length - 1].batterySocPercent).toBeNull()
    const idle = generatorProps.length
    emit('electrical-state', electrical(71))
    expect(generatorProps.length).toBe(idle)

    // An hour's timed run at +10 %/h: 71 % now lands at 81 % at the finish.
    emit('vessel-state', { generator_manual_start: true, generator_manual_start_timer: 3600, generator_state: 'running' })
    expect(generatorProps[generatorProps.length - 1].batterySocPercent).toBe(71)
    const running = generatorProps.length

    // 71.2 % still rounds to 81 % at the finish: nothing the tile shows moved.
    emit('electrical-state', electrical(71.2))
    expect(generatorProps.length).toBe(running)

    emit('electrical-state', electrical(72))
    expect(generatorProps[generatorProps.length - 1].batterySocPercent).toBe(72)
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
