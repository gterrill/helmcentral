import { signPhotoSrc } from '@/components/passages/sign-card'
import { signCardPhotoRect, type ReplaySign } from '@/lib/replay-signs'
import { momentRect, type MomentView } from '@/lib/replay-time'

/** Where the map puts a point, in its own pixels, and how big the map is. */
export interface ProjectedPoint {
  point: { x: number; y: number }
  container: { w: number; h: number }
}

export interface PhotoMomentProps {
  /** The moment as the replay controller has it: how far grown, and the zoom. */
  view: MomentView
  sign: ReplaySign
  /** How high the sign's card floats above its point, by the place it holds among the cards. */
  lift: number
  /** Asks the map where the sign's point is. Null while the map cannot say. */
  project: () => ProjectedPoint | null
}

/** A sign's photo, grown from its card to fill most of the map for a moment.
 * It draws what the controller says (`view`) and decides nothing about time:
 * the rect comes from lib/replay-time, starting from where lib/replay-signs puts
 * the card's photo for the point the map projects. It sits over the map and
 * lets touches through, so a drag or tap on the map ends the moment. */
export function PhotoMoment({ view, sign, lift, project }: PhotoMomentProps) {
  const src = signPhotoSrc(sign)
  const placed = project()
  if (!src || !placed) return null
  const rect = momentRect(view, signCardPhotoRect(sign, placed.point, placed.container, lift), placed.container)
  return (
    <div className="pointer-events-none absolute inset-0 z-30 overflow-hidden" data-testid="photo-moment" data-phase={view.phase}>
      <div
        className="absolute overflow-hidden rounded-md border border-border bg-card shadow-lg"
        style={{ left: rect.x, top: rect.y, width: rect.w, height: rect.h }}
      >
        <img src={src} alt={sign.title} className="h-full w-full object-cover" style={{ transform: `scale(${rect.scale})` }} />
      </div>
    </div>
  )
}
