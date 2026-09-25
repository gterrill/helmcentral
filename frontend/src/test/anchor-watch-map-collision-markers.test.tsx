import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { vi } from 'vitest'
import { AnchorWatchMap } from '@/components/anchor-watch-map'
import type { AlarmState } from '@/hooks/use-alarms'
import type { NearbyVessel } from '@/hooks/use-nearby-vessels'
import type { RodeMethodResult } from '@/lib/rode-plan'

// Same maplibre-gl / react-map-gl stubs as anchor-watch-map-radar-markers.test.tsx.
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

const SPLURGE_ID = 'urn:mrn:imo:mmsi:100000001'
const WANDERER_ID = 'urn:mrn:imo:mmsi:100000002'
const KESTREL_ID = 'urn:mrn:imo:mmsi:100000003'

const aisVessels: NearbyVessel[] = [
  { id: SPLURGE_ID, name: 'SPLURGE', lat: -25.2939, lon: 152.9103, range_m: 61, age_seconds: 5 },
  { id: WANDERER_ID, name: 'WANDERER', lat: -25.2949, lon: 152.9113, range_m: 140, age_seconds: 4 },
  { id: KESTREL_ID, name: 'KESTREL', lat: -25.2929, lon: 152.9093, range_m: 210, age_seconds: 6 },
]

const scopeRecommendation: RodeMethodResult = {
  id: 'ratio',
  label: 'Ratio Method',
  recommendedRodeM: 30,
  scopeRatio: 5,
  note: 'Depth 3.2 m (sounder only) · Wind 15 kts · 5:1',
}

function renderMap(aisCollisionAlarms?: ReadonlyMap<string, AlarmState>) {
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
      aisCollisionAlarms={aisCollisionAlarms}
      isDarkTheme={false}
      showImageryLayer
      onImageryToggle={() => undefined}
      onFullscreen={() => undefined}
    />,
  )
}

function circleFor(button: HTMLElement): HTMLElement {
  const circle = button.querySelector('.rounded-full')
  if (!circle) throw new Error('no circle inside AIS marker')
  return circle as HTMLElement
}

describe('AnchorWatchMap AIS collision-alarm markers', () => {
  it('colours a warn-tier target red, with no alarm-tier ring', () => {
    renderMap(new Map([[SPLURGE_ID, 'warn']]))

    const button = screen.getByRole('button', { name: 'AIS vessel: SPLURGE, collision warning' })
    const circle = circleFor(button)
    expect(circle.className).toContain('bg-red-600')
    expect(circle.className).not.toContain('bg-amber-500/90')
    expect(circle.className).not.toContain('ring-red-600/50')
  })

  it('colours an alarm-tier target red with the alarm-tier ring', () => {
    renderMap(new Map([[SPLURGE_ID, 'warn'], [KESTREL_ID, 'alarm']]))

    const button = screen.getByRole('button', { name: 'AIS vessel: KESTREL, collision alarm' })
    const circle = circleFor(button)
    expect(circle.className).toContain('bg-red-600')
    expect(circle.className).toContain('ring-red-600/50')
  })

  it('leaves a vessel with no collision alarm amber and its label unchanged', () => {
    renderMap(new Map([[SPLURGE_ID, 'warn'], [KESTREL_ID, 'alarm']]))

    const button = screen.getByRole('button', { name: 'AIS vessel: WANDERER' })
    const circle = circleFor(button)
    expect(circle.className).toContain('bg-amber-500/90')
    expect(circle.className).not.toContain('bg-red-600')
  })

  it('renders every marker amber with unchanged labels when the prop is omitted', () => {
    renderMap(undefined)

    for (const vessel of aisVessels) {
      const button = screen.getByRole('button', { name: `AIS vessel: ${vessel.name}` })
      const circle = circleFor(button)
      expect(circle.className).toContain('bg-amber-500/90')
      expect(circle.className).not.toContain('bg-red-600')
    }
    expect(document.querySelectorAll('[aria-label^="AIS vessel:"]')).toHaveLength(3)
  })

  it('ignores a vessel id in the alarm map that has no matching AIS target', () => {
    renderMap(new Map([[SPLURGE_ID, 'warn'], ['urn:mrn:imo:mmsi:999999999', 'alarm']]))

    expect(document.querySelectorAll('[aria-label^="AIS vessel:"]')).toHaveLength(3)
  })
})
