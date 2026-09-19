import { describe, it, expect } from 'vitest'
import { GRID_MARGIN, WALL_ROW_MARGIN } from '@/components/dashboard-bento-grid'
import {
  DISPLAY_ROOT_PADDING_PX,
  PIXEL_SHIFT_AMPLITUDE_PX,
  NARROW_STRIP_FOLD_PX,
  displayRowsThatFit,
  parseDisplayOptions,
  displayScale,
  displayFoldPx,
  displayRowMargin,
  displayFeed,
  nextDisplayIndex,
  prevDisplayIndex,
  type Display,
  type DisplayEligiblePage,
} from '@/lib/displays'
import type { DashboardLayoutItem } from '@/lib/dashboard-widgets'

const WIDGET: DashboardLayoutItem = { id: 'depth-tide', x: 0, y: 0, w: 4, h: 7 }

function page(id: string, overrides: Partial<DisplayEligiblePage> = {}): DisplayEligiblePage {
  return { id, widgets: [WIDGET], ...overrides }
}

function display(overrides: Partial<Display> = {}): Display {
  return {
    id: 'd1',
    name: 'Flybridge',
    slug: 'flybridge',
    width: 1920,
    height: 360,
    scale: 1,
    rotate: 0,
    pixel_shift: false,
    wake_lock: false,
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
    ...overrides,
  }
}

describe('NARROW_STRIP_FOLD_PX', () => {
  it('is the 352px fold budget of a 360px strip with the shell padding removed', () => {
    expect(NARROW_STRIP_FOLD_PX).toBe(352)
  })
})

describe('displayRowsThatFit', () => {
  it('fits 9 rows into the 352px fold budget at the tight (wall) margin', () => {
    expect(displayRowsThatFit(NARROW_STRIP_FOLD_PX, WALL_ROW_MARGIN)).toBe(9)
  })

  it('fits exactly 8 rows at 312px and not yet 9 at 344px, at the tight margin', () => {
    expect(displayRowsThatFit(312, WALL_ROW_MARGIN)).toBe(8)
    expect(displayRowsThatFit(344, WALL_ROW_MARGIN)).toBe(8)
  })

  it('takes the margin as a parameter rather than hardcoding the wall value', () => {
    // At the generous (dashboard) margin, the same 352px budget fits fewer rows.
    expect(displayRowsThatFit(NARROW_STRIP_FOLD_PX, GRID_MARGIN)).toBe(7)
  })
})

describe('parseDisplayOptions', () => {
  it('defaults to no pinned page', () => {
    expect(parseDisplayOptions('')).toEqual({ pageId: null })
  })

  it('pins a page from ?page=', () => {
    expect(parseDisplayOptions('?page=abc-123')).toEqual({ pageId: 'abc-123' })
  })

  it('no longer parses rotate or height - they moved onto the display record', () => {
    const result = parseDisplayOptions('?rotate=180&height=360&page=abc-123') as unknown as Record<string, unknown>
    expect(result).toEqual({ pageId: 'abc-123' })
    expect(result.rotate).toBeUndefined()
    expect(result.height).toBeUndefined()
  })
})

describe('displayScale', () => {
  it('defaults to 1 when scale is 0 or absent', () => {
    expect(displayScale(display({ scale: 0 }))).toBe(1)
    expect(displayScale(display({ scale: undefined as unknown as number }))).toBe(1)
  })

  it('returns the configured scale otherwise', () => {
    expect(displayScale(display({ scale: 1.5 }))).toBe(1.5)
  })
})

describe('displayFoldPx', () => {
  it('is null for a zero canvas', () => {
    expect(displayFoldPx(display({ width: 0, height: 0 }))).toBeNull()
  })

  it('subtracts the shell root padding from the configured height', () => {
    expect(displayFoldPx(display({ height: 360, pixel_shift: false }))).toBe(360 - 2 * DISPLAY_ROOT_PADDING_PX)
  })

  it('also subtracts room for the pixel shift when it is on', () => {
    const withoutShift = displayFoldPx(display({ height: 1080, pixel_shift: false }))!
    const withShift = displayFoldPx(display({ height: 1080, pixel_shift: true }))!
    expect(withShift).toBe(withoutShift - 2 * PIXEL_SHIFT_AMPLITUDE_PX)
  })
})

describe('displayRowMargin', () => {
  it('uses the tight WALL_ROW_MARGIN below a ~600px fold budget', () => {
    expect(displayRowMargin(display({ height: 360 }))).toBe(WALL_ROW_MARGIN)
  })

  it('uses the generous GRID_MARGIN above a ~600px fold budget', () => {
    expect(displayRowMargin(display({ height: 1080 }))).toBe(GRID_MARGIN)
  })

  it('uses the generous GRID_MARGIN for a zero (unmeasured) canvas', () => {
    expect(displayRowMargin(display({ width: 0, height: 0 }))).toBe(GRID_MARGIN)
  })
})

