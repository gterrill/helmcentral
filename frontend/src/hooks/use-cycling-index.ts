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
 * usual shape (see poi-map-tile-impl.tsx's rankedListKey), but any string
 * that changes exactly when the set's shape does works.
 *
 * Returns null when count is 0 (nothing to cycle through), and never
 * advances - no timer is even started - when count is 1, since there is
 * nothing to cycle to.
 */
export function useCyclingIndex(count: number, intervalSeconds: number, resetKey: string): number | null {
  const [index, setIndex] = useState(0)
  const [lastResetKey, setLastResetKey] = useState(resetKey)

  // Reset during render, not in an effect: an effect would let one render
  // through with the old index, and a caller that acts on each new index
  // (the Nearby map flies to it) would start toward a place that has moved.
  const resetting = resetKey !== lastResetKey
  if (resetting) {
    setLastResetKey(resetKey)
    setIndex(0)
  }

  useEffect(() => {
    if (count <= 1) return
    const timer = window.setInterval(() => {
      setIndex((i) => (i + 1) % count)
    }, intervalSeconds * 1000)
    return () => window.clearInterval(timer)
  }, [count, intervalSeconds])

  if (count === 0) return null
  return resetting ? 0 : index % count
}
