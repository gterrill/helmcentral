import { render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import { AnchorWatchMap } from '@/components/anchor-watch-map'
import type { RadarTarget } from '@/hooks/use-radar-targets'
import type { NearbyVessel } from '@/hooks/use-nearby-vessels'
import type { RodeMethodResult } from '@/lib/rode-plan'

// Same maplibre-gl / react-map-gl stubs as anchor-watch-map-ui.test.tsx: this
// suite only needs the marker layer, not a live map canvas.
vi.mock('maplibre-gl', () => ({
  default: {},
}))

vi.mock('react-map-gl/maplibre', async () => {
  const React = await import('react')
  return {
    Map: React.forwardRef(
      (
        { children }: { children?: React.ReactNode },
        ref: React.Ref<unknown>,
      ) => {
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
    Source: ({ children, id }: { children?: React.ReactNode; id: string }) => (
      <div data-testid={`source-${id}`}>{children}</div>
    ),
    Layer: ({ id, beforeId }: { id: string; beforeId?: string }) => (
      <div data-testid={`layer-${id}`} data-before-id={beforeId} />
    ),
  }
})

const aisVessels: NearbyVessel[] = [
  { id: 'urn:mrn:imo:mmsi:100000001', name: 'SPLURGE', lat: -25.2939, lon: 152.9103, range_m: 61, age_seconds: 5 },
]

const radarTargetWithPosition: RadarTarget = {
  id: 'fur6424A:100000016',
  radar_id: 'fur6424A',
  target_id: 100000016,
  status: 'tracking',
  bearing_rad: 0.768,
  range_m: 418,
  lat: -25.2945,
  lon: 152.911,
  position_derived: false,
  course_rad: 5.286,
  sog_knots: 13.1,
  cpa_m: 422.9,
  tcpa_seconds: 7.19,
  is_dangerous: true,
  acquisition: 'auto',
  source_zone: 1,
  age_seconds: 3,
  last_seen_at: '2026-08-28T02:00:12.326Z',
}

const radarTargetNoPosition: RadarTarget = {
  ...radarTargetWithPosition,
  id: 'fur6424A:100000017',
  target_id: 100000017,
  lat: undefined,
  lon: undefined,
}

const scopeRecommendation: RodeMethodResult = {
  id: 'ratio',
  label: 'Ratio Method',
  recommendedRodeM: 30,
  scopeRatio: 5,
  note: 'Depth 3.2 m (sounder only) · Wind 15 kts · 5:1',
}

function renderMap(radarTargets: RadarTarget[]) {
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
      aisVessels={aisVessels}
      aisTrails={() => new Map()}
      radarTargets={radarTargets}
      isDarkTheme={false}
      showImageryLayer
      onImageryToggle={() => undefined}
      onFullscreen={() => undefined}
    />,
  )
}

describe('AnchorWatchMap radar target markers', () => {
  it('renders a radar marker distinct from the AIS marker', () => {
    renderMap([radarTargetWithPosition])

    const radarMarker = document.querySelector('[aria-label^="Radar target:"]')
    expect(radarMarker).not.toBeNull()
    expect(radarMarker?.getAttribute('aria-label')).toBe(`Radar target: ${radarTargetWithPosition.id}`)

    expect(screen.getByRole('button', { name: 'AIS vessel: SPLURGE' })).toBeInTheDocument()
    // The two marker kinds never share a label prefix.
    expect(document.querySelectorAll('[aria-label^="AIS vessel:"]')).toHaveLength(1)
    expect(document.querySelectorAll('[aria-label^="Radar target:"]')).toHaveLength(1)
  })

  it('renders no marker for a radar target with no lat/lon', () => {
    renderMap([radarTargetNoPosition])
    expect(document.querySelector('[aria-label^="Radar target:"]')).toBeNull()
  })

  it('renders nothing radar-related when radarTargets is empty', () => {
    renderMap([])
    expect(document.querySelector('[aria-label^="Radar target:"]')).toBeNull()
  })
})

// Contrast, not just correctness.
//
// The first live run rendered 20 radar targets and only the three dangerous
// ones were visible. The rest used `bg-secondary`, which in the day theme is
// `--secondary: 0 0% 92%`, near-white on a near-white map: seventeen targets
// showed as a floating RDR label with no marker above it.
//
// `bg-foreground` is the token that is legible in both themes by definition
// (0% lightness on day, 92% on night). The ring gives it an edge against
// whatever the tiles happen to be underneath.
describe('radar marker contrast against the map', () => {
  it('does not paint an ordinary target in a near-invisible token', () => {
    const { container } = renderMap([
      { ...radarTargetWithPosition, is_dangerous: false, status: 'tracking' },
    ])

    const triangle = triangleFor(container, radarTargetWithPosition.id)
    expect(triangle).not.toBeNull()
    expect(triangle.className).not.toContain('bg-secondary')
    expect(triangle.className).toContain('bg-foreground')
  })

  it('keeps the acquiring outline legible too', () => {
    const { container } = renderMap([
      { ...radarTargetWithPosition, is_dangerous: false, status: 'acquiring' },
    ])

    const triangle = triangleFor(container, radarTargetWithPosition.id)
    expect(triangle.className).not.toContain('border-secondary')
    expect(triangle.className).toContain('border-foreground')
  })

  it('still uses the alert palette for a dangerous target', () => {
    const { container } = renderMap([
      { ...radarTargetWithPosition, is_dangerous: true, status: 'tracking' },
    ])

    const triangle = triangleFor(container, radarTargetWithPosition.id)
    expect(triangle.className).toContain('bg-red-600')
  })
})

// triangleFor pulls the clip-path element out of a marker so the assertions
// read against the shape itself rather than its wrapper.
function triangleFor(container: HTMLElement, id: string): HTMLElement {
  const marker = container.querySelector(`[aria-label="Radar target: ${id}"]`)
  if (!marker) throw new Error(`no marker for ${id}`)
  const triangle = marker.querySelector('[style*="clip-path"]')
  if (!triangle) throw new Error(`no triangle inside marker for ${id}`)
  return triangle as HTMLElement
}
