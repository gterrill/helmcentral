import { describe, expect, it, vi } from 'vitest'
import { render, screen } from '@testing-library/react'
import { AnchorWatchMap } from '@/components/anchor-watch-map'

vi.mock('maplibre-gl', () => ({
  default: {},
}))

vi.mock('react-map-gl/maplibre', () => ({
  Map: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  Marker: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  Source: ({ id, maxzoom, children }: { id: string; maxzoom?: number; children: React.ReactNode }) => (
    <div data-testid={`source-${id}`} data-maxzoom={maxzoom ?? ''}>
      {children}
    </div>
  ),
  Layer: () => null,
}))

describe('World imagery source config', () => {
  it('caps world imagery source maxzoom to avoid no-data tiles at higher map zoom', () => {
    render(
      <AnchorWatchMap
        vesselLat={-25.2939}
        vesselLon={152.9103}
        vesselHeadingDeg={265}
        anchorLat={-25.2939}
        anchorLon={152.9103}
        radiusMeters={34}
        depthMeters={3}
        currentDriftKts={0}
        currentSetDeg={0}
        distanceMeters={26}
        bearingDeg={83}
        isImperial={false}
        vesselTrail={() => []}
        aisVessels={[]}
        aisTrails={() => new Map()}
        isDarkTheme={false}
        showImageryLayer
        onImageryToggle={() => {}}
        onAnchorReposition={() => {}}
        onRadiusChange={() => {}}
        onClearAnchor={() => {}}
      />,
    )

    const source = screen.getByTestId('source-world-imagery')
    expect(source.getAttribute('data-maxzoom')).toBe('18')
  })
})
