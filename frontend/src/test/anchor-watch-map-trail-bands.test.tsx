import { act, render } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { AnchorWatchMap } from '@/components/anchor-watch-map'
import type { AisTrailPoint } from '@/hooks/use-ais-anchor-tracks'
import type { NearbyVessel } from '@/hooks/use-nearby-vessels'
import type { TrailPoint } from '@/hooks/use-server-trails'
import { AIS_BAND_COLORS, SELF_BAND_COLORS, TRAIL_BANDS } from '@/lib/trail-age-bands'
import { resetTrailClock, serverNow, setServerTimeMs } from '@/lib/trail-clock'

vi.mock('maplibre-gl', () => ({
  default: {},
}))

interface SourceProps { data?: GeoJSON.FeatureCollection<GeoJSON.LineString>; lineMetrics?: boolean }
interface LayerProps { paint?: Record<string, unknown>; layout?: Record<string, unknown> }
let sources: Record<string, SourceProps> = {}
let layers: Record<string, LayerProps> = {}

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
    Source: ({ children, id, ...rest }: { children?: React.ReactNode; id: string } & SourceProps) => {
      sources[id] = rest
      return <div data-testid={`source-${id}`}>{children}</div>
    },
    Layer: ({ id, paint, layout }: { id: string } & LayerProps) => {
      layers[id] = { paint, layout }
      return <div data-testid={`layer-${id}`} />
    },
  }
})

const HOUR = 60 * 60 * 1000
const MIN = 60 * 1000

function renderMap(overrides: Partial<React.ComponentProps<typeof AnchorWatchMap>> = {}) {
  sources = {}
  layers = {}
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
      isImperial={false}
      vesselTrail={[]}
      aisVessels={[]}
      aisTrails={new Map()}
      isDarkTheme={false}
      {...overrides}
    />,
  )
}

const vessel = (mmsi: string | undefined, name: string): NearbyVessel => ({
  id: `urn:mrn:imo:mmsi:${mmsi ?? 'unknown'}`, name, mmsi, lat: -25.2939, lon: 152.9103, range_m: 61, age_seconds: 5,
})

// The map's AIS clock moves in five minute steps, so these ages are in hours or
// leave a margin of at least ten minutes.
const aisPoint = (ageMs: number, lat: number, newSegment = false): AisTrailPoint => ({
  lat, lon: 152.91, timestampMs: Date.now() - ageMs, newSegment,
})

const tracksOf = (points: AisTrailPoint[], mmsi = '111111111') => new Map([[mmsi, points]])

const bandsOf = (id: string) => (sources[id].data?.features ?? []).map(f => (f.properties as { band: number }).band)

