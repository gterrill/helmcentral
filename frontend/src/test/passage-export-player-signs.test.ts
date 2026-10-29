import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { generateEmbed, PLAYER_SOURCE, type ExportInput } from '@/lib/passage-export'
import { EXPORT_PHOTO_PREFIX } from '@/lib/passage-export-signs'
import type { PassageDetail, PassageStats } from '@/lib/passages'
import type { ReplaySign } from '@/lib/replay-signs'

// The card's signs, driven through the built player with MapLibre replaced by
// a recorder. One passage of 3600 s played in 30 s: a sign at T0 + 600 s is
// reached after 5 s of watching.

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

class FakeMap {
  static instances: FakeMap[] = []
  handlers: Record<string, ((e?: unknown) => void)[]> = {}
  sources = new Map<string, { setData: ReturnType<typeof vi.fn> }>()
  constructor(public options: { container: HTMLElement }) { FakeMap.instances.push(this) }
  on(evt: string, fn: (e?: unknown) => void) { (this.handlers[evt] ??= []).push(fn) }
  off(evt: string, fn: (e?: unknown) => void) { this.handlers[evt] = (this.handlers[evt] ?? []).filter((h) => h !== fn) }
  fire(evt: string) { for (const fn of [...(this.handlers[evt] ?? [])]) fn() }
  addControl() {}
  setProjection() {}
  addSource(id: string) { this.sources.set(id, { setData: vi.fn() }) }
  addLayer() {}
  getSource(id: string) { return this.sources.get(id) }
  setPaintProperty() {}
  jumpTo() {}
  fitBounds() {}
  getCanvasContainer() { return this.options.container }
  getContainer() { return this.options.container }
  projected: { x: number; y: number } = { x: 450, y: 300 }
  project() { return this.projected }
}
interface MarkerOptions { element?: HTMLElement; anchor?: string }
class FakeMarker {
  static all: FakeMarker[] = []
  lngLat: [number, number] | null = null
  constructor(public options: MarkerOptions = {}) { FakeMarker.all.push(this) }
  setLngLat(p: [number, number]) { this.lngLat = p; return this }
  addTo(map: FakeMap) { if (this.options.element) map.options.container.appendChild(this.options.element); return this }
}
const fakeMaplibre = { Map: FakeMap, Marker: FakeMarker, NavigationControl: class {} }

const sign = (id: string, atS: number, over: Partial<ReplaySign> = {}): ReplaySign => ({
  id, t_ms: atS * 1000, title: `Sign ${id}`, note: `Note ${id}`, document_id: null, photo_url: null, visibility: 'passing', show_s: 4, starts_leg: false, ...over,
})
const URI = `${EXPORT_PHOTO_PREFIX}QUJD`
const photoSign = (id: string, atS: number, over: Partial<ReplaySign> = {}) => sign(id, atS, { photo_url: `/api/documents/${id}/content`, ...over })

function stripScripts(html: string) {
  return html.replace(/<script[\s\S]*?<\/script>/g, (m) => (m.includes('application/json') ? m : '')).replace(/<link[^>]*>|<style>[\s\S]*?<\/style>/g, '')
}

let tick: FrameRequestCallback = () => {}
let clockMs = 1000

function mount(signs: ReplaySign[], cardId = 'hcp-t', photos: Record<string, string> = {}, tamper?: (data: { signs: { ph?: string }[] }) => void) {
  let html = generateEmbed({
    kind: 'passage', title: 'Out', zone: { offsetHours: 10, label: 'UTC+10' }, units: 'metric', inset: false, playSeconds: 30,
    cardId, passages: [passage()], signs, photos: new Map(Object.entries(photos)),
  } satisfies ExportInput)
  html = stripScripts(html)
  if (tamper) {
    html = html.replace(/(<script type="application\/json"[^>]*>)([\s\S]*?)(<\/script>)/, (_m, a: string, body: string, c: string) => {
      const data = JSON.parse(body)
      tamper(data)
      return `${a}${JSON.stringify(data)}${c}`
    })
  }
  document.body.insertAdjacentHTML('beforeend', html)
  const init = new Function(`${PLAYER_SOURCE}\nreturn HcPlayer.initCard`)() as (id: string, lib: unknown, doc: Document) => unknown
  init(cardId, fakeMaplibre, document)
  const root = document.getElementById(cardId) as HTMLElement
  const map = FakeMap.instances[FakeMap.instances.length - 1]
  map.fire('style.load')
  return { root, map }
}

