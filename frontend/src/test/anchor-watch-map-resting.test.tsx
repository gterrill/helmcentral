import { act, render } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { AnchorWatchMap } from '@/components/anchor-watch-map'
import { resetTrailClock, setServerTimeMs } from '@/lib/trail-clock'
import { WallPageActiveContext } from '@/lib/wall-page-active'

vi.mock('maplibre-gl', () => ({
  default: {},
}))

// The mock Map renders whenever AnchorWatchMap does, so its count is the map's.
let mapRenders = 0

vi.mock('react-map-gl/maplibre', async () => {
  const React = await import('react')
  return {
    Map: React.forwardRef(
      ({ children }: { children?: React.ReactNode }, ref: React.Ref<unknown>) => {
        mapRenders += 1
        React.useImperativeHandle(ref, () => ({
          getCanvas: () => ({ style: { cursor: 'grab' } }),
          getZoom: () => 14,
          easeTo: vi.fn(),
        }))
        return <div data-testid="map-root">{children}</div>
      },
    ),
    Marker: ({ children }: { children?: React.ReactNode }) => <div>{children}</div>,
    Source: ({ children }: { children?: React.ReactNode }) => <div>{children}</div>,
    Layer: () => <div />,
  }
})

afterEach(() => {
  vi.useRealTimers()
})

function mapOnPage(active: boolean) {
  return (
    <WallPageActiveContext.Provider value={active}>
      <AnchorWatchMap
        vesselLat={-25.2939}
        vesselLon={152.9103}
        vesselHeadingDeg={265}
        anchorLat={-25.2939}
        anchorLon={152.9103}
        radiusMeters={34}
        depthMeters={3.2}
        currentDriftKts={0.1}
        currentSetDeg={120}
        distanceMeters={10}
        bearingDeg={80}
        isImperial={false}
        vesselTrail={[]}
        aisVessels={[]}
        aisTrails={new Map()}
        isDarkTheme={false}
      />
    </WallPageActiveContext.Provider>
  )
}

describe('AnchorWatchMap on a wall page', () => {
  it('mounts on an active page with one render, not a second for the clocks', () => {
    mapRenders = 0
    render(mapOnPage(true))
    expect(mapRenders).toBe(1)
  })

  it('renders once more when its page wakes, so the clocks catch up', () => {
    mapRenders = 0
    const { rerender } = render(mapOnPage(false))
    expect(mapRenders).toBe(1)
    rerender(mapOnPage(true))
    // One render for the context change, one for the clocks' catch-up.
    expect(mapRenders).toBe(3)
  })

  it('runs neither clock while its page rests', () => {
    vi.useFakeTimers()
    resetTrailClock()
    mapRenders = 0
    render(mapOnPage(false))
    expect(mapRenders).toBe(1)
    // Past the five minute AIS band mark, and a trail answer that moves the offset.
    act(() => { vi.advanceTimersByTime(11 * 60 * 1000) })
    act(() => { setServerTimeMs(Date.now() + 60_000) })
    expect(mapRenders).toBe(1)
    resetTrailClock()
  })
})
