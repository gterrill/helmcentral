import { fireEvent, render, screen } from '@testing-library/react'
import type { ReactNode } from 'react'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import AssistantAnchorageMapImpl from '@/components/assistant-anchorage-map-impl'

import { anchorageCard, anchorageCardWithoutDepth } from './helpers/anchorage-fixture'

// MapLibre needs WebGL, which jsdom has not got. The stand-ins record what the
// card asks the map to draw: the sources, the layers and the first view.
let mapProps: Record<string, unknown> = {}
let sourceProps: Record<string, Record<string, unknown>> = {}
let layerProps: Record<string, Record<string, unknown>> = {}
vi.mock('react-map-gl/maplibre', () => ({
  Map: (props: { children: ReactNode } & Record<string, unknown>) => {
    mapProps = props
    return <div data-testid="map">{props.children}</div>
  },
  Source: (props: { id: string; children?: ReactNode } & Record<string, unknown>) => {
    sourceProps[props.id] = props
    return <div data-testid={`source-${props.id}`}>{props.children}</div>
  },
  Layer: (props: { id: string } & Record<string, unknown>) => {
    layerProps[props.id] = props
    return <i data-testid={`layer-${props.id}`} />
  },
}))
vi.mock('maplibre-gl', () => ({ default: {} }))
vi.mock('maplibre-gl/dist/maplibre-gl.css', () => ({}))
const webgl = vi.hoisted(() => ({ on: true }))
vi.mock('@/lib/webgl', () => ({ hasWebGL2: () => webgl.on }))
vi.mock('@/hooks/use-app-config', () => ({ useAppConfig: () => ({ ui: { distanceUnits: 'metric' } }) }))

const TOKENS = ['--primary', '--gauge-primary', '--foreground', '--background', '--muted-foreground', '--destructive']

beforeEach(() => {
  mapProps = {}
  sourceProps = {}
  layerProps = {}
  webgl.on = true
  for (const t of TOKENS) document.documentElement.style.setProperty(t, '10 20% 30%')
})

describe('AssistantAnchorageMapImpl', () => {
  it('fits the anchorage and the mile round it, with cooperative gestures', () => {
    render(<AssistantAnchorageMapImpl card={anchorageCard()} />)
    const view = mapProps.initialViewState as { bounds: number[][]; fitBoundsOptions: { padding: number } }
    expect(view.bounds[0][0]).toBeLessThan(view.bounds[1][0])
    expect(view.fitBoundsOptions.padding).toBeGreaterThan(0)
    expect(mapProps.cooperativeGestures).toBe(true)
  })

  it('draws the satellite imagery and the OpenSeaMap marks', () => {
    render(<AssistantAnchorageMapImpl card={anchorageCard()} />)
    expect(screen.getByTestId('source-world-imagery')).toBeInTheDocument()
    expect(screen.getByTestId('layer-openseamap-layer')).toBeInTheDocument()
  })

  it('draws 36 rays, the moorings with their labels, and the anchorage point', () => {
    render(<AssistantAnchorageMapImpl card={anchorageCard()} />)
    expect((sourceProps['anchorage-rays'].data as GeoJSON.FeatureCollection).features).toHaveLength(36)
    expect((sourceProps['anchorage-moorings'].data as GeoJSON.FeatureCollection).features).toHaveLength(23)
    for (const id of ['anchorage-rays-line', 'anchorage-mooring-dot', 'anchorage-mooring-label', 'anchorage-point-dot', 'anchorage-restricted-fill', 'anchorage-areas-line', 'anchorage-seabed-line']) {
      expect(screen.getByTestId(`layer-${id}`)).toBeInTheDocument()
    }
    expect((layerProps['anchorage-rays-line'].paint as Record<string, unknown>)['line-color']).toEqual(expect.arrayContaining(['match']))
  })

  it('draws the swing circle round the anchorage when a depth made one', () => {
    render(<AssistantAnchorageMapImpl card={anchorageCard()} />)
    const swing = sourceProps['anchorage-swing'].data as GeoJSON.FeatureCollection
    expect(swing.features).toHaveLength(1)
    expect(swing.features[0].geometry.type).toBe('Polygon')
    expect(screen.getByTestId('layer-anchorage-swing-edge')).toBeInTheDocument()
  })

  it('draws no swing circle without a depth', () => {
    render(<AssistantAnchorageMapImpl card={anchorageCardWithoutDepth()} />)
    expect((sourceProps['anchorage-swing'].data as GeoJSON.FeatureCollection).features).toHaveLength(0)
  })

  it('does not draw the shoreline', () => {
    render(<AssistantAnchorageMapImpl card={anchorageCard()} />)
    expect(Object.keys(sourceProps).filter((id) => /shore|coast/.test(id))).toEqual([])
  })

  it('keeps every map label at 12 px or larger', () => {
    render(<AssistantAnchorageMapImpl card={anchorageCard()} />)
    for (const [id, props] of Object.entries(layerProps)) {
      const size = (props.layout as Record<string, unknown> | undefined)?.['text-size']
      if (size !== undefined) expect(size, id).toBeGreaterThanOrEqual(12)
    }
  })

  it('takes the satellite off when asked, and keeps the imagery under the OpenSeaMap marks', () => {
    render(<AssistantAnchorageMapImpl card={anchorageCard()} />)
    expect(layerProps['world-imagery-layer'].beforeId).toBe('openseamap-layer')
    expect(layerProps['openseamap-layer'].beforeId).toBe('anchorage-restricted-fill')
    fireEvent.click(screen.getByRole('button', { name: 'Toggle satellite imagery' }))
    expect(screen.queryByTestId('source-world-imagery')).not.toBeInTheDocument()
  })

  it('shows the fallback without reading a theme token when WebGL2 is missing', () => {
    webgl.on = false
    for (const t of TOKENS) document.documentElement.style.removeProperty(t)
    render(<AssistantAnchorageMapImpl card={anchorageCard()} />)
    expect(screen.getByTestId('anchorage-map-fallback')).toHaveTextContent(/WebGL2/)
    expect(screen.queryByTestId('map')).not.toBeInTheDocument()
  })

  it('fails loudly when a theme token is not defined', () => {
    document.documentElement.style.removeProperty('--destructive')
    vi.spyOn(console, 'error').mockImplementation(() => {})
    expect(() => render(<AssistantAnchorageMapImpl card={anchorageCard()} />)).toThrow(/--destructive/)
  })
})
