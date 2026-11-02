import { act, render, screen } from '@testing-library/react'
import type { ReactNode } from 'react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { PhotoMoment } from '@/components/passages/photo-moment'
import { SignMarkers } from '@/components/passages/sign-markers'
import { signCardBox, type ReplaySign, type ResolvedSign } from '@/lib/replay-signs'
import { signNoteLines } from '@/lib/sign-note'
import { momentView } from '@/lib/replay-time'

const start = { x: 790, y: 400 }
let point = { ...start }
let box = { clientWidth: 800, clientHeight: 600 }
const handlers = new Map<string, () => void>()
const fakeMap = {
  project: () => point,
  getContainer: () => box,
  on: (ev: string, fn: () => void) => { handlers.set(ev, fn) },
  off: (ev: string) => { handlers.delete(ev) },
}
vi.mock('react-map-gl/maplibre', () => ({
  Marker: ({ children, anchor }: { children: ReactNode; anchor: string }) => <div data-testid="marker" data-anchor={anchor}>{children}</div>,
  useMap: () => ({ current: { getMap: () => fakeMap } }),
}))

let observed: ((entries: { contentRect: { width: number; height: number } }[]) => void) | null = null
class FakeResizeObserver {
  constructor(cb: typeof observed) { observed = cb }
  observe() {}
  disconnect() {}
}

const photoSign = (extra: Partial<ReplaySign> = {}): ReplaySign => ({
  id: 's1', t_ms: 0, title: 'Reef', note: '', document_id: null, photo_url: '/api/x.jpg', visibility: 'always', show_s: 4, starts_leg: false, photo_moment: null, ...extra,
})
const resolvedOf = (sign: ReplaySign): ResolvedSign => ({ sign, clock: 0, at: 0, entryId: 'p1', lat: 0, lon: 0 })
const mount = (sign = photoSign()) => render(<SignMarkers cards={[resolvedOf(sign)]} pins={[]} openId={null} canOpenPhoto={false} still onOpenPhoto={() => {}} onPinClick={() => {}} onCloseOpen={() => {}} />)
/** The card measured by the browser: 256 wide, as high as lib/replay-signs says for the lines of the note it shows. */
const observeCard = (sign = photoSign(), lines?: number) => {
  const n = lines ?? signNoteLines(sign, box.clientWidth, (t) => t.length * 6).length
  act(() => observed?.([{ contentRect: { width: signCardBox(sign, box.clientWidth, n).w, height: signCardBox(sign, box.clientWidth, n).h } }]))
}
const wrapper = () => screen.getByTestId('sign-card').parentElement as HTMLElement
/** Where the card is from the point, and how much smaller than its own size, as the transform says. */
const placed = (el: HTMLElement = wrapper()) => {
  const m = /translate\((-?[\d.]+)px, (-?[\d.]+)px\) scale\(([\d.]+)\)/.exec(el.style.transform)!
  return { x: Number(m[1]), y: Number(m[2]), scale: Number(m[3]) }
}
const leader = () => {
  const el = screen.queryByTestId('sign-leader')
  if (!el) return null
  const angle = /rotate\((-?[\d.]+)deg\)/.exec(el.style.transform)![1]
  return { length: parseFloat(el.style.width), angle: Number(angle) }
}
const move = (p: { x: number; y: number }) => { point = p; act(() => handlers.get('move')?.()) }

