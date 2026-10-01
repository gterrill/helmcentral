import { useEffect, useRef, useState } from 'react'

/** Resizes settle (a TV changing UI scale, a window drag) before one report. */
export const VIEWPORT_REPORT_DEBOUNCE_MS = 1000

export interface ViewportSize {
  w: number
  h: number
}

function readViewport(): ViewportSize {
  return { w: Math.round(window.innerWidth), h: Math.round(window.innerHeight) }
}

/**
 * The wall page's live window size, and the reporting of it to the backend
 * so the display editor can compare it with the configured canvas (ADR
 * 0153). Reports on mount and after a resize settles, and only when the
 * size differs from the last one sent. A failed report is logged, not
 * hidden: the wall keeps drawing, but the operator's "measured on this
 * screen" line would otherwise go stale with no trace of why.
 */
export function useDisplayViewport(displayId: string, reporting = true): ViewportSize {
  const [size, setSize] = useState<ViewportSize>(readViewport)
  const lastSent = useRef<string | null>(null)

  useEffect(() => {
    let timer: ReturnType<typeof setTimeout> | null = null

    function report(next: ViewportSize) {
      if (!reporting) return
      const key = `${displayId}:${next.w}x${next.h}`
      if (lastSent.current === key) return
      lastSent.current = key
      fetch(`/api/displays/${encodeURIComponent(displayId)}/viewport`, {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ w: next.w, h: next.h, user_agent: navigator.userAgent }),
      })
        .then((res) => {
          if (!res.ok) throw new Error(`HTTP ${res.status}`)
        })
        .catch((err: unknown) => {
          lastSent.current = null
          console.error('display viewport report failed', err)
        })
    }

    function handleResize() {
      const next = readViewport()
      setSize((prev) => (prev.w === next.w && prev.h === next.h ? prev : next))
      if (timer !== null) clearTimeout(timer)
      timer = setTimeout(() => report(next), VIEWPORT_REPORT_DEBOUNCE_MS)
    }

    report(readViewport())
    window.addEventListener('resize', handleResize)
    return () => {
      window.removeEventListener('resize', handleResize)
      if (timer !== null) clearTimeout(timer)
    }
  }, [displayId, reporting])

  return size
}