describe('displayFeed', () => {
  it('keeps only pages assigned to this display with a positive dwell and at least one widget', () => {
    const pages = [
      page('a', { display_id: 'd1', dwell_seconds: 30 }),
      page('b', { display_id: 'd2', dwell_seconds: 30 }),
      page('c', { display_id: 'd1', dwell_seconds: 0 }),
      page('d', { display_id: 'd1', dwell_seconds: 30, widgets: [] }),
      page('e', { dwell_seconds: 30 }),
    ]
    expect(displayFeed(pages, { displayId: 'd1', navigationState: null }).map((p) => p.id)).toEqual(['a'])
  })

  it('preserves the given (server) order', () => {
    const pages = [
      page('z', { display_id: 'd1', dwell_seconds: 10 }),
      page('a', { display_id: 'd1', dwell_seconds: 10 }),
    ]
    expect(displayFeed(pages, { displayId: 'd1', navigationState: null }).map((p) => p.id)).toEqual(['z', 'a'])
  })

  it('includes a state-conditioned page only when navigation state matches', () => {
    const pages = [page('a', { display_id: 'd1', dwell_seconds: 10, show_when: 'anchored' })]
    expect(displayFeed(pages, { displayId: 'd1', navigationState: 'moored' })).toEqual([])
    expect(displayFeed(pages, { displayId: 'd1', navigationState: 'anchored' }).map((p) => p.id)).toEqual(['a'])
  })

  it('includes motoring/sailing/moored pages only in those states', () => {
    const pages = [
      page('m', { display_id: 'd1', dwell_seconds: 10, show_when: 'motoring' }),
      page('s', { display_id: 'd1', dwell_seconds: 10, show_when: 'sailing' }),
      page('d', { display_id: 'd1', dwell_seconds: 10, show_when: 'moored' }),
    ]
    expect(displayFeed(pages, { displayId: 'd1', navigationState: 'motoring' }).map((p) => p.id)).toEqual(['m'])
    expect(displayFeed(pages, { displayId: 'd1', navigationState: 'sailing' }).map((p) => p.id)).toEqual(['s'])
    expect(displayFeed(pages, { displayId: 'd1', navigationState: 'moored' }).map((p) => p.id)).toEqual(['d'])
  })

  it('treats engine-running navigation aliases as motoring', () => {
    const pages = [page('m', { display_id: 'd1', dwell_seconds: 10, show_when: 'motoring' })]
    expect(displayFeed(pages, { displayId: 'd1', navigationState: 'under way using engine' }).map((p) => p.id)).toEqual(['m'])
    expect(displayFeed(pages, { displayId: 'd1', navigationState: 'under_way_using_engine' }).map((p) => p.id)).toEqual(['m'])
  })

  it('includes an "always" or unset condition regardless of navigation state', () => {
    const pages = [
      page('a', { display_id: 'd1', dwell_seconds: 10, show_when: 'always' }),
      page('b', { display_id: 'd1', dwell_seconds: 10 }),
    ]
    expect(displayFeed(pages, { displayId: 'd1', navigationState: null }).map((p) => p.id)).toEqual(['a', 'b'])
    expect(displayFeed(pages, { displayId: 'd1', navigationState: 'motoring' }).map((p) => p.id)).toEqual(['a', 'b'])
    expect(displayFeed(pages, { displayId: 'd1', navigationState: 'anchored' }).map((p) => p.id)).toEqual(['a', 'b'])
  })
})

describe('nextDisplayIndex', () => {
  const feed = [{ id: 'a' }, { id: 'b' }, { id: 'c' }]

  it('advances to the next index', () => {
    expect(nextDisplayIndex(feed, 'a')).toBe(1)
    expect(nextDisplayIndex(feed, 'b')).toBe(2)
  })

  it('wraps around at the end', () => {
    expect(nextDisplayIndex(feed, 'c')).toBe(0)
  })

  it('restarts at 0 when the current id has vanished from the feed', () => {
    expect(nextDisplayIndex(feed, 'zzz')).toBe(0)
    expect(nextDisplayIndex(feed, null)).toBe(0)
  })

  it('returns -1 for an empty feed', () => {
    expect(nextDisplayIndex([], 'a')).toBe(-1)
  })
})

describe('prevDisplayIndex', () => {
  const feed = [{ id: 'a' }, { id: 'b' }, { id: 'c' }]

  it('steps back to the previous index', () => {
    expect(prevDisplayIndex(feed, 'c')).toBe(1)
    expect(prevDisplayIndex(feed, 'b')).toBe(0)
  })

  it('wraps around at the start', () => {
    expect(prevDisplayIndex(feed, 'a')).toBe(2)
  })

  it('restarts at 0 when the current id has vanished from the feed', () => {
    expect(prevDisplayIndex(feed, 'zzz')).toBe(0)
    expect(prevDisplayIndex(feed, null)).toBe(0)
  })

  it('returns -1 for an empty feed', () => {
    expect(prevDisplayIndex([], 'a')).toBe(-1)
  })
})
