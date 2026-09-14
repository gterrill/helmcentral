import { useEffect, useRef, useState } from 'react'

interface UseInViewOptions {
  /** Passed straight through to IntersectionObserver's rootMargin. Defaults
   *  to 200px so a caller gating a mount on this starts a moment before the
   *  element is actually visible, not the instant it crosses the edge. */
  rootMargin?: string
}

/**
 * Tracks whether an element has ever intersected the viewport.
 *
 * Sticky: once the element has been seen, the observer disconnects and
 * `inView` stays true even after the element scrolls back out. This is
 * deliberate for the one caller today (EmbedTile) — an expensive mount
 * gated on this should not be torn down again just because the element left
 * the screen for a moment.
 */
export function useInView<T extends Element>({ rootMargin = '200px' }: UseInViewOptions = {}) {
  const ref = useRef<T>(null)
  const [inView, setInView] = useState(false)

  useEffect(() => {
    // Already seen: nothing left to observe, and the observer that reported
    // it was already disconnected below.
    if (inView) return

    const el = ref.current
    if (!el) return

    const observer = new IntersectionObserver(
      (entries) => {
        if (entries.some((entry) => entry.isIntersecting)) setInView(true)
      },
      { rootMargin },
    )
    observer.observe(el)
    return () => observer.disconnect()
  }, [inView, rootMargin])

  return [ref, inView] as const
}
