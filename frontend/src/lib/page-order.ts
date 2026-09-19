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
