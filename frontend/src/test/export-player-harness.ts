// A stand-in for MapLibre and a clock to drive the exported player with. The
// player is the app's replay put on a plain MapLibre map, so what these check
// is what it asks of the map, and when; the replay's own arithmetic has its
// own tests.

import { vi } from 'vitest'

import { initCard } from '@/export-player/main'
import { generateEmbed, PLAYER_SOURCE, type ExportInput } from '@/lib/passage-export'
import type { PassageDetail, PassageStats } from '@/lib/passages'

export const STATS: PassageStats = {
  distance_nm: 10, underway_seconds: 3600, mean_sog_kts: 5, max_sog_kts: 6, fuel_burned_l: null,
  tws_mean_kts: null, max_gust_kts: null, soc_start_pct: null, soc_end_pct: null, soc_min_pct: null,
}
export const T0 = Date.UTC(2026, 8, 14, 0) / 1000

/** A passage of an hour, from `t0`, on a diagonal of eleven fixes. */
export function passage(id: string, t0: number = T0, over: Partial<PassageDetail> = {}): PassageDetail {
  const times = Array.from({ length: 11 }, (_, i) => t0 + i * 360)
  const pts = times.map((_, i) => [-20 + i * 0.01, 148 + i * 0.01] as [number, number])
  return {
    id, start: new Date(t0 * 1000).toISOString(), end: new Date((t0 + 3600) * 1000).toISOString(),
    from_name: 'A', to_name: `Anchorage ${id}`, title: `Leg ${id}`, notes: '', hidden: false, edited: false,
    cruise_id: null, created_at: '', stats: STATS, timezone: 'Australia/Brisbane',
    track: [pts], track_times: [times], replay_track: [pts], replay_times: [times], ...over,
  }
}

export class FakeMap {
  static last: FakeMap
  static all: FakeMap[] = []
  handlers: Record<string, ((e?: unknown) => void)[]> = {}
  calls: [string, unknown?][] = []
  sources = new Map<string, { setData: ReturnType<typeof vi.fn>; spec: unknown }>()
  present = new Set<string>()
  order: string[] = []
  paint = new Map<string, unknown>()
  cam = { center: [148.05, -19.95], zoom: 11, pitch: 0, bearing: 0 }
  constructor(public options: Record<string, unknown>) { FakeMap.last = this; FakeMap.all.push(this) }
  on(evt: string, fn: (e?: unknown) => void) { (this.handlers[evt] ??= []).push(fn) }
  once(evt: string, fn: (e?: unknown) => void) { this.on(evt, fn) }
  off(evt: string, fn: (e?: unknown) => void) { this.handlers[evt] = (this.handlers[evt] ?? []).filter((h) => h !== fn) }
  fire(evt: string, arg?: unknown) { for (const fn of [...(this.handlers[evt] ?? [])]) fn(arg) }
  addControl() {}
  setProjection(p: unknown) { this.calls.push(['setProjection', p]) }
  addSource(id: string, spec: unknown) { this.sources.set(id, { setData: vi.fn(), spec }); this.calls.push(['addSource', { id, spec }]) }
  removeSource(id: string) { this.sources.delete(id) }
  addLayer(l: { id: string }, before?: string) {
    this.present.add(l.id)
    const at = before ? this.order.indexOf(before) : -1
    if (at >= 0) this.order.splice(at, 0, l.id)
    else this.order.push(l.id)
    this.calls.push(['addLayer', { id: l.id, before }])
  }
  moveLayer(id: string) { this.order = [...this.order.filter((x) => x !== id), id] }
  removeLayer(id: string) { this.present.delete(id); this.order = this.order.filter((x) => x !== id); this.calls.push(['removeLayer', id]) }
  getLayer(id: string) { return this.present.has(id) ? { id } : undefined }
  getLayersOrder() { return this.order }
  getStyle() { return { layers: [{ id: 'base-fill', type: 'fill' }, { id: 'place-names', type: 'symbol' }] } }
  getSource(id: string) { return this.sources.get(id) }
  setPaintProperty(layer: string, name: string, value: unknown) { this.paint.set(`${layer}|${name}`, value); this.calls.push(['paint', { layer, name, value }]) }
  getPaintProperty(layer: string, name: string) { return this.paint.get(`${layer}|${name}`) }
  setLight(l: unknown) { this.calls.push(['setLight', l]) }
  setTerrain(t: unknown) { this.calls.push(['setTerrain', t]) }
  setSky(s: unknown) { this.calls.push(['setSky', s]) }
  jumpTo(o: Partial<typeof this.cam>) { Object.assign(this.cam, o); this.calls.push(['jumpTo', o]) }
  easeTo(o: unknown) { this.calls.push(['easeTo', o]) }
  cameraForBounds() { return { center: [148.05, -19.95], zoom: 11 } }
  getCenter() { return { lng: this.cam.center[0], lat: this.cam.center[1] } }
  getZoom() { return this.cam.zoom }
  getPitch() { return this.cam.pitch }
  getBearing() { return this.cam.bearing }
  /** The size the map says it is, and where it puts a point. */
  size = { w: 800, h: 480 }
  projected = { x: 400, y: 240 }
  getContainer() {
    const el = this.options.container as HTMLElement
    Object.defineProperty(el, 'clientWidth', { value: this.size.w, configurable: true })
    Object.defineProperty(el, 'clientHeight', { value: this.size.h, configurable: true })
    return el
  }
  project() { return this.projected }
  surface = document.createElement('div')
  getCanvasContainer() { return this.surface }
  jumps() { return this.calls.filter(([k]) => k === 'jumpTo').map(([, v]) => v as { pitch?: number; bearing?: number; zoom?: number }) }
  projections() { return this.calls.filter(([k]) => k === 'setProjection').map(([, v]) => v as { type: unknown }) }
  sourceIds() { return this.calls.filter(([k]) => k === 'addSource').map(([, v]) => (v as { id: string }).id) }
}

