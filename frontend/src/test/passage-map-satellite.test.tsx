import { fireEvent, render, screen } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { saveImageryPreference } from '@/lib/passage-imagery-preference'
import { insetZoom } from '@/lib/passage-export-style'
import { InsetMap, PassageMap } from '@/components/passages/passage-map'

vi.mock('maplibre-gl', () => ({ default: {} }))
vi.mock('@/lib/webgl', () => ({ hasWebGL2: () => true }))

interface LayerProps { id: string; beforeId?: string; minzoom?: number; paint?: Record<string, unknown> }
let order: string[] = []
let layers: LayerProps[] = []
const jumpTo = vi.hoisted(() => vi.fn())

vi.mock('react-map-gl/maplibre', async () => {
  const React = await import('react')
  return {
    Map: React.forwardRef(({ children, initialViewState }: { children?: React.ReactNode; initialViewState?: { zoom?: number } }, ref: React.Ref<unknown>) => {
      React.useImperativeHandle(ref, () => ({ getMap: () => ({ getSource: () => ({}) }), getCanvas: () => ({}), jumpTo }))
      return <div data-testid="map-root" data-zoom={initialViewState?.zoom}>{children}</div>
    }),
    Marker: ({ children }: { children?: React.ReactNode }) => <div>{children}</div>,
    Source: ({ id, children }: { id: string; children?: React.ReactNode }) => <div data-testid={`source-${id}`}>{children}</div>,
    Layer: (props: LayerProps) => {
      layers.push(props)
      if (!order.includes(props.id)) order.push(props.id)
      return <div data-testid={`layer-${props.id}`} data-before-id={props.beforeId} />
    },
  }
})

vi.mock('@/components/map-place-labels', () => ({
  MapPlaceLabels: ({ overImagery }: { overImagery: boolean }) => <div data-testid="place-labels" data-over-imagery={String(overImagery)} />,
  warnIfBaseVectorSourceMissing: () => {},
}))

const passages = [{ id: 'p1', segments: [[{ t: 0, lat: -20, lon: 148 }, { t: 60, lat: -20.1, lon: 148.1 }]] }]

function renderMap() {
  return render(<PassageMap passages={passages} playedIds={new Set()} boat={null} isDarkTheme={false} />)
}

// The preference keeps an in-page copy for browsers without storage; reset it.
beforeEach(() => { saveImageryPreference(true); localStorage.clear(); layers = []; order = [] })

