import { describe, expect, it, vi } from 'vitest'

import { SEAMARK_LAYER_ID } from '@/lib/basemap'
import {
  TINT_LAYER_ID, WAKE_LAYER_ID, applyLight, hillshadeLayer, imageryLayerProps, lightController, plainSailedOverlay, sailedEndFor, seamarkLayer,
  speedLineOverlay, tintLayer, wakeOverlay, wholeLineLayers, wholeLineOpacity,
} from '@/lib/replay-map-style'
import { lightFor } from '@/lib/replay-light'
import { SPEED_RAMP_LIGHT, nextSpeedGradient, speedProfile, speedFrameAt, speedTrack } from '@/lib/replay-speed'
import { HILLSHADE_LAYER_ID, hillshadePaint, skyFor } from '@/lib/terrain'
import { WORLD_IMAGERY_LAYER_ID } from '@/lib/world-imagery'

const dusk = lightFor({ altitudeDeg: -3, azimuthDeg: 250 })

function fakeMap(initial: string[] = [HILLSHADE_LAYER_ID, WORLD_IMAGERY_LAYER_ID]) {
  const layers = new Set(initial)
  const paint = new Map<string, unknown>()
  // A layer is the same object each time it is asked for, until it is added again.
  const objects = new Map<string, object>()
  const handlers: Record<string, Set<() => void>> = {}
  return {
    layers,
    objects,
    paint,
    handlers,
    emit: (type: string) => handlers[type]?.forEach((f) => f()),
    setSky: vi.fn(),
    setLight: vi.fn(),
    setPaintProperty: vi.fn((layer: string, name: string, value: unknown) => { paint.set(`${layer}/${name}`, value) }),
    getPaintProperty: (layer: string, name: string) => paint.get(`${layer}/${name}`),
    getLayer: (id: string) => (layers.has(id) ? (objects.get(id) ?? objects.set(id, {}).get(id)) : undefined),
    on: (type: string, f: () => void) => { (handlers[type] ??= new Set()).add(f) },
    off: (type: string, f: () => void) => { handlers[type]?.delete(f) },
  }
}
const scene = { isDarkTheme: false, satellite: true, terrain: true, camera3d: true }

describe('applyLight', () => {
  it('puts the sky, the light, the satellite dimming and the relief shading for the sun on the map', () => {
    const map = fakeMap()
    applyLight(map, dusk, scene)
    expect(map.setSky).toHaveBeenCalledWith(dusk.sky)
    expect(map.setLight).toHaveBeenCalledWith(dusk.light)
    expect(map.setPaintProperty).toHaveBeenCalledWith(WORLD_IMAGERY_LAYER_ID, 'raster-brightness-max', dusk.raster['raster-brightness-max'])
    expect(map.setPaintProperty).toHaveBeenCalledWith(HILLSHADE_LAYER_ID, 'hillshade-illumination-anchor', 'map')
    expect(map.setPaintProperty).toHaveBeenCalledWith(HILLSHADE_LAYER_ID, 'hillshade-illumination-direction', dusk.hillshade.direction)
  })

  it('leaves the sky to the app theme while there is no light, and does nothing else', () => {
    const map = fakeMap()
    applyLight(map, null, { ...scene, isDarkTheme: true })
    expect(map.setSky).toHaveBeenCalledWith(skyFor(true))
    expect(map.setLight).not.toHaveBeenCalled()
    expect(map.setPaintProperty).not.toHaveBeenCalled()
  })

  it('paints no sky on a flat map, but still lights it', () => {
    const map = fakeMap()
    applyLight(map, dusk, { ...scene, camera3d: false })
    expect(map.setSky).toHaveBeenCalledWith(undefined)
    expect(map.setLight).toHaveBeenCalledWith(dusk.light)
  })

  it('skips the imagery and the relief when they are not there', () => {
    const map = fakeMap([])
    applyLight(map, dusk, scene)
    expect(map.setPaintProperty).not.toHaveBeenCalled()
  })

  it('puts it all back when a new style loads, until it is told to stop', () => {
    const map = fakeMap()
    const stop = applyLight(map, dusk, scene)
    map.setLight.mockClear()
    map.emit('style.load')
    expect(map.setLight).toHaveBeenCalledTimes(1)
    stop()
    expect(map.handlers['style.load'].size + map.handlers.styledata.size).toBe(0)
  })
})

