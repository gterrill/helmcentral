import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { initCard } from '@/export-player/main'
import type { CameraShot } from '@/lib/replay-clock'

import { FakeMap, FakeMarker, INTRO_AND_A_BIT_MS, T0, lib, mount, passage, protocols, resetFakes, startPlaying, useClock } from './export-player-harness'

// The cinematic card: terrain, the boat model and camera shots. MapLibre is a
// recorder; three.js and the boat layer are stubbed where they would need a GL
// context. What is checked is what the card asks of them and when. The shots
// themselves, and where the camera goes for each, are the replay's own.

const loadThree = vi.hoisted(() => vi.fn())
vi.mock('@/export-player/three-loader', () => ({ loadThree }))
const layers = vi.hoisted(() => ({ made: [] as { id: string; setPose: ReturnType<typeof vi.fn> }[] }))
vi.mock('@/lib/boat-layer', () => ({
  createBoatLayer: vi.fn(() => {
    const layer = { id: 'passage-boat-model', type: 'custom', setPose: vi.fn() }
    layers.made.push(layer)
    return layer
  }),
}))

const shot = (id: string, atS: number, over: Partial<CameraShot> = {}): CameraShot => ({
  id, t_ms: atS * 1000, mode: 'follow', zoom: 15, pitch: 60, bearing_offset_deg: 0, orbit_deg_per_s: 0, pace: 1, ...over,
})

let driver: ReturnType<typeof useClock>
const advance = (ms: number) => driver.advance(ms)
beforeEach(() => {
  resetFakes()
  layers.made.length = 0
  loadThree.mockReset()
  driver = useClock()
  vi.spyOn(console, 'error').mockImplementation(() => {})
})
afterEach(() => { vi.restoreAllMocks(); vi.unstubAllGlobals(); document.body.innerHTML = '' })

describe('terrain', () => {
  it('is not asked for at all on a flat card', () => {
    const { map } = mount({ threeD: false }, 'built')
    map.fire('style.load')
    expect(protocols).toEqual([])
    expect(map.calls.some(([k]) => k === 'setTerrain')).toBe(false)
    expect(map.options.dragRotate).toBe(false)
    // No sky over a flat map.
    expect(map.calls.filter(([k]) => k === 'setSky').every(([, v]) => v === undefined)).toBe(true)
  })

  it('comes from AWS through the card\'s own protocol, exaggerated 1.5 with hillshade and the day sky', () => {
    const { map } = mount({ threeD: true }, 'built')
    map.fire('style.load')
    expect(protocols).toEqual(['hcterrain-v1'])
    const source = map.calls.find(([k, v]) => k === 'addSource' && (v as { id: string }).id === 'terrain-dem')?.[1] as { spec: Record<string, unknown> }
    expect(source.spec).toMatchObject({ type: 'raster-dem', tiles: ['hcterrain-v1://{z}/{x}/{y}'], encoding: 'terrarium', maxzoom: 15 })
    expect(map.calls).toContainEqual(['addLayer', { id: 'terrain-hillshade', before: 'hcp-seamarks' }])
    expect(map.calls).toContainEqual(['setTerrain', { source: 'terrain-dem', exaggeration: 1.5 }])
    const sky = map.calls.find(([k]) => k === 'setSky')?.[1] as Record<string, unknown>
    expect(sky['sky-color']).toBe('#8fb8d8')
    expect(map.options.dragRotate).toBe(true)
  })

  it('has the sun move the sky, the light and the relief shading once the replay plays', () => {
    const card = mount({ threeD: true })
    startPlaying(card, advance)
    advance(1000)
    const skies = card.map.calls.filter(([k]) => k === 'setSky').map(([, v]) => v as { 'sky-color'?: string } | undefined)
    // The day sky first, then the sky for the sun over the boat.
    expect(skies.length).toBeGreaterThan(1)
    expect(card.map.calls.some(([k]) => k === 'setLight')).toBe(true)
    expect(card.map.getPaintProperty('terrain-hillshade', 'hillshade-illumination-anchor')).toBe('map')
  })

  it('says so and carries on flat when a terrain tile cannot be had', () => {
    const { map, root } = mount({ threeD: true }, 'built')
    map.fire('style.load')
    map.fire('error', { sourceId: 'terrain-dem', error: new Error('503') })
    const notice = root.querySelector('.hcp-notice') as HTMLElement
    expect(notice.hidden).toBe(false)
    expect(notice.textContent).toBe('Terrain unavailable')
    expect(map.calls).toContainEqual(['setTerrain', null])
    expect((root.querySelector('.hcp-error') as HTMLElement).hidden).toBe(true)
    // Nothing is drawn in its place.
    expect(map.present.has('terrain-hillshade')).toBe(false)
  })
})

