import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { generateEmbed, PLAYER_SOURCE, type ExportInput } from '@/lib/passage-export'
import type { PassageDetail, PassageStats } from '@/lib/passages'

// happy-dom has no WebGL, so MapLibre is replaced by a recorder. What is checked
// is the player's own logic: the plan, the readout, the captions and the order
// of camera moves. The player under test is the built one, the text a blog
// page receives.

const STATS: PassageStats = {
  distance_nm: 10, underway_seconds: 3600, mean_sog_kts: 5, max_sog_kts: 6, fuel_burned_l: null,
  tws_mean_kts: null, max_gust_kts: null, soc_start_pct: null, soc_end_pct: null, soc_min_pct: null,
}

function passage(id: string, t0: number, over: Partial<PassageDetail> = {}): PassageDetail {
  const times = Array.from({ length: 11 }, (_, i) => t0 + i * 360)
  const pts = times.map((_, i) => [-20 + i * 0.01, 148 + i * 0.01] as [number, number])
  return {
    id, start: new Date(t0 * 1000).toISOString(), end: new Date((t0 + 3600) * 1000).toISOString(),
    from_name: 'A', to_name: `Anchorage ${id}`, title: `Leg ${id}`, notes: '', hidden: false, edited: false,
    cruise_id: null, created_at: '', stats: STATS, start_lon: 148,
    track: [pts], track_times: [times], replay_track: [pts], replay_times: [times], ...over,
  }
}

const T0 = Date.UTC(2026, 8, 14, 0) / 1000

class FakeMap {
  static instances: FakeMap[] = []
  handlers: Record<string, (() => void)[]> = {}
  calls: [string, unknown?][] = []
  sources = new Map<string, { setData: ReturnType<typeof vi.fn> }>()
  constructor(public options: Record<string, unknown>) { FakeMap.instances.push(this) }
  on(evt: string, fn: () => void) { (this.handlers[evt] ??= []).push(fn) }
  once(evt: string, fn: () => void) { this.on(evt, fn) }
  off(evt: string, fn: () => void) { this.handlers[evt] = (this.handlers[evt] ?? []).filter((h) => h !== fn) }
  fire(evt: string) { for (const fn of [...(this.handlers[evt] ?? [])]) fn() }
  addControl() {}
  setProjection(p: unknown) { this.calls.push(['setProjection', p]) }
  addSource(id: string) { this.sources.set(id, { setData: vi.fn() }); this.calls.push(['addSource', id]) }
  addLayer(l: { id: string }) { this.calls.push(['addLayer', l.id]) }
  getSource(id: string) { return this.sources.get(id) }
  setPaintProperty(layer: string, prop: string, v: unknown) { this.calls.push(['paint', { layer, prop, v }]) }
  jumpTo(o: unknown) { this.calls.push(['jumpTo', o]) }
  fitBounds(b: unknown, o: unknown) { this.calls.push(['fitBounds', { b, o }]) }
  getCanvasContainer() { return document.createElement('div') }
}
class FakeMarker {
  static last: FakeMarker | null = null
  lngLat: unknown = null
  added = false
  constructor() { FakeMarker.last = this }
  setLngLat(p: unknown) { this.lngLat = p; return this }
  addTo() { this.added = true; return this }
}
class FakeNav {}
const fakeMaplibre = { Map: FakeMap, Marker: FakeMarker, NavigationControl: FakeNav }

type Api = {
  stateAt: (c: number) => { pos: number[] | null; t: number; passage: string; index: number; night: { n: number; place: string } | null; done: boolean }
  total: number
  steps: { kind: string; dur: number }[]
  fmtLocal: (t: number) => string
}

function stripScripts(html: string) {
  return html
    .replace(/<script[\s\S]*?<\/script>/g, (m) => (m.includes('application/json') ? m : ''))
    .replace(/<link[^>]*>|<style>[\s\S]*?<\/style>/g, '')
}

function mount(input: ExportInput, lib: unknown = fakeMaplibre) {
  document.body.innerHTML = stripScripts(generateEmbed(input))
  const id = input.cardId as string
  const init = new Function(`${PLAYER_SOURCE}\nreturn HcPlayer.initCard`)() as (id: string, lib: unknown, doc: Document) => Api
  return { api: init(id, lib, document), root: document.getElementById(id) as HTMLElement }
}

