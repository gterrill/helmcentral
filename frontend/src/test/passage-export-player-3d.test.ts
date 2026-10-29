import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { initCard } from '@/export-player/main'
import { generateEmbed, PLAYER_SOURCE, type ExportInput } from '@/lib/passage-export'
import type { PassageDetail, PassageStats } from '@/lib/passages'
import type { CameraShot } from '@/lib/replay-clock'

// The cinematic card: terrain, the boat model and camera shots. MapLibre is a
// recorder; three.js and the boat layer are stubbed where they would need a GL
// context. What is checked is what the card asks of them and when.

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

const STATS: PassageStats = {
  distance_nm: 10, underway_seconds: 3600, mean_sog_kts: 5, max_sog_kts: 6, fuel_burned_l: null,
  tws_mean_kts: null, max_gust_kts: null, soc_start_pct: null, soc_end_pct: null, soc_min_pct: null,
}
const T0 = Date.UTC(2026, 8, 14, 0) / 1000

function passage(): PassageDetail {
  const times = Array.from({ length: 11 }, (_, i) => T0 + i * 360)
  const pts = times.map((_, i) => [-20 + i * 0.01, 148 + i * 0.01] as [number, number])
  return {
    id: '1', start: new Date(T0 * 1000).toISOString(), end: new Date((T0 + 3600) * 1000).toISOString(),
    from_name: 'A', to_name: 'B', title: 'Leg', notes: '', hidden: false, edited: false,
    cruise_id: null, created_at: '', stats: STATS, timezone: 'Australia/Brisbane',
    track: [pts], track_times: [times], replay_track: [pts], replay_times: [times],
  }
}

const shot = (id: string, atS: number, over: Partial<CameraShot> = {}): CameraShot => ({
  id, t_ms: atS * 1000, mode: 'follow', zoom: 15, pitch: 60, bearing_offset_deg: 0, orbit_deg_per_s: 0, pace: 1, ...over,
})

class FakeMap {
  static last: FakeMap
  static all: FakeMap[] = []
  handlers: Record<string, ((e?: unknown) => void)[]> = {}
  calls: [string, unknown?][] = []
  sources = new Map<string, { setData: ReturnType<typeof vi.fn> }>()
  present = new Set<string>()
  cam = { center: [148.05, -19.95], zoom: 11, pitch: 0, bearing: 0 }
  constructor(public options: Record<string, unknown>) { FakeMap.last = this; FakeMap.all.push(this) }
  on(evt: string, fn: (e?: unknown) => void) { (this.handlers[evt] ??= []).push(fn) }
  off(evt: string, fn: (e?: unknown) => void) { this.handlers[evt] = (this.handlers[evt] ?? []).filter((h) => h !== fn) }
  fire(evt: string, arg?: unknown) { for (const fn of [...(this.handlers[evt] ?? [])]) fn(arg) }
  addControl() {}
  setProjection() {}
  addSource(id: string, spec: unknown) { this.sources.set(id, { setData: vi.fn() }); this.calls.push(['addSource', { id, spec }]) }
  addLayer(l: { id: string }, before?: string) { this.present.add(l.id); this.calls.push(['addLayer', { id: l.id, before }]) }
  removeLayer(id: string) { this.present.delete(id); this.calls.push(['removeLayer', id]) }
  getLayer(id: string) { return this.present.has(id) ? { id } : undefined }
  getSource(id: string) { return this.sources.get(id) }
  setPaintProperty() {}
  setTerrain(t: unknown) { this.calls.push(['setTerrain', t]) }
  setSky(s: unknown) { this.calls.push(['setSky', s]) }
  jumpTo(o: Partial<typeof this.cam>) { Object.assign(this.cam, o); this.calls.push(['jumpTo', o]) }
  easeTo(o: unknown) { this.calls.push(['easeTo', o]) }
  fitBounds(b: unknown, o: unknown) { this.calls.push(['fitBounds', { b, o }]) }
  cameraForBounds() { return { center: [148.05, -19.95], zoom: 11 } }
  getCenter() { return { lng: this.cam.center[0], lat: this.cam.center[1] } }
  getZoom() { return this.cam.zoom }
  getPitch() { return this.cam.pitch }
  getBearing() { return this.cam.bearing }
  container = Object.assign(document.createElement('div'), {})
  getContainer() {
    Object.defineProperty(this.container, 'clientWidth', { value: 800, configurable: true })
    Object.defineProperty(this.container, 'clientHeight', { value: 480, configurable: true })
    return this.container
  }
  project() { return { x: 400, y: 240 } }
  surface = document.createElement('div')
  getCanvasContainer() { return this.surface }
  jumps() { return this.calls.filter(([k]) => k === 'jumpTo').map(([, v]) => v as { pitch?: number; bearing?: number; zoom?: number }) }
}
class FakeMarker {
  static last: FakeMarker
  el: HTMLElement
  constructor(o: { element: HTMLElement }) { this.el = o.element; FakeMarker.last = this }
  setLngLat() { return this }
  addTo() { return this }
}
const protocols: string[] = []
const lib = {
  Map: FakeMap, Marker: FakeMarker, NavigationControl: class {}, MercatorCoordinate: {},
  addProtocol: (name: string) => { protocols.push(name) },
}