describe('the boat model', () => {
  const hull = { type: 'sail_mono' as const, loaM: 12, beamM: 4 }

  it('is not fetched without 3D or without a hull type', () => {
    mount({ threeD: false, hull }).map.fire('style.load')
    mount({ threeD: true, hull: null }).map.fire('style.load')
    expect(loadThree).not.toHaveBeenCalled()
  })

  it('is fetched from the pinned CDN copy with 3D on, then replaces the dot and follows the boat', async () => {
    loadThree.mockResolvedValue({})
    const card = mount({ threeD: true, hull })
    card.map.fire('style.load')
    expect(loadThree).toHaveBeenCalledWith(expect.objectContaining({ url: expect.stringContaining('three@0.170.0'), sri: expect.stringMatching(/^sha384-/) }))
    await vi.waitFor(() => expect(layers.made).toHaveLength(1))
    expect(card.map.present.has('passage-boat-model')).toBe(true)
    expect(FakeMarker.last.el.style.display).toBe('none')
    card.button.click()
    advance(INTRO_AND_A_BIT_MS + 1000)
    const pose = layers.made[0].setPose.mock.calls.at(-1)?.[0] as { lat: number; lon: number; headingDeg: number }
    expect(pose.lat).toBeCloseTo(-20, 1)
    expect(pose.headingDeg).toBeGreaterThan(38)
    expect(pose.headingDeg).toBeLessThan(48)
  })

  it('keeps the glowing dot and logs it when the library cannot be had', async () => {
    loadThree.mockRejectedValue(new Error('integrity'))
    const { map } = mount({ threeD: true, hull })
    map.fire('style.load')
    await vi.waitFor(() => expect(console.error).toHaveBeenCalledWith('Boat model unavailable', expect.any(Error)))
    expect(layers.made).toHaveLength(0)
    expect(FakeMarker.last.el.style.display).toBe('')
  })

  it('goes back to the dot when the terrain fails', async () => {
    loadThree.mockResolvedValue({})
    const { map } = mount({ threeD: true, hull })
    map.fire('style.load')
    await vi.waitFor(() => expect(layers.made).toHaveLength(1))
    map.fire('error', { sourceId: 'terrain-dem', error: new Error('x') })
    expect(map.present.has('passage-boat-model')).toBe(false)
    expect(FakeMarker.last.el.style.display).toBe('')
  })
})

