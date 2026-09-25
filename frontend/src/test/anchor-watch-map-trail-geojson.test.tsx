import { act, render } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import { AnchorWatchMap } from '@/components/anchor-watch-map'
import type { NearbyVessel } from '@/hooks/use-nearby-vessels'
import type { RodeMethodResult } from '@/lib/rode-plan'
import type { TrailPoint } from '@/hooks/use-server-trails'

// react-map-gl re-uploads a Source's `data` to maplibre whenever the prop's
// *identity* changes, even when the GeoJSON it describes is byte-identical
// - and these two sources (self trail, AIS trails) use lineMetrics with
// line-gradient, which is one of the more expensive re-uploads to redo for
// nothing. The old 5s interval bumped renderKey unconditionally, forcing a
// brand new GeoJSON object (and re-upload) every tick regardless of whether
// a single point had actually arrived. This suite proves the fix: `data`
// keeps its identity across a tick where nothing changed, and only gets a
// new one once the underlying trail actually does.
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

const scopeRecommendation: RodeMethodResult = {
  id: 'ratio',
  label: 'Ratio Method',
  recommendedRodeM: 30,
  scopeRatio: 5,
  note: 'note',
}

function renderMap(overrides: Partial<React.ComponentProps<typeof AnchorWatchMap>> = {}) {
  return render(
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
      scopeRecommendation={scopeRecommendation}
      isImperial={false}
      vesselTrail={() => []}
      aisVessels={[]}
      aisTrails={() => new Map()}
      radarTargets={[]}
      isDarkTheme={false}
      {...overrides}
    />,
  )
}

describe('AnchorWatchMap trail GeoJSON identity across the 5s renderKey tick', () => {
  it('keeps the self-trail Source data identity stable across a tick with no new points, and produces a new one once a point actually arrives', () => {
    vi.useFakeTimers()
    lastSourceData = {}

    let points: TrailPoint[] = [
      { lat: -25.29, lon: 152.91, timestampMs: 1000 },
      { lat: -25.291, lon: 152.911, timestampMs: 2000 },
    ]
    const vesselTrail = () => points

    renderMap({ vesselTrail })

    const firstData = lastSourceData['vessel-trail-anchor']
    expect(firstData).toBeDefined()

    // A tick where the underlying trail did not change at all.
    act(() => { vi.advanceTimersByTime(5000) })
    const secondData = lastSourceData['vessel-trail-anchor']
    expect(secondData).toBe(firstData)

    // Now a real point actually arrives before the next tick.
    points = [...points, { lat: -25.292, lon: 152.912, timestampMs: 3000 }]
    act(() => { vi.advanceTimersByTime(5000) })
    const thirdData = lastSourceData['vessel-trail-anchor']
    expect(thirdData).not.toBe(secondData)

    vi.useRealTimers()
  })

  it('keeps the AIS-trails Source data identity stable across a tick with no new points, and produces a new one once a point actually arrives', () => {
    vi.useFakeTimers()
    lastSourceData = {}

    const nowMs = Date.now()
    const vessels: NearbyVessel[] = [
      { id: 'urn:mrn:imo:mmsi:100000001', name: 'SPLURGE', lat: -25.2939, lon: 152.9103, range_m: 61, age_seconds: 5 },
    ]
    let trail: TrailPoint[] = [
      { lat: -25.29, lon: 152.91, timestampMs: nowMs - 2000 },
      { lat: -25.291, lon: 152.911, timestampMs: nowMs - 1000 },
    ]
    const aisTrails = () => new Map([['SPLURGE', trail]])

    renderMap({ aisVessels: vessels, aisTrails })

    const firstData = lastSourceData['ais-trails']
    expect(firstData).toBeDefined()

    act(() => { vi.advanceTimersByTime(5000) })
    const secondData = lastSourceData['ais-trails']
    expect(secondData).toBe(firstData)

    trail = [...trail, { lat: -25.292, lon: 152.912, timestampMs: Date.now() }]
    act(() => { vi.advanceTimersByTime(5000) })
    const thirdData = lastSourceData['ais-trails']
    expect(thirdData).not.toBe(secondData)

    vi.useRealTimers()
  })
})