describe('PassageMap satellite imagery', () => {
  it('is on by default, with the toggle pressed', () => {
    renderMap()
    expect(screen.getByRole('button', { name: 'Toggle satellite imagery' }).getAttribute('aria-pressed')).toBe('true')
    expect(screen.getByTestId('layer-world-imagery-layer')).toBeInTheDocument()
  })

  it('puts imagery under the seamarks and the passage lines', () => {
    renderMap()
    expect(screen.getByTestId('layer-world-imagery-layer').dataset.beforeId).toBe('openseamap-layer')
    const imagery = layers.find((l) => l.id === 'world-imagery-layer')
    expect(imagery?.minzoom).toBe(9)
  })

  it('draws a white casing under each passage line only while imagery is on', () => {
    renderMap()
    const casing = screen.getByTestId('layer-passage-casing-p1')
    expect(casing.dataset.beforeId).toBe('passage-line-p1')
    expect(screen.getByTestId('place-labels').dataset.overImagery).toBe('true')
    fireEvent.click(screen.getByRole('button', { name: 'Toggle satellite imagery' }))
    expect(screen.queryByTestId('layer-passage-casing-p1')).toBeNull()
    expect(screen.queryByTestId('layer-world-imagery-layer')).toBeNull()
    expect(screen.getByTestId('place-labels').dataset.overImagery).toBe('false')
  })

  it('fades the casing in with the imagery, not just with the preference', () => {
    renderMap()
    const casing = layers.find((l) => l.id === 'passage-casing-p1')
    expect(casing?.minzoom).toBe(9)
    expect(JSON.stringify(casing?.paint?.['line-opacity'])).toBe(JSON.stringify(['interpolate', ['linear'], ['zoom'], 9, 0, 10, 0.5]))
  })

  it('adds every layer after the layer it is placed before', () => {
    renderMap()
    const withTarget = layers.filter((l) => l.beforeId)
    expect(withTarget.length).toBeGreaterThan(0)
    for (const l of withTarget) {
      expect(order.indexOf(l.beforeId as string)).toBeGreaterThanOrEqual(0)
      expect(order.indexOf(l.beforeId as string)).toBeLessThan(order.indexOf(l.id))
    }
  })

  it('stacks the overview under the toggle in the top right corner by default', () => {
    render(<PassageMap passages={passages} playedIds={new Set()} boat={null} isDarkTheme={false} inset={<div data-testid="inset" />} />)
    const toggle = screen.getByRole('button', { name: 'Toggle satellite imagery' })
    // The toggle sits in a row beside the 3D button; the row and the overview share the corner.
    expect(toggle.parentElement?.parentElement).toBe(screen.getByTestId('inset').parentElement)
  })

  it.each([
    ['top-left', ['left-2', 'top-2']],
    ['top-right', ['right-2', 'top-2']],
    ['bottom-left', ['left-2', 'bottom-8']],
    ['bottom-right', ['right-2', 'bottom-8']],
  ] as const)('puts the overview in the %s corner, away from the toggles', (corner, classes) => {
    render(<PassageMap passages={passages} playedIds={new Set()} boat={null} isDarkTheme={false} inset={<div data-testid="inset" />} insetCorner={corner} />)
    expect(screen.getAllByTestId('inset')).toHaveLength(1)
    const holder = screen.getByTestId('inset').parentElement as HTMLElement
    for (const c of classes) expect(holder.className).toContain(c)
    const toggle = screen.getByRole('button', { name: 'Toggle satellite imagery' })
    if (corner !== 'top-right') expect(toggle.parentElement?.parentElement).not.toBe(holder)
  })

  it('remembers the choice under its own key, not the anchor map one', () => {
    localStorage.setItem('anchorWatch.imagery.enabled', 'false')
    renderMap()
    expect(screen.getByTestId('layer-world-imagery-layer')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Toggle satellite imagery' }))
    expect(localStorage.getItem('passages.satelliteImagery')).toBe('0')
    expect(localStorage.getItem('anchorWatch.imagery.enabled')).toBe('false')
  })

  it('starts off when the stored choice is off', () => {
    localStorage.setItem('passages.satelliteImagery', '0')
    renderMap()
    expect(screen.queryByTestId('layer-world-imagery-layer')).toBeNull()
    expect(screen.getByRole('button', { name: 'Toggle satellite imagery' }).getAttribute('aria-pressed')).toBe('false')
  })
})

describe('InsetMap zoom', () => {
  const bounds: [number, number, number, number] = [148, -20.4, 149, -19.9]
  const zoomOf = (offset?: number) => {
    const { unmount } = render(<InsetMap bounds={bounds} isDarkTheme={false} zoomOffset={offset} />)
    const z = Number(screen.getByTestId('map-root').dataset.zoom)
    unmount()
    return z
  }

  it('starts from the same automatic zoom the export uses, then adds the steps', () => {
    const auto = insetZoom(bounds)
    expect(zoomOf()).toBeCloseTo(auto, 5)
    expect(zoomOf(0)).toBeCloseTo(auto, 5)
    expect(zoomOf(2)).toBeCloseTo(auto + 2, 5)
    expect(zoomOf(-3)).toBeCloseTo(auto - 3, 5)
  })

  it('stays inside zoom 0 to 12', () => {
    expect(zoomOf(30)).toBe(12)
    expect(zoomOf(-30)).toBe(0)
  })

  it('moves the map it already has when the step changes, rather than drawing a new one', () => {
    jumpTo.mockClear()
    const { rerender } = render(<InsetMap bounds={bounds} isDarkTheme={false} zoomOffset={0} />)
    const first = screen.getByTestId('map-root')
    jumpTo.mockClear()
    rerender(<InsetMap bounds={bounds} isDarkTheme={false} zoomOffset={1} />)
    expect(screen.getByTestId('map-root')).toBe(first)
    expect(jumpTo).toHaveBeenCalledTimes(1)
    const arg = jumpTo.mock.calls[0][0] as { zoom: number; center: [number, number] }
    expect(arg.zoom).toBeCloseTo(insetZoom(bounds) + 1, 5)
    expect(arg.center[0]).toBeCloseTo(148.5, 5)
  })
})
