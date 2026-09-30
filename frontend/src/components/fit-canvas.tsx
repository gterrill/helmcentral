import { useEffect, useRef, useState, type CSSProperties, type ReactNode } from 'react'

import { fitMinHeight, fitScale } from '@/lib/cluster-canvas'
import { cn } from '@/lib/utils'

interface FitCanvasProps {
  designWidth: number
  designHeight: number
  /** Classes for the measured box, e.g. a breakpoint visibility class. */
  className?: string
  /** Attributes for the canvas itself (data-* hooks, extra classes). */
  canvasProps?: React.HTMLAttributes<HTMLDivElement> & { [key: `data-${string}`]: string | undefined }
  children: ReactNode
}

/**
 * Draws a fixed design canvas as large as its tile allows, centred.
 *
 * The box fills the space the tile gives it (`flex-1`, so the tile must be a
 * flex column with `min-h-0` down to here: `Tile` does that when asked to
 * `fill`). The canvas is absolutely positioned inside it, so nothing the
 * scaling does can change the size being measured. The box's `min-height` is
 * the design scaled to its width without growth: where the tile has no height
 * of its own, such as the stacked phone layout where tiles only have a minimum
 * height, that is the box's whole height and the canvas is fitted by width
 * alone. Where the tile does have a height, the box grows into it and the
 * canvas grows with it, up to MAX_FIT_SCALE.
 */
export function FitCanvas({ designWidth, designHeight, className, canvasProps, children }: FitCanvasProps) {
  const ref = useRef<HTMLDivElement>(null)
  const [box, setBox] = useState<{ w: number; h: number }>({ w: 0, h: 0 })

  useEffect(() => {
    const el = ref.current
    if (!el) return
    const observer = new ResizeObserver(([entry]) => {
      setBox({ w: entry.contentRect.width, h: entry.contentRect.height })
    })
    observer.observe(el)
    return () => observer.disconnect()
  }, [])

  const scale = fitScale({ availW: box.w, availH: box.h, designW: designWidth, designH: designHeight })
  const left = (box.w - designWidth * scale) / 2
  const top = (box.h - designHeight * scale) / 2

  return (
    <div
      ref={ref}
      className={cn('relative min-h-0 w-full flex-1', className)}
      style={{ minHeight: fitMinHeight({ availW: box.w, designW: designWidth, designH: designHeight }) }}
    >
      <div
        {...canvasProps}
        className={cn('absolute left-0 top-0 origin-top-left', canvasProps?.className)}
        style={{
          ...canvasProps?.style,
          width: designWidth,
          height: designHeight,
          transform: `translate(${left}px, ${top}px) scale(${scale})`,
        } as CSSProperties}
      >
        {children}
      </div>
    </div>
  )
}