describe('SignMarkers placement', () => {
  beforeEach(() => {
    point = { ...start }
    box = { clientWidth: 800, clientHeight: 600 }
    handlers.clear()
    observed = null
    vi.stubGlobal('ResizeObserver', FakeResizeObserver)
  })
  afterEach(() => {
    vi.unstubAllGlobals()
    vi.restoreAllMocks()
  })

  it('places a card once its size is known, with no map move: slid left to stay inside, the leader on it', () => {
    mount()
    observeCard()
    // 224 high with a photo and a title: 20 above the point and 4 for its dot. Its right edge is at the map's edge less 4: the nearest to the margin it can be with the leader still on it.
    expect(placed()).toEqual({ x: 540 - 790, y: -4 - 20 - 224, scale: 1 })
    expect(leader()).toEqual({ length: 24, angle: -90 })
  })

  it('keeps one marker anchored at its point and moves the card by layout as the camera moves', () => {
    mount()
    observeCard()
    expect(screen.getByTestId('marker')).toHaveAttribute('data-anchor', 'center')
    move({ x: 400, y: 100 })
    // No room above: the card goes under the point, centred on it, with the leader down to it.
    expect(placed()).toEqual({ x: -128, y: 24, scale: 1 })
    expect(leader()).toEqual({ length: 24, angle: 90 })
    expect(screen.getByTestId('marker')).toHaveAttribute('data-anchor', 'center')
  })

  it('shortens the leader when the card only just fits', () => {
    mount()
    observeCard()
    move({ x: 400, y: 290 })
    // 290 - 224 - 48 = 18 to spare: the leader is 18 and the dot 4.
    expect(placed().y).toBe(-(4 + 18 + 224))
    expect(leader()!.length).toBe(22)
  })

  it('puts a card beside its point when it fits neither above nor below, whole inside the map', () => {
    box = { clientWidth: 1076, clientHeight: 416 }
    point = { x: 538, y: 215 }
    mount()
    observeCard()
    // To the right of the point, centred on it as far as the map allows, at full size.
    expect(placed()).toEqual({ x: 20, y: 103 - 215, scale: 1 })
    expect(leader()).toEqual({ length: 20, angle: 0 })
  })

  it('makes a card smaller, from its corner, on a map too short to hold it', () => {
    box = { clientWidth: 1076, clientHeight: 200 }
    point = { x: 538, y: 100 }
    mount()
    observeCard()
    const { x, y, scale } = placed()
    expect(scale).toBeLessThan(1)
    expect(wrapper().style.transformOrigin).toBe('0 0')
    expect(100 + y).toBeGreaterThanOrEqual(6 - 1e-6)
    expect(100 + y + 224 * scale).toBeLessThanOrEqual(200 - 6 + 1e-6)
    expect(538 + x + 256 * scale).toBeLessThanOrEqual(1076)
  })

  it('does not flip a card across the boat as the camera dithers about the middle of the map', () => {
    box = { clientWidth: 1076, clientHeight: 416 }
    point = { x: 530, y: 215 }
    mount()
    observeCard()
    expect(placed().x).toBeGreaterThan(0)
    for (const x of [538, 546, 552, 541, 533, 538]) {
      move({ x, y: 215 })
      expect(placed().x).toBeGreaterThan(0)
    }
    // Out of room on the right, it goes to the left.
    move({ x: 900, y: 215 })
    expect(placed().x).toBeLessThan(0)
  })

  it('keeps a card whole inside the map when its point is just past the edge, with no leader', () => {
    box = { clientWidth: 800, clientHeight: 600 }
    point = { x: -12, y: 400 }
    mount()
    observeCard()
    const { x, y } = placed()
    expect(-12 + x).toBeGreaterThanOrEqual(0)
    expect(-12 + x + 256).toBeLessThanOrEqual(800)
    expect(400 + y).toBeGreaterThanOrEqual(6)
    expect(leader()).toBeNull()
    expect(wrapper().style.visibility).toBe('')
  })

  it('hides the card of a point far off the map', () => {
    point = { x: -900, y: 400 }
    mount()
    observeCard()
    expect(wrapper().style.visibility).toBe('hidden')
    expect(leader()).toBeNull()
  })
})

describe('a card and its photo moment', () => {
  beforeEach(() => {
    handlers.clear()
    observed = null
    vi.stubGlobal('ResizeObserver', FakeResizeObserver)
  })
  afterEach(() => { vi.unstubAllGlobals() })

  it('agree on the card, its place and its lines, for a note that wraps', () => {
    box = { clientWidth: 1076, clientHeight: 416 }
    point = { x: 538, y: 215 }
    const sign = photoSign({ note: 'A humpback and her calf off the bow of the boat' })
    expect(sign.note.length).toBeGreaterThan(40)
    mount(sign)
    observeCard(sign)
    const onMap = placed()
    // The note is wrapped into two lines by measuring it, on the map and in the moment alike.
    const lines = signNoteLines(sign, 1076, (t) => t.length * 6)
    expect(lines).toHaveLength(2)
    const marker = document.querySelector('[data-testid="sign-card"]') as HTMLElement
    for (const line of lines) expect(marker).toHaveTextContent(line)
    const { unmount } = render(<PhotoMoment view={momentView('s1', 0, false)} sign={sign} lift={20} project={() => ({ point, container: { w: 1076, h: 416 } })} />)
    const moment = screen.getByTestId('photo-moment-card')
    const m = /translate\((-?[\d.]+)px, (-?[\d.]+)px\) scale\(([\d.]+)\)/.exec(moment.style.transform)!
    // At the start of the moment the card is where it stands on the map: the same side, the same rectangle.
    expect(Number(m[1])).toBeCloseTo(point.x + onMap.x)
    expect(Number(m[2])).toBeCloseTo(point.y + onMap.y)
    expect(Number(m[3])).toBe(1)
    for (const line of lines) expect(moment).toHaveTextContent(line)
    // Both draw the caption one line at a time, so their heights are the lib's and not the browser's.
    expect(moment.querySelectorAll('span.truncate')).toHaveLength(1 + lines.length)
    unmount()
  })
})
