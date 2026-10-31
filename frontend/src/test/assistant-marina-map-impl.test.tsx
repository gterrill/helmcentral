import { fireEvent, render, screen } from '@testing-library/react'
import type { ReactNode } from 'react'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import AssistantMarinaMapImpl from '@/components/assistant-marina-map-impl'

import { marinaCard } from './helpers/marina-fixture'

// MapLibre needs WebGL, which jsdom has not got. The stand-ins record what the
// card asks the map to draw: the sources, the layers and the first view.
let mapProps: Record<string, unknown> = {}
vi.mock('react-map-gl/maplibre', () => ({
  Map: (props: { children: ReactNode } & Record<string, unknown>) => {
    mapProps = props
    return <div data-testid="map">{props.children}</div>
  },
  Source: ({ id, children }: { id: string; children?: ReactNode }) => (
    <div data-testid={`source-${id}`}>{children}</div>
  ),
  Layer: (props: { id: string } & Record<string, unknown>) => {
    layerProps[props.id] = props
    return <i data-testid={`layer-${props.id}`} />
  },
}))
let layerProps: Record<string, Record<string, unknown>> = {}
vi.mock('maplibre-gl', () => ({ default: {} }))
vi.mock('maplibre-gl/dist/maplibre-gl.css', () => ({}))
const webgl = vi.hoisted(() => ({ on: true }))
vi.mock('@/lib/webgl', () => ({ hasWebGL2: () => webgl.on }))

beforeEach(() => {
  mapProps = {}
  layerProps = {}
  webgl.on = true
  for (const t of ['--primary', '--gauge-primary', '--foreground', '--background', '--muted-foreground']) {
    document.documentElement.style.setProperty(t, '10 20% 30%')
  }
})

describe('AssistantMarinaMapImpl', () => {
  it('fits the outline and the pontoons with padding', () => {
    render(<AssistantMarinaMapImpl card={marinaCard()} />)
    const view = mapProps.initialViewState as { bounds: number[][]; fitBoundsOptions: { padding: number } }
    expect(view.bounds[0][0]).toBeLessThan(view.bounds[1][0])
    expect(view.fitBoundsOptions.padding).toBeGreaterThan(0)
    expect(mapProps.cooperativeGestures).toBe(true)
  })

  it('draws the satellite imagery and the OpenSeaMap marks, not marks of its own', () => {
    render(<AssistantMarinaMapImpl card={marinaCard()} />)
    expect(screen.getByTestId('source-world-imagery')).toBeInTheDocument()
    expect(screen.getByTestId('layer-openseamap-layer')).toBeInTheDocument()
    expect(screen.queryByTestId(/layer-marina-mark/)).not.toBeInTheDocument()
  })

  it('draws the outline, the pontoons with their names and the facilities', () => {
    render(<AssistantMarinaMapImpl card={marinaCard()} />)
    for (const id of ['marina-outline-line', 'marina-pontoons-line', 'marina-pontoon-label', 'marina-facility-dot']) {
      expect(screen.getByTestId(`layer-${id}`)).toBeInTheDocument()
    }
  })

  it('names each facility at the first view, so a dot is never unexplained', () => {
    render(<AssistantMarinaMapImpl card={marinaCard()} />)
    expect(layerProps['marina-facility-label'].minzoom).toBeUndefined()
  })

  it('keeps every map label at 12 px or larger', () => {
    render(<AssistantMarinaMapImpl card={marinaCard()} />)
    fireEvent.click(screen.getByRole('button', { name: 'Toggle shops and services ashore' }))
    for (const [id, props] of Object.entries(layerProps)) {
      const size = (props.layout as Record<string, unknown> | undefined)?.['text-size']
      if (size !== undefined) expect(size, id).toBeGreaterThanOrEqual(12)
    }
  })

  it('keeps shops and services ashore off until asked', () => {
    render(<AssistantMarinaMapImpl card={marinaCard()} />)
    expect(screen.queryByTestId('layer-marina-ashore-dot')).not.toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Toggle shops and services ashore' }))
    expect(screen.getByTestId('layer-marina-ashore-dot')).toBeInTheDocument()
  })

  it('offers no ashore button when the card has no services to place', () => {
    const base = marinaCard()
    render(<AssistantMarinaMapImpl card={marinaCard({ geometry: { ...base.geometry, ashore: [] } })} />)
    expect(screen.queryByRole('button', { name: 'Toggle shops and services ashore' })).not.toBeInTheDocument()
  })

  it('takes the satellite off when asked', () => {
    render(<AssistantMarinaMapImpl card={marinaCard()} />)
    fireEvent.click(screen.getByRole('button', { name: 'Toggle satellite imagery' }))
    expect(screen.queryByTestId('source-world-imagery')).not.toBeInTheDocument()
  })

  it('keeps the imagery under the OpenSeaMap marks, also after it is toggled off and on', () => {
    render(<AssistantMarinaMapImpl card={marinaCard()} />)
    const toggle = screen.getByRole('button', { name: 'Toggle satellite imagery' })
    fireEvent.click(toggle)
    fireEvent.click(toggle)
    expect(layerProps['world-imagery-layer'].beforeId).toBe('openseamap-layer')
    expect(layerProps['openseamap-layer'].beforeId).toBe('marina-outline-casing')
  })

  it('shows the fallback without reading a theme token when WebGL2 is missing', () => {
    webgl.on = false
    for (const t of ['--primary', '--gauge-primary', '--foreground', '--background', '--muted-foreground']) {
      document.documentElement.style.removeProperty(t)
    }
    render(<AssistantMarinaMapImpl card={marinaCard()} />)
    expect(screen.getByTestId('marina-map-fallback')).toBeInTheDocument()
    expect(screen.queryByTestId('map')).not.toBeInTheDocument()
  })

  it('fails loudly when a theme token is not defined', () => {
    document.documentElement.style.removeProperty('--gauge-primary')
    vi.spyOn(console, 'error').mockImplementation(() => {})
    expect(() => render(<AssistantMarinaMapImpl card={marinaCard()} />)).toThrow(/--gauge-primary/)
  })
})
