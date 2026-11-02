import { describe, expect, it, vi } from 'vitest'

import { addSpecLayer, createOverlays, type OverlayMap } from '@/lib/replay-overlays'
import type { OverlaySpec } from '@/lib/replay-map-style'

// The sailed track, speed colours and wake on a plain map: added when first
// wanted, brought up to date only when they changed, taken off when not wanted.

function fakeMap() {
  const sources = new Map<string, { setData: ReturnType<typeof vi.fn> }>()
  const layers: string[] = []
  const map = {
    addSource: vi.fn((id: string) => { sources.set(id, { setData: vi.fn() }) }),
    addLayer: vi.fn((l: { id: string }, before?: string) => { layers.push(l.id); void before }),
    removeLayer: vi.fn((id: string) => { layers.splice(layers.indexOf(id), 1) }),
    removeSource: vi.fn((id: string) => { sources.delete(id) }),
    getSource: vi.fn((id: string) => sources.get(id)),
    setPaintProperty: vi.fn(),
  }
  return { map: map as unknown as OverlayMap & typeof map, sources, layers }
}

const line = (coords: [number, number][]): GeoJSON.Feature => ({ type: 'Feature', properties: {}, geometry: { type: 'LineString', coordinates: coords } })
const overlay = (id: string, data: GeoJSON.Feature, gradient?: unknown): OverlaySpec => ({
  source: { id, data, lineMetrics: true },
  layers: [{ id: `${id}-line`, type: 'line', layout: { 'line-cap': 'round', 'line-join': 'round' }, paint: gradient === undefined ? { 'line-width': 3 } : { 'line-gradient': gradient } }],
})

describe('the overlays on a map', () => {
  it('adds a source and its layers once, with the layer on that source', () => {
    const { map, layers } = fakeMap()
    const o = createOverlays(map)
    const data = line([[0, 0], [1, 1]])
    o.want(overlay('a', data))
    o.settle()
    o.want(overlay('a', data))
    o.settle()
    expect(map.addSource).toHaveBeenCalledTimes(1)
    expect(map.addSource).toHaveBeenCalledWith('a', { type: 'geojson', data, lineMetrics: true })
    expect(layers).toEqual(['a-line'])
    expect(map.addLayer.mock.calls[0][0]).toMatchObject({ id: 'a-line', source: 'a' })
  })

  it('sets the data only when the line moved, and the gradient only when its expression changed', () => {
    const { map, sources } = fakeMap()
    const o = createOverlays(map)
    const a = line([[0, 0], [1, 1]])
    o.want(overlay('a', a, ['interpolate', 0]))
    o.settle()
    o.want(overlay('a', a, ['interpolate', 0]))
    o.settle()
    expect(sources.get('a')?.setData).not.toHaveBeenCalled()
    expect(map.setPaintProperty).not.toHaveBeenCalled()
    o.want(overlay('a', line([[0, 0], [1, 1], [2, 2]]), ['interpolate', 1]))
    o.settle()
    expect(sources.get('a')?.setData).toHaveBeenCalledTimes(1)
    expect(map.setPaintProperty).toHaveBeenCalledWith('a-line', 'line-gradient', ['interpolate', 1])
  })

  it('takes off an overlay that a frame no longer wants, and leaves the ones it does', () => {
    const { map, layers, sources } = fakeMap()
    const o = createOverlays(map)
    o.want(overlay('a', line([[0, 0], [1, 1]])))
    o.want(overlay('b', line([[0, 0], [2, 2]])))
    o.settle()
    o.want(overlay('b', line([[0, 0], [2, 2]])))
    o.settle()
    expect(layers).toEqual(['b-line'])
    expect([...sources.keys()]).toEqual(['b'])
  })

  it('hands a layer\'s own place in the stack to the map as the second argument', () => {
    const { map } = fakeMap()
    addSpecLayer(map, { id: 'x', type: 'line', beforeId: 'y' }, 'src')
    expect(map.addLayer).toHaveBeenCalledWith({ id: 'x', type: 'line', source: 'src' }, 'y')
  })
})