describe('applyLight puts the defaults back', () => {
  const defaults = (map: ReturnType<typeof fakeMap>) => {
    expect(map.setLight).toHaveBeenLastCalledWith({ anchor: 'viewport', color: '#ffffff', intensity: 0.5, position: [1.15, 210, 30] })
    expect(map.setPaintProperty).toHaveBeenCalledWith(WORLD_IMAGERY_LAYER_ID, 'raster-brightness-max', 1)
    expect(map.setPaintProperty).toHaveBeenCalledWith(WORLD_IMAGERY_LAYER_ID, 'raster-saturation', 0)
    expect(map.setPaintProperty).toHaveBeenCalledWith(HILLSHADE_LAYER_ID, 'hillshade-illumination-anchor', 'viewport')
    expect(map.setPaintProperty).toHaveBeenCalledWith(HILLSHADE_LAYER_ID, 'hillshade-illumination-direction', 335)
    expect(map.setPaintProperty).toHaveBeenCalledWith(HILLSHADE_LAYER_ID, 'hillshade-exaggeration', hillshadePaint['hillshade-exaggeration'])
  }

  it('when the light goes away', () => {
    const map = fakeMap()
    const stop = applyLight(map, dusk, scene)
    map.setLight.mockClear()
    map.setPaintProperty.mockClear()
    stop()
    applyLight(map, null, scene)
    defaults(map)
    expect(map.setSky).toHaveBeenLastCalledWith(skyFor(false))
  })

  it('when it is stopped, so a night does not outlast the replay', () => {
    const map = fakeMap()
    const stop = applyLight(map, dusk, scene)
    map.setLight.mockClear()
    map.setPaintProperty.mockClear()
    stop()
    defaults(map)
  })

  it('without touching layers that are not there', () => {
    const map = fakeMap([])
    applyLight(map, dusk, scene)()
    expect(map.setPaintProperty).not.toHaveBeenCalled()
    expect(map.setLight).toHaveBeenLastCalledWith(expect.objectContaining({ anchor: 'viewport' }))
  })
})

describe('applyLight after a style change', () => {
  it('paints the imagery and the relief once they are back, even though they came after style.load', () => {
    const map = fakeMap([])
    applyLight(map, dusk, scene)
    // A new style: the layers are gone at style.load and added a moment later.
    map.emit('style.load')
    expect(map.setPaintProperty).not.toHaveBeenCalled()
    map.layers.add(WORLD_IMAGERY_LAYER_ID)
    map.layers.add(HILLSHADE_LAYER_ID)
    map.emit('styledata')
    expect(map.setPaintProperty).toHaveBeenCalledWith(WORLD_IMAGERY_LAYER_ID, 'raster-brightness-max', dusk.raster['raster-brightness-max'])
    expect(map.setPaintProperty).toHaveBeenCalledWith(HILLSHADE_LAYER_ID, 'hillshade-illumination-direction', dusk.hillshade.direction)
  })

  it('does not paint again what is already painted', () => {
    const map = fakeMap()
    applyLight(map, dusk, scene)
    map.setPaintProperty.mockClear()
    map.emit('styledata')
    map.emit('styledata')
    expect(map.setPaintProperty).not.toHaveBeenCalled()
  })
})

