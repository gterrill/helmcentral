import { useLayoutEffect, useRef, useState } from 'react'

// Measures the actual rendered width of a container element, so an SVG's
// viewBox can be set 1:1 with real pixels instead of a fixed coordinate
// space. With a fixed-height SVG and the default preserveAspectRatio
// ("xMidYMid meet"), a fixed viewBox width narrower than the container
// would otherwise get letterboxed (centered, with empty space on each
// side) instead of actually filling the available width.
//
// useLayoutEffect, not useEffect: a caller that falls back to a placeholder
// width while width is still 0 (forecast-drawer.tsx's forecastChartWidth)
// needs that placeholder to never actually reach the screen. useEffect only
// guarantees it runs after the browser has already painted the current
// frame, so the placeholder-width chart paints first and then visibly snaps
// to the real size a frame later. useLayoutEffect runs synchronously after
// DOM mutations but before the browser paints, so React can apply the
// measured width before anything is shown.
export function useMeasuredWidth() {
  const ref = useRef<HTMLDivElement>(null)
  const [width, setWidth] = useState(0)

  useLayoutEffect(() => {
    const el = ref.current
    if (!el) return
    setWidth(el.getBoundingClientRect().width)
    const observer = new ResizeObserver(([entry]) => setWidth(entry.contentRect.width))
    observer.observe(el)
    return () => observer.disconnect()
  }, [])

  return [ref, width] as const
}
