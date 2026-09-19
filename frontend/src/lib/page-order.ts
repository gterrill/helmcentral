// Pure and React-free, same reasoning app-location.ts and displays.ts give
// for their own purity.
//
// ADR 0110 moves wall pages out of the Dashboard sub-list and the header
// page switcher into their own sidebar group, so the only reorder UI left
// for the operator only ever offers the *visible* (non-wall) pages. But
// `PATCH /api/dashboard-pages/reorder` takes the whole page list and
// rejects a partial one — a subset silently 400s. mergeVisibleOrder is the
// pure helper that reconciles the two: rebuild the full list the endpoint
// needs from a reorder of just the visible subset.

export interface PageOrderPage {
  id: string
}

/**
 * Walks `allPages` in its current (server) order. At each slot held by a
 * page that also appears in `visibleOrderedIds`, takes the next id off that
 * list instead of the slot's own id — so the visible pages end up in their
 * new order. A slot held by a page absent from `visibleOrderedIds` (a wall
 * page) is left exactly as it was: its own id, in its own slot.
 *
 * That is what keeps a wall page's absolute position stable across a
 * reorder of the pages around it — and since feed order *is* page order for
 * a wall display, a dashboard reorder can never disturb a rotation it had
 * nothing to do with.
 */
export function mergeVisibleOrder(
  allPages: readonly PageOrderPage[],
  visibleOrderedIds: readonly string[],
): string[] {
  const visibleIds = new Set(visibleOrderedIds)
  let nextVisibleIndex = 0
  return allPages.map((page) => {
    if (!visibleIds.has(page.id)) return page.id
    const id = visibleOrderedIds[nextVisibleIndex]
    nextVisibleIndex += 1
    return id
  })
}

/**
 * ADR 0112 decision 3: moves one page up or down within its own wall
 * display's feed order, and returns the complete page-id list
 * `PUT /api/dashboard-pages/order` needs (the endpoint rejects a partial
 * list the same way the reorder endpoint mergeVisibleOrder serves does).
 *
 * The subtlety this exists to hide: a wall display's feed order *is* the
 * global page order (there's no separate wall ordering - see
 * mergeVisibleOrder's own comment), but "move up" in the per-display editor
 * has to mean "swap with the previous page on *this* display", not "swap
 * with whatever page happens to sit before it in the global list". Those
 * differ the moment another display's page (or an unassigned dashboard
 * page) sits between two of this display's pages: swapping globally-adjacent
 * pages would either do nothing (they're not this display's neighbour) or
 * silently move the interloping page.
 *
 * So this filters `allPages` down to the pages on `displayId`, in their
 * current relative order, swaps `pageId` with its neighbour *within that
 * filtered list*, and feeds the result through mergeVisibleOrder using the
 * display's own pages as the "visible" subset. That's what keeps every
 * other page - another display's, or a plain dashboard page - pinned to its
 * own absolute slot while only this display's pages permute through the
 * slots they occupy.
 *
 * Returns null - never a partial or malformed list - when the move can't be
 * made: `pageId` isn't in `allPages` at all, it's not on `displayId`, or
 * it's already at the end it's being moved toward.
 */
export function moveWithinDisplay(
  allPages: readonly (PageOrderPage & { display_id?: string })[],
  displayId: string,
  pageId: string,
  direction: 'up' | 'down',
): string[] | null {
  const onDisplay = allPages.filter((page) => page.display_id === displayId)
  const index = onDisplay.findIndex((page) => page.id === pageId)
  if (index === -1) return null

  const swapIndex = direction === 'up' ? index - 1 : index + 1
  if (swapIndex < 0 || swapIndex >= onDisplay.length) return null

  const reordered = onDisplay.map((page) => page.id)
  ;[reordered[index], reordered[swapIndex]] = [reordered[swapIndex], reordered[index]]

  return mergeVisibleOrder(allPages, reordered)
}
