import { describe, it, expect } from 'vitest'
import { mergeVisibleOrder } from '@/lib/page-order'

function pages(ids: string[]) {
  return ids.map((id) => ({ id }))
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
