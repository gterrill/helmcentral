/**
 * The wrappers in live-surfaces.tsx that apply the live fix to the anchor
 * watch record (ADR 0215). A memoized map below them redraws on a new watch
 * object, so a fix that changes nothing the watch says must hand down the
 * same object.
 */
import { act, render } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import type { AnchorWatchRecord, AnchorWatchResult } from '@/hooks/use-anchor-watch'

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

vi.mock('@/hooks/use-place-name', () => ({ usePlaceName: () => null }))

import { LiveAnchorWatchTile, LiveClockTile } from '@/components/live-surfaces'

function emitFix(latitude: number, longitude: number) {
  act(() => {
    listeners.get('vessel-state')?.(JSON.stringify({ latitude, longitude, gnss_critical_alert: false }))
  })
}

function record(overrides: Partial<AnchorWatchRecord> = {}): AnchorWatchRecord {
  return {
    active: false, anchorLat: null, anchorLon: null, radiusMeters: 30, rodeDeployedM: 0,
    seaState: 'calm', seabedType: 'sand', setAt: null, loaded: true, error: null,
    bowOffsetM: 0, bowOffsetApplied: false, bowOffsetReason: '', headingAtSetDeg: -1, placeName: '',
    planningDepthM: null, planningTideHeightFt: null, lastAutoRaise: null,
    setAnchorHere: vi.fn(), updateRadius: vi.fn(), adjustAnchor: vi.fn(),
    updateRodeAndConditions: vi.fn(), updatePlanningDepth: vi.fn(), clearAnchor: vi.fn(),
    ...overrides,
  }
}

function renderTile(watch: AnchorWatchRecord) {
  render(
    <LiveAnchorWatchTile
      watch={watch}
      isImperial={false}
      vesselTrail={vi.fn()}
      aisVessels={[]}
      aisTrails={vi.fn()}
      radarTargets={[]}
      radars={[]}
      radarSource="disabled"
      isDarkTheme={false}
      showImageryLayer={false}
      onImageryToggle={vi.fn()}
      showRadarEcho={false}
      onRadarEchoToggle={vi.fn()}
      onFullscreen={vi.fn()}
      placemarks={[]}
      onPlacemarkCreate={vi.fn()}
      onPlacemarkRemove={vi.fn()}
      tide={null}
      interactive
    />,
  )
}

describe('LiveAnchorWatchTile', () => {
  it('keeps the watch object while the fix moves and no watch is set', () => {
    tileWatches.length = 0
    renderTile(record())
    emitFix(-20.2747, 149.0737)
    const first = tileWatches[tileWatches.length - 1]

    emitFix(-20.2748, 149.0738)

    expect(tileWatches.length).toBeGreaterThan(1)
    expect(tileWatches[tileWatches.length - 1]).toBe(first)
  })

  it('hands down a new watch once the distance from the anchor changes', () => {
    tileWatches.length = 0
    renderTile(record({ active: true, anchorLat: -20.2747, anchorLon: 149.0737 }))
    emitFix(-20.2747, 149.0737)
    const first = tileWatches[tileWatches.length - 1]

    emitFix(-20.2749, 149.0737)

    const last = tileWatches[tileWatches.length - 1]
    expect(last).not.toBe(first)
    expect(last.distanceMeters).toBeGreaterThan(first.distanceMeters ?? 0)
    expect(last.anchorState).toBe('set')
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
