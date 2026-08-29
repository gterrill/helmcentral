import { render } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { AnchorWatchMap } from '@/components/anchor-watch-map'

vi.mock('maplibre-gl', () => ({
  default: {},
}))

let lastInitialViewState: { latitude: number; longitude: number; zoom: number } | null = null
const easeToMock = vi.fn()

vi.mock('react-map-gl/maplibre', async () => {
  const React = await import('react')
  return {
    Map: React.forwardRef(
      (
        {
          children,
          initialViewState,
        }: {
          children?: React.ReactNode
          initialViewState?: { latitude: number; longitude: number; zoom: number }
        },
        ref: React.Ref<unknown>,
      ) => {
        lastInitialViewState = initialViewState ?? null
        React.useImperativeHandle(ref, () => ({
          getCanvas: () => ({ style: { cursor: 'grab' } }),
          getZoom: () => 14,
          easeTo: easeToMock,
        }))
        return <div data-testid="map-root">{children}</div>
      },
    ),
    Marker: ({ children }: { children?: React.ReactNode }) => <div>{children}</div>,
    Source: ({ children, id }: { children?: React.ReactNode; id: string }) => (
      <div data-testid={`source-${id}`}>{children}</div>
    ),
    Layer: ({ id }: { id: string }) => <div data-testid={`layer-${id}`} />,
  }
})

const FIRST_SESSION = '2026-08-20T06:30:00Z'
const SECOND_SESSION = '2026-08-21T09:15:00Z'

// Whitehaven, then a bay 20-odd miles away: two anchorages far enough apart
// that a view left over from the first shows none of the second.
const FIRST_ANCHOR = { lat: -20.2820, lon: 148.9540 }
const SECOND_ANCHOR = { lat: -20.0630, lon: 148.8890 }

function mapElement(overrides: Partial<React.ComponentProps<typeof AnchorWatchMap>> = {}) {
  return (
    <AnchorWatchMap
      vesselLat={FIRST_ANCHOR.lat}
      vesselLon={FIRST_ANCHOR.lon}
      vesselHeadingDeg={265}
      anchorLat={FIRST_ANCHOR.lat}
      anchorLon={FIRST_ANCHOR.lon}
      anchorSetAt={FIRST_SESSION}
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
      {...overrides}
    />
  )
}

function storeCentre(latitude: number, longitude: number, sessionId: string | null) {
  localStorage.setItem('anchor-watch-map-center', JSON.stringify({ latitude, longitude, sessionId }))
}

function storedCentre() {
  const raw = localStorage.getItem('anchor-watch-map-center')
  return raw === null ? null : (JSON.parse(raw) as { latitude: number; longitude: number; sessionId: string | null })
}

describe('AnchorWatchMap follows the anchor session', () => {
  beforeEach(() => {
    localStorage.clear()
    easeToMock.mockClear()
    lastInitialViewState = null
  })

  it('centres on the new anchor when a new session starts under a mounted map', () => {
    // Panned off the anchor during the first anchorage, as anyone watching a
    // neighbour drag would.
    storeCentre(-20.2900, 148.9600, FIRST_SESSION)
    const { rerender } = render(mapElement())
    expect(easeToMock).not.toHaveBeenCalled()

    rerender(
      mapElement({
        anchorLat: SECOND_ANCHOR.lat,
        anchorLon: SECOND_ANCHOR.lon,
        anchorSetAt: SECOND_SESSION,
        vesselLat: SECOND_ANCHOR.lat,
        vesselLon: SECOND_ANCHOR.lon,
      }),
    )

    expect(easeToMock).toHaveBeenCalledWith({
      center: [SECOND_ANCHOR.lon, SECOND_ANCHOR.lat],
      duration: 600,
    })
    expect(storedCentre()).toEqual({
      latitude: SECOND_ANCHOR.lat,
      longitude: SECOND_ANCHOR.lon,
      sessionId: SECOND_SESSION,
    })
  })

  it('leaves the view alone when the anchor moves within the same session', () => {
    storeCentre(-20.2900, 148.9600, FIRST_SESSION)
    const { rerender } = render(mapElement())

    // A reposition drag: the anchor point moves, the session does not.
    rerender(mapElement({ anchorLat: -20.2825, anchorLon: 148.9548 }))

    expect(easeToMock).not.toHaveBeenCalled()
  })

  it('opens on the new anchor when the client loads after the session changed', () => {
    storeCentre(-20.2900, 148.9600, FIRST_SESSION)

    render(
      mapElement({
        anchorLat: SECOND_ANCHOR.lat,
        anchorLon: SECOND_ANCHOR.lon,
        anchorSetAt: SECOND_SESSION,
      }),
    )

    expect(lastInitialViewState).toMatchObject({
      latitude: SECOND_ANCHOR.lat,
      longitude: SECOND_ANCHOR.lon,
    })
  })

  it('keeps a pan made in the session still running when the watch state has not loaded yet', () => {
    storeCentre(-20.2900, 148.9600, FIRST_SESSION)

    // Mount is the first paint, before GET /api/anchor-watch answers: the
    // map has no session id to judge the stored centre against yet.
    const { rerender } = render(mapElement({ anchorLat: null, anchorLon: null, anchorSetAt: null }))
    expect(lastInitialViewState).toMatchObject({ latitude: -20.2900, longitude: 148.9600 })

    // The poll lands on the same session the pan was made in.
    rerender(mapElement())

    expect(easeToMock).not.toHaveBeenCalled()
  })

  it('centres on the anchor when the first watch of a stored-centre-free client drops', () => {
    const { rerender } = render(mapElement({ anchorLat: null, anchorLon: null, anchorSetAt: null }))

    rerender(mapElement())

    expect(easeToMock).toHaveBeenCalledWith({
      center: [FIRST_ANCHOR.lon, FIRST_ANCHOR.lat],
      duration: 600,
    })
  })
})
