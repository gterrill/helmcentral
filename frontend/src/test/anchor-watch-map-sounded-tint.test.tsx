import { render } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { AnchorWatchMap } from '@/components/anchor-watch-map'
import type { SoundedCellsAnswer } from '@/lib/sounded-cells'

vi.mock('maplibre-gl', () => ({ default: {} }))

let answer: SoundedCellsAnswer | null = null
const enabledCalls: boolean[] = []
vi.mock('@/hooks/use-anchor-sounded-cells', () => ({
  useAnchorSoundedCells: ({ enabled }: { enabled: boolean }) => {
    enabledCalls.push(enabled)
    return { answer: enabled ? answer : null, error: null }
  },
}))
let webgl2 = true
vi.mock('@/lib/webgl', () => ({ hasWebGL2: () => webgl2 }))

let sourceData: Record<string, unknown> = {}
vi.mock('react-map-gl/maplibre', async () => {
  const React = await import('react')
  return {
    Map: React.forwardRef(({ children }: { children?: React.ReactNode }, ref: React.Ref<unknown>) => {
      React.useImperativeHandle(ref, () => ({ getCanvas: () => ({ style: {} }), getZoom: () => 14, easeTo: vi.fn() }))
      return <div data-testid="map-root">{children}</div>
    }),
    Marker: ({ children }: { children?: React.ReactNode }) => <div>{children}</div>,
    Source: ({ children, id, data }: { children?: React.ReactNode; id: string; data?: unknown }) => {
      sourceData[id] = data
      return <div data-testid={`source-${id}`}>{children}</div>
    },
    Layer: ({ id }: { id: string }) => <div data-testid={`layer-${id}`} />,
  }
})

function element(overrides: Partial<React.ComponentProps<typeof AnchorWatchMap>> = {}) {
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
      isImperial={false}
      vesselTrail={[]}
      aisVessels={[]}
      aisTrails={new Map()}
      isDarkTheme={false}
      {...overrides}
    />
  )
}

const ready: SoundedCellsAnswer = {
  status: 'ready',
  frame: { originLat: -25.2939, originLon: 152.9103, cellM: 2.5 },
  cells: [[0, 0]],
}

describe('AnchorWatchMap sounded tint', () => {
  beforeEach(() => {
    sourceData = {}
    enabledCalls.length = 0
    answer = null
    webgl2 = true
  })

  it('draws the tint over the swing circle once the sounded cells are ready', () => {
    answer = ready
    const { getByTestId } = render(element())
    expect(getByTestId('source-sounded-tint')).toBeTruthy()
    expect(getByTestId('layer-sounded-tint-fill')).toBeTruthy()
    const data = sourceData['sounded-tint'] as GeoJSON.Feature<GeoJSON.MultiPolygon>
    expect(data.geometry.coordinates.length).toBeGreaterThan(0)
  })

  it('draws nothing with no watch, and does not ask the server', () => {
    answer = ready
    const { queryByTestId } = render(element({ anchorLat: null, anchorLon: null }))
    expect(queryByTestId('source-sounded-tint')).toBeNull()
    expect(enabledCalls.every((w) => w === false)).toBe(true)
  })

  it('draws nothing while the server has not worked the cells out for this watch', () => {
    answer = { status: 'not_ready' }
    const { queryByTestId } = render(element())
    expect(queryByTestId('source-sounded-tint')).toBeNull()
  })

  it('draws nothing from cells measured from another anchor position', () => {
    answer = { ...ready, frame: { ...ready.frame, originLat: -25.3 } } as SoundedCellsAnswer
    const { queryByTestId } = render(element())
    expect(queryByTestId('source-sounded-tint')).toBeNull()
  })

  it('does not ask for the cells where the map cannot draw', () => {
    answer = ready
    webgl2 = false
    render(element())
    expect(enabledCalls.every((w) => w === false)).toBe(true)
  })

  it('hides the tint during Adjust and brings it back after', () => {
    answer = ready
    const props = { onAdjust: () => {}, onAdjustCancelKey: () => {} }
    const { queryByTestId, rerender } = render(element({ ...props, adjustActive: false }))
    expect(queryByTestId('source-sounded-tint')).not.toBeNull()
    rerender(element({ ...props, adjustActive: true }))
    expect(queryByTestId('source-sounded-tint')).toBeNull()
    rerender(element({ ...props, adjustActive: false }))
    expect(queryByTestId('source-sounded-tint')).not.toBeNull()
  })
})