const play = (root: HTMLElement, map: FakeMap) => {
  ;(root.querySelector('.hcp-btn') as HTMLElement).click()
  map.fire('moveend')
}
/** Watch for this many seconds more. */
const advance = (s: number) => { clockMs += s * 1000; tick(clockMs) }
const click = (el: Element | null) => (el as HTMLElement).click()
const cards = (root: HTMLElement) => [...root.querySelectorAll<HTMLElement>('.hcp-sign')]
const shown = (root: HTMLElement) => cards(root).filter((c) => !c.hidden).map((c) => c.querySelector('.hcp-sign-title')?.textContent)
const pinsShown = (root: HTMLElement) => [...root.querySelectorAll<HTMLElement>('.hcp-sign-pin')].filter((p) => !p.hidden).map((p) => p.getAttribute('aria-label'))

beforeEach(() => {
  FakeMap.instances = []
  FakeMarker.all = []
  clockMs = 1000
  vi.spyOn(globalThis, 'requestAnimationFrame').mockImplementation((cb) => { tick = cb; return 1 })
  vi.spyOn(performance, 'now').mockImplementation(() => clockMs)
})
afterEach(() => { document.body.innerHTML = ''; vi.restoreAllMocks() })

describe('signs on the card before and after the replay', () => {
  it('shows a pin for every sign at rest, and no card', () => {
    const { root } = mount([sign('a', T0 + 600), sign('b', T0 + 1800, { visibility: 'always' })])
    expect(pinsShown(root)).toEqual(['Sign: Sign a', 'Sign: Sign b'])
    expect(shown(root)).toEqual([])
  })

  it('puts each sign on its point of the track', () => {
    mount([sign('a', T0 + 1800)])
    const places = FakeMarker.all.filter((m) => m.lngLat && m.options.element?.className.includes('hcp-sign'))
    expect(places.length).toBeGreaterThan(0)
    for (const m of places) {
      expect(m.lngLat?.[0]).toBeCloseTo(148.05, 4)
      expect(m.lngLat?.[1]).toBeCloseTo(-19.95, 4)
    }
  })

  it('opens a card from a pin and closes it again', () => {
    const { root } = mount([sign('a', T0 + 600)])
    ;(root.querySelector('.hcp-sign-pin') as HTMLElement).click()
    expect(shown(root)).toEqual(['Sign a'])
    expect(pinsShown(root)).toEqual([])
    ;(root.querySelector('.hcp-sign-close') as HTMLElement).click()
    expect(shown(root)).toEqual([])
    expect(pinsShown(root)).toEqual(['Sign: Sign a'])
  })

  it('goes back to pins when the replay ends', () => {
    const { root, map } = mount([sign('a', T0 + 600)])
    play(root, map)
    advance(5.2)
    expect(shown(root)).toEqual(['Sign a'])
    advance(60)
    expect(root.className).toContain('hcp-done')
    expect(shown(root)).toEqual([])
    expect(pinsShown(root)).toEqual(['Sign: Sign a'])
  })
})

