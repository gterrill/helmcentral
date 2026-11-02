import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { generateEmbed, PLAYER_SOURCE } from '@/lib/passage-export'
import { EXPORT_PHOTO_PREFIX } from '@/lib/passage-export-signs'
import { MOMENT_TOTAL_S } from '@/lib/replay-time'
import { placeSignCard, type ReplaySign } from '@/lib/replay-signs'

import { FakeMap, FakeMarker, INTRO_AND_A_BIT_MS, T0, fullInput, lib, markup, passage, resetFakes, useClock } from './export-player-harness'

// The card's signs, driven through the built player with MapLibre replaced by
// a recorder. One passage of 3600 s played in 30 s: a sign at T0 + 600 s is
// reached after 5 s of sailing. Which cards are up, and how a photo moment
// goes, are the replay controller's; what is checked here is that the card
// draws what it is told.

const sign = (id: string, atS: number, over: Partial<ReplaySign> = {}): ReplaySign => ({
  id, t_ms: atS * 1000, title: `Sign ${id}`, note: `Note ${id}`, document_id: null, photo_url: null, visibility: 'passing', show_s: 4, starts_leg: false, photo_moment: null, ...over,
})
const URI = `${EXPORT_PHOTO_PREFIX}QUJD`
// A card photo with no moment, unless a test asks for one.
const photoSign = (id: string, atS: number, over: Partial<ReplaySign> = {}) => sign(id, atS, { photo_url: `/api/documents/${id}/content`, photo_moment: false, ...over })
const momentSign = (id: string, atS: number, over: Partial<ReplaySign> = {}) => photoSign(id, atS, { photo_moment: null, ...over })

let driver: ReturnType<typeof useClock>

function mount(signs: ReplaySign[], cardId = 'hcp-t', photos: Record<string, string> = {}, tamper?: (data: { signs: { ph?: string }[] }) => void) {
  let html = markup(fullInput({ signs, photos: new Map(Object.entries(photos)) }, cardId))
  if (tamper) {
    html = html.replace(/(<script type="application\/json"[^>]*>)([\s\S]*?)(<\/script>)/, (_m, a: string, body: string, c: string) => {
      const data = JSON.parse(body)
      tamper(data)
      return `${a}${JSON.stringify(data)}${c}`
    })
  }
  document.body.insertAdjacentHTML('beforeend', html)
  const init = new Function(`${PLAYER_SOURCE}\nreturn HcPlayer.initCard`)() as (id: string, lib: unknown, doc: Document) => unknown
  init(cardId, lib, document)
  const root = document.getElementById(cardId) as HTMLElement
  const map = FakeMap.all[FakeMap.all.length - 1]
  map.fire('style.load')
  return { root, map }
}

/** Taps Play and lets the opening move land. */
const play = (root: HTMLElement) => {
  ;(root.querySelector('.hcp-btn') as HTMLElement).click()
  driver.advance(INTRO_AND_A_BIT_MS)
}
/** Watch for this many seconds more. */
const advance = (s: number) => driver.advance(s * 1000)
const click = (el: Element | null) => (el as HTMLElement).click()
const cards = (root: HTMLElement) => [...root.querySelectorAll<HTMLElement>('.hcp-sign')]
const shown = (root: HTMLElement) => cards(root).filter((c) => !c.hidden).map((c) => c.querySelector('.hcp-sign-title')?.textContent)
const pinsShown = (root: HTMLElement) => [...root.querySelectorAll<HTMLElement>('.hcp-sign-pin')].filter((p) => !p.hidden).map((p) => p.getAttribute('aria-label'))

