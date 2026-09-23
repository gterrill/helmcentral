import { useEffect, useState } from 'react'

/**
 * Cycles through the indices of a set of size `count`, advancing by one
 * every `intervalSeconds`, wrapping back to 0 after the last one.
 *
 * `resetKey` is the caller's signal that the underlying set changed shape,
 * not just identity - a new array of the same items every poll must not
 * restart the cycle, but a real change (an item dropping out, gaining or
 * losing whatever made it eligible, or the set reordering) should, so the
 * index can never point past the end of a shorter set or land on an item
 * unrelated to the one that was showing before. A joined-ids string is the
 * usual shape (see poi-map-tile-impl.tsx's cyclableKey), but any string that
 * changes exactly when the set's shape does works.
 *
 * Returns null when count is 0 (nothing to cycle through), and never
 * advances - no timer is even started - when count is 1, since there is
 * nothing to cycle to.
 */
export function useCyclingIndex(count: number, intervalSeconds: number, resetKey: string): number | null {
  const [index, setIndex] = useState(0)

  useEffect(() => {
    setIndex(0)
  }, [resetKey])

  useEffect(() => {
    if (count <= 1) return
    const timer = window.setInterval(() => {
      setIndex((i) => (i + 1) % count)
    }, intervalSeconds * 1000)
    return () => window.clearInterval(timer)
  }, [count, intervalSeconds])

  return count > 0 ? index % count : null
}
