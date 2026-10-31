import { useEffect, useState, type RefObject } from 'react'

/** The nearest ancestor that scrolls vertically, or null when the page itself does. */
function scrollParent(el: Element): Element | null {
  for (let p = el.parentElement; p; p = p.parentElement) {
    const overflow = getComputedStyle(p).overflowY
    if (overflow === 'auto' || overflow === 'scroll' || overflow === 'overlay') return p
  }
  return null
}

/**
 * Whether the element is within `margin` of the visible area of its nearest
 * scrolling parent (the page, when nothing around it scrolls). It turns false
 * again once the element is well away, so a heavy child (a WebGL map) can be
 * mounted for the cards the operator is near and dropped for the rest. It is
 * null until the first measurement, so a card already on screen is not shown
 * as far away for a frame.
 *
 * The margin only reaches a scroll container if that container is the
 * observer's root. With the page as root, content inside an overflow-y box is
 * clipped by it and the margin never applies.
 */
export function useNearViewport(ref: RefObject<Element | null>, margin = '600px'): boolean | null {
  const [near, setNear] = useState<boolean | null>(null)
  useEffect(() => {
    const el = ref.current
    if (!el) return
    const observer = new IntersectionObserver(
      (entries) => {
        const latest = entries[entries.length - 1]
        if (latest) setNear(latest.isIntersecting)
      },
      { root: scrollParent(el), rootMargin: `${margin} 0px` },
    )
    observer.observe(el)
    return () => observer.disconnect()
  }, [ref, margin])
  return near
}
