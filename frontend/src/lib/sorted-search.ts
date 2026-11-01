// Searches in a list sorted by a number.

/** How many of the items in a list sorted by `key` come before `x` (or, with `orAt`, up to and including it). */
export function lowerBound<T>(list: readonly T[], key: (item: T) => number, x: number, orAt = false): number {
  let lo = 0
  let hi = list.length
  while (lo < hi) {
    const mid = (lo + hi) >> 1
    if (key(list[mid]) < x || (orAt && key(list[mid]) === x)) lo = mid + 1
    else hi = mid
  }
  return lo
}

/** The index of the first item that satisfies `test`, which must hold for a suffix of the list. */
export function firstWhere<T>(list: readonly T[], test: (item: T) => boolean): number {
  let lo = 0
  let hi = list.length
  while (lo < hi) {
    const mid = (lo + hi) >> 1
    if (test(list[mid])) hi = mid
    else lo = mid + 1
  }
  return lo
}