function base(over: Partial<ExportInput> = {}): ExportInput {
  return {
    kind: 'combined', title: 'Two days out', zone: { offsetHours: 10, label: 'UTC+10' }, units: 'metric', inset: true,
    playSeconds: 30, cardId: 'hcp-t',
    passages: [passage('1', T0), passage('2', T0 + 2 * 86400, { to_name: '' })],
    ...over,
  }
}

beforeEach(() => { FakeMap.instances = []; FakeMarker.last = null })
afterEach(() => { document.body.innerHTML = '' })

describe('the card on load', () => {
  it('fills the title, dates and stats strip from the data, as text', () => {
    const evil = '</script><img src=x onerror=alert(1)>'
    const { root } = mount(base({ title: evil, passages: [passage('1', T0, { title: evil }), passage('2', T0 + 2 * 86400)] }))
    expect(root.querySelector('.hcp-title')?.textContent).toBe(evil)
    expect(root.querySelector('.hcp-dates')?.textContent).toBe('14 to 16 Sep 2026')
    expect(root.querySelectorAll('img')).toHaveLength(0)
    const stats = [...root.querySelectorAll('.hcp-stat')].map((s) => s.textContent)
    expect(stats).toEqual(['Distance20.0 NM', 'Underway2 h 00 m', 'Nights2'])
  })

  it('shows the whole track and waits for a tap: no camera move, no play', () => {
    const { root } = mount(base())
    const map = FakeMap.instances[0]
    expect(map.options.bounds).toEqual([[148, -20], [148.1, -19.9]])
    expect(map.options.cooperativeGestures).toBe(true)
    map.fire('style.load')
    expect(map.calls.some(([k]) => k === 'jumpTo' || k === 'fitBounds')).toBe(false)
    expect(FakeMarker.last?.added).toBe(false)
    expect(FakeMap.instances).toHaveLength(1)
    expect(root.className).toContain('hcp-idle')
    expect((root.querySelector('.hcp-btn-label') as HTMLElement).textContent).toBe('Play')
  })

  it('puts the map on the globe with seamarks under the track', () => {
    mount(base())
    const map = FakeMap.instances[0]
    map.fire('style.load')
    expect(map.calls).toContainEqual(['setProjection', { type: 'globe' }])
    const layers = map.calls.filter(([k]) => k === 'addLayer').map(([, v]) => v)
    expect(layers[0]).toBe('hcp-seamarks')
    expect(layers).toContain('hcp-track-line')
  })

  it('says so when the map library did not load', () => {
    const { root } = mount(base(), null)
    expect((root.querySelector('.hcp-error') as HTMLElement).hidden).toBe(false)
    expect((root.querySelector('.hcp-btn') as HTMLElement).hidden).toBe(true)
  })
})

