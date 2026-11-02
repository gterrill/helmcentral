import { SignCardCaption, signPhotoSrc } from '@/components/passages/sign-card'
import { SIGN_CARD_GAP_PX, SIGN_CARD_PAD_PX, signCardBox, signCardPlacement, type ReplaySign } from '@/lib/replay-signs'
import { cardModes } from '@/lib/sign-modes'
import { signMeasure, signNoteLines } from '@/lib/sign-note'
import { PHOTO_ASPECT, momentRect, type MomentView } from '@/lib/replay-time'

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

/** A sign's card, grown from where it stands on the map to fill most of it for
 * a moment: the frame, the photo and the caption together, as one card. It
 * draws what the controller says (`view`) and decides nothing about time: the
 * card's place and size come from lib/replay-time, starting from where
 * lib/replay-signs puts the card for the point the map projects, with the note
 * wrapped as the card on the map wraps it (lib/sign-note), so it starts exactly
 * on that card. The card is laid out at its own size and made bigger by a
 * transform, so its caption reads as it does on the map. Only the photo moves
 * inside its frame, in the slow zoom. It sits over the map and lets touches
 * through, so a drag or tap on the map ends the moment. */
export function PhotoMoment({ view, sign, lift, project }: PhotoMomentProps) {
  const src = signPhotoSrc(sign)
  const placed = project()
  if (!src || !placed) return null
  const noteLines = signNoteLines(sign, placed.container.w, signMeasure())
  const box = signCardBox(sign, placed.container.w, noteLines.length)
  const card = signCardPlacement(sign, placed.point, placed.container, lift, noteLines.length, cardModes.get(sign.id))
  const grown = momentRect(view, card.rect, { w: box.w, h: box.h }, placed.container)
  return (
    <div className="pointer-events-none absolute inset-0 z-30 overflow-hidden" data-testid="photo-moment" data-phase={view.phase}>
      <div
        data-testid="photo-moment-card"
        className="absolute left-0 top-0 flex flex-col items-stretch rounded-md border border-border bg-card text-card-foreground shadow-lg"
        style={{
          width: box.w,
          padding: SIGN_CARD_PAD_PX,
          gap: SIGN_CARD_GAP_PX,
          transform: `translate(${grown.card.x}px, ${grown.card.y}px) scale(${grown.k})`,
          transformOrigin: '0 0',
        }}
      >
        <div style={{ aspectRatio: PHOTO_ASPECT }} className="w-full shrink-0 overflow-hidden rounded-sm bg-muted">
          <img src={src} alt={sign.title} className="h-full w-full object-cover" style={{ transform: `scale(${grown.zoom})` }} />
        </div>
        <SignCardCaption sign={sign} noteLines={noteLines} />
      </div>
    </div>
  )
}
