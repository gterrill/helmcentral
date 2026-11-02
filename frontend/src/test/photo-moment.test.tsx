import { fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { PhotoMoment } from '@/components/passages/photo-moment'
import { SignCard } from '@/components/passages/sign-card'
import { SIGN_CARD_PAD_PX, SIGN_CARD_PHOTO_W_PX, SIGN_CARD_TEXT_W_PX, signCardBox, signCardPlacement, type ReplaySign } from '@/lib/replay-signs'
import { signNoteLines } from '@/lib/sign-note'
import { MOMENT_GROW_S, momentRect, momentView } from '@/lib/replay-time'

const sign: ReplaySign = {
  id: 's1', t_ms: 0, title: 'Whale', note: '', document_id: 'd1', photo_url: '/api/documents/d1/content',
  visibility: 'passing', show_s: 4, starts_leg: false, photo_moment: null,
}
const container = { w: 1000, h: 400 }
const point = { x: 500, y: 300 }
const project = () => ({ point, container })

const card = () => screen.getByTestId('photo-moment-card')
const px = (v: string) => parseFloat(v)
/** The card's place and scale as the transform says. */
const placed = () => {
  const m = /translate\((-?[\d.]+)px, (-?[\d.]+)px\) scale\(([\d.]+)\)/.exec(card().style.transform)!
  return { x: Number(m[1]), y: Number(m[2]), k: Number(m[3]) }
}
// The test measure writes 6 px a character (test/setup.ts).
const measure = (t: string) => t.length * 6
const linesOf = (sg: ReplaySign, w = container.w) => signNoteLines(sg, w, measure)
const face = () => {
  const b = signCardBox(sign, container.w, linesOf(sign).length)
  return { w: b.w, h: b.h }
}

describe('PhotoMoment', () => {
  it('shows the sign\'s photo, named for the sign, with its caption', () => {
    render(<PhotoMoment view={momentView('s1', 1, false)} sign={{ ...sign, note: 'A humpback off the bow' }} lift={20} project={project} />)
    expect(screen.getByAltText('Whale')).toHaveAttribute('src', expect.stringContaining('/api/documents/d1/content'))
    expect(card()).toHaveTextContent('Whale')
    expect(card()).toHaveTextContent('A humpback off the bow')
  })

  it('is the whole card: its frame, photo and caption together, at the card\'s own size', () => {
    render(<PhotoMoment view={momentView('s1', 1, false)} sign={sign} lift={20} project={project} />)
    expect(card().style.width).toBe(`${SIGN_CARD_PHOTO_W_PX}px`)
    expect(card().style.padding).toBe(`${SIGN_CARD_PAD_PX}px`)
    expect(card().contains(screen.getByAltText('Whale'))).toBe(true)
    expect(card().style.transformOrigin).toBe('0 0')
  })

  it('places the card as lib/replay-time works out from the card at the projected point', () => {
    const view = momentView('s1', 0.2, false)
    const want = momentRect(view, signCardPlacement(sign, point, container, 20, 0).rect, face(), container)
    render(<PhotoMoment view={view} sign={sign} lift={20} project={project} />)
    expect(placed().x).toBeCloseTo(want.card.x)
    expect(placed().y).toBeCloseTo(want.card.y)
    expect(placed().k).toBeCloseTo(want.k)
  })

  it('starts as the card on the map and ends filling 70 percent of it', () => {
    const { unmount } = render(<PhotoMoment view={momentView('s1', 0, false)} sign={sign} lift={20} project={project} />)
    const start = signCardPlacement(sign, point, container, 20, 0).rect
    expect(placed()).toEqual({ x: start.x, y: start.y, k: 1 })
    unmount()
    render(<PhotoMoment view={momentView('s1', MOMENT_GROW_S + 0.1, false)} sign={sign} lift={20} project={project} />)
    // 70 percent of the 400 high map is 280, which is 1.25 times the card's 224: the card is made that much bigger.
    expect(224 * placed().k).toBeCloseTo(280)
    expect(placed().k).toBeCloseTo(1.25)
  })

  it('stays inside a short, wide map: 1076 by 416, with the boat low on it', () => {
    const wide = { w: 1076, h: 416 }
    render(<PhotoMoment view={momentView('s1', MOMENT_GROW_S + 0.5, false)} sign={sign} lift={20} project={() => ({ point: { x: 538, y: 215 }, container: wide })} />)
    const { x, y, k } = placed()
    expect(x).toBeGreaterThanOrEqual(0)
    expect(y).toBeGreaterThanOrEqual(0)
    expect(x + px(card().style.width) * k).toBeLessThanOrEqual(wide.w)
    expect(y + 224 * k).toBeLessThanOrEqual(wide.h)
  })

  it('zooms the photo inside its frame, and nothing else', () => {
    const { unmount } = render(<PhotoMoment view={momentView('s1', MOMENT_GROW_S + 0.3, false)} sign={sign} lift={20} project={project} />)
    const first = placed()
    const zoomOf = () => parseFloat((screen.getByAltText('Whale') as HTMLElement).style.transform.replace('scale(', ''))
    const z1 = zoomOf()
    unmount()
    render(<PhotoMoment view={momentView('s1', MOMENT_GROW_S + 1.3, false)} sign={sign} lift={20} project={project} />)
    expect(zoomOf()).toBeGreaterThan(z1)
    expect(zoomOf()).toBeLessThanOrEqual(1.06)
    // The photo's frame, which clips it, and the card do not move.
    expect(placed()).toEqual(first)
    expect(screen.getByAltText('Whale').parentElement!.className).toContain('overflow-hidden')
  })

  it('shows the card whole and still for the hold with reduced motion', () => {
    render(<PhotoMoment view={momentView('s1', 0, true)} sign={sign} lift={20} project={project} />)
    expect(224 * placed().k).toBeCloseTo(280)
    expect(parseFloat((screen.getByAltText('Whale') as HTMLElement).style.transform.replace('scale(', ''))).toBe(1)
  })

  it('lets touches through to the map', () => {
    render(<PhotoMoment view={momentView('s1', 1, false)} sign={sign} lift={20} project={project} />)
    expect(screen.getByTestId('photo-moment').className).toContain('pointer-events-none')
  })

  it('draws nothing for a sign whose photo is gone, or while the map cannot say where the sign is', () => {
    const { unmount } = render(<PhotoMoment view={momentView('s1', 1, false)} sign={{ ...sign, photo_url: null }} lift={20} project={project} />)
    expect(screen.queryByTestId('photo-moment')).toBeNull()
    unmount()
    render(<PhotoMoment view={momentView('s1', 1, false)} sign={sign} lift={20} project={() => null} />)
    expect(screen.queryByTestId('photo-moment')).toBeNull()
  })
})

describe('the sign card the photo grows from', () => {
  const surface = () => screen.getByRole('group', { name: 'Sign: Whale' })

  it('is as wide and as padded as lib/replay-signs says', () => {
    render(<SignCard sign={sign} canOpenPhoto={false} still onOpenPhoto={() => {}} noteLines={[]} />)
    expect(surface().style.width).toBe(`${SIGN_CARD_PHOTO_W_PX}px`)
    expect(surface().style.padding).toBe(`${SIGN_CARD_PAD_PX}px`)
  })

  it('is narrower with text alone', () => {
    render(<SignCard sign={{ ...sign, document_id: null, photo_url: null }} canOpenPhoto={false} still onOpenPhoto={() => {}} noteLines={[]} />)
    expect(surface().style.width).toBe(`${SIGN_CARD_TEXT_W_PX}px`)
  })

  it('shows the note as the lines it was wrapped into, one line each', () => {
    render(<SignCard sign={sign} canOpenPhoto={false} still onOpenPhoto={() => {}} noteLines={['A humpback and her calf', 'off the bow of the boat']} />)
    expect(screen.getByText('A humpback and her calf').style.lineHeight).toBe('16px')
    expect(screen.getByText('off the bow of the boat')).toBeInTheDocument()
  })

  it('says a photo that will not load inside the photo\'s place, with no line added to the card', () => {
    render(<SignCard sign={sign} canOpenPhoto={false} still onOpenPhoto={() => {}} noteLines={[]} />)
    fireEvent.error(document.querySelector('img') as HTMLImageElement)
    const missing = screen.getByTestId('sign-photo-missing')
    expect(missing).toHaveTextContent('Photo not available')
    expect(parseFloat(missing.style.aspectRatio)).toBeCloseTo(4 / 3)
    // Photo place, title: the same two blocks as with the photo.
    expect(surface().children).toHaveLength(2)
  })
})