describe('signs while the replay plays', () => {
  it('follow the app\'s rules: lead-in, show time, then gone', () => {
    const { root, map } = mount([sign('a', T0 + 600)])
    play(root, map)
    advance(4)
    expect(shown(root)).toEqual([])
    advance(0.6)
    expect(shown(root)).toEqual(['Sign a'])
    expect(pinsShown(root)).toEqual([])
    advance(4.4)
    expect(shown(root)).toEqual([])
  })

  it('never show more than two, and the earliest gives way for good', () => {
    const { root, map } = mount([
      sign('a', T0 + 600, { show_s: 30 }), sign('b', T0 + 900, { show_s: 30 }), sign('c', T0 + 1200, { show_s: 3 }),
    ])
    play(root, map)
    advance(5.2)
    expect(shown(root)).toEqual(['Sign a'])
    advance(2)
    expect(shown(root)).toEqual(['Sign a', 'Sign b'])
    advance(3)
    expect(shown(root)).toEqual(['Sign b', 'Sign c'])
    advance(4)
    // c has expired; a does not come back.
    expect(shown(root)).toEqual(['Sign b'])
  })

  it('keeps an always sign up and an after sign from its time on', () => {
    const { root, map } = mount([sign('a', T0 + 1800, { visibility: 'always' }), sign('b', T0 + 600, { visibility: 'after' })])
    play(root, map)
    advance(5.2)
    expect(shown(root).sort()).toEqual(['Sign a', 'Sign b'])
  })

  it('cannot open a photo while playing', () => {
    const { root, map } = mount([photoSign('a', T0 + 600)], 'hcp-t', { a: URI })
    play(root, map)
    advance(5.2)
    expect((root.querySelector('.hcp-sign-photo') as HTMLButtonElement).disabled).toBe(true)
  })
})

describe('the photo', () => {
  it('opens full size in a dialog inside the card while paused, and Escape closes it', () => {
    const { root, map } = mount([photoSign('a', T0 + 600, { title: 'Whale', note: 'Off the bow' })], 'hcp-t', { a: URI })
    play(root, map)
    advance(5.2)
    ;(root.querySelector('.hcp-btn') as HTMLElement).click()
    const photoBtn = root.querySelector('.hcp-sign-photo') as HTMLButtonElement
    expect(photoBtn.disabled).toBe(false)
    expect(photoBtn.getAttribute('aria-label')).toBe('Open photo: Whale')
    photoBtn.click()
    const dialog = root.querySelector('[role="dialog"]') as HTMLElement
    expect(dialog.hidden).toBe(false)
    expect(dialog.getAttribute('aria-modal')).toBe('true')
    expect(document.getElementById(dialog.getAttribute('aria-labelledby') as string)?.textContent).toBe('Whale')
    expect(dialog.querySelector('img')?.getAttribute('src')).toBe(URI)
    expect(dialog.textContent).toContain('Off the bow')
    expect(document.activeElement).toBe(dialog.querySelector('button'))
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))
    expect(dialog.hidden).toBe(true)
    expect(document.activeElement).toBe(photoBtn)
  })

  it('closes with its button too', () => {
    const { root } = mount([photoSign('a', T0 + 600)], 'hcp-t', { a: URI })
    ;(root.querySelector('.hcp-sign-pin') as HTMLElement).click()
    ;(root.querySelector('.hcp-sign-photo') as HTMLElement).click()
    const dialog = root.querySelector('[role="dialog"]') as HTMLElement
    expect(dialog.hidden).toBe(false)
    ;(dialog.querySelector('.hcp-dlg-close') as HTMLElement).click()
    expect(dialog.hidden).toBe(true)
  })

  it('is only ever a JPEG data URI the export made', () => {
    const { root } = mount([photoSign('a', T0 + 600), photoSign('b', T0 + 900)], 'hcp-t', { a: URI, b: URI }, (d) => {
      d.signs[0].ph = 'https://evil.example/x.jpg'
      d.signs[1].ph = 'data:image/svg+xml;base64,QUJD'
    })
    const srcs = [...root.querySelectorAll('img[src]')].map((i) => i.getAttribute('src'))
    expect(srcs).toEqual([])
    expect(root.querySelectorAll('.hcp-sign-photo')).toHaveLength(0)
  })

  it('shows a sign with no photo as its note', () => {
    const { root } = mount([sign('a', T0 + 600)])
    expect(root.querySelectorAll('img[src]')).toHaveLength(0)
    expect(root.querySelector('.hcp-sign-photo')).toBeNull()
  })
})