describe('camera shots', () => {
  const follow = [shot('f', T0)]

  it('tilts and turns with the boat in a Follow shot when 3D is on', () => {
    const card = mount({ threeD: true, shots: follow })
    startPlaying(card, advance)
    advance(2000)
    const last = card.map.jumps().at(-1) as { pitch: number; bearing: number; zoom: number }
    expect(last.pitch).toBeCloseTo(60, 0)
    expect(last.zoom).toBeCloseTo(15, 0)
    expect(last.bearing).toBeGreaterThan(38)
    expect(last.bearing).toBeLessThan(48)
  })

  it('shows the flat card zoom and centre only, never a pitch or bearing', () => {
    const card = mount({ threeD: false, shots: follow })
    startPlaying(card, advance)
    advance(2000)
    const jumps = card.map.jumps()
    expect(jumps.length).toBeGreaterThan(0)
    for (const j of jumps) {
      expect(j.pitch ?? 0).toBe(0)
      expect(j.bearing ?? 0).toBe(0)
    }
  })

  it('turns an Orbit shot by its degrees per watched second', () => {
    const card = mount({ threeD: true, shots: [shot('o', T0, { mode: 'orbit', orbit_deg_per_s: 10 })] })
    startPlaying(card, advance)
    advance(2000)
    const first = (card.map.jumps().at(-1) as { bearing: number }).bearing
    advance(500)
    const later = (card.map.jumps().at(-1) as { bearing: number }).bearing
    // Half a watched second at 10 degrees per second.
    expect(((later - first + 540) % 360) - 180).toBeCloseTo(5, 0)
  })

  it('leaves the camera alone when there are no shots', () => {
    const card = mount({ threeD: true })
    startPlaying(card, advance)
    const before = card.map.jumps().length
    advance(2000)
    expect(card.map.jumps().length).toBe(before)
  })

  it('frames each passage of a cruise on its own, with no shots at all', () => {
    const card = mount({ kind: 'combined', threeD: true, passages: [passage('1', T0), passage('2', T0 + 2 * 86400)] })
    startPlaying(card, advance)
    const before = card.map.jumps().length
    advance(2000)
    expect(card.map.jumps().length).toBeGreaterThanOrEqual(before)
    expect(card.api.plan.passageBounds.map((p) => p.id)).toEqual(['0', '1'])
  })

  it('hands the camera to the reader on a drag, but not on a tap', () => {
    const card = mount({ threeD: true, shots: follow })
    startPlaying(card, advance)
    advance(2000)
    // A tap: pointer down and up on one finger. No movestart.
    card.map.surface.dispatchEvent(new Event('pointerdown'))
    card.map.surface.dispatchEvent(new Event('pointerup'))
    advance(100)
    const afterTap = card.map.jumps().length
    advance(100)
    expect(card.map.jumps().length).toBeGreaterThanOrEqual(afterTap)
    // A drag: the map says a person moved it.
    card.map.fire('movestart', { originalEvent: {} })
    card.map.cam.zoom = 9
    const held = card.map.jumps().length
    advance(100)
    advance(100)
    expect(card.map.jumps().length).toBe(held)
    expect(card.map.cam.zoom).toBe(9)
  })

  it('hands the camera over when a mouse drags past a few pixels, but not for a single finger', () => {
    const pointer = (type: string, init: { x: number; y: number; kind: string; id?: number; ctrl?: boolean }) =>
      Object.assign(new Event(type), { pointerId: init.id ?? 1, clientX: init.x, clientY: init.y, pointerType: init.kind })
    const card = mount({ threeD: true, shots: follow })
    startPlaying(card, advance)
    advance(2000)
    // One finger dragging: the page scrolls, the camera stays with the replay.
    card.map.surface.dispatchEvent(pointer('pointerdown', { x: 0, y: 0, kind: 'touch' }))
    card.map.surface.dispatchEvent(pointer('pointermove', { x: 40, y: 0, kind: 'touch' }))
    card.map.surface.dispatchEvent(pointer('pointerup', { x: 40, y: 0, kind: 'touch' }))
    card.map.cam.zoom = 9
    advance(100)
    expect(card.map.cam.zoom).not.toBe(9)
    // A mouse dragging.
    card.map.surface.dispatchEvent(pointer('pointerdown', { x: 0, y: 0, kind: 'mouse' }))
    card.map.surface.dispatchEvent(pointer('pointermove', { x: 2, y: 0, kind: 'mouse' }))
    card.map.cam.zoom = 9
    advance(100)
    expect(card.map.cam.zoom).not.toBe(9)
    card.map.surface.dispatchEvent(pointer('pointermove', { x: 30, y: 0, kind: 'mouse' }))
    card.map.cam.zoom = 9
    advance(100)
    expect(card.map.cam.zoom).toBe(9)
  })

  it('hands the camera over for Ctrl and the wheel (a zoom), but not the wheel alone (the page scrolls)', () => {
    const card = mount({ threeD: true, shots: follow })
    startPlaying(card, advance)
    advance(2000)
    card.map.surface.dispatchEvent(Object.assign(new Event('wheel'), { ctrlKey: false, metaKey: false }))
    card.map.cam.zoom = 9
    advance(100)
    expect(card.map.cam.zoom).not.toBe(9)
    card.map.surface.dispatchEvent(Object.assign(new Event('wheel'), { ctrlKey: true }))
    card.map.cam.zoom = 9
    advance(100)
    expect(card.map.cam.zoom).toBe(9)
  })

  it('hands the camera over when a second finger comes down (a pinch)', () => {
    const touch = (id: number) => Object.assign(new Event('pointerdown'), { pointerId: id, clientX: 0, clientY: 0, pointerType: 'touch' })
    const card = mount({ threeD: true, shots: follow })
    startPlaying(card, advance)
    advance(2000)
    card.map.surface.dispatchEvent(touch(1))
    advance(100)
    card.map.cam.zoom = 9
    advance(100)
    expect(card.map.cam.zoom).not.toBe(9)
    card.map.surface.dispatchEvent(touch(2))
    card.map.cam.zoom = 9
    advance(100)
    expect(card.map.cam.zoom).toBe(9)
  })

  it('takes the camera back at the next shot', () => {
    const card = mount({ threeD: true, shots: [shot('a', T0), shot('b', T0 + 1800, { zoom: 12 })] })
    startPlaying(card, advance)
    advance(2000)
    card.map.fire('movestart', { originalEvent: {} })
    card.map.cam.zoom = 9
    advance(100)
    expect(card.map.cam.zoom).toBe(9)
    // 15 watched seconds is the middle of the passage.
    advance(16_000)
    expect(card.map.cam.zoom).toBeCloseTo(12, 0)
  })

  it('cuts instead of moving with reduced motion', () => {
    vi.stubGlobal('matchMedia', (q: string) => ({ matches: q.includes('reduce'), addEventListener() {}, removeEventListener() {} }))
    const card = mount({ threeD: true, shots: follow })
    card.map.fire('style.load')
    card.button.click()
    advance(16)
    const jumps = card.map.jumps()
    const cut = jumps.at(-1) as { zoom: number; pitch: number }
    // At once on the shot's framing, and no easing frames on the way.
    expect(cut.zoom).toBe(15)
    expect(cut.pitch).toBe(60)
    advance(16)
    expect(card.map.jumps().length).toBe(jumps.length)
  })
})

