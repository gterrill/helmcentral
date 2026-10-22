import { fireEvent, render, screen } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { saveImageryPreference } from '@/lib/passage-imagery-preference'
import { PassageMap } from '@/components/passages/passage-map'

vi.mock('maplibre-gl', () => ({ default: {} }))
vi.mock('@/lib/webgl', () => ({ hasWebGL2: () => true }))

interface LayerProps { id: string; beforeId?: string; minzoom?: number; paint?: Record<string, unknown> }
let order: string[] = []
let layers: LayerProps[] = []

vi.mock('react-map-gl/maplibre', async () => {
  const React = await import('react')
  return {
    Map: React.forwardRef(({ children }: { children?: React.ReactNode }, ref: React.Ref<unknown>) => {
      React.useImperativeHandle(ref, () => ({ getMap: () => ({ getSource: () => ({}) }), getCanvas: () => ({}) }))
      return <div data-testid="map-root">{children}</div>
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

  it('stacks overlay content under the toggle, in the same corner', () => {
    render(<PassageMap passages={passages} playedIds={new Set()} boat={null} isDarkTheme={false} overlay={<div data-testid="inset" />} />)
    const toggle = screen.getByRole('button', { name: 'Toggle satellite imagery' })
    expect(toggle.parentElement).toBe(screen.getByTestId('inset').parentElement)
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
