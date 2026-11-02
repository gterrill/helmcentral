// Signs on the replay (ADR 0204). A sign is a note pinned to a real instant;
// these functions say where each one stands on the track and which ones are on
// screen at a given moment of watching. Everything here is pure.

import { positionAt, type Timeline } from '@/lib/passages'
import { PHOTO_ASPECT, type Rect } from '@/lib/replay-time'

export type SignVisibility = 'always' | 'passing' | 'after'

export const SIGN_VISIBILITIES: readonly { id: SignVisibility; label: string }[] = [
  { id: 'always', label: 'Always' },
  { id: 'passing', label: 'While passing' },
  { id: 'after', label: 'After passing' },
]

/** What a join with no title yet is called in lists. */
export const AWAITING_NAME = 'Awaiting a name'

export const SIGN_TITLE_MAX = 80
export const SIGN_NOTE_MAX = 280
export const SIGN_SHOW_MIN = 1
export const SIGN_SHOW_MAX = 30
export const SIGN_SHOW_DEFAULT = 4

/** One sign as the server holds it. The photo fields are null when there is no
 * photo or its document has been deleted. */
export interface ReplaySign {
  id: string
  t_ms: number
  title: string
  note: string
  document_id: string | null
  photo_url: string | null
  visibility: SignVisibility
  show_s: number
  /** A join: the track from here on is drawn as a new leg. Only a join may
   * have no title; it waits for a name from the track. */
  starts_leg: boolean
  /** The operator's choice for the card to grow to fill the frame, with its photo, in a replay. Null is the default (see photoMomentOn). */
  photo_moment: boolean | null
}

/** Whether a sign's photo gets a moment in the replay: it must have a photo,
 * and the operator must not have switched it off. Null means the default, which is on. */
export function photoMomentOn(sign: Pick<ReplaySign, 'photo_url' | 'photo_moment'>): boolean {
  return sign.photo_url != null && sign.photo_moment !== false
}

/** The signs whose photos hold the replay for a moment, and where the boat meets them: the ones with a title to show (a join waiting for a name draws nothing) and a photo that is not switched off, less any in `excluded` (a photo that would not load). */
export function momentsOf(placed: readonly ResolvedSign[], excluded: ReadonlySet<string> = new Set()): { clock: number; id: string; at: number }[] {
  return placed.filter((r) => r.sign.title !== '' && photoMomentOn(r.sign) && !excluded.has(r.sign.id)).map((r) => ({ clock: r.clock, id: r.sign.id, at: r.at }))
}

/** A sign placed on the replay. */
export interface ResolvedSign {
  sign: ReplaySign
  /** Timeline seconds where the sign is. */
  clock: number
  /** Unix seconds of the sign. */
  at: number
  entryId: string
  lat: number
  lon: number
}

/** How long before the boat arrives a passing sign appears, in seconds of watching. */
export const SIGN_LEAD_S = 0.5
/** The most cards on screen at once. */
export const MAX_SIGN_CARDS = 2

/** Where an instant stands on the replay: the passage entry and the track
 * position, or null in a trimmed-out stretch, a gap, or where the track has no
 * position. The one rule for "does this moment land in the replay". */
export function placeAt(at: number, timeline: Timeline) {
  const entry = timeline.entries.find((e) => at >= e.start && at <= e.end)
  if (!entry) return null
  const pos = positionAt(entry.segments, at)
  return pos ? { entry, pos } : null
}

/** The signs whose time falls inside a replayed passage, oldest first, each at
 * the track position for its time. A sign in a trimmed-out stretch, in the gap
 * between passages, or where the track has no position is not placed; it is
 * left as it is, not deleted. */
export function resolveSigns(signs: readonly ReplaySign[], timeline: Timeline): ResolvedSign[] {
  const out: ResolvedSign[] = []
  for (const sign of signs) {
    const at = sign.t_ms / 1000
    const placed = placeAt(at, timeline)
    if (!placed) continue
    const { entry, pos } = placed
    out.push({ sign, at, entryId: entry.id, clock: entry.offset + (at - entry.start), lat: pos.lat, lon: pos.lon })
  }
  return out.sort((a, b) => a.sign.t_ms - b.sign.t_ms || a.sign.id.localeCompare(b.sign.id))
}

