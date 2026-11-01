import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { PhotoMoment } from '@/components/passages/photo-moment'
import { SignCard } from '@/components/passages/sign-card'
import { SIGN_CARD_PAD_PX, SIGN_CARD_PHOTO_W_PX, SIGN_CARD_TEXT_W_PX, signCardPhotoRect, type ReplaySign } from '@/lib/replay-signs'
import { MOMENT_GROW_S, momentRect, momentView } from '@/lib/replay-time'

const sign: ReplaySign = {
  id: 's1', t_ms: 0, title: 'Whale', note: '', document_id: 'd1', photo_url: '/api/documents/d1/content',
  visibility: 'passing', show_s: 4, starts_leg: false, photo_moment: null,
}
const container = { w: 1000, h: 400 }
const point = { x: 500, y: 300 }
const project = () => ({ point, container })

const box = () => screen.getByAltText('Whale').parentElement as HTMLElement
const px = (v: string) => parseFloat(v)

describe('PhotoMoment', () => {
  it('shows the sign\'s photo, named for the sign', () => {
    render(<PhotoMoment view={momentView('s1', 1, false)} sign={sign} lift={20} project={project} />)
    expect(screen.getByAltText('Whale')).toHaveAttribute('src', expect.stringContaining('/api/documents/d1/content'))
  })

  it('draws the rect lib/replay-time works out from the card\'s photo at the projected point', () => {
    const view = momentView('s1', 0.2, false)
    const want = momentRect(view, signCardPhotoRect(sign, point, container, 20), container)
    render(<PhotoMoment view={view} sign={sign} lift={20} project={project} />)
    expect(px(box().style.left)).toBeCloseTo(want.x)
    expect(px(box().style.top)).toBeCloseTo(want.y)
    expect(px(box().style.width)).toBeCloseTo(want.w)
    expect(px(box().style.height)).toBeCloseTo(want.h)
  })

  it('starts as the card\'s photo and ends filling 70 percent of the map', () => {
    const { unmount } = render(<PhotoMoment view={momentView('s1', 0, false)} sign={sign} lift={20} project={project} />)
    expect([px(box().style.left), px(box().style.top), px(box().style.width), px(box().style.height)]).toEqual([380, 60, 240, 180])
    unmount()
    render(<PhotoMoment view={momentView('s1', MOMENT_GROW_S + 0.1, false)} sign={sign} lift={20} project={project} />)
    expect(px(box().style.height)).toBeCloseTo(280)
  })

  it('zooms the photo inside its frame', () => {
    render(<PhotoMoment view={momentView('s1', 1.5, false)} sign={sign} lift={20} project={project} />)
    const scale = parseFloat((screen.getByAltText('Whale') as HTMLElement).style.transform.replace('scale(', ''))
    expect(scale).toBeGreaterThan(1)
    expect(scale).toBeLessThanOrEqual(1.06)
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
    render(<SignCard sign={sign} canOpenPhoto={false} still onOpenPhoto={() => {}} />)
    expect(surface().style.width).toBe(`${SIGN_CARD_PHOTO_W_PX}px`)
    expect(surface().style.padding).toBe(`${SIGN_CARD_PAD_PX}px`)
  })

  it('is narrower with text alone', () => {
    render(<SignCard sign={{ ...sign, document_id: null, photo_url: null }} canOpenPhoto={false} still onOpenPhoto={() => {}} />)
    expect(surface().style.width).toBe(`${SIGN_CARD_TEXT_W_PX}px`)
  })
})
