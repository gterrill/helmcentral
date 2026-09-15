import { useEffect, useRef, useState } from 'react'
import type { KeyboardEvent as ReactKeyboardEvent, PointerEvent as ReactPointerEvent } from 'react'

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
    const rawPixelX = Math.max(0, Math.min(rect.width, clientX - rect.left))
    // Keep the bubble centered on the pointer while preventing it from being
    // pushed past the chart edge. The bubble itself is translated by 50% of its
    // own width in ChartTooltipBubble, so the clamp here should be a small inset,
    // not the CSS clamp that was mixing different coordinate systems.
    const pixelX = Math.max(58, Math.min(rect.width - 58, rawPixelX))
    const viewBoxWidth = svg.viewBox.baseVal.width || chartRight
    const xInViewBox = (pixelX / rect.width) * viewBoxWidth
    const fraction = count <= 1 ? 0 : (xInViewBox - chartLeft) / (chartRight - chartLeft)
    const idx = Math.max(0, Math.min(count - 1, Math.round(fraction * (count - 1))))
    setPoint({ index: idx, pixelX })
  }

  // The inverse of updateFromClientX, for the keyboard path: the index is
  // known and the pixel X (which is what positions the bubble) has to be
  // derived from it. Same rect and viewBox reads, same bail when the chart
  // hasn't been laid out yet.
  const showIndex = (index: number) => {
    const svg = svgRef.current
    if (!svg || count <= 0) return
    const rect = svg.getBoundingClientRect()
    if (rect.width <= 0) return
    const idx = Math.max(0, Math.min(count - 1, index))
    const viewBoxWidth = svg.viewBox.baseVal.width || chartRight
    const fraction = count <= 1 ? 0 : idx / (count - 1)
    const xInViewBox = chartLeft + fraction * (chartRight - chartLeft)
    const pixelX = (xInViewBox / viewBoxWidth) * rect.width
    setPoint({ index: idx, pixelX: Math.max(58, Math.min(rect.width - 58, pixelX)) })
  }

  // The only non-pointer read path. Every chart's summary sentence gives a
  // range, so without this a keyboard or switch user cannot get an exact
  // hourly value off the chart at all.
  const onKeyDown = (event: ReactKeyboardEvent<SVGSVGElement>) => {
    switch (event.key) {
      // Entering from nothing showing: the arrow that moves right starts at
      // the first sample, the one that moves left at the last.
      case 'ArrowRight':
        showIndex(point === null ? 0 : point.index + 1)
        break
      case 'ArrowLeft':
        showIndex(point === null ? count - 1 : point.index - 1)
        break
      case 'Home':
        showIndex(0)
        break
      case 'End':
        showIndex(count - 1)
        break
      case 'Escape':
        // Escape belongs to the tooltip only while one is up; with nothing
        // showing it has to fall through to whatever else listens for it.
        if (point === null) return
        setPoint(null)
        break
      // Everything else, Tab included, keeps its default - preventing it
      // wholesale would trap focus on the chart.
      default:
        return
    }
    event.preventDefault()
  }

  // Tabbing to a chart should reveal a value rather than an empty focus ring.
  // No matching onBlur: clearing here is pointer-type-aware (see above), and a
  // blur-clear would fight that.
  const onFocus = () => {
    if (point === null) showIndex(0)
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
    onKeyDown,
    onFocus,
  }
}
