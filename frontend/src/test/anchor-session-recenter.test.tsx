import { render } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { AnchorWatchMap } from '@/components/anchor-watch-map'

vi.mock('maplibre-gl', () => ({
  default: {},
}))

let lastInitialViewState: { latitude: number; longitude: number; zoom: number } | null = null
const easeToMock = vi.fn()
// Defaults to 0x0 (unmeasurable, matching jsdom's real getBoundingClientRect
// with no layout) so every existing test below keeps exercising the
// center-only easeTo fallback unchanged; only the "fits zoom to the
// container" suite at the foot of this file sets a real size.
const containerSize = vi.hoisted(() => ({ width: 0, height: 0 }))

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
          jumpTo: vi.fn(),
          getMap: () => ({
            getContainer: () => ({
              getBoundingClientRect: () => ({ width: containerSize.width, height: containerSize.height }),
            }),
          }),
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
    // map has no session id to judge the stored centre against yet, and the
    // host says so via anchorStateKnown={false}.
    const { rerender } = render(
      mapElement({ anchorLat: null, anchorLon: null, anchorSetAt: null, anchorStateKnown: false }),
    )
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

// code-review finding (PR #30): anchorStateKnown used to default to true,
// so a caller that forgot to wire up useAnchorWatch's `loaded` silently got
// "confirmed no anchor" instead of the safe "still waiting to hear back".
// Defaulting to false means the ambiguous case is the one a careless mount
// gets, and a stored centre survives it exactly as if the caller had passed
// anchorStateKnown={false} explicitly.
describe('AnchorWatchMap: anchorStateKnown defaults to false (not known)', () => {
  beforeEach(() => {
    localStorage.clear()
    easeToMock.mockClear()
    lastInitialViewState = null
  })

  it('trusts a stored centre at mount when the caller omits anchorStateKnown entirely', () => {
    storeCentre(-20.2900, 148.9600, FIRST_SESSION)

    render(
      mapElement({ anchorLat: null, anchorLon: null, anchorSetAt: null, anchorStateKnown: undefined }),
    )

    expect(lastInitialViewState).toMatchObject({ latitude: -20.2900, longitude: 148.9600 })
    expect(easeToMock).not.toHaveBeenCalled()
  })
})