export class FakeMarker {
  static last: FakeMarker
  static all: FakeMarker[] = []
  el: HTMLElement
  added = false
  lngLat: unknown = null
  constructor(o: { element?: HTMLElement } = {}) { this.el = o.element ?? document.createElement('div'); FakeMarker.last = this; FakeMarker.all.push(this) }
  setLngLat(p: unknown) { this.lngLat = p; return this }
  addTo(map: FakeMap) { this.added = true; map.getContainer().appendChild(this.el); return this }
}

export const protocols: string[] = []
export const lib = {
  Map: FakeMap, Marker: FakeMarker, NavigationControl: class {}, MercatorCoordinate: {},
  addProtocol: (name: string) => { protocols.push(name) },
}

export function fullInput(input: Partial<ExportInput> = {}, cardId = 'hcp-t'): ExportInput {
  return {
    kind: 'passage', title: 'Out', zone: { offsetHours: 10, label: 'UTC+10' }, units: 'metric', inset: false,
    playSeconds: 30, cardId, passages: [passage('1')], ...input,
  }
}

/** The card's markup with its scripts and stylesheets taken out, the way a page shows it before the player runs. */
export function markup(input: ExportInput): string {
  return generateEmbed(input).replace(/<script[\s\S]*?<\/script>/g, (m) => (m.includes('application/json') ? m : '')).replace(/<link[^>]*>|<style>[\s\S]*?<\/style>/g, '')
}

export function mount(input: Partial<ExportInput> = {}, source: 'ts' | 'built' = 'ts', cardId = 'hcp-t', maplibre: unknown = lib) {
  const full = fullInput(input, cardId)
  document.body.innerHTML = markup(full)
  const init = source === 'ts' ? initCard : (new Function(`${PLAYER_SOURCE}\nreturn HcPlayer.initCard`)() as typeof initCard)
  const api = init(cardId, maplibre as never, document)
  const map = FakeMap.last
  const root = document.getElementById(cardId) as HTMLElement
  return { api, map, root, button: root.querySelector('.hcp-btn') as HTMLElement }
}
export type Card = ReturnType<typeof mount>

/** Drives requestAnimationFrame and performance.now by hand. */
export function useClock() {
  const clock = { now: 1000, tick: (() => {}) as FrameRequestCallback }
  vi.spyOn(globalThis, 'requestAnimationFrame').mockImplementation((cb) => { clock.tick = cb; return 1 })
  vi.spyOn(performance, 'now').mockImplementation(() => clock.now)
  /** Lets `ms` of watching go by, a frame at a time. */
  const advance = (ms: number) => {
    for (let left = ms; left > 0; left -= 50) { clock.now += Math.min(50, left); clock.tick(clock.now) }
  }
  return { clock, advance }
}

export const INTRO_AND_A_BIT_MS = 3300

export function resetFakes() {
  FakeMap.all = []
  FakeMarker.all = []
  protocols.length = 0
  delete (globalThis as { __hcpProtocols?: unknown }).__hcpProtocols
  delete (globalThis as { __hcpThree?: unknown }).__hcpThree
}

/** Taps Play and lets the opening move land, so the replay is playing from the start. */
export function startPlaying(card: Card, advance: (ms: number) => void) {
  card.map.fire('style.load')
  card.button.click()
  advance(INTRO_AND_A_BIT_MS)
}
