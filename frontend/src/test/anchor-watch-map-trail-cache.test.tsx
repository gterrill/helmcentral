import { render } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { AnchorWatchMap } from '@/components/anchor-watch-map'
import type { AisTrailPoint } from '@/hooks/use-ais-anchor-tracks'
import type { NearbyVessel } from '@/hooks/use-nearby-vessels'
import { groupLinesByAge } from '@/lib/trail-age-bands'

vi.mock('maplibre-gl', () => ({
  default: {},
}))

vi.mock('@/lib/trail-age-bands', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/lib/trail-age-bands')>()
  return { ...actual, groupLinesByAge: vi.fn(actual.groupLinesByAge) }
})

vi.mock('react-map-gl/maplibre', async () => {
  const React = await import('react')
  return {
    Map: React.forwardRef(
      ({ children }: { children?: React.ReactNode }, ref: React.Ref<unknown>) => {
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
    Layer: () => null,
  }
})

const MIN = 60 * 1000

function mapElement(aisTrails: Map<string, AisTrailPoint[]>, vessels: NearbyVessel[]) {
  return (
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
      aisVessels={vessels}
      aisTrails={aisTrails}
      radarTargets={[]}
      isDarkTheme={false}
    />
  )
}

const vessel = (mmsi: string): NearbyVessel => ({
  id: `urn:mrn:imo:mmsi:${mmsi}`, name: `BOAT ${mmsi}`, mmsi, lat: -25.2939, lon: 152.9103, range_m: 61, age_seconds: 5,
})

const points = (lat: number): AisTrailPoint[] => {
  const nowMs = Date.now()
  return [
    { lat, lon: 152.91, timestampMs: nowMs - 40 * MIN, newSegment: true },
    { lat: lat - 0.001, lon: 152.91, timestampMs: nowMs - 20 * MIN, newSegment: false },
  ]
}

describe('AnchorWatchMap AIS band cache', () => {
  beforeEach(() => {
    vi.mocked(groupLinesByAge).mockClear()
  })

  it('re-bands only the target that got a point, not every track', () => {
    const vessels = [vessel('111111111'), vessel('222222222')]
    const a = points(-25.29)
    const b = points(-25.30)
    // The own trail is banded by the same function; only AIS arrays are counted.
    const bandedAis = () => vi.mocked(groupLinesByAge).mock.calls.map(([p]) => p).filter(p => p.length > 0)
    const { rerender } = render(mapElement(new Map([['111111111', a], ['222222222', b]]), vessels))
    expect(bandedAis()).toHaveLength(2)

    vi.mocked(groupLinesByAge).mockClear()
    // A poll brings a new array for A only; B keeps its identity.
    const a2 = [...a, { lat: -25.292, lon: 152.91, timestampMs: Date.now() - 5 * MIN, newSegment: false }]
    rerender(mapElement(new Map([['111111111', a2], ['222222222', b]]), vessels))
    expect(bandedAis()).toEqual([a2])
    expect(bandedAis().some(p => p === b)).toBe(false)
  })
})
