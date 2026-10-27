/**
 * The wrappers in live-surfaces.tsx that apply the live fix to the anchor
 * watch record (ADR 0215). A memoized map below them redraws on a new watch
 * object, so a fix that changes nothing the watch says must hand down the
 * same object.
 */
import { act, render } from '@testing-library/react'
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

vi.mock('@/hooks/use-place-name', () => ({ usePlaceName: () => null }))

import { LiveAnchorWatchTile, LiveClockTile, LivePoiMapTile } from '@/components/live-surfaces'

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
      tide={null}
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
          activeRoute={null}
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
  const clockProps = { sunriseTime: null, sunsetTime: null, moonPhase: null }

  it('does not redraw on a moving fix while there is no trip to time', () => {
    render(<LiveClockTile {...clockProps} routeStatus={{ state: 'inactive', destination: null }} />)
    emitFix(-20.2747, 149.0737)
    const renders = clockRenders.mock.calls.length

    emitFix(-20.2748, 149.0738)
    emitFix(-20.2749, 149.0739)

    expect(clockRenders.mock.calls.length).toBe(renders)
  })

  it('follows the fix while a go-to destination is set', () => {
    const destination = { name: 'Airlie', lat: -20.27, lon: 148.72 }
    render(<LiveClockTile {...clockProps} routeStatus={{ state: 'inactive', destination }} />)
    emitFix(-20.2747, 149.0737)
    const renders = clockRenders.mock.calls.length

    emitFix(-20.2748, 149.0738)

    expect(clockRenders.mock.calls.length).toBeGreaterThan(renders)
  })
})
