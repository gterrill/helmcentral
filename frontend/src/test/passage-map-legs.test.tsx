import { render } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { PASSAGE_COLOURS, PassageMap } from '@/components/passages/passage-map'
import { drawnPieces, passageLegs } from '@/lib/passage-legs'
import { SPEED_RAMP_LIGHT, speedFrameAt, speedTrack } from '@/lib/replay-speed'

vi.mock('maplibre-gl', () => ({ default: {} }))
vi.mock('@/lib/webgl', () => ({ hasWebGL2: () => true }))

const { layers, sources } = vi.hoisted(() => ({
  layers: [] as { id: string; paint: Record<string, unknown> }[],
  sources: [] as { id: string; data: GeoJSON.Feature<GeoJSON.MultiLineString> }[],
}))

vi.mock('react-map-gl/maplibre', async () => {
  const React = await import('react')
  const map = {
    getSource: () => ({}), easeTo: vi.fn(), fitBounds: vi.fn(), jumpTo: vi.fn(), once: vi.fn(), setSky: vi.fn(),
    getCanvasContainer: () => document.createElement('div'), on: vi.fn(), off: vi.fn(),
    cameraForBounds: () => ({ center: { lng: 148.05, lat: -20.05 }, zoom: 9 }),
    getContainer: () => ({ clientWidth: 800, clientHeight: 400 }),
    getLayer: () => undefined, addLayer: vi.fn(), removeLayer: vi.fn(), triggerRepaint: vi.fn(),
  }
  return {
    Map: React.forwardRef((props: { children?: React.ReactNode }, ref: React.Ref<unknown>) => {
      React.useImperativeHandle(ref, () => ({ getMap: () => map, getCanvas: () => ({}) }))
      return <div>{props.children}</div>
    }),
    Marker: ({ children }: { children?: React.ReactNode }) => <div>{children}</div>,
    Source: ({ id, data, children }: { id: string; data: GeoJSON.Feature<GeoJSON.MultiLineString>; children?: React.ReactNode }) => {
      sources.push({ id, data })
      return <div>{children}</div>
    },
    Layer: (p: { id: string; paint: Record<string, unknown> }) => { layers.push({ id: p.id, paint: p.paint }); return null },
  }
})
vi.mock('@/components/map-place-labels', () => ({ MapPlaceLabels: () => null, warnIfBaseVectorSourceMissing: () => {} }))

// One passage, fixes every 10 s from t=0 to t=100.
const seg = Array.from({ length: 11 }, (_, i) => ({ t: i * 10, lat: -20 - i * 0.01, lon: 148 + i * 0.01 }))
const passages = [{ id: 'p1', segments: [seg] }]
const legs = passageLegs([{ id: 'p1', startMs: 0, endMs: 100_000 }], [{ t_ms: 40_000, starts_leg: true }])

const lineColours = () => layers.filter((l) => l.id.startsWith('passage-line-')).map((l) => [l.id, l.paint['line-color']])

beforeEach(() => { layers.length = 0; sources.length = 0 })

describe('PassageMap legs', () => {
  it('draws one passage as one line in the first colour without legs', () => {
    render(<PassageMap passages={passages} playedIds={new Set()} boat={null} isDarkTheme={false} />)
    expect(lineColours()).toEqual([['passage-line-p1', PASSAGE_COLOURS[0]]])
  })

  it('draws a line per leg, each in its own colour, meeting at the join', () => {
    render(<PassageMap passages={passages} legs={legs} playedIds={new Set()} boat={null} isDarkTheme={false} />)
    expect(lineColours()).toEqual([['passage-line-p1-0', PASSAGE_COLOURS[0]], ['passage-line-p1-1', PASSAGE_COLOURS[1]]])
    const a = sources.find((s) => s.id === 'passage-p1-0')!.data.geometry.coordinates[0]
    const b = sources.find((s) => s.id === 'passage-p1-1')!.data.geometry.coordinates[0]
    expect(a[a.length - 1]).toEqual(b[0])
    expect(a).toHaveLength(5)
    expect(b).toHaveLength(7)
  })

  it('draws the sailed overlay per leg, only the reached part, coloured by speed', () => {
    const sailedTo = new Map([['p1', { at: 20, end: { lat: -20.02, lon: 148.02 } }]])
    const pieces = drawnPieces(passages, legs)
    const speed = speedFrameAt(pieces, speedTrack(pieces)!, sailedTo, SPEED_RAMP_LIGHT, null, { zoom: 12 })
    render(<PassageMap passages={passages} legs={legs} playedIds={new Set()} boat={{ lat: -20.02, lon: 148.02 }} sailedTo={sailedTo} speed={speed} isDarkTheme={false} />)
    // The unsailed road ahead keeps each leg's colour; the part sailed is the speed ramp, leg by leg.
    const sailed = layers.filter((l) => l.id.startsWith('passage-speed-line-'))
    expect(sailed.map((l) => l.id)).toEqual(['passage-speed-line-p1-0-0'])
    expect(sailed[0].paint['line-gradient']).toBeDefined()
    expect(sources.find((s) => s.id === 'passage-speed-p1-0-0')!.data.geometry.coordinates).toHaveLength(3)
    expect(sources.find((s) => s.id === 'passage-speed-p1-1-0')).toBeUndefined()
  })
})