describe('what is written to the page', () => {
  it('is text, whatever the note says', () => {
    const evil = '<img src=x onerror=alert(1)></script><!--'
    const { root } = mount([sign('a', T0 + 600, { title: evil, note: evil })])
    expect(root.querySelector('.hcp-sign-title')?.textContent).toBe(evil)
    expect(root.querySelector('.hcp-sign-note')?.textContent).toBe(evil)
    expect(root.querySelectorAll('img[src]')).toHaveLength(0)
    expect(PLAYER_SOURCE).not.toMatch(/innerHTML|outerHTML|insertAdjacentHTML/)
  })

  it('keeps two cards on one page apart', () => {
    const a = mount([photoSign('a', T0 + 600, { title: 'One' })], 'hcp-one', { a: URI })
    const b = mount([photoSign('a', T0 + 600, { title: 'Two' })], 'hcp-two', { a: URI })
    const idsOf = (r: HTMLElement) => [...r.querySelectorAll('[id]')].map((e) => e.id)
    const ids = [...idsOf(a.root), ...idsOf(b.root)]
    expect(new Set(ids).size).toBe(ids.length)
    ;(a.root.querySelector('.hcp-sign-pin') as HTMLElement).click()
    ;(a.root.querySelector('.hcp-sign-photo') as HTMLElement).click()
    expect((a.root.querySelector('[role="dialog"]') as HTMLElement).hidden).toBe(false)
    expect((b.root.querySelector('[role="dialog"]') as HTMLElement).hidden).toBe(true)
    expect(shown(b.root)).toEqual([])
  })

  it('has no entrance animation under reduced motion', () => {
    vi.spyOn(window, 'matchMedia').mockImplementation((q: string) => ({ matches: q.includes('reduce'), media: q, addEventListener() {}, removeEventListener() {} }) as unknown as MediaQueryList)
    const { root } = mount([sign('a', T0 + 600)])
    ;(root.querySelector('.hcp-sign-pin') as HTMLElement).click()
    expect(root.querySelector('.hcp-sign')?.classList.contains('hcp-in')).toBe(true)
  })
})

describe('focus', () => {
  it('moves into a card opened from a pin, and back to the pin when it closes', () => {
    const { root } = mount([sign('a', T0 + 600, { title: 'Reef' })])
    const pin = root.querySelector('.hcp-sign-pin') as HTMLElement
    pin.focus()
    pin.click()
    const close = root.querySelector('.hcp-sign-close') as HTMLElement
    expect(document.activeElement).toBe(close)
    expect(close.getAttribute('aria-label')).toBe('Close sign: Reef')
    expect(root.querySelector('.hcp-sign-card')?.getAttribute('aria-label')).toBe('Sign: Reef')
    close.click()
    expect(document.activeElement).toBe(pin)
  })
})

describe('a damaged photo', () => {
  it('is said in the card\'s own alert, and the sign still shows its text', () => {
    const { root } = mount([photoSign('a', T0 + 600, { title: 'Whale', note: 'Off the bow' })], 'hcp-t', { a: URI }, (d) => { d.signs[0].ph = 'https://evil.example/x.jpg' })
    const alert = root.querySelector('.hcp-error') as HTMLElement
    expect(alert.hidden).toBe(false)
    expect(alert.textContent).toMatch(/sign photo.*damaged/i)
    click(root.querySelector('.hcp-sign-pin'))
    expect(root.querySelector('.hcp-sign-title')?.textContent).toBe('Whale')
    expect(root.querySelector('.hcp-sign-note')?.textContent).toBe('Off the bow')
  })

  it('leaves the alert alone when every photo is good', () => {
    const { root } = mount([photoSign('a', T0 + 600)], 'hcp-t', { a: URI })
    expect((root.querySelector('.hcp-error') as HTMLElement).hidden).toBe(true)
  })
})

