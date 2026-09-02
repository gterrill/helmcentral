import { beforeEach, describe, expect, it, vi } from 'vitest'
import { render, screen } from '@testing-library/react'
import { AnchorWatchMap, resolveRadarEchoAvailability } from '@/components/anchor-watch-map'
import type { RadarInfo } from '@/hooks/use-radar-targets'
import type { RadarCapabilities } from '@/lib/radar-echo/legend'
import type { NearbyVessel } from '@/hooks/use-nearby-vessels'
import type { RodeMethodResult } from '@/lib/rode-plan'

// The toggle must never be a switch that silently does nothing: this suite
// covers the precedence chain resolveRadarEchoAvailability implements (no
// mayara address configured, no radar present, not transmitting (STANDBY),
// capabilities unreadable, stream disconnected), and that a disabled toggle
// actually carries its reason where the operator can see it.

const capabilities: RadarCapabilities = {
  spokesPerRevolution: 8192,
  maxSpokeLength: 1024,
  legend: { pixels: [] },
}

describe('resolveRadarEchoAvailability precedence', () => {
  const clear = {
    radarSource: 'mayara' as const,
    radars: [{ id: 'fur6424A', name: 'Furuno', transmitting: true }] as RadarInfo[],
    capabilitiesError: null,
    capabilitiesLoading: false,
    echoEnabled: false,
    echoStatus: 'idle' as const,
  }

  it('is available when every check passes', () => {
    expect(resolveRadarEchoAvailability(clear)).toEqual({ available: true, reason: null })
  })

  it('reports no mayara address configured first, even if radars/capabilities look fine', () => {
    expect(
      resolveRadarEchoAvailability({ ...clear, radarSource: 'disabled' }),
    ).toEqual({ available: false, reason: 'No mayara radar configured' })
  })

  it('reports no radar found when the source is live but nothing was discovered', () => {
    expect(
      resolveRadarEchoAvailability({ ...clear, radarSource: 'mayara-unreachable', radars: [] }),
    ).toEqual({ available: false, reason: 'No radar found' })
    expect(
      resolveRadarEchoAvailability({ ...clear, radars: [] }),
    ).toEqual({ available: false, reason: 'No radar found' })
  })

  it('reports STANDBY when the radar is present but not transmitting, ahead of a capabilities error', () => {
    expect(
      resolveRadarEchoAvailability({
        ...clear,
        radars: [{ id: 'fur6424A', name: 'Furuno', transmitting: false }],
        capabilitiesError: 'radar capabilities for fur6424A could not be parsed',
      }),
    ).toEqual({ available: false, reason: 'Radar in STANDBY' })
  })

  it('reports a loading state ahead of an error, and the error verbatim once it resolves', () => {
    expect(
      resolveRadarEchoAvailability({ ...clear, capabilitiesLoading: true, capabilitiesError: 'stale error from a previous id' }),
    ).toEqual({ available: false, reason: 'Loading radar capabilities…' })

    expect(
      resolveRadarEchoAvailability({ ...clear, capabilitiesError: 'radar capabilities request for fur6424A failed with status 502' }),
    ).toEqual({ available: false, reason: 'radar capabilities request for fur6424A failed with status 502' })
  })

  it('reports the stream as disconnected only while the overlay is actually enabled', () => {
    // Not enabled: a lingering 'disconnected' from a previous, intentional
    // toggle-off (use-radar-echo-stream.ts's release() sets exactly this
    // status) must not lock the toggle out of being turned back on.
    expect(
      resolveRadarEchoAvailability({ ...clear, echoEnabled: false, echoStatus: 'disconnected' }),
    ).toEqual({ available: true, reason: null })

    expect(
      resolveRadarEchoAvailability({ ...clear, echoEnabled: true, echoStatus: 'disconnected' }),
    ).toEqual({ available: false, reason: 'Radar stream disconnected' })

    expect(
      resolveRadarEchoAvailability({ ...clear, echoEnabled: true, echoStatus: 'reconnecting' }),
    ).toEqual({ available: false, reason: 'Radar stream disconnected' })

    expect(
      resolveRadarEchoAvailability({ ...clear, echoEnabled: true, echoStatus: 'connected' }),
    ).toEqual({ available: true, reason: null })
  })
})

// --- Rendering: the button itself must reflect resolveRadarEchoAvailability.

vi.mock('maplibre-gl', () => ({
  default: {},
}))

let mockMap: {
  isStyleLoaded: ReturnType<typeof vi.fn>
  getSource: ReturnType<typeof vi.fn>
  addSource: ReturnType<typeof vi.fn>
  removeSource: ReturnType<typeof vi.fn>
  getLayer: ReturnType<typeof vi.fn>
  addLayer: ReturnType<typeof vi.fn>
  removeLayer: ReturnType<typeof vi.fn>
  moveLayer: ReturnType<typeof vi.fn>
}

