import { render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import { AnchorWatchMap } from '@/components/anchor-watch-map'

vi.mock('maplibre-gl', () => ({ default: {} }))

vi.mock('react-map-gl/maplibre', async () => {
  const React = await import('react')
  return {
    Map: React.forwardRef(({ children }: { children?: React.ReactNode }, ref: React.Ref<unknown>) => {
      React.useImperativeHandle(ref, () => ({
        getCanvas: () => ({ style: { cursor: 'grab' } }),
        getZoom: () => 14,
        easeTo: vi.fn(),
      }))
      return <div data-testid="map-root">{children}</div>
    }),
    Marker: ({ children }: { children?: React.ReactNode }) => <div>{children}</div>,
    Source: ({ children }: { children?: React.ReactNode }) => <div>{children}</div>,
    Layer: () => <div />,
  }
})

// The map's own overlays (metrics panel, zoom and layer controls, markers)
// stack at z-index 900 to 2100 so they sit above MapLibre's canvas. Without
// a stacking context on the wrapper those values escape into the page and
// beat the Sheet primitive's z-[70], so the controls drew through the
// manual sheet on the Anchor Watch panel. `isolate` keeps them inside.
describe('AnchorWatchMap stacking', () => {
  it('isolates its overlays so they cannot draw above a sheet', () => {
    render(
      <AnchorWatchMap
        vesselLat={-20.282}
        vesselLon={148.954}
        vesselHeadingDeg={265}
        anchorLat={-20.282}
        anchorLon={148.954}
        anchorSetAt="2026-08-20T06:30:00Z"
        radiusMeters={34}
        depthMeters={3.2}
        currentDriftKts={0.1}
        currentSetDeg={120}
        distanceMeters={10}
        bearingDeg={80}
        scopeRecommendation={null}
        isImperial={false}
        vesselTrail={() => []}
        aisVessels={[]}
        aisTrails={() => new Map()}
        radarTargets={[]}
        isDarkTheme={false}
        onAnchorReposition={() => undefined}
        onRadiusChange={() => undefined}
      />,
    )
    const wrapper = screen.getByTestId('map-root').parentElement
    expect(wrapper).not.toBeNull()
    expect(wrapper!.className.split(/\s+/)).toContain('isolate')
  })
})