describe('layers', () => {
  it('draws pins under cards: the pin\'s own marker holds the low z-index', () => {
    const { root } = mount([sign('a', T0 + 600)])
    const holder = root.querySelector('.hcp-sign-holder') as HTMLElement
    expect(holder.style.zIndex).toBe('5')
    click(root.querySelector('.hcp-sign-pin'))
    expect(Number((root.querySelector('.hcp-sign') as HTMLElement).style.zIndex)).toBeGreaterThan(5)
  })
})

describe('keeping a card inside the map', () => {
  const size = (el: HTMLElement, w: number, h: number) => {
    Object.defineProperty(el, 'clientWidth', { configurable: true, value: w })
    Object.defineProperty(el, 'clientHeight', { configurable: true, value: h })
  }
  beforeEach(() => {
    Object.defineProperty(HTMLElement.prototype, 'offsetWidth', { configurable: true, get() { return (this as HTMLElement).classList.contains('hcp-sign-card') ? 220 : 0 } })
    Object.defineProperty(HTMLElement.prototype, 'offsetHeight', { configurable: true, get() { return (this as HTMLElement).classList.contains('hcp-sign-card') ? 70 : 0 } })
  })
  afterEach(() => {
    delete (HTMLElement.prototype as unknown as Record<string, unknown>).offsetWidth
    delete (HTMLElement.prototype as unknown as Record<string, unknown>).offsetHeight
  })
  const open = (x: number, y: number) => {
    const { root, map } = mount([sign('a', T0 + 600)])
    size(map.options.container, 900, 480)
    map.projected = { x, y }
    click(root.querySelector('.hcp-sign-pin'))
    map.fire('move')
    const surface = root.querySelector('.hcp-sign-card') as HTMLElement
    const box = root.querySelector('.hcp-sign-box') as HTMLElement
    const dx = Number(/translateX\((-?[\d.]+)px\)/.exec(surface.style.transform)?.[1] ?? 0)
    return { dx, below: box.classList.contains('hcp-below'), left: x - 110 + dx, right: x + 110 + dx }
  }

  it('leaves a card in the middle where it is, above its point', () => {
    expect(open(450, 300)).toMatchObject({ dx: 0, below: false })
  })
  it('slides a card in from the left edge, its leader still under it', () => {
    const r = open(60, 300)
    expect(r.left).toBeGreaterThanOrEqual(6)
    expect(60).toBeGreaterThan(r.left)
  })
  it('slides a card in from the right edge', () => {
    const r = open(870, 300)
    expect(r.right).toBeLessThanOrEqual(894)
    expect(870).toBeLessThan(r.right)
  })
  it('puts a card under its point near the top', () => {
    expect(open(450, 30).below).toBe(true)
  })
  it('keeps it above near the bottom', () => {
    expect(open(450, 460).below).toBe(false)
  })
})

describe('the button while paused', () => {
  it('shows the play triangle with Resume, and the pause bars only while playing', () => {
    const { root, map } = mount([sign('a', T0 + 600)])
    play(root, map)
    advance(1)
    expect(root.classList.contains('hcp-paused')).toBe(false)
    click(root.querySelector('.hcp-btn'))
    expect(root.classList.contains('hcp-paused')).toBe(true)
    expect(root.querySelector('.hcp-btn-label')?.textContent).toBe('Resume')
    click(root.querySelector('.hcp-btn'))
    expect(root.classList.contains('hcp-paused')).toBe(false)
    expect(root.querySelector('.hcp-btn-label')?.textContent).toBe('Pause')
  })

  it('is drawn by the card\'s own style: bars only when playing and not paused', () => {
    const css = generateEmbed({ kind: 'passage', title: 'x', zone: { offsetHours: 0, label: 'UTC' }, units: 'metric', inset: false, playSeconds: 30, cardId: 'hcp-c', passages: [passage()] })
    expect(css).toMatch(/#hcp-c\.hcp-playing:not\(\.hcp-paused\) \.hcp-btn-icon/)
  })
})