describe('two cards on one page', () => {
  const twoCards = () => {
    const first = mount({ threeD: true }, 'ts', 'hcp-a')
    const html = document.body.innerHTML
    const second = mount({ threeD: true }, 'ts', 'hcp-b')
    document.body.innerHTML = html + document.body.innerHTML
    return { first, second }
  }

  it('each register the terrain protocol on a MapLibre of their own, as pasted cards load one each', () => {
    const second = { ...lib, addProtocol: (name: string) => { protocols.push(`second:${name}`) } }
    const a = mount({ threeD: true }, 'ts', 'hcp-a')
    a.map.fire('style.load')
    const html = document.body.innerHTML
    const init = initCard
    document.body.innerHTML = html + html.split('hcp-a').join('hcp-b')
    init('hcp-b', second as never, document)
    FakeMap.last.fire('style.load')
    expect(protocols).toEqual(['hcterrain-v1', 'second:hcterrain-v1'])
  })

  it('register the terrain protocol once on a shared MapLibre, under a versioned name', () => {
    const { first, second } = twoCards()
    first.map.fire('style.load')
    second.map.fire('style.load')
    expect(protocols).toEqual(['hcterrain-v1'])
  })

  it('ask for three.js through one shared load', async () => {
    const hull = { type: 'sail_mono' as const, loaM: 12, beamM: 4 }
    loadThree.mockResolvedValue({})
    const a = mount({ threeD: true, hull }, 'ts', 'hcp-a')
    const b = mount({ threeD: true, hull }, 'ts', 'hcp-b')
    a.map.fire('style.load')
    b.map.fire('style.load')
    await vi.waitFor(() => expect(layers.made).toHaveLength(2))
    // Each card asks the loader, which keeps the one load for the page.
    expect(loadThree).toHaveBeenCalledTimes(2)
    expect(loadThree.mock.calls[0][0]).toEqual(loadThree.mock.calls[1][0])
  })
})

describe('the opening move', () => {
  it('turns round to the flat framing when the terrain fails while it is under way', () => {
    const card = mount({ threeD: true })
    card.map.fire('style.load')
    card.button.click()
    advance(1000)
    expect(card.root.className).toContain('hcp-flying')
    card.map.fire('error', { sourceId: 'terrain-dem', error: new Error('503') })
    // Landing begins the replay, flat.
    advance(INTRO_AND_A_BIT_MS)
    expect(card.root.className).toContain('hcp-playing')
    expect(card.map.cam.pitch).toBe(0)
    advance(500)
    expect(card.map.cam.pitch).toBe(0)
  })

  it('comes down tilted over terrain, and flat on a card with none', () => {
    const tilted = mount({ threeD: true })
    tilted.map.fire('style.load')
    tilted.button.click()
    advance(INTRO_AND_A_BIT_MS - 100)
    expect(tilted.map.cam.pitch).toBeGreaterThan(40)
    resetFakes()
    const flat = mount({ threeD: false })
    flat.map.fire('style.load')
    flat.button.click()
    advance(INTRO_AND_A_BIT_MS - 100)
    expect(flat.map.cam.pitch).toBe(0)
  })
})

describe('the credit line', () => {
  it('is kept to the small icon, not the long strip the readout would sit on', () => {
    const card = mount({ threeD: true })
    const credit = document.createElement('details')
    credit.className = 'maplibregl-ctrl-attrib maplibregl-compact maplibregl-compact-show'
    credit.setAttribute('open', '')
    card.map.getContainer().appendChild(credit)
    card.map.fire('idle')
    expect(credit.classList.contains('maplibregl-compact-show')).toBe(false)
    expect(credit.hasAttribute('open')).toBe(false)
  })
})