// The operator's actual symptom: with no active anchor, a stored centre from
// a past anchorage used to win regardless, so the map opened on last night's
// bay instead of the boat — right when the operator is about to drop a fresh
// anchor and most needs to see where the vessel actually is. anchorSetAt is
// null both "no watch" and "haven't heard back yet", so these tests use the
// caller's anchorStateKnown (useAnchorWatch's own `loaded`) to disambiguate.
describe('AnchorWatchMap discards a stale stored centre once "no watch" is confirmed', () => {
  const CURRENT_VESSEL = { lat: -20.1500, lon: 148.9000 }

  beforeEach(() => {
    localStorage.clear()
    easeToMock.mockClear()
    lastInitialViewState = null
  })

  it('opens on the vessel, not a stored centre from an old anchorage, once the watch is confirmed inactive at mount', () => {
    storeCentre(-20.2900, 148.9600, FIRST_SESSION)

    render(
      mapElement({
        anchorLat: null,
        anchorLon: null,
        anchorSetAt: null,
        anchorStateKnown: true,
        vesselLat: CURRENT_VESSEL.lat,
        vesselLon: CURRENT_VESSEL.lon,
      }),
    )

    expect(lastInitialViewState).toMatchObject({
      latitude: CURRENT_VESSEL.lat,
      longitude: CURRENT_VESSEL.lon,
    })
  })

  it('eases to the vessel once "no watch" is confirmed for a map that mounted before the first poll resolved', () => {
    storeCentre(-20.2900, 148.9600, FIRST_SESSION)

    // Mount before the poll answers — the ambiguous case, so the stored
    // centre is trusted for now, same as ever.
    const { rerender } = render(
      mapElement({
        anchorLat: null,
        anchorLon: null,
        anchorSetAt: null,
        anchorStateKnown: false,
        vesselLat: CURRENT_VESSEL.lat,
        vesselLon: CURRENT_VESSEL.lon,
      }),
    )
    expect(lastInitialViewState).toMatchObject({ latitude: -20.2900, longitude: 148.9600 })
    expect(easeToMock).not.toHaveBeenCalled()

    // The poll resolves: there is genuinely no watch running.
    rerender(
      mapElement({
        anchorLat: null,
        anchorLon: null,
        anchorSetAt: null,
        anchorStateKnown: true,
        vesselLat: CURRENT_VESSEL.lat,
        vesselLon: CURRENT_VESSEL.lon,
      }),
    )

    expect(easeToMock).toHaveBeenCalledWith({
      center: [CURRENT_VESSEL.lon, CURRENT_VESSEL.lat],
      duration: 600,
    })
    // Nothing left to restore on a later reload — the stale centre is gone,
    // not merely overridden for this session.
    expect(storedCentre()).toBeNull()
  })

  it('keeps the stored centre once the same-session poll resolves active, then eases to the anchor on a genuinely new drop', () => {
    storeCentre(-20.2900, 148.9600, FIRST_SESSION)

    // Mount before the poll answers.
    const { rerender } = render(
      mapElement({ anchorLat: null, anchorLon: null, anchorSetAt: null, anchorStateKnown: false }),
    )
    expect(lastInitialViewState).toMatchObject({ latitude: -20.2900, longitude: 148.9600 })

    // The poll resolves, confirming the very anchorage the stored pan
    // belongs to — nothing should move.
    rerender(mapElement({ anchorStateKnown: true }))
    expect(easeToMock).not.toHaveBeenCalled()

    // A genuinely new drop: a different session, a different anchorage.
    rerender(
      mapElement({
        anchorLat: SECOND_ANCHOR.lat,
        anchorLon: SECOND_ANCHOR.lon,
        anchorSetAt: SECOND_SESSION,
        vesselLat: SECOND_ANCHOR.lat,
        vesselLon: SECOND_ANCHOR.lon,
        anchorStateKnown: true,
      }),
    )

    expect(easeToMock).toHaveBeenCalledWith({
      center: [SECOND_ANCHOR.lon, SECOND_ANCHOR.lat],
      duration: 600,
    })
  })

  it('does not yank the view on an ordinary Raise while mounted (already-known active going inactive)', () => {
    const { rerender } = render(mapElement())
    expect(easeToMock).not.toHaveBeenCalled()

    // Raise: the anchor goes away, but anchorStateKnown was already true
    // throughout — this must not be mistaken for the unknown -> known
    // transition the effect above exists for.
    rerender(mapElement({ anchorLat: null, anchorLon: null, anchorSetAt: null }))

    expect(easeToMock).not.toHaveBeenCalled()
  })
})

// The session-change ease above also fits the swing circle to the new
// anchorage (fitRadiusZoom, lib/anchor-view.ts) whenever the map's real
// rendered size is available — every test above runs with an unmeasurable
// 0x0 container (jsdom has no layout) and so never sees a zoom key at all,
// proving the fallback is exactly today's center-only ease. This suite gives
// the container a real size to prove the zoom half of "ease to centre+zoom"
// actually fires too.
describe('AnchorWatchMap fits zoom to the container on a new session', () => {
  beforeEach(() => {
    localStorage.clear()
    easeToMock.mockClear()
    containerSize.width = 390
    containerSize.height = 500
  })

  afterEach(() => {
    containerSize.width = 0
    containerSize.height = 0
  })

  it('includes a fitted zoom in the easeTo call when the container size is known', () => {
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

    expect(easeToMock).toHaveBeenCalledTimes(1)
    const [call] = easeToMock.mock.calls
    expect(call[0]).toMatchObject({
      center: [SECOND_ANCHOR.lon, SECOND_ANCHOR.lat],
      duration: 600,
    })
    // radiusMeters is 34 (mapElement's default) at SECOND_ANCHOR's latitude,
    // fit to a 390x500 container — independently derived in anchor-view.test.ts.
    expect(call[0].zoom).toBeCloseTo(17.9487841248938, 6)

    // The fitted zoom is also stored tagged with the new session, same as
    // the centre.
    expect(JSON.parse(localStorage.getItem('anchor-watch-map-zoom')!)).toEqual({
      zoom: call[0].zoom,
      sessionId: SECOND_SESSION,
    })
  })
})
