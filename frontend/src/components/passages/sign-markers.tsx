import { Marker } from 'react-map-gl/maplibre'

import { SignCard, SignPin } from '@/components/passages/sign-card'
import type { ReplaySign, ResolvedSign } from '@/lib/replay-signs'

/** How far above its point a card floats, by the place it holds, so two cards
 * for nearby points do not sit on top of each other. */
const LIFT_PX = [20, 104] as const

export interface SignMarkersProps {
  /** The cards on screen, in order. */
  cards: ResolvedSign[]
  /** Signs shown as small pins. */
  pins: ResolvedSign[]
  /** A pin the operator opened into its card. */
  openId: string | null
  canOpenPhoto: boolean
  still: boolean
  onOpenPhoto: (sign: ReplaySign) => void
  onPinClick: (id: string) => void
  onCloseOpen: () => void
}

/** The signs as map markers: a card on a leader line from its point on the
 * track, or a pin. They are map markers, so they stay on their point through
 * pitch and terrain. */
export function SignMarkers({ cards, pins, openId, canOpenPhoto, still, onOpenPhoto, onPinClick, onCloseOpen }: SignMarkersProps) {
  const shown = new Set(cards.map((c) => c.sign.id))
  const opened = openId && !shown.has(openId) ? pins.find((p) => p.sign.id === openId) : undefined
  return (
    <>
      {pins.filter((p) => !shown.has(p.sign.id) && p.sign.id !== opened?.sign.id).map((p) => (
        <Marker key={`pin-${p.sign.id}`} longitude={p.lon} latitude={p.lat} anchor="center" style={{ zIndex: 5 }}>
          <SignPin sign={p.sign} onClick={() => onPinClick(p.sign.id)} />
        </Marker>
      ))}
      {[...cards, ...(opened ? [opened] : [])].map((c, i) => (
        <Marker key={`card-${c.sign.id}`} longitude={c.lon} latitude={c.lat} anchor="bottom" style={{ zIndex: 10 + i }}>
          <div className="flex flex-col items-center">
            <SignCard
              sign={c.sign}
              canOpenPhoto={canOpenPhoto}
              still={still}
              onOpenPhoto={onOpenPhoto}
              onClose={c === opened ? onCloseOpen : undefined}
            />
            <span className="w-px bg-foreground" style={{ height: LIFT_PX[Math.min(i, LIFT_PX.length - 1)] }} aria-hidden="true" />
            <span className="-mb-1 h-2 w-2 rounded-full border border-background bg-foreground" aria-hidden="true" />
          </div>
        </Marker>
      ))}
    </>
  )
}