describe('the layers', () => {
  it('lays a tint that is clear while there is no light and by day', () => {
    expect(tintLayer(null, false).paint['background-opacity']).toBe(0)
    expect(tintLayer(lightFor({ altitudeDeg: 40, azimuthDeg: 0 }), false).paint['background-opacity']).toBe(0)
    expect(tintLayer(dusk, false)).toMatchObject({ id: TINT_LAYER_ID, type: 'background', paint: { 'background-color': dusk.tint.colour, 'background-opacity': dusk.tint.opacity } })
  })

  it('eases the tint over satellite imagery, which is dimmed itself', () => {
    expect(tintLayer(dusk, true).paint['background-opacity']).toBe(dusk.tint.opacityOverImagery)
    expect(dusk.tint.opacityOverImagery).toBeLessThan(dusk.tint.opacity)
  })

  it('draws a speed line with its gradient, and a casing under it only over imagery', () => {
    const profile = speedProfile([{ t: 0, lat: 0, lon: 150 }, { t: 60, lat: 0, lon: 150.01 }, { t: 120, lat: 0, lon: 150.02 }])
    const gradient = nextSpeedGradient(profile, profile.totalM, null, 10, SPEED_RAMP_LIGHT)
    const line = { data: { type: 'Feature', properties: {}, geometry: { type: 'LineString', coordinates: [[150, 0], [150.02, 0]] } } as GeoJSON.Feature<GeoJSON.LineString>, gradient, extentM: profile.totalM }
    const plain = speedLineOverlay('p1', 0, line, false)
    expect(plain.source).toEqual({ id: 'passage-speed-p1-0', data: line.data, lineMetrics: true })
    expect(plain.layers.map((l) => l.id)).toEqual(['passage-speed-line-p1-0'])
    expect(plain.layers[0].paint['line-gradient']).toBe(gradient.expression)
    const over = speedLineOverlay('p1', 0, line, true)
    expect(over.layers.map((l) => l.id)).toEqual(['passage-speed-line-p1-0', 'passage-speed-casing-p1-0'])
    // The casing goes under its line, and so is added after it.
    expect(over.layers[1].beforeId).toBe('passage-speed-line-p1-0')
  })

  it('draws the plain sailed line in the leg colour', () => {
    const o = plainSailedOverlay('p1', { type: 'Feature', properties: {}, geometry: { type: 'MultiLineString', coordinates: [] } }, '#123456', true)
    expect(o.source.id).toBe('passage-sailed-p1')
    expect(o.layers[0].paint['line-color']).toBe('#123456')
    expect(o.layers[1].beforeId).toBe(o.layers[0].id)
  })

  it('draws the whole track faint, with a casing of its own opacity over imagery', () => {
    const [line, casing] = wholeLineLayers('p1', '#abcdef', 0.4, 0.35, true)
    expect(line.paint).toMatchObject({ 'line-color': '#abcdef', 'line-opacity': 0.4 })
    expect(casing.beforeId).toBe(line.id)
    expect((casing.paint['line-opacity'] as unknown[]).at(-1)).toBe(0.35)
    expect(wholeLineLayers('p1', '#abcdef', 0.4, 0.35, false)).toHaveLength(1)
  })

  it('draws the wake wider than a sailed line, with its foam gradient', () => {
    const seg = Array.from({ length: 50 }, (_, i) => ({ t: i * 60, lat: 0, lon: 150 + i * 0.001 }))
    const pieces = [{ key: 'p1', passageId: 'p1', segments: [seg], from: -Infinity, to: Infinity }]
    const f = speedFrameAt(pieces, speedTrack(pieces)!, new Map([['p1', { at: 1500, end: { lat: 0, lon: 150.025 } }]]), SPEED_RAMP_LIGHT, null, { zoom: 12 })
    const o = wakeOverlay(f.wake!)
    expect(o.layers[0].id).toBe(WAKE_LAYER_ID)
    expect(o.layers[0].paint['line-width']).toBeGreaterThan(3.5)
    expect(o.layers[0].paint['line-gradient']).toBe(f.wake!.gradient)
    expect(o.source.lineMetrics).toBe(true)
  })
})