beforeEach(() => {
  resetFakes()
  driver = useClock()
  vi.spyOn(console, 'error').mockImplementation(() => {})
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
    const places = FakeMarker.all.filter((m) => m.lngLat && m.el.className.includes('hcp-sign'))
    expect(places.length).toBeGreaterThan(0)
    for (const m of places) {
      const at = m.lngLat as [number, number]
      expect(at[0]).toBeCloseTo(148.05, 4)
      expect(at[1]).toBeCloseTo(-19.95, 4)
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

  it('goes back to pins when the replay ends, so a reader can open a photo', () => {
    const { root } = mount([sign('a', T0 + 600)])
    play(root)
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
    const { root } = mount([sign('a', T0 + 600)])
    play(root)
    advance(4)
    expect(shown(root)).toEqual([])
    advance(0.6)
    expect(shown(root)).toEqual(['Sign a'])
    expect(pinsShown(root)).toEqual([])
    advance(4.4)
    expect(shown(root)).toEqual([])
  })

  it('never show more than two, and the earliest gives way for good', () => {
    const { root } = mount([
      sign('a', T0 + 600, { show_s: 30 }), sign('b', T0 + 900, { show_s: 30 }), sign('c', T0 + 1200, { show_s: 3 }),
    ])
    play(root)
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
    const { root } = mount([sign('a', T0 + 1800, { visibility: 'always' }), sign('b', T0 + 600, { visibility: 'after' })])
    play(root)
    advance(5.2)
    expect(shown(root).sort()).toEqual(['Sign a', 'Sign b'])
  })

  it('cannot open a photo while playing', () => {
    const { root } = mount([photoSign('a', T0 + 600)], 'hcp-t', { a: URI })
    play(root)
    advance(5.2)
    expect((root.querySelector('.hcp-sign-photo') as HTMLButtonElement).disabled).toBe(true)
  })
})

describe('the photo', () => {
  it('opens full size in a dialog inside the card while paused, and Escape closes it', () => {
    const { root } = mount([photoSign('a', T0 + 600, { title: 'Whale', note: 'Off the bow' })], 'hcp-t', { a: URI })
    play(root)
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

describe('a photo moment', () => {
  // A card 256 by 224 on a map of 800 by 480: the moment grows it to 1.5 times its own size, in the middle.
  beforeEach(() => {
    Object.defineProperty(HTMLElement.prototype, 'offsetWidth', { configurable: true, get() { return (this as HTMLElement).classList.contains('hcp-sign-card') ? 256 : 0 } })
    Object.defineProperty(HTMLElement.prototype, 'offsetHeight', { configurable: true, get() { return (this as HTMLElement).classList.contains('hcp-sign-card') ? 224 : 0 } })
  })
  afterEach(() => {
    delete (HTMLElement.prototype as unknown as Record<string, unknown>).offsetWidth
    delete (HTMLElement.prototype as unknown as Record<string, unknown>).offsetHeight
  })
  const scaleOf = (root: HTMLElement) => Number(/scale\(([\d.]+)\)/.exec((root.querySelector('.hcp-sign-box') as HTMLElement).style.transform)?.[1] ?? 1)
  const imgOf = (root: HTMLElement) => root.querySelector('.hcp-sign-img') as HTMLImageElement
  /** Plays to the moment's start (5 s of sailing in), with the map as large as the box above. */
  const settle = () => new Promise((resolve) => { setTimeout(resolve, 0) })
  const toMoment = async (signs: ReplaySign[], tamper?: Parameters<typeof mount>[3]) => {
    const card = mount(signs, 'hcp-t', { a: URI }, tamper)
    play(card.root)
    advance(4.9)
    await settle()
    return card
  }

  it('holds the replay at the sign, and grows the whole card to fill the map, with the same photo in it', async () => {
    const { root } = await toMoment([momentSign('a', T0 + 600)])
    advance(0.3)
    const wall = driver.clock.now
    // In the moment: the sailing stands still while the card grows (0.4 s) and holds.
    expect(imgOf(root).getAttribute('src')).toBe(URI)
    advance(1)
    expect(scaleOf(root)).toBeCloseTo(1.5, 2)
    expect(imgOf(root).getAttribute('src')).toBe(URI)
    const box = root.querySelector('.hcp-sign-box') as HTMLElement
    // Centred in the 800 by 480 map: the point is at (400, 240), the card 384 by 336 wide.
    expect(parseFloat(box.style.left)).toBeCloseTo((800 - 384) / 2 - 400, 0)
    expect(parseFloat(box.style.top)).toBeCloseTo((480 - 336) / 2 - 240, 0)
    expect(driver.clock.now).toBeGreaterThan(wall)
  })

  it('zooms the photo slowly inside its frame, and shrinks the card back when it is done', async () => {
    const { root } = await toMoment([momentSign('a', T0 + 600)])
    advance(1.2)
    const zoomed = Number(/scale\(([\d.]+)\)/.exec(imgOf(root).style.transform)?.[1] ?? 1)
    expect(zoomed).toBeGreaterThan(1)
    expect(zoomed).toBeLessThanOrEqual(1.06 + 1e-9)
    advance(MOMENT_TOTAL_S)
    expect(imgOf(root).style.transform).toBe('')
    expect(scaleOf(root)).toBe(1)
  })

  it('does not grow a card whose moment was switched off', async () => {
    const { root } = await toMoment([photoSign('a', T0 + 600)])
    advance(1)
    expect(imgOf(root).getAttribute('src')).toBe(URI)
    expect(scaleOf(root)).toBe(1)
  })

  it('ends at a tap on the map, and at a tap on the grown card, as in the app, whose card lets touches through', async () => {
    const { root, map } = await toMoment([momentSign('a', T0 + 600)])
    advance(1)
    expect(scaleOf(root)).toBeCloseTo(1.5, 2)
    // A tap on the card, a marker on the map, is a tap on the map while the card is grown.
    const marker = document.createElement('div')
    marker.className = 'maplibregl-marker'
    const inside = document.createElement('span')
    marker.appendChild(inside)
    map.surface.appendChild(marker)
    inside.dispatchEvent(new Event('pointerdown', { bubbles: true }))
    advance(0.1)
    expect(scaleOf(root)).toBe(1)
  })

  it('ends at a tap on the map itself', async () => {
    const { root, map } = await toMoment([momentSign('a', T0 + 600)])
    advance(1)
    expect(scaleOf(root)).toBeCloseTo(1.5, 2)
    map.surface.dispatchEvent(Object.assign(new Event('pointerdown'), { pointerId: 1, clientX: 0, clientY: 0, pointerType: 'mouse' }))
    advance(0.1)
    expect(scaleOf(root)).toBe(1)
  })

  it('grows the card over the middle of the frame when its sign is far off the map', async () => {
    const card = mount([momentSign('a', T0 + 600)], 'hcp-t', { a: URI })
    card.map.projected = { x: -900, y: 215 }
    play(card.root)
    advance(4.9)
    await settle()
    advance(1)
    const box = card.root.querySelector('.hcp-sign-box') as HTMLElement
    expect(box.style.visibility).toBe('')
    expect(scaleOf(card.root)).toBeCloseTo(1.5, 2)
    // Centred in the 800 by 480 map, though the point is nowhere near it.
    expect(parseFloat(box.style.left) - 900).toBeCloseTo((800 - 384) / 2, 0)
    expect(parseFloat(box.style.top) + 215).toBeCloseTo((480 - 336) / 2, 0)
    // And it goes back to nothing when the moment is over.
    advance(MOMENT_TOTAL_S)
    expect(box.style.visibility).toBe('hidden')
  })

  it('has the browser decode the photo while the card is up, ahead of the moment', async () => {
    const decode = vi.fn(async () => {})
    Object.defineProperty(HTMLImageElement.prototype, 'decode', { configurable: true, value: decode })
    try {
      const { root } = await toMoment([momentSign('a', T0 + 600)])
      expect(decode).toHaveBeenCalled()
      expect(decode.mock.contexts[0]).toBe(imgOf(root))
    } finally {
      delete (HTMLImageElement.prototype as unknown as Record<string, unknown>).decode
    }
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
  let card = { w: 220, h: 70 }
  beforeEach(() => {
    card = { w: 220, h: 70 }
    Object.defineProperty(HTMLElement.prototype, 'offsetWidth', { configurable: true, get() { return (this as HTMLElement).classList.contains('hcp-sign-card') ? card.w : 0 } })
    Object.defineProperty(HTMLElement.prototype, 'offsetHeight', { configurable: true, get() { return (this as HTMLElement).classList.contains('hcp-sign-card') ? card.h : 0 } })
  })
  afterEach(() => {
    delete (HTMLElement.prototype as unknown as Record<string, unknown>).offsetWidth
    delete (HTMLElement.prototype as unknown as Record<string, unknown>).offsetHeight
  })
  /** Opens a card on a map of this size with the boat at (x, y), and says where the card is on the map and where its leader goes. */
  const open = (x: number, y: number, map = { w: 900, h: 480 }, cardSize?: { w: number; h: number }) => {
    if (cardSize) card = cardSize
    const { root, map: fake } = mount([sign('a', T0 + 600)])
    fake.size = { w: map.w, h: map.h }
    fake.fire('resize')
    fake.projected = { x, y }
    click(root.querySelector('.hcp-sign-pin'))
    fake.fire('move')
    const box = root.querySelector('.hcp-sign-box') as HTMLElement
    const line = root.querySelector('.hcp-sign-line') as HTMLElement
    const scale = Number(/scale\(([\d.]+)\)/.exec(box.style.transform)?.[1] ?? 1)
    const left = x + parseFloat(box.style.left)
    const top = y + parseFloat(box.style.top)
    return {
      left, top, right: left + card.w * scale, bottom: top + card.h * scale, scale, hidden: box.style.visibility === 'hidden',
      line: line.hidden ? null : { length: parseFloat(line.style.width), angle: Number(/rotate\((-?[\d.]+)deg\)/.exec(line.style.transform)?.[1]) },
    }
  }
  const inside = (r: ReturnType<typeof open>, map: { w: number; h: number }) => {
    expect(r.left).toBeGreaterThanOrEqual(0)
    expect(r.right).toBeLessThanOrEqual(map.w)
    expect(r.top).toBeGreaterThanOrEqual(6 - 1e-6)
    expect(r.bottom).toBeLessThanOrEqual(map.h - 6 + 1e-6)
  }

  it('leaves a card in the middle where it is, above its point, the leader running up to it', () => {
    const r = open(450, 300)
    expect(r.left).toBe(340)
    expect(r.bottom).toBe(300 - 4 - 20)
    expect(r.line).toEqual({ length: 24, angle: -90 })
  })
  it('slides a card in from the left edge, its leader still on it', () => {
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
    const r = open(450, 30)
    expect(r.top).toBeGreaterThan(30)
    expect(r.line!.angle).toBe(90)
  })
  it('keeps it above near the bottom', () => {
    expect(open(450, 460).top).toBeLessThan(460)
  })

  describe('on a map too short for a card above or below its point', () => {
    const wide = { w: 1076, h: 416 }
    const photoCard = { w: 256, h: 224 }

    it('puts it beside the boat, whole, with the leader across: the 1076 by 416 case at (50, 215)', () => {
      const r = open(50, 215, wide, photoCard)
      // Right of the boat, centred on it as far as the map allows, at its own size.
      expect(r.left).toBe(70)
      expect(r.top).toBe(103)
      expect(r.scale).toBe(1)
      inside(r, wide)
      expect(r.line).toEqual({ length: 20, angle: 0 })
    })

    it('puts it on the left of a boat near the right edge', () => {
      const r = open(1040, 215, wide, photoCard)
      expect(r.right).toBeLessThanOrEqual(1040 - 20)
      inside(r, wide)
      expect(r.line!.angle).toBe(180)
    })

    it('keeps it beside the boat, not under the controls, when the map is shorter than the card and its margins allow', () => {
      const short = { w: 1076, h: 260 }
      const r = open(538, 130, short, photoCard)
      expect(r.scale).toBeLessThan(1)
      expect(r.top).toBeGreaterThanOrEqual(48)
      inside(r, short)
    })

    it('makes it smaller, rather than cutting it off, on a very short map', () => {
      const tiny = { w: 1076, h: 150 }
      const r = open(538, 75, tiny, photoCard)
      expect(r.scale).toBeLessThan(1)
      inside(r, tiny)
    })

    it('keeps it whole for a boat just past the edge, with no leader', () => {
      const r = open(-8, 215, wide, photoCard)
      inside(r, wide)
      expect(r.line).toBeNull()
    })

    it('shows no card for a boat far off the map', () => {
      expect(open(-900, 215, wide, photoCard).hidden).toBe(true)
    })

    it('draws the card in the same place the app\'s map does', () => {
      // The same function places it; the player only reads what the browser says of the card's size.
      const r = open(538, 215, wide, photoCard)
      const lib = placeSignCard({ point: { x: 538, y: 215 }, container: wide, card: photoCard, lift: 20 })
      expect(r.left).toBeCloseTo(lib.rect.x, 0)
      expect(r.top).toBeCloseTo(lib.rect.y, 0)
    })
  })
})

describe('the button while paused', () => {
  it('shows the play triangle with Resume, and the pause bars only while playing', () => {
    const { root } = mount([sign('a', T0 + 600)])
    play(root)
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
    const css = generateEmbed({ kind: 'passage', title: 'x', zone: { offsetHours: 0, label: 'UTC' }, units: 'metric', inset: false, playSeconds: 30, cardId: 'hcp-c', passages: [passage('1')] })
    expect(css).toMatch(/#hcp-c\.hcp-playing:not\(\.hcp-paused\) \.hcp-btn-icon/)
  })
})