/** When a sign is up, in seconds of watching: from (inclusive) to (exclusive). */
function window(r: ResolvedSign, reachedAt: number): { from: number; to: number } {
  switch (r.sign.visibility) {
    case 'always': return { from: -Infinity, to: Infinity }
    case 'after': return { from: reachedAt, to: Infinity }
    case 'passing': return { from: reachedAt - SIGN_LEAD_S, to: reachedAt + r.sign.show_s }
  }
}

/**
 * The cards on screen after `wall` seconds of watching, in the order they
 * stand: always signs first, then the rest oldest to newest. At most
 * MAX_SIGN_CARDS.
 *
 * Signs take their places in the order they appear. When a third would be up,
 * the one that appeared earliest gives way for good: it does not come back when
 * a later one expires. Always signs are never the one that gives way: with two
 * of them nothing else shows, and with more than two the earliest in time show.
 * The answer is worked out from the signs alone, replaying the appearances up
 * to `wall`, so a seek lands on exactly what continuous play would show.
 * Watching time is wall-clock: `wallAt` (the replay clock's mapping) is what
 * makes a slowed stretch last as long as it feels.
 */
export function visibleSigns(resolved: readonly ResolvedSign[], wall: number, wallAt: (clock: number, sign: ResolvedSign) => number): ResolvedSign[] {
  interface Entry { r: ResolvedSign; from: number; to: number }
  const entries: Entry[] = []
  for (const r of resolved) {
    const w = window(r, wallAt(r.clock, r))
    if (w.from <= wall) entries.push({ r, ...w })
  }
  entries.sort((a, b) => a.from - b.from || a.r.sign.t_ms - b.r.sign.t_ms || a.r.sign.id.localeCompare(b.r.sign.id))
  let up: Entry[] = []
  for (const e of entries) {
    up = up.filter((u) => u.to > e.from)
    up.push(e)
    if (up.length > MAX_SIGN_CARDS) {
      const give = up.find((u) => u.r.sign.visibility !== 'always') ?? up[up.length - 1]
      up = up.filter((u) => u !== give)
    }
  }
  const live = up.filter((u) => u.to > wall)
  const always = live.filter((u) => u.r.sign.visibility === 'always')
  return [...always, ...live.filter((u) => u.r.sign.visibility !== 'always')].map((u) => u.r)
}

/** How far above its point a card floats, by the place it holds, so two cards
 * for nearby points do not sit on top of each other. */
export const SIGN_LIFT_PX = [20, 104] as const
/** The gap kept between a card and the edge of the map. */
export const SIGN_EDGE_MARGIN_PX = 6
/** The shortest leader line a card is squeezed down to. */
export const SIGN_MIN_LEADER_PX = 8
/** Room kept clear at the top for the Play, Pause and zoom controls. */
export const SIGN_TOP_MARGIN_PX = 48
/** The leader must still meet the card, so a card slides no further than this
 * short of its edge from centred over its point. */
const LEADER_INSET_PX = 6

export interface SignPlacementInput {
  /** The sign's point in the map container, in pixels. */
  point: { x: number; y: number }
  container: { w: number; h: number }
  /** The card's own size. */
  card: { w: number; h: number }
  /** The leader line length the card would like. */
  lift: number
  /** Where the card was last put. It stays there for as long as it fits, so it
   * does not hop from one side of the boat to the other on a small move of the camera. */
  previous?: CardMode
}

/** Where a card sits against its point: above it, below it, or beside it. */
export type CardMode = 'above' | 'below' | 'left' | 'right'

/** A line from the sign's point to the nearest place on its card's edge. */
export interface Leader { x1: number; y1: number; x2: number; y2: number }

/** Where a card goes on the map, in the map's pixels: one result, which the
 * page's marker, the video's canvas and the exported player all draw from. */
export interface CardPlacement {
  /** What the card takes up on the map: smaller than its own size by `scale` when the map is too short to hold it. */
  rect: Rect
  /** 1 at the card's own size. The card is drawn at its own size and made this much smaller. */
  scale: number
  mode: CardMode
  /** From the point to the card, or null when the point is off the map (it has no dot to run a line from) or the card sits over it. */
  leader: Leader | null
  /** The point is so far off the map that its card is not shown (it would be off the map with it). */
  hidden: boolean
}

/** How far a card beside its point stands from it: the leader across. */
export const SIGN_SIDE_LEADER_PX = 16
const SIGN_SCALE_STEP = 0.05