function mount(input: Partial<ExportInput> = {}, source: 'ts' | 'built' = 'ts', cardId = 'hcp-3') {
  const full: ExportInput = {
    kind: 'passage', title: 'Out', zone: { offsetHours: 10, label: 'UTC+10' }, units: 'metric', inset: false,
    playSeconds: 30, cardId, passages: [passage()], ...input,
  }
  document.body.innerHTML = generateEmbed(full).replace(/<script[\s\S]*?<\/script>/g, (m) => (m.includes('application/json') ? m : '')).replace(/<link[^>]*>|<style>[\s\S]*?<\/style>/g, '')
  const init = source === 'ts' ? initCard : (new Function(`${PLAYER_SOURCE}\nreturn HcPlayer.initCard`)() as typeof initCard)
  const api = init(cardId, lib as never, document)
  const map = FakeMap.last
  const root = document.getElementById(cardId) as HTMLElement
  return { api, map, root, button: root.querySelector('.hcp-btn') as HTMLElement }
}

let tick: FrameRequestCallback = () => {}
let now = 1000
beforeEach(() => {
  protocols.length = 0
  FakeMap.all = []
  delete (globalThis as { __hcpProtocols?: unknown }).__hcpProtocols
  delete (globalThis as { __hcpThree?: unknown }).__hcpThree
  layers.made.length = 0
  loadThree.mockReset()
  now = 1000
  vi.spyOn(globalThis, 'requestAnimationFrame').mockImplementation((cb) => { tick = cb; return 1 })
  vi.spyOn(performance, 'now').mockImplementation(() => now)
  vi.spyOn(console, 'error').mockImplementation(() => {})
})
afterEach(() => { vi.restoreAllMocks(); vi.unstubAllGlobals(); document.body.innerHTML = '' })

/** Taps Play and lets the fly-in land. */
function startPlaying(card: ReturnType<typeof mount>) {
  card.map.fire('style.load')
  card.button.click()
  card.map.fire('moveend')
}
/** Plays on for `ms` of watching, a frame at a time. */
const advance = (ms: number) => {
  for (let left = ms; left > 0; left -= 50) { now += Math.min(50, left); tick(now) }
}

