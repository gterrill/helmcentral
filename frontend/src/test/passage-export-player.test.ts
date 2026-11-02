import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { prepareExport, type ExportInput } from '@/lib/passage-export'
import { INTRO_MS } from '@/lib/replay-intro'
import { STAY_S } from '@/lib/replay-time'

import { FakeMap, FakeMarker, INTRO_AND_A_BIT_MS, T0, mount, passage, resetFakes, startPlaying, useClock } from './export-player-harness'

// MapLibre is a recorder (happy-dom has no WebGL). What is checked is what the
// player asks of the map, and when: the replay itself is the app's, with its
// own tests. The player under test is the built one, the text a blog page
// receives, except where a test reaches into the TypeScript for its state.

const DAY = 86400

function base(over: Partial<ExportInput> = {}): ExportInput {
  return {
    kind: 'combined', title: 'Two days out', zone: { offsetHours: 10, label: 'UTC+10' }, units: 'metric', inset: true,
    playSeconds: 30, cardId: 'hcp-t',
    passages: [passage('1', T0), passage('2', T0 + 2 * DAY, { to_name: '' })],
    ...over,
  }
}

let driver: ReturnType<typeof useClock>
beforeEach(() => {
  resetFakes()
  driver = useClock()
  vi.spyOn(console, 'error').mockImplementation(() => {})
})
afterEach(() => { vi.restoreAllMocks(); vi.unstubAllGlobals(); document.body.innerHTML = '' })

const label = (root: HTMLElement) => (root.querySelector('.hcp-btn-label') as HTMLElement).textContent
const labels = (root: HTMLElement) => [...root.querySelectorAll('.hcp-maplabel')] as HTMLElement[]
const shownLabels = (root: HTMLElement) => labels(root).filter((e) => !e.hidden).map((e) => e.textContent)

describe('the card on load', () => {
  it('fills the title, dates and stats strip from the data, as text', () => {
    const evil = '</script><img src=x onerror=alert(1)>'
    const { root } = mount(base({ title: evil, passages: [passage('1', T0, { title: evil }), passage('2', T0 + 2 * DAY)] }), 'built')
    expect(root.querySelector('.hcp-title')?.textContent).toBe(evil)
    expect(root.querySelector('.hcp-dates')?.textContent).toBe('14 to 16 Sep 2026')
    expect(root.querySelectorAll('img')).toHaveLength(0)
    const stats = [...root.querySelectorAll('.hcp-stat')].map((s) => s.textContent)
    expect(stats).toEqual(['Distance20.0 NM', 'Underway2 h 00 m', 'Passages2', 'Nights2'])
  })

  it('shows the whole track and waits for a tap: no camera move, no projection change, no play', () => {
    const { root } = mount(base(), 'built')
    const map = FakeMap.last
    expect(map.options.bounds).toEqual([[148, -20], [148.1, -19.9]])
    expect(map.options.cooperativeGestures).toBe(true)
    map.fire('style.load')
    expect(map.calls.some(([k]) => k === 'jumpTo' || k === 'easeTo')).toBe(false)
    expect(map.projections()).toEqual([])
    expect(FakeMarker.last?.added ?? false).toBe(false)
    expect(FakeMap.all).toHaveLength(1)
    expect(root.className).toContain('hcp-idle')
    expect(label(root)).toBe('Play')
  })

  it('stacks the chart, the sea marks, the time of day, the place names and the whole track, in that order', () => {
    mount(base(), 'built')
    const map = FakeMap.last
    map.fire('style.load')
    expect(map.order).toEqual(['hcp-seamarks', 'replay-tint', 'place-names', 'passage-line-0', 'passage-line-1'])
    // The sailed track and the wake are not there until the replay has moved.
    expect(map.sourceIds().some((id) => id.startsWith('passage-speed') || id === 'replay-wake')).toBe(false)
  })

  it('says so when the map library did not load', () => {
    const { root } = mount(base(), 'built', 'hcp-t', null)
    expect((root.querySelector('.hcp-error') as HTMLElement).hidden).toBe(false)
    expect((root.querySelector('.hcp-btn') as HTMLElement).hidden).toBe(true)
  })
})