describe('lightController', () => {
  const night = lightFor({ altitudeDeg: -30, azimuthDeg: 10 })

  it('moves from one light to the next in place, without going back to the defaults between', () => {
    const map = fakeMap()
    const c = lightController(map, scene)
    c.set(dusk)
    c.set(night)
    expect(map.setLight.mock.calls.map((x) => x[0])).toEqual([dusk.light, night.light])
    expect(map.setSky.mock.calls.map((x) => x[0])).toEqual([dusk.sky, night.sky])
  })

  it('does nothing for the light it already has', () => {
    const map = fakeMap()
    const c = lightController(map, scene)
    c.set(dusk)
    map.setLight.mockClear()
    map.setSky.mockClear()
    c.set(dusk)
    expect(map.setLight).not.toHaveBeenCalled()
    expect(map.setSky).not.toHaveBeenCalled()
  })

  it('puts the defaults back when the light goes, and again on stop only if there is one', () => {
    const map = fakeMap()
    const c = lightController(map, scene)
    c.set(dusk)
    c.set(null)
    expect(map.setLight).toHaveBeenLastCalledWith(expect.objectContaining({ anchor: 'viewport' }))
    map.setLight.mockClear()
    c.stop()
    expect(map.setLight).not.toHaveBeenCalled()
  })

  it('leaves the imagery and the relief alone while other layers change, and repaints them when they are re-added', () => {
    const map = fakeMap()
    const c = lightController(map, scene)
    c.set(dusk)
    map.setPaintProperty.mockClear()
    // Layers other than those two come and go; styledata fires for each.
    map.layers.add('somewhere-else')
    for (let i = 0; i < 5; i++) map.emit('styledata')
    expect(map.setPaintProperty).not.toHaveBeenCalled()
    // The style replaces the imagery layer.
    map.layers.delete(WORLD_IMAGERY_LAYER_ID)
    map.objects.delete(WORLD_IMAGERY_LAYER_ID)
    map.paint.clear()
    map.layers.add(WORLD_IMAGERY_LAYER_ID)
    map.emit('styledata')
    expect(map.setPaintProperty).toHaveBeenCalledWith(WORLD_IMAGERY_LAYER_ID, 'raster-brightness-max', dusk.raster['raster-brightness-max'])
  })
})

describe('wholeLineOpacity', () => {
  it('dims the road ahead while the sailed part is drawn over it', () => {
    expect(wholeLineOpacity(true, true)).toEqual({ line: 0.4, casing: 0.35 })
    expect(wholeLineOpacity(true, false)).toEqual({ line: 0.4, casing: 0.35 })
  })
  it('is bright for a passage the replay has begun, and mid for one it has not', () => {
    expect(wholeLineOpacity(false, true)).toEqual({ line: 0.95, casing: 0.95 })
    expect(wholeLineOpacity(false, false)).toEqual({ line: 0.45, casing: 0.5 })
  })
})

describe('sailedEndFor', () => {
  const end = { lat: -20, lon: 149 }
  it('ends the line on the boat while the replay is inside the piece', () => {
    expect(sailedEndFor({ at: 150, end }, { from: 100, to: 200 })).toBe(end)
    expect(sailedEndFor({ at: 100, end }, { from: 100, to: 200 })).toBe(end)
    expect(sailedEndFor({ at: 200, end }, { from: 100, to: 200 })).toBe(end)
  })
  it('leaves it to the fixes outside the piece, or with no boat', () => {
    expect(sailedEndFor({ at: 99, end }, { from: 100, to: 200 })).toBeUndefined()
    expect(sailedEndFor({ at: 201, end }, { from: 100, to: 200 })).toBeUndefined()
    expect(sailedEndFor({ at: 150 }, { from: 100, to: 200 })).toBeUndefined()
  })
})

describe('the base raster stack', () => {
  it('draws the sea marks at 0.85', () => {
    expect(seamarkLayer()).toMatchObject({ id: SEAMARK_LAYER_ID, type: 'raster', paint: { 'raster-opacity': 0.85 } })
  })
  it('tucks the relief and the imagery under the sea marks', () => {
    expect(hillshadeLayer()).toMatchObject({ id: HILLSHADE_LAYER_ID, beforeId: SEAMARK_LAYER_ID, paint: hillshadePaint })
    expect(imageryLayerProps()).toMatchObject({ beforeId: SEAMARK_LAYER_ID, minzoom: 9 })
    expect(imageryLayerProps().opacity).toEqual(['interpolate', ['linear'], ['zoom'], 9, 0, 10, 1])
  })
})