describe('AnchorWatchMap AIS trails', () => {
  it('draws a track for a target on the map by MMSI, whatever name it carries now', () => {
    renderMap({
      aisVessels: [vessel('111111111', 'NEW NAME')],
      aisTrails: tracksOf([aisPoint(40 * MIN, -25.29), aisPoint(30 * MIN, -25.291), aisPoint(20 * MIN, -25.292)]),
    })
    expect(sources['ais-trails'].data?.features.length).toBeGreaterThan(0)
  })

  it('matches an MMSI the feed carries with padding, as the server stores it trimmed', () => {
    renderMap({
      aisVessels: [vessel(' 111111111 ', 'PADDED')],
      aisTrails: tracksOf([aisPoint(40 * MIN, -25.29), aisPoint(30 * MIN, -25.291)]),
    })
    expect(sources['ais-trails']?.data?.features.length).toBeGreaterThan(0)
  })

  it('draws nothing for a track whose target is not on the map, even under a name that matches', () => {
    renderMap({
      aisVessels: [vessel('222222222', 'SPLURGE')],
      aisTrails: tracksOf([aisPoint(40 * MIN, -25.29), aisPoint(20 * MIN, -25.291)]),
    })
    expect(sources['ais-trails']).toBeUndefined()
  })

  it('shows a target that never came within range as a marker only: no track, no line', () => {
    renderMap({ aisVessels: [vessel('333333333', 'FAR AWAY')], aisTrails: new Map() })
    expect(sources['ais-trails']).toBeUndefined()
  })

  it('leaves a gap where a target left and came back, and where over 15 minutes passed', () => {
    renderMap({
      aisVessels: [vessel('111111111', 'ALPHA')],
      aisTrails: tracksOf([
        aisPoint(60 * MIN, -25.290),
        aisPoint(58 * MIN, -25.291),
        aisPoint(56 * MIN, -25.292, true), // re-entry, two minutes after
        aisPoint(54 * MIN, -25.293),
        aisPoint(30 * MIN, -25.294, true), // 24 minutes of silence, marked by the server
        aisPoint(28 * MIN, -25.295),
      ]),
    })
    const features = sources['ais-trails'].data!.features
    // Three separate stretches.
    expect(features.length).toBeGreaterThanOrEqual(3)
    const joined = features.some(f =>
      f.geometry.coordinates.some(c => c[1] === -25.291) && f.geometry.coordinates.some(c => c[1] === -25.292))
    expect(joined).toBe(false)
  })

  it('brightens with age, not distance: each band of the 24 hours gets its own band number, newest highest', () => {
    const step = (24 * HOUR) / TRAIL_BANDS
    const points = [4, 3, 2, 1, 0].map((i, n) => aisPoint(i * step + 10 * MIN, -25.29 - n * 0.001))
    renderMap({ aisVessels: [vessel('111111111', 'ALPHA')], aisTrails: tracksOf(points) })
    const bands = bandsOf('ais-trails')
    expect(new Set(bands).size).toBe(bands.length)
    expect(Math.max(...bands)).toBe(TRAIL_BANDS - 1)
    expect(Math.min(...bands)).toBeLessThan(TRAIL_BANDS - 1)
  })

  it('fades a track only hours old across every band, as the own trail does', () => {
    const points = [8 * HOUR, 6.5 * HOUR, 5 * HOUR, 4 * HOUR, 2.5 * HOUR, 10 * MIN].map((age, n) => aisPoint(age, -25.29 - n * 0.001))
    renderMap({ aisVessels: [vessel('111111111', 'ALPHA')], aisTrails: tracksOf(points) })
    expect(bandsOf('ais-trails').sort()).toEqual([0, 1, 2, 3, 4])
  })

  it('keeps a track only minutes old in the newest bands rather than fading its first minutes to clear', () => {
    const points = [20 * MIN, 15 * MIN, 10 * MIN, 5 * MIN, 1 * MIN].map((age, n) => aisPoint(age, -25.29 - n * 0.001))
    renderMap({ aisVessels: [vessel('111111111', 'ALPHA')], aisTrails: tracksOf(points) })
    expect(Math.min(...bandsOf('ais-trails'))).toBeGreaterThanOrEqual(TRAIL_BANDS - 2)
  })

  it('draws the newest on top by the line sort key alone, and uses the fading amber with no line gradient', () => {
    renderMap({
      aisVessels: [vessel('111111111', 'ALPHA')],
      aisTrails: tracksOf([aisPoint(40 * MIN, -25.29), aisPoint(20 * MIN, -25.291)]),
    })
    expect(sources['ais-trails'].lineMetrics).toBeUndefined()
    expect(layers['ais-trails-layer'].layout?.['line-sort-key']).toEqual(['get', 'band'])
    const paint = layers['ais-trails-layer'].paint!
    expect(paint['line-gradient']).toBeUndefined()
    expect(JSON.stringify(paint['line-color'])).toContain(AIS_BAND_COLORS[TRAIL_BANDS - 1])
    expect(JSON.stringify(paint['line-color'])).toContain(AIS_BAND_COLORS[0])
  })

  it('does not draw a point older than 24 hours', () => {
    renderMap({
      aisVessels: [vessel('111111111', 'ALPHA')],
      aisTrails: tracksOf([aisPoint(30 * HOUR, -25.29), aisPoint(26 * HOUR, -25.291)]),
    })
    expect(sources['ais-trails']).toBeUndefined()
  })
})