describe('what drives it', () => {
  it('is the app\'s replay: one time map, with a stay held between the passages', () => {
    const { api } = mount(base())
    // 30 s of sailing and one stay, with no clock, night counter or heading of its own.
    expect(api.config.time.totalWall).toBeCloseTo(30 + STAY_S, 6)
    expect(api.plan.stays.map((s) => s.label)).toEqual(['2 nights · Anchorage 1'])
    expect(api.fmtLocal(T0)).toBe('14 Sep 10:00 UTC+10')
  })

  it('spends the chosen play length underway', () => {
    for (const playSeconds of [15, 30, 60] as const) {
      const { api } = mount(base({ playSeconds }))
      expect(api.config.time.sailingWallAt(api.config.time.total)).toBeCloseTo(playSeconds, 6)
    }
  })
})

describe('tapping play', () => {
  it('starts over the globe at zoom 1.5, comes down onto the track, then shows the readout and plays', () => {
    const { root } = mount(base())
    const map = FakeMap.last
    map.fire('style.load')
    ;(root.querySelector('.hcp-btn') as HTMLElement).click()
    // The first picture of the opening move: the whole earth, on the globe.
    expect(map.jumps()[0]).toMatchObject({ zoom: 1.5, pitch: 0, bearing: 0 })
    expect(JSON.stringify(map.projections()[0])).toContain('vertical-perspective')
    expect(root.className).toContain('hcp-flying')
    // The overview waits for the camera to land.
    expect(FakeMap.all).toHaveLength(1)
    driver.advance(INTRO_MS / 2)
    expect(root.className).toContain('hcp-flying')
    driver.advance(INTRO_AND_A_BIT_MS - INTRO_MS / 2)
    expect(map.projections().at(-1)).toEqual({ type: 'mercator' })
    expect(FakeMap.all).toHaveLength(2)
    expect((root.querySelector('.hcp-inset') as HTMLElement).hidden).toBe(false)
    expect((root.querySelector('.hcp-readout') as HTMLElement).hidden).toBe(false)
    expect(FakeMarker.last.added).toBe(true)
    expect(root.className).toContain('hcp-playing')
    expect((root.querySelector('.hcp-time') as HTMLElement).textContent).toMatch(/^14 Sep 10:0\d UTC\+10$/)
    expect((root.querySelector('.hcp-passage') as HTMLElement).textContent).toBe('Leg 1')
    // A cruise comes down on its first passage, so the opening move lands on a camera of its own.
    expect(map.jumps().some((j) => j.zoom === 11)).toBe(true)
  })

  it('begins once however many times Play is pressed while it flies', () => {
    const card = mount(base())
    card.map.fire('style.load')
    card.button.click()
    card.button.click()
    card.button.click()
    driver.advance(INTRO_AND_A_BIT_MS)
    expect(card.root.className).toContain('hcp-playing')
    expect(FakeMap.all).toHaveLength(2) // the card's map and one overview map
    driver.advance(INTRO_AND_A_BIT_MS)
    expect(FakeMap.all).toHaveLength(2)
    expect(card.map.projections().filter((p) => p.type === 'mercator')).toHaveLength(1)
  })

  it('draws the sailed track coloured by speed, the wake, the light and the tint as the replay moves', () => {
    const card = mount(base({ kind: 'passage', passages: [passage('1', T0)] }))
    startPlaying(card, driver.advance)
    driver.advance(5000)
    const ids = card.map.sourceIds()
    expect(ids.some((id) => id.startsWith('passage-speed-0'))).toBe(true)
    expect(ids).toContain('replay-wake')
    // The sun over the boat: its light, its sky, and the tint over the chart.
    expect(card.map.calls.some(([k]) => k === 'setLight')).toBe(true)
    expect(card.map.getPaintProperty('replay-tint', 'background-color')).toMatch(/^#|^rgb/)
    expect(card.map.order.at(-1)).not.toBe('replay-tint')
  })

  it('pauses and resumes in place, with no second opening move', () => {
    const card = mount(base())
    startPlaying(card, driver.advance)
    driver.advance(2000)
    const jumps = card.map.jumps().length
    card.button.click()
    expect(label(card.root)).toBe('Resume')
    expect(card.root.className).toContain('hcp-paused')
    const at = card.api.player.wall
    driver.advance(1000)
    expect(card.api.player.wall).toBe(at)
    card.button.click()
    expect(label(card.root)).toBe('Pause')
    expect(card.root.className).not.toContain('hcp-flying')
    // Played from the middle: no jump back to the globe, no new projection.
    expect(card.map.jumps().slice(jumps).some((j) => j.zoom === 1.5)).toBe(false)
    expect(card.map.projections().filter((p) => JSON.stringify(p).includes('vertical-perspective'))).toHaveLength(1)
    driver.advance(1000)
    expect(card.api.player.wall).toBeGreaterThan(at)
  })

  it('holds the replay in a stay, names it, and then names the next passage as it sets sail', () => {
    const card = mount(base({ inset: false }))
    startPlaying(card, driver.advance)
    // The passage label from the moment the first passage sets sail.
    driver.advance(100)
    expect(shownLabels(card.root)).toEqual(['Day 1 · A → Anchorage 1'])
    // Into the stay at 15 s of sailing: the boat sits at its anchorage and the label says how long.
    driver.advance(15_000)
    expect(shownLabels(card.root)).toEqual(['2 nights · Anchorage 1'])
    expect(card.api.player.hold).toMatchObject({ kind: 'stay' })
    const during = card.api.player.clock
    driver.advance(500)
    expect(card.api.player.clock).toBe(during)
    // Out of the stay, on the next passage.
    driver.advance(STAY_S * 1000)
    expect(shownLabels(card.root)).not.toContain('2 nights · Anchorage 1')
    expect((card.root.querySelector('.hcp-passage') as HTMLElement).textContent).toBe('Leg 2')
  })

  it('keeps the labels clear of the overview map in a bottom corner, by the replay\'s own rule', () => {
    const left = (inset: boolean, corner: 'bottom-left' | 'top-right') => {
      document.body.innerHTML = ''
      resetFakes()
      const card = mount(base({ inset, insetView: { corner, zoomOffset: 0 } }))
      startPlaying(card, driver.advance)
      driver.advance(200)
      return labels(card.root).find((e) => !e.hidden)?.style.left
    }
    expect(left(false, 'top-right')).toBe('24px')
    expect(left(true, 'top-right')).toBe('24px')
    // 8 px from the side, the overview's 176 px width and a gap of 12.
    expect(left(true, 'bottom-left')).toBe('196px')
  })

  it.each([
    ['top-left', 'hcp-inset-tl'], ['top-right', 'hcp-inset-tr'], ['bottom-left', 'hcp-inset-bl'], ['bottom-right', 'hcp-inset-br'],
  ] as const)('places the overview in the %s corner and draws it at the exported zoom', (corner, cls) => {
    const card = mount(base({ insetView: { corner, zoomOffset: 2 } }))
    const inset = card.root.querySelector('.hcp-inset') as HTMLElement
    expect(inset.classList.contains(cls)).toBe(true)
    expect([...inset.classList].filter((c) => c.startsWith('hcp-inset-'))).toEqual([cls])
    startPlaying(card, driver.advance)
    const zoomed = prepareExport(base({ insetView: { corner, zoomOffset: 2 } })).insetZoom
    expect(FakeMap.all[1].options.zoom).toBe(zoomed)
  })

  it('lifts a bottom corner overview to just above the readout, wherever its top is', () => {
    const card = mount(base({ insetView: { corner: 'bottom-left', zoomOffset: 0 } }))
    const stage = card.root.querySelector('.hcp-stage') as HTMLElement
    const readout = card.root.querySelector('.hcp-readout') as HTMLElement
    Object.defineProperty(stage, 'clientHeight', { configurable: true, value: 400 })
    // A taller readout has its top higher.
    let top = 330
    Object.defineProperty(readout, 'offsetTop', { configurable: true, get: () => top })
    expect(card.root.style.getPropertyValue('--hcp-ro-gap')).toBe('')
    startPlaying(card, driver.advance)
    expect(card.root.style.getPropertyValue('--hcp-ro-gap')).toBe('78px')
    top = 290
    window.dispatchEvent(new Event('resize'))
    expect(card.root.style.getPropertyValue('--hcp-ro-gap')).toBe('118px')
  })

  it('leaves the overview out when it was off at export', () => {
    const card = mount(base({ inset: false }))
    startPlaying(card, driver.advance)
    expect(FakeMap.all).toHaveLength(1)
    expect((card.root.querySelector('.hcp-inset') as HTMLElement).hidden).toBe(true)
  })

  it('offers a replay at the end, and flies in again when it is taken', () => {
    const card = mount(base({ inset: false }))
    startPlaying(card, driver.advance)
    driver.advance((card.api.config.time.totalWall + 2) * 1000)
    expect(card.root.className).toContain('hcp-done')
    expect(label(card.root)).toBe('Replay')
    expect(card.api.player.ended).toBe(true)
    const globes = () => card.map.jumps().filter((j) => j.zoom === 1.5).length
    const before = globes()
    card.button.click()
    expect(globes()).toBe(before + 1)
    expect(card.root.className).toContain('hcp-flying')
    driver.advance(INTRO_AND_A_BIT_MS)
    expect(card.root.className).toContain('hcp-playing')
    expect(card.api.player.wall).toBeLessThan(1)
  })

  it('stops the opening move at a tap on the map, and starts playing from the start', () => {
    const card = mount(base())
    card.map.fire('style.load')
    card.button.click()
    driver.advance(1000)
    expect(card.root.className).toContain('hcp-flying')
    card.map.surface.dispatchEvent(Object.assign(new Event('pointerdown'), { pointerId: 1, clientX: 0, clientY: 0, pointerType: 'mouse' }))
    expect(card.root.className).toContain('hcp-playing')
    expect(card.map.projections().at(-1)).toEqual({ type: 'mercator' })
    expect(card.api.player.playing).toBe(true)
    expect(card.api.player.wall).toBeLessThan(1)
  })

  it('does not stop the opening move for a tap on a sign\'s card', () => {
    const card = mount(base())
    card.map.fire('style.load')
    card.button.click()
    const marker = document.createElement('div')
    marker.className = 'maplibregl-marker'
    const inner = document.createElement('span')
    marker.appendChild(inner)
    card.map.surface.appendChild(marker)
    inner.dispatchEvent(new Event('pointerdown', { bubbles: true }))
    expect(card.root.className).toContain('hcp-flying')
  })

  it('opens without a flight and at once with reduced motion', () => {
    vi.stubGlobal('matchMedia', (q: string) => ({ matches: q.includes('reduce'), addEventListener() {}, removeEventListener() {} }))
    const card = mount(base())
    card.map.fire('style.load')
    card.button.click()
    expect(card.map.jumps().some((j) => j.zoom === 1.5)).toBe(false)
    expect(card.root.className).toContain('hcp-playing')
    expect(card.api.player.playing).toBe(true)
  })

  it('waits for the style to load before it begins', () => {
    const card = mount(base())
    card.button.click()
    expect(card.map.jumps()).toEqual([])
    card.map.fire('style.load')
    expect(card.map.jumps()[0]).toMatchObject({ zoom: 1.5 })
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
    button.click()
    card.map.fire('idle')
    expect(credit.classList.contains('maplibregl-compact-show')).toBe(true)
  })
})

describe('keeping clear of the readout', () => {
  /** The readout at the foot of the map, 150 by 60, 28 px up, as the stylesheet puts it. */
  const readoutAt = (card: ReturnType<typeof mount>, h: number) => {
    const readout = card.root.querySelector('.hcp-readout') as HTMLElement
    for (const [name, value] of Object.entries({ offsetLeft: 10, offsetTop: h - 28 - 60, offsetWidth: 150, offsetHeight: 60 })) {
      Object.defineProperty(readout, name, { configurable: true, value })
    }
  }

  it('starts the passage label to the right of it, not over it', () => {
    const card = mount(base({ inset: false }))
    readoutAt(card, 480)
    startPlaying(card, driver.advance)
    driver.advance(200)
    // The readout ends at 160; the label gives it the usual 12 px.
    expect(labels(card.root).find((e) => !e.hidden)?.style.left).toBe('172px')
  })

  it('keeps the centred stay label clear of it on a narrow phone-sized card', () => {
    const card = mount(base({ inset: false }))
    card.map.size = { w: 360, h: 420 }
    card.map.fire('resize')
    readoutAt(card, 420)
    startPlaying(card, driver.advance)
    driver.advance(15_000)
    const stay = labels(card.root).find((e) => !e.hidden && e.textContent?.includes('nights')) as HTMLElement
    expect(stay).toBeDefined()
    const centre = parseFloat(stay.style.left)
    const width = parseFloat(stay.style.maxWidth)
    expect(centre - width / 2).toBeGreaterThanOrEqual(172 - 1e-6)
    expect(centre + width / 2).toBeLessThanOrEqual(360 - 24 + 1e-6)
  })
})

describe('a finger on the map', () => {
  const pointer = (type: string, x: number, y: number, kind: string) => Object.assign(new Event(type), { pointerId: 1, clientX: x, clientY: y, pointerType: kind })
  const flying = () => {
    const card = mount(base())
    card.map.fire('style.load')
    card.button.click()
    driver.advance(1000)
    expect(card.root.className).toContain('hcp-flying')
    return card
  }

  it('resting on the map to scroll the page does not end the opening move', () => {
    const card = flying()
    card.map.surface.dispatchEvent(pointer('pointerdown', 100, 100, 'touch'))
    expect(card.root.className).toContain('hcp-flying')
    card.map.surface.dispatchEvent(pointer('pointermove', 100, 160, 'touch'))
    card.map.surface.dispatchEvent(pointer('pointerup', 100, 160, 'touch'))
    expect(card.root.className).toContain('hcp-flying')
  })

  it('lifted without moving past a few pixels it is a tap, and ends the opening move', () => {
    const card = flying()
    card.map.surface.dispatchEvent(pointer('pointerdown', 100, 100, 'touch'))
    card.map.surface.dispatchEvent(pointer('pointermove', 103, 102, 'touch'))
    card.map.surface.dispatchEvent(pointer('pointerup', 103, 102, 'touch'))
    expect(card.root.className).toContain('hcp-playing')
  })

  it('a pinch is not a tap', () => {
    const card = flying()
    const touch = (id: number, type: string) => Object.assign(new Event(type), { pointerId: id, clientX: 0, clientY: 0, pointerType: 'touch' })
    card.map.surface.dispatchEvent(touch(1, 'pointerdown'))
    card.map.surface.dispatchEvent(touch(2, 'pointerdown'))
    card.map.surface.dispatchEvent(touch(2, 'pointerup'))
    card.map.surface.dispatchEvent(touch(1, 'pointerup'))
    expect(card.root.className).toContain('hcp-flying')
  })

  it('a mouse press ends it at once, and so does the wheel with Ctrl', () => {
    const mouse = flying()
    mouse.map.surface.dispatchEvent(pointer('pointerdown', 100, 100, 'mouse'))
    expect(mouse.root.className).toContain('hcp-playing')
    resetFakes()
    const wheel = flying()
    wheel.map.surface.dispatchEvent(Object.assign(new Event('wheel'), { ctrlKey: false }))
    expect(wheel.root.className).toContain('hcp-flying')
    wheel.map.surface.dispatchEvent(Object.assign(new Event('wheel'), { ctrlKey: true }))
    expect(wheel.root.className).toContain('hcp-playing')
  })
})

describe('where the camera is left when the opening move is cut short', () => {
  const landing = { center: [148.05, -19.95], zoom: 11, pitch: 0, bearing: 0 }

  it('a tap lands it on the framing, which a single passage with no shots keeps for the whole replay', () => {
    const card = mount({ kind: 'passage', passages: [passage('1', T0)] })
    card.map.fire('style.load')
    card.button.click()
    driver.advance(1000)
    expect(card.map.cam.zoom).toBeLessThan(11)
    card.map.surface.dispatchEvent(Object.assign(new Event('pointerdown'), { pointerId: 1, clientX: 0, clientY: 0, pointerType: 'mouse' }))
    expect(card.map.cam).toEqual(landing)
    driver.advance(5000)
    expect(card.map.cam).toEqual(landing)
  })

  it('a drag takes the camera, and the framing is not forced on it', () => {
    const card = mount({ kind: 'passage', passages: [passage('1', T0)] })
    card.map.fire('style.load')
    card.button.click()
    driver.advance(1000)
    card.map.surface.dispatchEvent(Object.assign(new Event('pointerdown'), { pointerId: 1, clientX: 0, clientY: 0, pointerType: 'mouse' }))
    // The mouse press already landed it; a drag now moves the map where the reader likes.
    card.map.cam.zoom = 9
    card.map.surface.dispatchEvent(Object.assign(new Event('pointermove'), { pointerId: 1, clientX: 40, clientY: 0, pointerType: 'mouse' }))
    driver.advance(2000)
    expect(card.map.cam.zoom).toBe(9)
  })

  it('a hand that takes the map before the tap lands keeps it', () => {
    const card = mount({ kind: 'passage', passages: [passage('1', T0)] })
    card.map.fire('style.load')
    card.button.click()
    driver.advance(1000)
    card.map.fire('movestart', { originalEvent: {} })
    card.map.cam.zoom = 9
    card.map.fire('movestart', { originalEvent: {} })
    expect(card.map.cam.zoom).toBe(9)
  })
})