vi.mock('react-map-gl/maplibre', async () => {
  const React = await import('react')
  return {
    Map: React.forwardRef(({ children }: { children?: React.ReactNode }, ref: React.Ref<unknown>) => {
      React.useImperativeHandle(ref, () => ({
        getCanvas: () => ({ style: { cursor: 'grab' } }),
        getZoom: () => 14,
        easeTo: vi.fn(),
        getMap: () => mockMap,
      }))
      return <div data-testid="map-root">{children}</div>
    }),
    Marker: ({ children, onClick }: { children?: React.ReactNode; onClick?: () => void }) => (
      <div onClick={onClick}>{children}</div>
    ),
    Source: ({ children, id }: { children?: React.ReactNode; id: string }) => (
      <div data-testid={`source-${id}`}>{children}</div>
    ),
    Layer: ({ id }: { id: string }) => <div data-testid={`layer-${id}`} />,
  }
})

vi.mock('@/hooks/use-radar-capabilities', () => ({
  useRadarCapabilities: vi.fn(() => ({ capabilities: null, palette: null, error: null, loading: false })),
}))

import { useRadarCapabilities } from '@/hooks/use-radar-capabilities'

const scopeRecommendation: RodeMethodResult = {
  id: 'ratio',
  label: 'Ratio Method',
  recommendedRodeM: 30,
  scopeRatio: 5,
  note: 'note',
}

const defaultAisVessels: NearbyVessel[] = []

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
      scopeRecommendation={scopeRecommendation}
      isImperial={false}
      vesselTrail={() => []}
      aisVessels={defaultAisVessels}
      aisTrails={() => new Map()}
      radarTargets={[]}
      isDarkTheme={false}
      onAnchorReposition={() => undefined}
      onRadiusChange={() => undefined}
      {...overrides}
    />
  )
}

function radarButton() {
  return screen.getByLabelText('Toggle radar echo overlay')
}

describe('AnchorWatchMap radar echo toggle availability', () => {
  beforeEach(() => {
    mockMap = {
      isStyleLoaded: vi.fn(() => true),
      getSource: vi.fn(() => undefined),
      addSource: vi.fn(),
      removeSource: vi.fn(),
      getLayer: vi.fn(() => undefined),
      addLayer: vi.fn(),
      removeLayer: vi.fn(),
      moveLayer: vi.fn(),
    }
    vi.mocked(useRadarCapabilities).mockReturnValue({ capabilities: null, palette: null, error: null, loading: false })
  })

  it('disables the toggle with no mayara address configured at all (the default props)', () => {
    render(mapElement())

    expect(radarButton()).toBeDisabled()
    expect(radarButton()).toHaveAttribute('title', 'No mayara radar configured')
  })

  it('disables the toggle with mayara reachable but no radar found', () => {
    render(mapElement({ radarSource: 'mayara' }))

    expect(radarButton()).toBeDisabled()
    expect(radarButton()).toHaveAttribute('title', 'No radar found')
  })

  it('disables the toggle and shows STANDBY for a radar that is not transmitting — and never mounts the layer, even if toggled on', () => {
    render(
      mapElement({
        radars: [{ id: 'fur6424A', name: 'Furuno', transmitting: false }],
        radarSource: 'mayara',
        showRadarEcho: true,
      }),
    )

    expect(radarButton()).toBeDisabled()
    expect(radarButton()).toHaveAttribute('title', 'Radar in STANDBY')
    expect(mockMap.addLayer).not.toHaveBeenCalled()
    expect(mockMap.addSource).not.toHaveBeenCalled()
    // A standby radar has nothing to show capabilities for either — no
    // wasted round trip to a picture that couldn't be drawn regardless.
    expect(useRadarCapabilities).toHaveBeenLastCalledWith(null)
  })

  it('enables the toggle once a transmitting radar has readable capabilities, with no title needed', () => {
    vi.mocked(useRadarCapabilities).mockReturnValue({ capabilities, palette: new Uint32Array(256), error: null, loading: false })

    render(
      mapElement({
        radars: [{ id: 'fur6424A', name: 'Furuno', transmitting: true }],
        radarSource: 'mayara',
      }),
    )

    expect(radarButton()).not.toBeDisabled()
    expect(radarButton()).not.toHaveAttribute('title')
  })

  it('disables the toggle and surfaces the exact backend error when capabilities fail to parse', () => {
    vi.mocked(useRadarCapabilities).mockReturnValue({
      capabilities: null,
      palette: null,
      error: 'radar capabilities for fur6424A could not be parsed',
      loading: false,
    })

    render(
      mapElement({
        radars: [{ id: 'fur6424A', name: 'Furuno', transmitting: true }],
        radarSource: 'mayara',
      }),
    )

    expect(radarButton()).toBeDisabled()
    expect(radarButton()).toHaveAttribute('title', 'radar capabilities for fur6424A could not be parsed')
  })
})
