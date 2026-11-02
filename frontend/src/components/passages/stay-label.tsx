import type { InsetCorner } from '@/lib/passage-inset-view'
import { STAY_LABEL, stayLabelBox } from '@/lib/replay-stay'
import type { StayView } from '@/lib/replay-stay'

export interface StayLabelProps {
  view: StayView
  /** How big the map is. Null while it cannot say. */
  container: { w: number; h: number } | null
  /** The corner the overview map is shown in, if it is, which the label keeps clear of. */
  insetCorner?: InsetCorner | null
}

/** The small label over the map while the replay stays between two passages:
 * how long the stay was, and where. It draws what the controller says and
 * decides nothing about time or place; lib/replay-stay gives its words and
 * where it sits, so a video can draw the same label on a canvas. It lets
 * touches through. */
export function StayLabel({ view, container, insetCorner = null }: StayLabelProps) {
  if (!container) return null
  const box = stayLabelBox(container, view.elapsed, insetCorner)
  return (
    <div className="pointer-events-none absolute inset-0 z-20 overflow-hidden" data-testid="stay-label">
      <span
        className="absolute truncate bg-card/80 text-foreground shadow-sm backdrop-blur-sm"
        style={{ left: box.cx, top: box.y, transform: 'translateX(-50%)', fontSize: box.fontPx, fontWeight: STAY_LABEL.weight, lineHeight: STAY_LABEL.lineHeight, borderRadius: STAY_LABEL.radius, padding: `${box.padY}px ${box.padX}px`, maxWidth: box.maxW, opacity: box.alpha }}
      >
        {view.stay.label}
      </span>
    </div>
  )
}