describe('AnchorWatchMap own swing trail', () => {
  const trailPoints = (): TrailPoint[] => {
    const nowMs = Date.now()
    return [10 * HOUR, 8 * HOUR, 6 * HOUR, 4 * HOUR, 2 * HOUR, 5 * MIN].map((age, i) => ({
      lat: -25.29 - i * 0.0001, lon: 152.91, timestampMs: nowMs - age,
    }))
  }

  it('is brightest where it is newest: the newest band is the highest number', () => {
    renderMap({ vesselTrail: trailPoints() })
    const bands = bandsOf('vessel-trail-anchor')
    expect(bands.length).toBeGreaterThan(1)
    expect(Math.max(...bands)).toBe(TRAIL_BANDS - 1)
    expect(Math.min(...bands)).toBeLessThan(TRAIL_BANDS - 1)
    expect(layers['vessel-trail-anchor-layer'].layout?.['line-sort-key']).toEqual(['get', 'band'])
  })

  it('colours the bands dim yellow to bright yellow, solid, with no gradient along the line', () => {
    renderMap({ vesselTrail: trailPoints() })
    expect(sources['vessel-trail-anchor'].lineMetrics).toBeUndefined()
    const paint = layers['vessel-trail-anchor-layer'].paint!
    expect(paint['line-gradient']).toBeUndefined()
    const expr = JSON.stringify(paint['line-color'])
    for (const c of SELF_BAND_COLORS) expect(expr).toContain(c)
  })
})

// The trail points carry the server's times, so the map ages them by the
// server's clock (lib/trail-clock.ts), whatever the browser's says.
describe('AnchorWatchMap trails on a browser clock that is wrong', () => {
  afterEach(() => resetTrailClock())

  it('bands AIS points by their age on the server clock, and keeps 23 hour old points, with the browser 3 hours ahead', () => {
    setServerTimeMs(Date.now() - 3 * HOUR) // the server is 3 hours behind this browser
    const at = (ageMs: number) => serverNow() - ageMs
    const points: AisTrailPoint[] = [
      { lat: -25.29, lon: 152.91, timestampMs: at(23 * HOUR), newSegment: true },
      { lat: -25.291, lon: 152.91, timestampMs: at(22 * HOUR), newSegment: false },
      { lat: -25.292, lon: 152.91, timestampMs: at(HOUR), newSegment: false },
    ]
    renderMap({ aisVessels: [vessel('111111111', 'ALPHA')], aisTrails: tracksOf(points) })
    // 22 hours old is the oldest of the five bands, 1 hour old the newest.
    expect(bandsOf('ais-trails').sort()).toEqual([0, TRAIL_BANDS - 1])
  })

  it('re-bands the AIS tracks when the first server time arrives after the map is up', () => {
    const serverMs = Date.now() - 3 * HOUR // the boat's clock, 3 hours behind this browser's
    const points: AisTrailPoint[] = [
      { lat: -25.29, lon: 152.91, timestampMs: serverMs - 23 * HOUR, newSegment: true },
      { lat: -25.291, lon: 152.91, timestampMs: serverMs - 22 * HOUR, newSegment: false },
      { lat: -25.292, lon: 152.91, timestampMs: serverMs - HOUR, newSegment: false },
    ]
    renderMap({ aisVessels: [vessel('111111111', 'ALPHA')], aisTrails: tracksOf(points) })
    // Against the browser's clock the two oldest points are past 24 hours.
    expect(bandsOf('ais-trails')).toEqual([TRAIL_BANDS - 1])
    act(() => setServerTimeMs(serverMs))
    expect(bandsOf('ais-trails').sort()).toEqual([0, TRAIL_BANDS - 1])
  })

  it('keeps the own trail newest band the newest with the browser 3 hours behind', () => {
    setServerTimeMs(Date.now() + 3 * HOUR) // the server is 3 hours ahead of this browser
    const nowMs = serverNow()
    const vesselTrail: TrailPoint[] = [10 * HOUR, 8 * HOUR, 6 * HOUR, 4 * HOUR, 2 * HOUR, 5 * MIN].map((age, i) => ({
      lat: -25.29 - i * 0.0001, lon: 152.91, timestampMs: nowMs - age,
    }))
    renderMap({ vesselTrail })
    expect(bandsOf('vessel-trail-anchor').sort()).toEqual([0, 1, 2, 3, 4])
  })
})