describe('terrain', () => {
  it('is not asked for at all on a flat card', () => {
    const { map } = mount({ threeD: false }, 'built')
    map.fire('style.load')
    expect(protocols).toEqual([])
    expect(map.calls.some(([k]) => k === 'setTerrain')).toBe(false)
    expect(map.options.dragRotate).toBe(false)
  })

  it('comes from AWS through the card\'s own protocol, exaggerated 1.5 with hillshade and the day sky', () => {
    const { map } = mount({ threeD: true }, 'built')
    map.fire('style.load')
    expect(protocols).toEqual(['hcterrain-v1'])
    const source = map.calls.find(([k, v]) => k === 'addSource' && (v as { id: string }).id === 'terrain-dem')?.[1] as { spec: Record<string, unknown> }
    expect(source.spec).toMatchObject({ type: 'raster-dem', tiles: ['hcterrain-v1://{z}/{x}/{y}'], encoding: 'terrarium', maxzoom: 15 })
    expect(map.calls).toContainEqual(['addLayer', { id: 'hcp-hillshade', before: 'hcp-seamarks' }])
    expect(map.calls).toContainEqual(['setTerrain', { source: 'terrain-dem', exaggeration: 1.5 }])
    const sky = map.calls.find(([k]) => k === 'setSky')?.[1] as Record<string, unknown>
    expect(sky['sky-color']).toBe('#8fb8d8')
    expect(map.options.dragRotate).toBe(true)
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
    expect(map.present.has('hcp-hillshade')).toBe(false)
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
    const { map, button } = mount({ threeD: true, hull })
    map.fire('style.load')
    expect(loadThree).toHaveBeenCalledWith(expect.objectContaining({ url: expect.stringContaining('three@0.170.0'), sri: expect.stringMatching(/^sha384-/) }))
    await vi.waitFor(() => expect(layers.made).toHaveLength(1))
    expect(map.present.has('passage-boat-model')).toBe(true)
    expect(FakeMarker.last.el.style.display).toBe('none')
    button.click()
    map.fire('moveend')
    const pose = layers.made[0].setPose.mock.calls.at(-1)?.[0] as { lat: number; lon: number; headingDeg: number }
    expect(pose.lat).toBeCloseTo(-20, 3)
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
    startPlaying(card)
    advance(2000)
    const last = card.map.jumps().at(-1) as { pitch: number; bearing: number; zoom: number }
    expect(last.pitch).toBeCloseTo(60, 0)
    expect(last.zoom).toBeCloseTo(15, 0)
    expect(last.bearing).toBeGreaterThan(38)
    expect(last.bearing).toBeLessThan(48)
  })

  it('shows the flat card zoom and centre only, never a pitch or bearing', () => {
    const card = mount({ threeD: false, shots: follow })
    startPlaying(card)
    advance(2000)
    const jumps = card.map.jumps().slice(1)
    expect(jumps.length).toBeGreaterThan(0)
    for (const j of jumps) {
      expect(j.pitch ?? 0).toBe(0)
      expect(j.bearing ?? 0).toBe(0)
    }
  })

  it('turns an Orbit shot by its degrees per watched second', () => {
    const card = mount({ threeD: true, shots: [shot('o', T0, { mode: 'orbit', orbit_deg_per_s: 10 })] })
    startPlaying(card)
    advance(2000)
    const first = (card.map.jumps().at(-1) as { bearing: number }).bearing
    advance(500)
    const later = (card.map.jumps().at(-1) as { bearing: number }).bearing
    // Half a watched second at 10 degrees per second.
    expect(((later - first + 540) % 360) - 180).toBeCloseTo(5, 0)
  })

  it('leaves the camera alone when there are no shots', () => {
    const card = mount({ threeD: true })
    startPlaying(card)
    const before = card.map.jumps().length
    advance(2000)
    expect(card.map.jumps().length).toBe(before)
  })

  it('hands the camera to the reader on a drag, but not on a tap', () => {
    const card = mount({ threeD: true, shots: follow })
    startPlaying(card)
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
    startPlaying(card)
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
    startPlaying(card)
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
    startPlaying(card)
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
    startPlaying(card)
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
    startPlaying(card)
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

describe('the fly-in', () => {
  it('turns round to the flat framing when the terrain fails while it is under way', () => {
    const card = mount({ threeD: true })
    card.map.fire('style.load')
    card.button.click()
    const before = card.map.calls.filter(([k]) => k === 'easeTo').length
    expect((card.map.calls.find(([k]) => k === 'easeTo')?.[1] as { pitch: number }).pitch).toBe(50)
    card.map.fire('error', { sourceId: 'terrain-dem', error: new Error('503') })
    const eases = card.map.calls.filter(([k]) => k === 'easeTo')
    expect(eases.length).toBe(before + 1)
    expect(eases.at(-1)?.[1]).toMatchObject({ pitch: 0, bearing: 0 })
    // Landing begins the replay, flat.
    card.map.fire('moveend')
    expect(card.root.className).toContain('hcp-playing')
    advance(500)
    expect(card.map.cam.pitch).toBe(0)
  })

  it('begins once however many times Play is pressed while it flies', () => {
    const card = mount({ threeD: true, inset: true })
    card.map.fire('style.load')
    card.button.click()
    card.button.click()
    card.button.click()
    const insets = () => FakeMap.all.length
    card.map.fire('moveend')
    expect(card.root.className).toContain('hcp-playing')
    expect(insets()).toBe(2) // the card's map and one overview map
    card.map.fire('moveend')
    expect(insets()).toBe(2)
    expect(card.map.handlers.moveend?.length ?? 0).toBeLessThanOrEqual(1)
  })

  it('does not let a stale five-second timer start a newer run', () => {
    vi.useFakeTimers()
    try {
      const card = mount({ threeD: true })
      card.map.fire('style.load')
      card.button.click()
      vi.advanceTimersByTime(4000)
      card.button.click()
      vi.advanceTimersByTime(2000)
      // The first run's timer would have fired by now.
      expect(card.root.className).toContain('hcp-flying')
      vi.advanceTimersByTime(3500)
      expect(card.root.className).toContain('hcp-playing')
    } finally {
      vi.useRealTimers()
    }
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

  it('stays open once the reader has opened it', () => {
    const card = mount({ threeD: true })
    const credit = document.createElement('details')
    credit.className = 'maplibregl-ctrl-attrib maplibregl-compact maplibregl-compact-show'
    const button = document.createElement('summary')
    button.className = 'maplibregl-ctrl-attrib-button'
    credit.appendChild(button)
    card.map.getContainer().appendChild(credit)
    card.root.querySelector('.hcp-map')?.appendChild(card.map.getContainer())
    button.click()
    card.map.fire('idle')
    expect(credit.classList.contains('maplibregl-compact-show')).toBe(true)
  })
})