describe('the plan', () => {
  it('spends the chosen play length underway and 1.5 s on each night', () => {
    for (const playSeconds of [15, 30, 60] as const) {
      const { api } = mount(base({ playSeconds }))
      const runTime = api.steps.filter((s) => s.kind === 'run').reduce((n, s) => n + s.dur, 0)
      const nightTime = api.steps.filter((s) => s.kind === 'night').reduce((n, s) => n + s.dur, 0)
      expect(runTime).toBeCloseTo(playSeconds, 6)
      expect(api.steps.filter((s) => s.kind === 'night')).toHaveLength(2)
      expect(nightTime).toBeCloseTo(3, 6)
      expect(api.total).toBeCloseTo(playSeconds + 3, 6)
    }
  })

  it('caps a long stay so it does not stall the replay', () => {
    const { api } = mount(base({ passages: [passage('1', T0), passage('2', T0 + 20 * 86400)] }))
    const nights = api.steps.filter((s) => s.kind === 'night')
    expect(nights.length).toBe(20)
    expect(nights.reduce((n, s) => n + s.dur, 0)).toBeCloseTo(6, 6)
  })

  it('captions each night with its number and the anchorage, and drops an empty name', () => {
    const { api } = mount(base({ passages: [passage('1', T0), passage('2', T0 + 2 * 86400, { to_name: '' }), passage('3', T0 + 3 * 86400, { to_name: 'x' })] }))
    const firstNight = api.steps.find((s) => s.kind === 'night') as unknown as { at: number }
    const s1 = api.stateAt(firstNight.at + 0.1)
    expect(s1.night).toEqual({ n: 1, place: 'Anchorage 1' })
    expect(s1.pos).toEqual([148.1, -19.9])
    // Passage 2 arrives at '' so the night before passage 3 has no name.
    const third = api.steps.filter((s) => s.kind === 'night')[2] as unknown as { at: number }
    expect(api.stateAt(third.at + 0.1).night).toEqual({ n: 3, place: '' })
  })

  it('runs the boat-local clock through the night and reads the passage heading', () => {
    const { api } = mount(base())
    const start = api.stateAt(0)
    expect(start.passage).toBe('Leg 1')
    expect(api.fmtLocal(start.t)).toBe('14 Sep 10:00 UTC+10')
    const end = api.stateAt(api.total)
    expect(end.done).toBe(true)
    expect(end.passage).toBe('Leg 2')
    expect(api.fmtLocal(end.t)).toBe('16 Sep 11:00 UTC+10')
  })
})

describe('tapping play', () => {
  it('starts on the globe at zoom 1.5, flies to the bounds, then shows the inset and plays', () => {
    const raf = vi.spyOn(globalThis, 'requestAnimationFrame').mockReturnValue(1)
    const { root } = mount(base())
    const map = FakeMap.instances[0]
    map.fire('style.load')
    ;(root.querySelector('.hcp-btn') as HTMLElement).click()
    const order = map.calls.filter(([k]) => k === 'jumpTo' || k === 'fitBounds').map(([k]) => k)
    expect(order).toEqual(['jumpTo', 'fitBounds'])
    expect(map.calls.find(([k]) => k === 'jumpTo')?.[1]).toMatchObject({ zoom: 1.5 })
    // The overview waits for the camera to land.
    expect(FakeMap.instances).toHaveLength(1)
    expect(root.className).toContain('hcp-flying')
    map.fire('moveend')
    expect(FakeMap.instances).toHaveLength(2)
    expect((root.querySelector('.hcp-inset') as HTMLElement).hidden).toBe(false)
    expect(FakeMarker.last?.added).toBe(true)
    expect(root.className).toContain('hcp-playing')
    expect((root.querySelector('.hcp-time') as HTMLElement).textContent).toBe('14 Sep 10:00 UTC+10')
    expect((root.querySelector('.hcp-passage') as HTMLElement).textContent).toBe('Leg 1')
    raf.mockRestore()
  })

  it('leaves the overview out when it was off at export', () => {
    const raf = vi.spyOn(globalThis, 'requestAnimationFrame').mockReturnValue(1)
    const { root } = mount(base({ inset: false }))
    const map = FakeMap.instances[0]
    map.fire('style.load')
    ;(root.querySelector('.hcp-btn') as HTMLElement).click()
    map.fire('moveend')
    expect(FakeMap.instances).toHaveLength(1)
    expect((root.querySelector('.hcp-inset') as HTMLElement).hidden).toBe(true)
    raf.mockRestore()
  })

  it('offers a replay at the end', () => {
    let tick: FrameRequestCallback = () => {}
    const raf = vi.spyOn(globalThis, 'requestAnimationFrame').mockImplementation((cb) => { tick = cb; return 1 })
    const now = vi.spyOn(performance, 'now').mockReturnValue(1000)
    const { root } = mount(base({ inset: false }))
    const map = FakeMap.instances[0]
    map.fire('style.load')
    ;(root.querySelector('.hcp-btn') as HTMLElement).click()
    map.fire('moveend')
    tick(1000 + 60_000)
    expect(root.className).toContain('hcp-done')
    expect((root.querySelector('.hcp-btn-label') as HTMLElement).textContent).toBe('Replay')
    raf.mockRestore()
    now.mockRestore()
  })
})
