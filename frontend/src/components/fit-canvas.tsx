import { useEffect, useRef, useState, type CSSProperties, type ReactNode } from 'react'

import { fitBasisHeight, pickDesign, type FitDesign } from '@/lib/cluster-canvas'
import { cn } from '@/lib/utils'

/** What the children of a multi-design canvas are told about the design in use. */
export type FitChoice = { key: string; width: number; height: number }

interface FitCanvasProps {
  /** The ways of drawing the canvas; the one that fits the box best is used. The first is the default. */
  designs: readonly FitDesign[]
  /** Classes for the measured box, e.g. a breakpoint visibility class. */
  className?: string
  /** Attributes for the canvas itself (data-* hooks, extra classes). */
  canvasProps?: React.HTMLAttributes<HTMLDivElement> & { [key: `data-${string}`]: string | undefined }
  /**
   * A function when the content depends on which design was chosen and how wide
   * it was drawn (the engine cluster); plain content when it does not (the wind
   * compass has one design and fills it).
   */
  children: ReactNode | ((choice: FitChoice) => ReactNode)
}

/**
 * Draws a fixed design canvas as large as its tile allows, centred.
 *
 * The box fills the space the tile gives it (`flex-1`, so the tile must be a
 * flex column with `min-h-0` down to here: `Tile` does that when asked to
 * `fill`). The canvas is absolutely positioned inside it, so nothing the
 * scaling does can change the size being measured.
 *
 * What the box asks for is its flex-basis, the design scaled to its width
 * without growth, and not a min-height. Where the tile has no height of its
 * own, such as the stacked phone layout where tiles only have a minimum
 * height, the basis is what sizes the box and the canvas is fitted by width
 * alone. Where the tile does have a height the box is flexed to it: it grows
 * past the basis up to MAX_FIT_SCALE, and it shrinks below it. A min-height
 * could not shrink, and on a short wide tile it held the box taller than the
 * tile, so the canvas was fitted to a height the tile did not have and ran out
 * of the bottom of it.
 *
 * Given several designs, the one that draws at the largest scale is used and,
 * if it stretches, the canvas is as wide as the box needs at that scale. The
 * basis comes from the first design, so the pick never feeds back into the
 * height it is made against.
 */
export function FitCanvas({ designs, className, canvasProps, children }: FitCanvasProps) {
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

  const first = designs[0]
  const { design, scale, width } = pickDesign(designs, { availW: box.w, availH: box.h })
  const left = (box.w - width * scale) / 2
  const top = (box.h - design.height * scale) / 2

  return (
    <div
      ref={ref}
      className={cn('relative min-h-0 w-full grow shrink', className)}
      style={{ flexBasis: fitBasisHeight({ availW: box.w, designW: first.width, designH: first.height }) }}
    >
      <div
        {...canvasProps}
        data-layout={design.key}
        className={cn('absolute left-0 top-0 origin-top-left', canvasProps?.className)}
        style={{
          ...canvasProps?.style,
          width,
          height: design.height,
          transform: `translate(${left}px, ${top}px) scale(${scale})`,
        } as CSSProperties}
      >
        {typeof children === 'function' ? children({ key: design.key, width, height: design.height }) : children}
      </div>
    </div>
  )
}
