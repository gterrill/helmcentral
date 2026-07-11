import { useEffect, useRef, useState } from 'react'
import type { PointerEvent as ReactPointerEvent } from 'react'

// Shows a tooltip for the nearest hourly entry on mouse hover (desktop/
// tablet) or touch (mobile) - both go through the same pointer events, since
// `pointermove` only fires for a mouse on hover (no button needed) and only
// fires for touch while a finger is actually down and moving. Positioned at
// the pointer's own X so it never requires looking elsewhere.
//
// Clearing is pointer-type-aware: a mouse hides the tooltip as soon as it
// leaves the chart (normal hover behavior), but touch does NOT hide on
// pointerup/pointerleave - those fire the instant a finger lifts, which
// would otherwise make a quick tap flash the tooltip for a single frame
// instead of actually showing it. A touch tooltip stays until the next tap
// (anywhere) replaces or clears it.
export function useChartTooltip(count: number, resetKey: string | number, chartLeft: number, chartRight: number) {
  const [point, setPoint] = useState<{ index: number; pixelX: number } | null>(null)
  const svgRef = useRef<SVGSVGElement>(null)

  // Switching days/stations swaps in a different hourly array (different length,
  // different times) - drop any tooltip from the previous chart.
  useEffect(() => {
    setPoint(null)
  }, [resetKey])

  const updateFromClientX = (clientX: number) => {
    const svg = svgRef.current
    if (!svg || count <= 0) return
    const rect = svg.getBoundingClientRect()
    if (rect.width <= 0) return
    const pixelX = Math.max(0, Math.min(rect.width, clientX - rect.left))
    const viewBoxWidth = svg.viewBox.baseVal.width || chartRight
    const xInViewBox = (pixelX / rect.width) * viewBoxWidth
    const fraction = count <= 1 ? 0 : (xInViewBox - chartLeft) / (chartRight - chartLeft)
    const idx = Math.max(0, Math.min(count - 1, Math.round(fraction * (count - 1))))
    setPoint({ index: idx, pixelX })
  }

  // Only a mouse leaving the chart should hide the tooltip - for touch,
  // "leave" fires the moment a finger lifts, which isn't a meaningful signal
  // that the user is done looking at the value.
  const clearForMouse = (event: ReactPointerEvent<SVGSVGElement>) => {
    if (event.pointerType === 'mouse') setPoint(null)
  }

  return {
    svgRef,
    activeIndex: point === null ? null : Math.min(point.index, Math.max(0, count - 1)),
    tooltipPixelX: point?.pixelX ?? null,
    onPointerDown: (event: ReactPointerEvent<SVGSVGElement>) => updateFromClientX(event.clientX),
    onPointerMove: (event: ReactPointerEvent<SVGSVGElement>) => updateFromClientX(event.clientX),
    onPointerLeave: clearForMouse,
  }
}
