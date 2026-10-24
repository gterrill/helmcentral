import { useCallback, useEffect, useRef, useState } from 'react'
import { Marker, useMap } from 'react-map-gl/maplibre'

import { SignCard, SignPin } from '@/components/passages/sign-card'
import { SIGN_EDGE_MARGIN_PX, SIGN_LIFT_PX, placeSignCard, type ReplaySign, type ResolvedSign, type SignPlacement } from '@/lib/replay-signs'
import { cn } from '@/lib/utils'

/** One card on its leader line. The marker sits on the sign's point and never
 * changes; the card is moved round that point in CSS so it stays whole inside
 * the map as the camera moves, with the leader and dot staying on the point.
 * Sizes are cached (card by observer, map on resize), so a camera move costs
 * one projection and the placement arithmetic. */
function SignCardMarker({ c, lift, z, opened, canOpenPhoto, still, onOpenPhoto, onClose }: {
  c: ResolvedSign
  lift: number
  z: number
  opened: boolean
  canOpenPhoto: boolean
  still: boolean
  onOpenPhoto: (sign: ReplaySign) => void
  onClose: () => void
}) {
  const { current: mapRef } = useMap()
  const map = mapRef?.getMap()
  const cardRef = useRef<HTMLDivElement>(null)
  const cardSize = useRef({ w: 0, h: 0 })
  const boxSize = useRef({ w: 0, h: 0 })
  const [place, setPlace] = useState<SignPlacement>({ dx: 0, below: false, leader: lift })
  const [maxW, setMaxW] = useState<number | undefined>(undefined)
  const update = useCallback(() => {
    if (!map) return
    const p = map.project([c.lon, c.lat])
    const next = placeSignCard({ point: { x: p.x, y: p.y }, container: boxSize.current, card: cardSize.current, lift })
    const dx = Math.round(next.dx)
    const leader = Math.round(next.leader)
    setPlace((prev) => (prev.dx === dx && prev.below === next.below && prev.leader === leader ? prev : { dx, below: next.below, leader }))
  }, [map, c.lon, c.lat, lift])
  useEffect(() => {
    if (!map) return
    const readBox = () => {
      const box = map.getContainer()
      boxSize.current = { w: box.clientWidth, h: box.clientHeight }
      setMaxW(Math.max(0, box.clientWidth - 2 * SIGN_EDGE_MARGIN_PX))
    }
    const onResize = () => { readBox(); update() }
    readBox()
    update()
    map.on('move', update)
    map.on('resize', onResize)
    // The card is attached to the map after this runs, so it is measured when
    // it is laid out, and again if its size changes.
    const el = cardRef.current
    const ro = el ? new ResizeObserver((entries) => {
      const r = entries[entries.length - 1]?.contentRect
      if (!r) return
      cardSize.current = { w: r.width, h: r.height }
      update()
    }) : null
    if (el) ro?.observe(el)
    return () => {
      map.off('move', update)
      map.off('resize', onResize)
      ro?.disconnect()
    }
  }, [map, update])
  return (
    <Marker longitude={c.lon} latitude={c.lat} anchor="center" style={{ zIndex: z }}>
      <div className="relative h-0 w-0">
        <div className={cn('absolute left-0 flex -translate-x-1/2 items-center', place.below ? 'top-0 flex-col-reverse' : 'bottom-0 flex-col')}>
          <div ref={cardRef} style={place.dx === 0 ? undefined : { transform: `translateX(${place.dx}px)` }}>
            <SignCard sign={c.sign} canOpenPhoto={canOpenPhoto} still={still} onOpenPhoto={onOpenPhoto} onClose={opened ? onClose : undefined} maxWidthPx={maxW} />
          </div>
          <span className="w-px bg-foreground" style={{ height: place.leader }} aria-hidden="true" />
          <span className={cn('h-2 w-2 rounded-full border border-background bg-foreground', place.below ? '-mt-1' : '-mb-1')} aria-hidden="true" />
        </div>
      </div>
    </Marker>
  )
}

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
        <SignCardMarker
          key={`card-${c.sign.id}`}
          c={c}
          lift={SIGN_LIFT_PX[Math.min(i, SIGN_LIFT_PX.length - 1)]}
          z={10 + i}
          opened={c === opened}
          canOpenPhoto={canOpenPhoto}
          still={still}
          onOpenPhoto={onOpenPhoto}
          onClose={onCloseOpen}
        />
      ))}
    </>
  )
}