interface Found { rect: Rect; leader: Leader | null }

/** The card at one size in one place against its point; null if it fits there
 * whole inside the map. */
function tryMode(mode: CardMode, input: SignPlacementInput, w: number, h: number): Found | null {
  const { point, container, lift } = input
  const m = SIGN_EDGE_MARGIN_PX
  const onMap = point.x >= 0 && point.x <= container.w && point.y >= 0 && point.y <= container.h
  if (mode === 'above' || mode === 'below') {
    // Across: centred on the point, slid to stay within the map's margin, and the leader must still meet the card.
    if (w + 2 * m > container.w || h + 2 * m > container.h) return null
    let x = Math.max(m, Math.min(container.w - m - w, point.x - w / 2))
    if (onMap) {
      // The leader must still meet the card, so it may sit a little closer to the edge than the margin, but never off the map.
      x = Math.max(point.x - w + LEADER_INSET_PX, Math.min(point.x - LEADER_INSET_PX, x))
      if (x < 0 || x + w > container.w) return null
    }
    let y: number
    if (mode === 'above') {
      // With the top clear for the map's controls.
      const room = point.y - h - SIGN_TOP_MARGIN_PX
      if (room < SIGN_MIN_LEADER_PX) return null
      y = point.y - SIGN_DOT_NET_PX - Math.min(lift, room) - h
    } else {
      const under = container.h - m - point.y - SIGN_DOT_NET_PX - h
      if (under < SIGN_MIN_LEADER_PX) return null
      y = point.y + SIGN_DOT_NET_PX + Math.min(lift, under)
    }
    // A point just past the top or foot of the map still has its card whole inside it.
    if (!onMap) y = Math.max(m, Math.min(container.h - m - h, y))
    const edge = mode === 'above' ? y + h : y
    return { rect: { x, y, w, h }, leader: onMap ? { x1: point.x, y1: point.y, x2: point.x, y2: edge } : null }
  }
  // Beside the point, centred on it as far as the map allows, and never in the band kept clear for the controls.
  if (h + SIGN_TOP_MARGIN_PX + m > container.h) return null
  const reach = SIGN_DOT_NET_PX + SIGN_SIDE_LEADER_PX
  const x = mode === 'right' ? Math.max(m, point.x + reach) : Math.min(container.w - m - w, point.x - reach - w)
  if (x < m - 1e-6 || x + w > container.w - m + 1e-6) return null
  const y = Math.max(SIGN_TOP_MARGIN_PX, Math.min(container.h - m - h, point.y - h / 2))
  const nearest = Math.max(y, Math.min(y + h, point.y))
  return { rect: { x, y, w, h }, leader: onMap ? { x1: point.x, y1: point.y, x2: mode === 'right' ? x : x + w, y2: nearest } : null }
}

/** Where a card sits so it is always whole inside the map. Above its point
 * when there is room, else below it, else beside it; a card keeps the place it
 * had (`previous`) for as long as that still fits. When the map is too short
 * for any of those, the card is made smaller until one fits, rather than cut
 * off. A point just past the edge of the map still has its card whole inside
 * it, with no leader to run; one far off the map has no card shown. Shared by
 * the app, the video and the exported player. */
