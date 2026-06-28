import { useEffect, useRef, useState } from 'react'

// Measures the actual rendered width of a container element, so an SVG's
// viewBox can be set 1:1 with real pixels instead of a fixed coordinate
// space. With a fixed-height SVG and the default preserveAspectRatio
// ("xMidYMid meet"), a fixed viewBox width narrower than the container
// would otherwise get letterboxed (centered, with empty space on each
// side) instead of actually filling the available width.
export function useMeasuredWidth() {
  const ref = useRef<HTMLDivElement>(null)
  const [width, setWidth] = useState(0)

  useEffect(() => {
    const el = ref.current
    if (!el) return
    setWidth(el.getBoundingClientRect().width)
    const observer = new ResizeObserver(([entry]) => setWidth(entry.contentRect.width))
    observer.observe(el)
    return () => observer.disconnect()
  }, [])

  return [ref, width] as const
}
