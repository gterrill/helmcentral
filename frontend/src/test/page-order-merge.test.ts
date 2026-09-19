import { describe, it, expect } from 'vitest'
import { mergeVisibleOrder, moveWithinDisplay } from '@/lib/page-order'

function pages(ids: string[]) {
  return ids.map((id) => ({ id }))
}

function displayPages(entries: [id: string, displayId?: string][]) {
  return entries.map(([id, display_id]) => (display_id === undefined ? { id } : { id, display_id }))
}

// ADR 0110, plan §6 trap 1: PATCH /api/dashboard-pages/:id/reorder rejects a
// partial page list, but once wall pages leave the Dashboard list and the
// header switcher, the only thing the operator can reorder from the UI is
// the *visible* (non-wall) subset. mergeVisibleOrder rebuilds the full list
// the endpoint needs from that partial reorder, leaving every wall page in
// its own absolute slot so an unrelated dashboard reorder can never move a
// wall page's position in the feed (feed order is page order).
describe('mergeVisibleOrder', () => {
  it('is a no-op reorder when every page is visible', () => {
    expect(mergeVisibleOrder(pages(['a', 'b', 'c']), ['a', 'b', 'c'])).toEqual(['a', 'b', 'c'])
  })

  it('applies a full reorder when every page is visible', () => {
    expect(mergeVisibleOrder(pages(['a', 'b', 'c']), ['c', 'a', 'b'])).toEqual(['c', 'a', 'b'])
  })

  it('keeps a wall page in its absolute slot while the visible pages around it reorder', () => {
    // W is a wall page, sitting at index 1 in the full list; only A and B
    // (the visible pages) are offered to the switcher/reorder UI.
    const all = pages(['a', 'w', 'b'])
    expect(mergeVisibleOrder(all, ['b', 'a'])).toEqual(['b', 'w', 'a'])
  })

  it('keeps multiple wall pages fixed across a full reversal of the visible pages', () => {
    const all = pages(['w1', 'a', 'b', 'c', 'w2'])
    expect(mergeVisibleOrder(all, ['c', 'b', 'a'])).toEqual(['w1', 'c', 'b', 'a', 'w2'])
  })

  it('returns the original order unchanged when there are no visible pages', () => {
    const all = pages(['w1', 'w2'])
    expect(mergeVisibleOrder(all, [])).toEqual(['w1', 'w2'])
  })

  it('handles a single visible page among several wall pages', () => {
    const all = pages(['w1', 'a', 'w2', 'w3'])
    expect(mergeVisibleOrder(all, ['a'])).toEqual(['w1', 'a', 'w2', 'w3'])
  })
})

// moveWithinDisplay: a wall display's feed order is the global page order,
// but "move up/down" in the editor has to mean "swap with the previous/next
// page on *this* display", not the previous/next page globally — otherwise
// moving a display's 2nd page up when some other display's page sits
// between it and the 1st would silently do nothing (they're not adjacent in
// the global list) or swap with the wrong page. So this filters allPages
// down to the one display's pages in current order, swaps within that
// filtered list, and feeds the result through mergeVisibleOrder using the
// display's own pages as the "visible" subset — which is exactly what keeps
// every other display's pages pinned to their absolute slots.
describe('moveWithinDisplay', () => {
  it('moves a page up, swapping with the previous page on the same display', () => {
    const all = displayPages([['a', 'd1'], ['b', 'd1'], ['c', 'd1']])
    expect(moveWithinDisplay(all, 'd1', 'b', 'up')).toEqual(['b', 'a', 'c'])
  })

  it('moves a page down, swapping with the next page on the same display', () => {
    const all = displayPages([['a', 'd1'], ['b', 'd1'], ['c', 'd1']])
    expect(moveWithinDisplay(all, 'd1', 'b', 'down')).toEqual(['a', 'c', 'b'])
  })

  it('returns null moving the first page on the display up', () => {
    const all = displayPages([['a', 'd1'], ['b', 'd1'], ['c', 'd1']])
    expect(moveWithinDisplay(all, 'd1', 'a', 'up')).toBeNull()
  })

  it('returns null moving the last page on the display down', () => {
    const all = displayPages([['a', 'd1'], ['b', 'd1'], ['c', 'd1']])
    expect(moveWithinDisplay(all, 'd1', 'c', 'down')).toBeNull()
  })

  it('returns null for a page id that is not on the given display', () => {
    const all = displayPages([['a', 'd1'], ['x', 'd2'], ['b', 'd1']])
    expect(moveWithinDisplay(all, 'd1', 'x', 'up')).toBeNull()
  })

  it('returns null for a page id absent from allPages entirely', () => {
    const all = displayPages([['a', 'd1'], ['b', 'd1']])
    expect(moveWithinDisplay(all, 'd1', 'ghost', 'up')).toBeNull()
  })

  it('returns null for a dashboard page with no display_id at all', () => {
    const all = displayPages([['a', 'd1'], ['b']])
    expect(moveWithinDisplay(all, 'd1', 'b', 'up')).toBeNull()
  })

  it('keeps another display\'s page sitting between two of this display\'s pages in its own absolute slot', () => {
    // x (d2) sits between a and b (both d1). Moving b up swaps it with a in
    // d1's own order, but x never moves out of index 1.
    const all = displayPages([['a', 'd1'], ['x', 'd2'], ['b', 'd1']])
    expect(moveWithinDisplay(all, 'd1', 'b', 'up')).toEqual(['b', 'x', 'a'])
  })

  it('leaves an interloping page from another display untouched when moving down instead', () => {
    const all = displayPages([['a', 'd1'], ['x', 'd2'], ['b', 'd1']])
    expect(moveWithinDisplay(all, 'd1', 'a', 'down')).toEqual(['b', 'x', 'a'])
  })

  it('returns every id exactly once, a permutation of the input', () => {
    const all = displayPages([['w1'], ['a', 'd1'], ['w2'], ['b', 'd1'], ['c', 'd1'], ['w3']])
    const result = moveWithinDisplay(all, 'd1', 'b', 'up')
    expect(result).not.toBeNull()
    expect(result).toHaveLength(all.length)
    expect(new Set(result)).toEqual(new Set(all.map((p) => p.id)))
  })
})