export function placeSignCard(input: SignPlacementInput): CardPlacement {
  const { point, container, card, lift, previous } = input
  if (container.w <= 0 || container.h <= 0 || card.w <= 0 || card.h <= 0) {
    const rect = { x: point.x - card.w / 2, y: point.y - SIGN_DOT_NET_PX - lift - card.h, w: card.w, h: card.h }
    return { rect, scale: 1, mode: 'above', leader: { x1: point.x, y1: point.y, x2: point.x, y2: rect.y + rect.h }, hidden: false }
  }
  // Far off the map: the card goes with its point, off the map, and is not shown.
  if (point.x < -card.w || point.x > container.w + card.w || point.y < -card.h || point.y > container.h + card.h) {
    const rect = { x: point.x - card.w / 2, y: point.y - SIGN_DOT_NET_PX - lift - card.h, w: card.w, h: card.h }
    return { rect, scale: 1, mode: 'above', leader: null, hidden: true }
  }
  const reach = SIGN_DOT_NET_PX + SIGN_SIDE_LEADER_PX
  const roomRight = container.w - SIGN_EDGE_MARGIN_PX - (point.x + reach + card.w)
  const roomLeft = point.x - reach - card.w - SIGN_EDGE_MARGIN_PX
  const sides: CardMode[] = roomRight >= roomLeft ? ['right', 'left'] : ['left', 'right']
  const preferred: CardMode[] = ['above', 'below', ...sides]
  const order: CardMode[] = previous ? [previous, ...preferred.filter((mode) => mode !== previous)] : preferred
  for (let scale = 1; scale > SIGN_SCALE_STEP; scale -= SIGN_SCALE_STEP) {
    const w = card.w * scale
    const h = card.h * scale
    for (const mode of order) {
      const found = tryMode(mode, input, w, h)
      if (found) return { rect: found.rect, scale, mode, leader: found.leader, hidden: false }
    }
  }
  // A map too small for any way round: the card is fitted to it, beside the point where it can.
  const m = SIGN_EDGE_MARGIN_PX
  const scale = Math.max(0.01, Math.min(1, (container.w - 2 * m) / card.w, (container.h - 2 * m) / card.h))
  const w = card.w * scale
  const h = card.h * scale
  const x = Math.max(m, Math.min(container.w - m - w, point.x + SIGN_DOT_NET_PX + SIGN_SIDE_LEADER_PX))
  const y = Math.max(m, Math.min(container.h - m - h, point.y - h / 2))
  const over = point.x >= x && point.x <= x + w && point.y >= y && point.y <= y + h
  const mode: CardMode = x > point.x ? 'right' : 'left'
  return { rect: { x, y, w, h }, scale, mode, leader: over ? null : { x1: point.x, y1: point.y, x2: mode === 'right' ? x : x + w, y2: Math.max(y, Math.min(y + h, point.y)) }, hidden: false }
}

// ── the card's size ──────────────────────────────────────────────────────
// SignCard is built from these, so the page and the replay's photo moment, which
// grows from the card, cannot disagree about where the card is.

/** Seconds a card takes to fade in, and the stay's label to fade in and out. */
export const CARD_FADE_S = 0.3

/** The width of a card with a photo, and of one with text alone. */
export const SIGN_CARD_PHOTO_W_PX = 256
export const SIGN_CARD_TEXT_W_PX = 224
/** The space round the card's contents, and between its photo and its text. */
export const SIGN_CARD_PAD_PX = 8
export const SIGN_CARD_GAP_PX = 8
/** The line heights of the title and of the note, which shows two lines at most. */
export const SIGN_TITLE_LINE_PX = 20
export const SIGN_NOTE_LINE_PX = 16
/** The most lines of the note a card shows. */
export const SIGN_NOTE_MAX_LINES = 2
/** How far the card's leader meets the point: the dot's height that shows. */
const SIGN_DOT_NET_PX = 4

/** The size of a sign's card in a map `containerW` wide, and of its photo. The
 * height comes from the contents: the photo, the title's one line, and the
 * `noteLines` lines of the note, which the caller has wrapped by measuring the
 * text (lib/sign-note), so this is exact. A photo that cannot be loaded is a
 * placeholder of the photo's size, with its words inside. */
export function signCardBox(sign: Pick<ReplaySign, 'photo_url'>, containerW: number, noteLines: number): { w: number; h: number; photoW: number; photoH: number } {
  const w = Math.min(sign.photo_url != null ? SIGN_CARD_PHOTO_W_PX : SIGN_CARD_TEXT_W_PX, Math.max(0, containerW - 2 * SIGN_EDGE_MARGIN_PX))
  const photoW = w - 2 * SIGN_CARD_PAD_PX
  const photoH = photoW / PHOTO_ASPECT
  const text = SIGN_TITLE_LINE_PX + noteLines * SIGN_NOTE_LINE_PX
  return { w, h: 2 * SIGN_CARD_PAD_PX + (sign.photo_url != null ? photoH + SIGN_CARD_GAP_PX : 0) + text, photoW, photoH }
}

/** Where a sign's card is, as `placeSignCard` puts it for the sign's point. */
export function signCardPlacement(
  sign: Pick<ReplaySign, 'photo_url'>,
  point: { x: number; y: number },
  container: { w: number; h: number },
  lift: number,
  noteLines: number,
  previous?: CardMode,
): CardPlacement {
  const { w, h } = signCardBox(sign, container.w, noteLines)
  return placeSignCard({ point, container, card: { w, h }, lift, previous })
}
