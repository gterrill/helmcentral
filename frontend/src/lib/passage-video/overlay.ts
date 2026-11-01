// What goes over the map in a video frame, as numbers: the sign cards and pins,
// the boat's dot, and the photo of a moment. The page draws the same things
// with HTML; here they are worked out from the same functions (placeSignCard,
// the card's size constants, momentRect) and drawn on a canvas by draw.ts.
//
// Everything is in "design pixels", the size the page draws in, which the
// canvas scales up by UI_SCALE so a card is as readable on a 1080 high video as
// on the page.

import { FONT_STACK, wrapLines, type Measure } from '@/lib/passage-export-layout'
import type { ReplayFrame } from '@/lib/replay-controller'
import { SIGN_LIFT_PX, signCardBox, signCardPhotoRect, signCardRect, type ReplaySign, type ResolvedSign } from '@/lib/replay-signs'
import { momentRect, type MomentView } from '@/lib/replay-time'

/** Design pixels to video pixels. */
export const UI_SCALE = 1.5
/** The note's font on a card, and the most lines of it a card shows. */
export const NOTE_FONT = `400 12px ${FONT_STACK}`
const NOTE_MAX_LINES = 2
/** Seconds a card takes to fade in, as on the page. */
export const CARD_FADE_S = 0.3

export interface Point { x: number; y: number }
export interface Box { x: number; y: number; w: number; h: number }

export interface OverlayCard {
  sign: ReplaySign
  /** The sign's point on the map. */
  point: Point
  rect: Box
  below: boolean
  leader: number
  lift: number
  /** The note as it is drawn: the card is as high as these lines make it. */
  noteLines: string[]
  alpha: number
  /** Its photo is drawn ('bitmap'), could not be loaded ('missing'), or there is none. */
  photo: 'bitmap' | 'missing' | 'none'
}

export interface OverlayModel {
  scale: number
  /** The video's size in design pixels. */
  design: { w: number; h: number }
  cards: OverlayCard[]
  pins: Point[]
  moment: { signId: string; rect: Box; scale: number; fill: number } | null
  dot: Point | null
}

export interface OverlayInput {
  frame: ReplayFrame
  size: { width: number; height: number }
  /** Where the map puts a point, in video pixels. */
  project: (lon: number, lat: number) => Point
  /** Seconds of watching, for the fade. */
  wall: number
  /** When each card now up first appeared; kept by the caller from frame to frame. */
  firstSeen: Map<string, number>
  /** The signs whose photo is loaded. */
  bitmaps: ReadonlySet<string>
  /** The sign of the photo moment under way. */
  momentSign: ResolvedSign | undefined
  /** The boat is drawn as a dot (no model). */
  boatDot: boolean
  /** Measures text as the canvas will draw it. */
  measure: Measure
}

export function buildOverlay({ frame, size, project, wall, firstSeen, bitmaps, momentSign, boatDot, measure }: OverlayInput): OverlayModel {
  const design = { w: size.width / UI_SCALE, h: size.height / UI_SCALE }
  const toDesign = (lon: number, lat: number): Point => {
    const p = project(lon, lat)
    return { x: p.x / UI_SCALE, y: p.y / UI_SCALE }
  }
  // The note is wrapped by the width it is drawn in, once, and the card is as high as those lines.
  const noteOf = (sign: ReplaySign) => wrapLines(sign.note, signCardBox(sign, design.w).photoW, NOTE_FONT, measure, NOTE_MAX_LINES)
  const liftOf = (index: number) => SIGN_LIFT_PX[Math.min(Math.max(0, index), SIGN_LIFT_PX.length - 1)]

  // A card fades in over CARD_FADE_S from the first frame it is up, and is forgotten once it goes.
  const up = new Set(frame.cards.map((c) => c.sign.id))
  for (const id of [...firstSeen.keys()]) if (!up.has(id)) firstSeen.delete(id)

  const cards = frame.cards.map<OverlayCard>((c, i) => {
    if (!firstSeen.has(c.sign.id)) firstSeen.set(c.sign.id, wall)
    const point = toDesign(c.lon, c.lat)
    const lift = liftOf(i)
    const noteLines = noteOf(c.sign)
    const r = signCardRect(c.sign, point, design, lift, noteLines.length)
    return {
      sign: c.sign,
      point,
      rect: { x: r.x, y: r.y, w: r.w, h: r.h },
      below: r.below,
      leader: r.leader,
      lift,
      noteLines,
      alpha: Math.max(0, Math.min(1, (wall - firstSeen.get(c.sign.id)!) / CARD_FADE_S)),
      photo: c.sign.photo_url == null ? 'none' : bitmaps.has(c.sign.id) ? 'bitmap' : 'missing',
    }
  })

  const pins = frame.pins.filter((p) => !up.has(p.sign.id)).map((p) => toDesign(p.lon, p.lat))

  let moment: OverlayModel['moment'] = null
  const view: MomentView | null = frame.moment
  if (view && momentSign && bitmaps.has(view.signId)) {
    const index = frame.cards.findIndex((c) => c.sign.id === view.signId)
    const point = toDesign(momentSign.lon, momentSign.lat)
    const rect = momentRect(view, signCardPhotoRect(momentSign.sign, point, design, liftOf(index), noteOf(momentSign.sign).length), design)
    moment = { signId: view.signId, rect: { x: rect.x, y: rect.y, w: rect.w, h: rect.h }, scale: rect.scale, fill: view.fill }
  }

  const dot = boatDot && frame.boat ? toDesign(frame.boat.lon, frame.boat.lat) : null
  return { scale: UI_SCALE, design, cards, pins, moment, dot }
}
