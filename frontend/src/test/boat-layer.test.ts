import { beforeEach, describe, expect, it, vi } from 'vitest'

const { renderers } = vi.hoisted(() => ({ renderers: [] as Array<{ resetState: ReturnType<typeof vi.fn>; render: ReturnType<typeof vi.fn>; dispose: ReturnType<typeof vi.fn>; setViewport: ReturnType<typeof vi.fn> }> }))

vi.mock('three', async (importOriginal) => {
  const actual = await importOriginal<typeof import('three')>()
  class FakeRenderer {
    autoClear = true
    resetState = vi.fn()
    render = vi.fn()
    dispose = vi.fn()
    setViewport = vi.fn()
    constructor() { renderers.push(this) }
  }
  return { ...actual, WebGLRenderer: FakeRenderer }
})

import * as THREE from 'three'

import { BOAT_LAYER_ID, createBoatLayer as makeBoatLayer } from '@/lib/boat-layer'

const MercatorCoordinate = {
  fromLngLat: (_l: unknown, z: number) => ({ x: 0.5, y: 0.5, z: z * 1e-7, meterInMercatorCoordinateUnits: () => 1e-7 }),
} as unknown as Parameters<typeof makeBoatLayer>[1]['MercatorCoordinate']
const createBoatLayer = (hull: Parameters<typeof makeBoatLayer>[0]) => makeBoatLayer(hull, { three: THREE, MercatorCoordinate })

const setDirty = vi.fn()
const queryTerrainElevation = vi.fn(() => 3)
const triggerRepaint = vi.fn()
const on = vi.fn()
const off = vi.fn()
const map = {
  getCanvas: () => ({}),
  getZoom: () => 12,
  queryTerrainElevation,
  triggerRepaint,
  on,
  off,
  project: () => ({ x: 1, y: 2 }),
  painter: { context: { setDirty } },
}
const gl = { getParameter: () => new Int32Array([0, 0, 800, 600]), VIEWPORT: 1 }
const options = { defaultProjectionData: { mainMatrix: new Float64Array(16).map((_, i) => (i % 5 === 0 ? 1 : 0)) } }

const hull = { type: 'power_cat' as const, loaM: 12, beamM: 6 }
const asLayer = (l: ReturnType<typeof createBoatLayer>) => l as unknown as {
  onAdd: (m: unknown, g: unknown) => void
  onRemove: () => void
  render: (g: unknown, o: unknown) => void
  setPose: (p: unknown) => void
}

beforeEach(() => {
  renderers.length = 0
  for (const f of [setDirty, queryTerrainElevation, triggerRepaint, on, off]) f.mockClear()
})

describe('boat layer', () => {
  it('has the id the map component removes it by', () => {
    expect(createBoatLayer(hull).id).toBe(BOAT_LAYER_ID)
  })

  it('builds its renderer once however many times the map adds it', () => {
    const layer = asLayer(createBoatLayer(hull))
    layer.onAdd(map, gl)
    layer.onAdd(map, gl) // a style rebuild adds again without a remove
    layer.onAdd(map, gl)
    expect(renderers).toHaveLength(1)
  })

  it('builds afresh after a real removal', () => {
    const layer = asLayer(createBoatLayer(hull))
    layer.onAdd(map, gl)
    layer.onRemove()
    expect(renderers[0].dispose).toHaveBeenCalledTimes(1)
    layer.onAdd(map, gl)
    expect(renderers).toHaveLength(2)
  })

  it('puts MapLibre\'s state cache right after the renderer touched the context', () => {
    const layer = asLayer(createBoatLayer(hull))
    layer.onAdd(map, gl)
    expect(renderers[0].resetState).toHaveBeenCalled()
    expect(setDirty).toHaveBeenCalledTimes(1)
  })

  it('draws nothing without a position, and nothing again once the position is cleared', () => {
    const layer = asLayer(createBoatLayer(hull))
    layer.onAdd(map, gl)
    layer.render(gl, options)
    expect(renderers[0].render).not.toHaveBeenCalled()
    layer.setPose({ lat: 1, lon: 2, headingDeg: 10 })
    layer.render(gl, options)
    expect(renderers[0].render).toHaveBeenCalledTimes(1)
    layer.setPose(null)
    layer.render(gl, options)
    expect(renderers[0].render).toHaveBeenCalledTimes(1)
  })

  it('asks the terrain for the elevation when the pose changes, not on every frame', () => {
    const layer = asLayer(createBoatLayer(hull))
    layer.onAdd(map, gl)
    layer.setPose({ lat: 1, lon: 2, headingDeg: 10 })
    for (let i = 0; i < 5; i++) layer.render(gl, options)
    expect(queryTerrainElevation).toHaveBeenCalledTimes(1)
    layer.setPose({ lat: 1.1, lon: 2, headingDeg: 10 })
    layer.render(gl, options)
    expect(queryTerrainElevation).toHaveBeenCalledTimes(2)
  })

  it('asks again when terrain tiles arrive', () => {
    const layer = asLayer(createBoatLayer(hull))
    layer.onAdd(map, gl)
    layer.setPose({ lat: 1, lon: 2, headingDeg: 10 })
    layer.render(gl, options)
    const handler = on.mock.calls.find((c) => c[0] === 'sourcedata')?.[1] as (e: { sourceId: string }) => void
    handler({ sourceId: 'terrain-dem' })
    layer.render(gl, options)
    expect(queryTerrainElevation).toHaveBeenCalledTimes(2)
    handler({ sourceId: 'openseamap' })
    layer.render(gl, options)
    expect(queryTerrainElevation).toHaveBeenCalledTimes(2)
  })

  it('keeps its last heading when the pose carries none', () => {
    const layer = asLayer(createBoatLayer(hull))
    layer.onAdd(map, gl)
    layer.setPose({ lat: 1, lon: 2, headingDeg: 90 })
    layer.render(gl, options)
    layer.setPose({ lat: 1, lon: 2, headingDeg: null })
    layer.render(gl, options)
    expect((window as unknown as { __boatDebug: { headingDeg: number } }).__boatDebug.headingDeg).toBe(90)
  })

  it('does not repaint for an equal pose, but does when the pose changes or clears', () => {
    const layer = asLayer(createBoatLayer(hull))
    layer.onAdd(map, gl)
    layer.setPose({ lat: 1, lon: 2, headingDeg: 10 })
    layer.render(gl, options)
    triggerRepaint.mockClear()
    queryTerrainElevation.mockClear()
    layer.setPose({ lat: 1, lon: 2, headingDeg: 10 })
    layer.render(gl, options)
    expect(triggerRepaint).not.toHaveBeenCalled()
    expect(queryTerrainElevation).not.toHaveBeenCalled()
    layer.setPose({ lat: 1, lon: 2, headingDeg: 11 })
    expect(triggerRepaint).toHaveBeenCalledTimes(1)
    layer.setPose(null)
    expect(triggerRepaint).toHaveBeenCalledTimes(2)
    layer.setPose(null)
    expect(triggerRepaint).toHaveBeenCalledTimes(2)
  })
})
