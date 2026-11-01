import { beforeEach, describe, expect, it, vi } from 'vitest'

import { createVideoScene, type SceneFrame, type SceneSpec } from '@/lib/passage-video/scene'
import type { ReplayFrame } from '@/lib/replay-controller'
import { BASE_VECTOR_SOURCE_ID } from '@/lib/place-label-layers'
import { lightFor } from '@/lib/replay-light'

// A map that counts what it is asked to do, standing in for MapLibre.
const mapState = vi.hoisted(() => ({
  calls: {} as Record<string, number>,
  sources: new Map<string, { setData: (d: unknown) => void }>(),
  layers: new Map<string, object>(),
  options: null as null | { transformStyle?: unknown; map: Record<string, unknown> },
}))

vi.mock('@/lib/webgl', () => ({ hasWebGL2: () => true }))
vi.mock('maplibre-gl', () => ({ default: { MercatorCoordinate: {} } }))
vi.mock('@/lib/offscreen-map', () => ({
  openOffscreenMap: async (options: { transformStyle?: unknown; map: Record<string, unknown> }) => {
    mapState.options = options
    const count = (name: string) => { mapState.calls[name] = (mapState.calls[name] ?? 0) + 1 }
    const map = {
      getSource: (id: string) => mapState.sources.get(id),
      addSource: (id: string) => { count('addSource'); mapState.sources.set(id, { setData: () => count('setData') }) },
      removeSource: (id: string) => { count('removeSource'); mapState.sources.delete(id) },
      addLayer: (l: { id: string }) => { count('addLayer'); mapState.layers.set(l.id, l) },
      removeLayer: (id: string) => { count('removeLayer'); mapState.layers.delete(id) },
      getLayer: (id: string) => mapState.layers.get(id),
      getLayersOrder: () => [...mapState.layers.keys()],
      moveLayer: () => count('moveLayer'),
      getStyle: () => { count('getStyle'); return { layers: [] } },
      setPaintProperty: () => count('setPaintProperty'),
      getPaintProperty: () => undefined,
      setSky: () => count('setSky'),
      setLight: () => count('setLight'),
      setProjection: () => count('setProjection'),
      setTerrain: () => count('setTerrain'),
      jumpTo: () => count('jumpTo'),
      on: () => {},
      off: () => {},
      getCanvas: () => ({}),
    }
    mapState.sources.set(BASE_VECTOR_SOURCE_ID, { setData: () => {} })
    return { map, settle: async () => { count('settle') }, dispose: () => { count('dispose') } }
  },
}))

const spec: SceneSpec = {
  width: 1920, height: 1080, isDarkTheme: false, satellite: false, threeD: false, hull: null, bounds: [149, -20, 149.2, -20],
  start: { center: [149.1, -20], zoom: 1.5, pitch: 0, bearing: 0 },
  pieces: [
    { key: 'a', passageId: 'a', colourIndex: 0, from: 0, to: 100, segments: [Array.from({ length: 11 }, (_, i) => ({ t: i * 10, lat: -20, lon: 149 + i * 0.02 }))] },
    { key: 'b', passageId: 'b', colourIndex: 1, from: 100, to: 200, segments: [Array.from({ length: 11 }, (_, i) => ({ t: 100 + i * 10, lat: -20.1, lon: 149 + i * 0.02 }))] },
  ],
}

// Passage a is done and passage b is being sailed.
const frameAt = (at: number, light: ReturnType<typeof lightFor> | null = null): SceneFrame => ({
  camera: spec.start,
  projection: null,
  playedIds: new Set(['a', 'b']),
  frame: {
    light, speed: null, boat: null, heading: 0, cards: [], pins: [], moment: null,
    sailedTo: new Map([['a', { at: 100 }], ['b', { at, end: { lat: -20.1, lon: 149 + (at - 100) * 0.002 } }]]),
  } as unknown as ReplayFrame,
})

beforeEach(() => {
  mapState.calls = {}
  mapState.sources.clear()
  mapState.layers.clear()
})

describe('the video scene', () => {
  it('turns the style\'s transitions off, so the map is still as soon as it has drawn', async () => {
    await createVideoScene(spec)
    const transform = mapState.options!.transformStyle as (p: unknown, n: Record<string, unknown>) => Record<string, unknown>
    expect(transform(undefined, { version: 8, layers: [] })).toMatchObject({ transition: { duration: 0, delay: 0 } })
  })

  it('keeps a bigger tile cache than the page\'s, so the fly-in does not fetch its tiles again', async () => {
    await createVideoScene(spec)
    expect(mapState.options!.map.maxTileCacheSize).toBeGreaterThan(300)
    expect(mapState.options!.map.maxTileCacheZoomLevels).toBeGreaterThanOrEqual(10)
  })

  it('sets the data only of the piece that is being sailed, and never asks the style for its layers', async () => {
    const scene = await createVideoScene(spec)
    scene.show(frameAt(110))
    mapState.calls = {}
    // Fifty frames: passage b grows on every one, passage a is done and stays as it was.
    for (let k = 1; k <= 50; k++) scene.show(frameAt(110 + k))
    expect(mapState.calls.setData).toBe(50)
    expect(mapState.calls.getStyle ?? 0).toBe(0)
    expect(mapState.calls.setPaintProperty ?? 0).toBe(0)
    expect(mapState.calls.addSource ?? 0).toBe(0)
  })

  it('does nothing at all for a frame that is the same as the last', async () => {
    const scene = await createVideoScene(spec)
    scene.show(frameAt(150))
    mapState.calls = {}
    for (let k = 0; k < 30; k++) scene.show(frameAt(150))
    expect(mapState.calls.setData ?? 0).toBe(0)
    expect(mapState.calls.setPaintProperty ?? 0).toBe(0)
    expect(mapState.calls.setLight ?? 0).toBe(0)
    expect(mapState.calls.setSky ?? 0).toBe(0)
  })

  it('moves the light in place, once for each new light and not at all for the same one', async () => {
    const scene = await createVideoScene(spec)
    const dusk = lightFor({ altitudeDeg: -3, azimuthDeg: 250 })
    const night = lightFor({ altitudeDeg: -30, azimuthDeg: 10 })
    scene.show(frameAt(150, dusk))
    scene.show(frameAt(150, dusk))
    scene.show(frameAt(150, night))
    // The first light, then the next: no trip back to the defaults between them.
    expect(mapState.calls.setLight).toBe(2)
  })

  it('takes down the sailed track of a passage the replay has left, by the layers it added', async () => {
    const scene = await createVideoScene(spec)
    scene.show(frameAt(150))
    const before = mapState.layers.size
    scene.show({ ...frameAt(150), frame: { ...frameAt(150).frame, sailedTo: new Map([['a', { at: 100 }]]) } as unknown as ReplayFrame })
    expect(mapState.layers.size).toBeLessThan(before)
    expect(mapState.calls.getStyle ?? 0).toBe(0)
  })
})
