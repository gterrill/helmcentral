// The router keeps only the path and query, so a `#fragment` never reaches the
// page it names. A link that wants to land on a part of a page asks here, and
// the page scrolls to it once it has drawn. A fragment still on the address
// (a page loaded straight at `/settings/alarms#notifications`) counts too.

let pending: string | null = null

export function requestAnchorScroll(id: string): void {
  pending = id
}

/** Withdraws a request, for a move a guard held. */
export function cancelAnchorScroll(): void {
  pending = null
}

/** True once for a request made for `id`, or a fragment naming it. */
export function takeAnchorScroll(id: string): boolean {
  const asked = pending === id
  if (asked) pending = null
  return asked || globalThis.location?.hash === `#${id}`
}
