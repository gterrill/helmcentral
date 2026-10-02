import type { DeckLayout, InventoryZone } from '@/hooks/use-inventory'

// ADR 0156: the deck editor's draft. Outlines and pins are fractions (0..1) of
// the plan image. The helpers are pure so the editor's pointer handling stays
// thin and the rules (3 to 64 points, clamped to the plan) are tested alone.

export type Point = [number, number]

export interface LayoutDraft {
  /** zone id -> outline */
  polygons: Record<string, Point[]>
  /** bin id -> pin */
  pins: Record<string, { x: number; y: number }>
}

export const MIN_POINTS = 3
export const MAX_POINTS = 64

const clamp01 = (n: number) => Math.min(1, Math.max(0, n))

/** The draft a deck starts from: the zones already outlined on it, and their pinned bins. */
export function layoutFromZones(zones: InventoryZone[], deckId: string): LayoutDraft {
  const polygons: LayoutDraft['polygons'] = {}
  const pins: LayoutDraft['pins'] = {}
  for (const zone of zones) {
    if (zone.deck_id !== deckId || !zone.polygon) continue
    polygons[zone.id] = zone.polygon.map((p): Point => [p[0], p[1]])
    for (const bin of zone.bins) {
      if (bin.pin) pins[bin.id] = { x: bin.pin.x, y: bin.pin.y }
    }
  }
  return { polygons, pins }
}

/**
 * The request body for saving a draft. A pin is only sent when its bin's
 * location has an outline in the draft (the server refuses it otherwise), so
 * when `zones` is given, pins of bins in an un-outlined location are left out.
 */
export function layoutToPayload(draft: LayoutDraft, zones?: InventoryZone[]): DeckLayout {
  const binZone = new Map<string, string>()
  for (const zone of zones ?? []) for (const bin of zone.bins) binZone.set(bin.id, zone.id)
  return {
    zones: Object.entries(draft.polygons).map(([id, polygon]) => ({ id, polygon })),
    bins: Object.entries(draft.pins)
      .filter(([binId]) => {
        if (!zones) return true
        const zoneId = binZone.get(binId)
        return zoneId !== undefined && draft.polygons[zoneId] !== undefined
      })
      .map(([id, pin]) => ({ id, x: pin.x, y: pin.y })),
  }
}

export function sameLayout(a: LayoutDraft, b: LayoutDraft): boolean {
  return JSON.stringify(normalise(a)) === JSON.stringify(normalise(b))
}

function normalise(d: LayoutDraft) {
  return {
    polygons: Object.keys(d.polygons).sort().map((k) => [k, d.polygons[k]]),
    pins: Object.keys(d.pins).sort().map((k) => [k, d.pins[k].x, d.pins[k].y]),
  }
}

export function moveVertex(points: Point[], index: number, to: Point): Point[] {
  return points.map((p, i) => (i === index ? [clamp01(to[0]), clamp01(to[1])] : p))
}

/** Inserts a vertex after `index`; returns the same array when already at the limit. */
export function insertVertex(points: Point[], index: number, at: Point): Point[] {
  if (points.length >= MAX_POINTS) return points
  return [...points.slice(0, index + 1), [clamp01(at[0]), clamp01(at[1])], ...points.slice(index + 1)]
}

/** Removes a vertex; returns the same array when that would leave fewer than three. */
export function removeVertex(points: Point[], index: number): Point[] {
  if (points.length <= MIN_POINTS) return points
  return points.filter((_, i) => i !== index)
}

/** The draft without a zone's outline and without the pins of its bins. */
export function withoutZone(draft: LayoutDraft, zone: InventoryZone): LayoutDraft {
  const polygons = { ...draft.polygons }
  delete polygons[zone.id]
  const pins = { ...draft.pins }
  for (const bin of zone.bins) delete pins[bin.id]
  return { polygons, pins }
}
