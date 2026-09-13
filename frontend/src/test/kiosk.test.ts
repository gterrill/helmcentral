import { describe, it, expect } from 'vitest'
import {
  KIOSK_FOLD_PX,
  kioskRowsThatFit,
  parseKioskOptions,
  kioskFeed,
  nextKioskIndex,
  type KioskEligiblePage,
} from '@/lib/kiosk'
import type { DashboardLayoutItem } from '@/lib/dashboard-widgets'

const WIDGET: DashboardLayoutItem = { id: 'depth-tide', x: 0, y: 0, w: 4, h: 7 }

function page(id: string, overrides: Partial<KioskEligiblePage> = {}): KioskEligiblePage {
  return { id, widgets: [WIDGET], ...overrides }
}

describe('kioskRowsThatFit', () => {
  it('fits 7 rows into the 344px fold budget', () => {
    expect(KIOSK_FOLD_PX).toBe(344)
    expect(kioskRowsThatFit(KIOSK_FOLD_PX)).toBe(7)
  })

  it('fits exactly 7 rows at 320px and not yet 8 at 344px', () => {
    expect(kioskRowsThatFit(320)).toBe(7)
    expect(kioskRowsThatFit(344)).toBe(7)
  })

  it('fits 8 rows once the budget reaches 368px', () => {
    expect(kioskRowsThatFit(368)).toBe(8)
  })
})

describe('parseKioskOptions', () => {
  it('defaults to no rotation and no pinned page', () => {
    expect(parseKioskOptions('')).toEqual({ rotate: 0, pageId: null, height: null })
  })

  it('rotates only on the literal value 180', () => {
    expect(parseKioskOptions('?rotate=180')).toEqual({ rotate: 180, pageId: null, height: null })
    expect(parseKioskOptions('?rotate=90')).toEqual({ rotate: 0, pageId: null, height: null })
    expect(parseKioskOptions('?rotate=-180')).toEqual({ rotate: 0, pageId: null, height: null })
  })

  it('pins a page from ?page=', () => {
    expect(parseKioskOptions('?page=abc-123')).toEqual({ rotate: 0, pageId: 'abc-123', height: null })
  })

  it('reads both together', () => {
    expect(parseKioskOptions('?rotate=180&page=abc-123')).toEqual({ rotate: 180, pageId: 'abc-123', height: null })
  })

  it('accepts an integer height within 200 to 4320, inclusive of both ends', () => {
    expect(parseKioskOptions('?height=360')).toEqual({ rotate: 0, pageId: null, height: 360 })
    expect(parseKioskOptions('?height=200')).toEqual({ rotate: 0, pageId: null, height: 200 })
    expect(parseKioskOptions('?height=4320')).toEqual({ rotate: 0, pageId: null, height: 4320 })
  })

  it('falls back to null (full viewport) for an out-of-range height', () => {
    expect(parseKioskOptions('?height=199')).toEqual({ rotate: 0, pageId: null, height: null })
    expect(parseKioskOptions('?height=4321')).toEqual({ rotate: 0, pageId: null, height: null })
    expect(parseKioskOptions('?height=0')).toEqual({ rotate: 0, pageId: null, height: null })
    expect(parseKioskOptions('?height=-360')).toEqual({ rotate: 0, pageId: null, height: null })
  })

  it('falls back to null for a non-numeric or non-integer height', () => {
    expect(parseKioskOptions('?height=abc')).toEqual({ rotate: 0, pageId: null, height: null })
    expect(parseKioskOptions('?height=360.5')).toEqual({ rotate: 0, pageId: null, height: null })
    expect(parseKioskOptions('?height=')).toEqual({ rotate: 0, pageId: null, height: null })
  })

  it('reads height alongside rotate and page', () => {
    expect(parseKioskOptions('?rotate=180&page=abc-123&height=360')).toEqual({
      rotate: 180,
      pageId: 'abc-123',
      height: 360,
    })
  })
})

describe('kioskFeed', () => {
  it('keeps only pages flagged kiosk with a positive duration and at least one widget', () => {
    const pages = [
      page('a', { kiosk: true, kiosk_seconds: 30 }),
      page('b', { kiosk: false, kiosk_seconds: 30 }),
      page('c', { kiosk: true, kiosk_seconds: 0 }),
      page('d', { kiosk: true, kiosk_seconds: 30, widgets: [] }),
    ]
    expect(kioskFeed(pages, { navigationState: null }).map((p) => p.id)).toEqual(['a'])
  })

  it('preserves the given (server) order', () => {
    const pages = [
      page('z', { kiosk: true, kiosk_seconds: 10 }),
      page('a', { kiosk: true, kiosk_seconds: 10 }),
    ]
    expect(kioskFeed(pages, { navigationState: null }).map((p) => p.id)).toEqual(['z', 'a'])
  })

  it('includes a state-conditioned page only when navigation state matches', () => {
    const pages = [page('a', { kiosk: true, kiosk_seconds: 10, kiosk_when: 'anchored' })]
    expect(kioskFeed(pages, { navigationState: 'moored' })).toEqual([])
    expect(kioskFeed(pages, { navigationState: 'anchored' }).map((p) => p.id)).toEqual(['a'])
  })

  it('includes motoring/sailing/moored pages only in those states', () => {
    const pages = [
      page('m', { kiosk: true, kiosk_seconds: 10, kiosk_when: 'motoring' }),
      page('s', { kiosk: true, kiosk_seconds: 10, kiosk_when: 'sailing' }),
      page('d', { kiosk: true, kiosk_seconds: 10, kiosk_when: 'moored' }),
    ]
    expect(kioskFeed(pages, { navigationState: 'motoring' }).map((p) => p.id)).toEqual(['m'])
    expect(kioskFeed(pages, { navigationState: 'sailing' }).map((p) => p.id)).toEqual(['s'])
    expect(kioskFeed(pages, { navigationState: 'moored' }).map((p) => p.id)).toEqual(['d'])
  })

  it('includes an "always" or unset condition regardless of navigation state', () => {
    const pages = [
      page('a', { kiosk: true, kiosk_seconds: 10, kiosk_when: 'always' }),
      page('b', { kiosk: true, kiosk_seconds: 10 }),
    ]
    expect(kioskFeed(pages, { navigationState: null }).map((p) => p.id)).toEqual(['a', 'b'])
    expect(kioskFeed(pages, { navigationState: 'motoring' }).map((p) => p.id)).toEqual(['a', 'b'])
    expect(kioskFeed(pages, { navigationState: 'anchored' }).map((p) => p.id)).toEqual(['a', 'b'])
  })
})

describe('nextKioskIndex', () => {
  const feed = [{ id: 'a' }, { id: 'b' }, { id: 'c' }]

  it('advances to the next index', () => {
    expect(nextKioskIndex(feed, 'a')).toBe(1)
    expect(nextKioskIndex(feed, 'b')).toBe(2)
  })

  it('wraps around at the end', () => {
    expect(nextKioskIndex(feed, 'c')).toBe(0)
  })

  it('restarts at 0 when the current id has vanished from the feed', () => {
    expect(nextKioskIndex(feed, 'zzz')).toBe(0)
    expect(nextKioskIndex(feed, null)).toBe(0)
  })

  it('returns -1 for an empty feed', () => {
    expect(nextKioskIndex([], 'a')).toBe(-1)
  })
})
