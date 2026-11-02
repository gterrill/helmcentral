import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { Marker, useMap } from 'react-map-gl/maplibre'

import { SignCard, SignPin } from '@/components/passages/sign-card'
import { SIGN_EDGE_MARGIN_PX, SIGN_LIFT_PX, placeSignCard, type CardMode, type CardPlacement, type ReplaySign, type ResolvedSign } from '@/lib/replay-signs'
import { cardModes } from '@/lib/sign-modes'
import { signMeasure, signNoteLines } from '@/lib/sign-note'

/** Where a card is from its point, in CSS: the card's top left, how much smaller than its own size it is, and the leader from the point to it. */
interface Layout {
  x: number
  y: number
  scale: number
  hidden: boolean
  leader: { length: number; angle: number } | null
}

const layoutOf = (p: CardPlacement, point: { x: number; y: number }): Layout => {
  const leader = p.leader
    ? { length: Math.round(Math.hypot(p.leader.x2 - p.leader.x1, p.leader.y2 - p.leader.y1)), angle: Math.round((Math.atan2(p.leader.y2 - p.leader.y1, p.leader.x2 - p.leader.x1) * 1800) / Math.PI) / 10 }
    : null
  return { x: Math.round(p.rect.x - point.x), y: Math.round(p.rect.y - point.y), scale: Math.round(p.scale * 1000) / 1000, hidden: p.hidden, leader }
}
const sameLayout = (a: Layout, b: Layout) => a.x === b.x && a.y === b.y && a.scale === b.scale && a.hidden === b.hidden && a.leader?.length === b.leader?.length && a.leader?.angle === b.leader?.angle

/** One card on its leader line. The marker sits on the sign's point and never
 * changes; the card is placed round that point by lib/replay-signs so it stays
 * whole inside the map as the camera moves, with the leader and dot staying on
 * the point. Sizes are cached (card by observer, map on resize), so a camera
 * move costs one projection and the placement arithmetic. */
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
  // Where the card was put last; it stays there for as long as it fits.
  const mode = useRef<CardMode | undefined>(undefined)
  const [layout, setLayout] = useState<Layout>({ x: 0, y: 0, scale: 1, hidden: false, leader: null })
  const [mapW, setMapW] = useState(0)
  // The note wrapped by measuring the text, as the video and the photo moment wrap it: the card is that many lines high. (Until the map has been measured it is the note whole, on one line.)
  const noteLines = useMemo(() => (mapW > 0 ? signNoteLines(c.sign, mapW, signMeasure()) : c.sign.note ? [c.sign.note] : []), [c.sign, mapW])
  const update = useCallback(() => {
    if (!map) return
    const p = map.project([c.lon, c.lat])
    const next = placeSignCard({ point: { x: p.x, y: p.y }, container: boxSize.current, card: cardSize.current, lift, previous: mode.current })
    mode.current = next.mode
    cardModes.set(c.sign.id, next.mode)
    const l = layoutOf(next, p)
    setLayout((prev) => (sameLayout(prev, l) ? prev : l))
  }, [map, c.lon, c.lat, c.sign.id, lift])
  useEffect(() => () => { cardModes.delete(c.sign.id) }, [c.sign.id])
  useEffect(() => {
    if (!map) return
    const readBox = () => {
      const box = map.getContainer()
      boxSize.current = { w: box.clientWidth, h: box.clientHeight }
      setMapW(box.clientWidth)
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
        <div
          ref={cardRef}
          className="absolute left-0 top-0"
          style={{ transform: `translate(${layout.x}px, ${layout.y}px) scale(${layout.scale})`, transformOrigin: '0 0', visibility: layout.hidden ? 'hidden' : undefined }}
        >
          <SignCard sign={c.sign} canOpenPhoto={canOpenPhoto} still={still} onOpenPhoto={onOpenPhoto} onClose={opened ? onClose : undefined} noteLines={noteLines} maxWidthPx={mapW > 0 ? Math.max(0, mapW - 2 * SIGN_EDGE_MARGIN_PX) : undefined} />
        </div>
        {layout.leader && !layout.hidden && (
          <span
            data-testid="sign-leader"
            className="absolute left-0 top-0 h-px bg-foreground"
            style={{ width: layout.leader.length, transform: `rotate(${layout.leader.angle}deg)`, transformOrigin: '0 50%' }}
            aria-hidden="true"
          />
        )}
        <span className="absolute h-2 w-2 -translate-x-1/2 -translate-y-1/2 rounded-full border border-background bg-foreground" aria-hidden="true" />
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
