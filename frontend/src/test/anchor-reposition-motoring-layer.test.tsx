import { describe, it, expect, vi, beforeEach } from 'vitest'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { AnchorWatchMap } from '@/components/anchor-watch-map'

vi.mock('react-map-gl/maplibre', async () => {
  const React = await import('react')
  return {
    Map: React.forwardRef(({ children }: { children?: React.ReactNode }, ref) => {
      React.useImperativeHandle(ref, () => ({
        getCanvas: () => ({ style: { cursor: 'grab' } }),
        getZoom: () => 14,
        easeTo: () => undefined,
      }))
      return <div data-testid="map-root">{children}</div>
    }),
    Marker: ({ children }: { children?: React.ReactNode }) => <div>{children}</div>,
    Source: ({ children, id }: { children?: React.ReactNode; id: string }) => (
      <div data-testid={`source-${id}`}>{children}</div>
    ),
    Layer: ({ id }: { id: string }) => <div data-testid={`layer-${id}`} />,
  }
})

describe('Anchor reposition motoring trail rendering', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('fetches motoring points and renders motoring line layer in reposition mode', async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      json: async () => ({
        points: [
          { lat: -25.427162, lon: 152.987832, timestamp: '2026-06-07T04:19:43Z' },
          { lat: -25.419135, lon: 152.990420, timestamp: '2026-06-07T04:22:44Z' },
        ],
      }),
    })
    vi.stubGlobal('fetch', fetchMock)

    render(
      <AnchorWatchMap
        vesselLat={-25.2939}
        vesselLon={152.9103}
        vesselHeadingDeg={265}
        anchorLat={-25.2938}
        anchorLon={152.9102}
        radiusMeters={34}
        depthMeters={3.2}
        currentDriftKts={0.1}
        currentSetDeg={120}
        distanceMeters={10}
        bearingDeg={80}
        isImperial={false}
        vesselTrail={() => []}
        aisVessels={[]}
        aisTrails={() => new Map()}
        isDarkTheme={false}
        onAnchorReposition={() => undefined}
        onRadiusChange={() => undefined}
        onClearAnchor={() => undefined}
      />,
    )

    fireEvent.click(screen.getByLabelText('Anchor position — click to reposition'))

    await waitFor(() => {
      expect(fetchMock).toHaveBeenCalledWith('/api/tracks/motoring')
    })

    expect(await screen.findByTestId('layer-vessel-trail-motoring-layer')).toBeInTheDocument()
  })
})
