import { render } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import { AnchorWatchMap } from '@/components/anchor-watch-map'
import type { NearbyVessel } from '@/hooks/use-nearby-vessels'
import type { TrailPoint } from '@/hooks/use-server-trails'

// react-map-gl re-uploads a Source's `data` to maplibre whenever the prop's
// *identity* changes, even when the GeoJSON it describes is byte-identical
// - and these two sources (self trail, AIS trails) use lineMetrics with
// line-gradient, which is one of the more expensive re-uploads to redo for
// nothing. The trails arrive as snapshots that change identity only when a
// point does (ADR 0215), so `data` keeps its identity across renders where
// the trail did not change, and only gets a new one when it does.
vi.mock('maplibre-gl', () => ({
  default: {},
}))

let lastSourceData: Record<string, unknown> = {}

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
    Marker: ({ children, onClick }: { children?: React.ReactNode; onClick?: () => void }) => (
      <div onClick={onClick}>{children}</div>
    ),
    Source: ({ children, id, data }: { children?: React.ReactNode; id: string; data?: unknown }) => {
      lastSourceData[id] = data
      return <div data-testid={`source-${id}`}>{children}</div>
    },
    Layer: ({ id }: { id: string }) => <div data-testid={`layer-${id}`} />,
  }
})


function renderMap(overrides: Partial<React.ComponentProps<typeof AnchorWatchMap>> = {}) {
  return render(mapElement(overrides))
}

function mapElement(overrides: Partial<React.ComponentProps<typeof AnchorWatchMap>> = {}) {
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
      aisVessels={[]}
      aisTrails={new Map()}
      radarTargets={[]}
      isDarkTheme={false}
      {...overrides}
    />
  )
}

describe('AnchorWatchMap trail GeoJSON identity', () => {
  it('keeps the self-trail Source data identity while the trail is unchanged, and produces a new one once a point arrives', () => {
    lastSourceData = {}
    const points: TrailPoint[] = [
      { lat: -25.29, lon: 152.91, timestampMs: 1000 },
      { lat: -25.291, lon: 152.911, timestampMs: 2000 },
    ]

    const { rerender } = renderMap({ vesselTrail: points })
    const firstData = lastSourceData['vessel-trail-anchor']
    expect(firstData).toBeDefined()

    // The map re-renders for another reason; the trail is the same snapshot.
    rerender(mapElement({ vesselTrail: points, radiusMeters: 35 }))
    expect(lastSourceData['vessel-trail-anchor']).toBe(firstData)

    rerender(mapElement({ vesselTrail: [...points, { lat: -25.292, lon: 152.912, timestampMs: 3000 }] }))
    expect(lastSourceData['vessel-trail-anchor']).not.toBe(firstData)
  })

  it('keeps the AIS-trails Source data identity while the trails are unchanged, and produces a new one once a point arrives', () => {
    lastSourceData = {}
    const nowMs = Date.now()
    const vessels: NearbyVessel[] = [
      { id: 'urn:mrn:imo:mmsi:100000001', name: 'SPLURGE', lat: -25.2939, lon: 152.9103, range_m: 61, age_seconds: 5 },
    ]
    const trail: TrailPoint[] = [
      { lat: -25.29, lon: 152.91, timestampMs: nowMs - 2000 },
      { lat: -25.291, lon: 152.911, timestampMs: nowMs - 1000 },
    ]
    const aisTrails = new Map([['SPLURGE', trail]])

    const { rerender } = renderMap({ aisVessels: vessels, aisTrails })
    const firstData = lastSourceData['ais-trails']
    expect(firstData).toBeDefined()

    rerender(mapElement({ aisVessels: vessels, aisTrails, radiusMeters: 35 }))
    expect(lastSourceData['ais-trails']).toBe(firstData)

    // A fresh vessel array with the same names (every AIS delta makes one).
    rerender(mapElement({ aisVessels: vessels.map((v) => ({ ...v, range_m: 70 })), aisTrails }))
    expect(lastSourceData['ais-trails']).toBe(firstData)

    const next = new Map([['SPLURGE', [...trail, { lat: -25.292, lon: 152.912, timestampMs: nowMs }]]])
    rerender(mapElement({ aisVessels: vessels, aisTrails: next }))
    expect(lastSourceData['ais-trails']).not.toBe(firstData)
  })

  it('runs no timer of its own to look for new trail points', () => {
    vi.useFakeTimers()
    try {
      const spy = vi.spyOn(globalThis, 'setInterval')
      renderMap()
      expect(spy.mock.calls.filter(([, ms]) => ms === 5000)).toHaveLength(0)
    } finally {
      vi.useRealTimers()
    }
  })
})
