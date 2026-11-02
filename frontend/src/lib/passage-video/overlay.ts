// What goes over the map in a video frame, as numbers: the sign cards and pins,
// the boat's dot, and the grown card of a photo moment. The page draws the same things
// with HTML; here they are worked out from the same functions (placeSignCard,
// the card's size constants, momentRect) and drawn on a canvas by draw.ts.
//
// Everything is in "design pixels", the size the page draws in, which the
// canvas scales up by UI_SCALE so a card is as readable on a 1080 high video as
// on the page.

import type { Measure } from '@/lib/passage-export-layout'
import type { ReplayFrame } from '@/lib/replay-controller'
import type { MapLabelBox } from '@/lib/map-label'
import { passageLabelBox } from '@/lib/replay-passage-label'
import { stayLabelBox } from '@/lib/replay-stay'
import { CARD_FADE_S, SIGN_LIFT_PX, signCardBox, signCardPlacement, type CardMode, type CardPlacement, type ReplaySign, type ResolvedSign } from '@/lib/replay-signs'
import { signNoteLines } from '@/lib/sign-note'
import { momentRect, type MomentView } from '@/lib/replay-time'

/** Design pixels to video pixels. */
export const UI_SCALE = 1.5

export interface Point { x: number; y: number }
export interface Box { x: number; y: number; w: number; h: number }

export interface OverlayCard {
  sign: ReplaySign
  /** The sign's point on the map. */
  point: Point
  /** Where the card goes, and the leader to it. */
  placement: CardPlacement
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
  /** The photo moment under way: the whole card, grown. */
  moment: {
    sign: ReplaySign
    noteLines: string[]
    /** The card where it stands now, and how much its own size is made bigger. */
    card: Box
    k: number
    /** The card at its own size. */
    face: { w: number; h: number }
    /** The slow zoom of the photo inside its frame. */
    zoom: number
  } | null
  dot: Point | null
  /** The label of the stay under way, where it goes, in design pixels. */
  stay: { text: string; box: MapLabelBox } | null
  /** The lower-third naming the passage just started, where it goes, in design pixels. */
  passage: { text: string; box: MapLabelBox } | null
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
  /** Where each card was put last frame (above, below or beside its point), kept by the caller from frame to frame: a card keeps its place while it still fits. */
  modes?: Map<string, CardMode>
  /** The signs whose photo is loaded. */
  bitmaps: ReadonlySet<string>
  /** The sign of the photo moment under way. */
  momentSign: ResolvedSign | undefined
  /** The boat is drawn as a dot (no model). */
  boatDot: boolean
  /** Measures text as the canvas will draw it. */
  measure: Measure
}

export function buildOverlay({ frame, size, project, wall, firstSeen, modes = new Map(), bitmaps, momentSign, boatDot, measure }: OverlayInput): OverlayModel {
  const design = { w: size.width / UI_SCALE, h: size.height / UI_SCALE }
  const toDesign = (lon: number, lat: number): Point => {
    const p = project(lon, lat)
    return { x: p.x / UI_SCALE, y: p.y / UI_SCALE }
  }
  // The note is wrapped by the width it is drawn in, once, and the card is as high as those lines.
  const noteOf = (sign: ReplaySign) => signNoteLines(sign, design.w, measure)
  /** Where a sign's card goes, keeping the place it had if that still fits. */
  const placementOf = (sign: ReplaySign, point: Point, lift: number, lines: number): CardPlacement => {
    const placed = signCardPlacement(sign, point, design, lift, lines, modes.get(sign.id))
    modes.set(sign.id, placed.mode)
    return placed
  }
  const liftOf = (index: number) => SIGN_LIFT_PX[Math.min(Math.max(0, index), SIGN_LIFT_PX.length - 1)]

  // A card fades in over CARD_FADE_S from the first frame it is up, and is forgotten once it goes.
  const up = new Set(frame.cards.map((c) => c.sign.id))
  for (const id of [...firstSeen.keys()]) if (!up.has(id)) firstSeen.delete(id)
  for (const id of [...modes.keys()]) if (!up.has(id) && id !== momentSign?.sign.id) modes.delete(id)

  const cards = frame.cards.map<OverlayCard>((c, i) => {
    if (!firstSeen.has(c.sign.id)) firstSeen.set(c.sign.id, wall)
    const point = toDesign(c.lon, c.lat)
    const lift = liftOf(i)
    const noteLines = noteOf(c.sign)
    return {
      sign: c.sign,
      point,
      placement: placementOf(c.sign, point, lift, noteLines.length),
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
    const lines = noteOf(momentSign.sign)
    const card = placementOf(momentSign.sign, point, liftOf(index), lines.length)
    const box = signCardBox(momentSign.sign, design.w, lines.length)
    const grown = momentRect(view, card.rect, { w: box.w, h: box.h }, design)
    moment = { sign: momentSign.sign, noteLines: lines, card: grown.card, k: grown.k, face: { w: box.w, h: box.h }, zoom: grown.zoom }
  }

  const dot = boatDot && frame.boat ? toDesign(frame.boat.lon, frame.boat.lat) : null
  const stay = frame.stay ? { text: frame.stay.stay.label, box: stayLabelBox(design, frame.stay.elapsed) } : null
  const passage = frame.passageLabel ? { text: frame.passageLabel.label.text, box: passageLabelBox(design, frame.passageLabel.elapsed) } : null
  return { scale: UI_SCALE, design, cards, pins, moment, dot, stay, passage }
}
