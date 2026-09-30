import { GRID_MARGIN, rowsForHeight } from '@/lib/grid-metrics'

/**
 * A list tile (Nearby Vessels, alarms and the like) can report the height its
 * content needs, and the grid lets it shrink to that height when it is the
 * bottom tile of its column. The saved height stays the maximum and is never
 * rewritten; the shrink is a runtime view of the same layout.
 *
 * Only a bottom tile may shrink because shrinking frees the space under it,
 * and with another tile below, that tile would slide up and back down as the
 * list grew and emptied.
 */

/** Header plus one row: the least a shrunk tile is ever given. */
export const MIN_SHRUNK_ROWS = 2

interface Placed {
  id: string
  x: number
  y: number
  w: number
  h: number
}

/**
 * Tiles with nothing beneath them in their own columns. Another tile blocks
 * one if its x-range overlaps at all (a partial overlap counts) and it starts
 * at or below the first tile's saved bottom edge. Saved heights are used on
 * both sides, so the answer does not change as tiles shrink. The hero is
 * neither: its row is rendered apart from the grid and its slot is only held
 * empty.
 */
export function bottomOfColumnIds(widgets: readonly Placed[], heroId?: string): Set<string> {
  const candidates = widgets.filter((w) => w.id !== heroId)
  const out = new Set<string>()
  for (const w of candidates) {
    const blocked = candidates.some(
      (o) => o.id !== w.id && o.x < w.x + w.w && w.x < o.x + o.w && o.y >= w.y + w.h,
    )
    if (!blocked) out.add(w.id)
  }
  return out
}

/** The last tile in the stacked (y, then x) order of the narrow layout. */
export function lastInStackId(widgets: readonly Placed[], heroId?: string): string | null {
  const stack = widgets.filter((w) => w.id !== heroId).sort((a, b) => a.y - b.y || a.x - b.x)
  return stack.length > 0 ? stack[stack.length - 1].id : null
}

/**
 * The rows to give a tile whose content needs `neededPx`: no more than the
 * saved height, no fewer than header plus one row. Unmeasured keeps the saved
 * height. A saved height already under the floor is left alone.
 */
export function effectiveRows(savedRows: number, neededPx: number | undefined, rowMargin: number = GRID_MARGIN): number {
  if (neededPx === undefined) return savedRows
  return Math.min(savedRows, Math.max(MIN_SHRUNK_ROWS, rowsForHeight(neededPx, rowMargin)))
}

interface EffectiveRowsInput {
  widgets: readonly Placed[]
  /** Measured content height in px, by widget id; only list tiles report. */
  needed: Readonly<Record<string, number>>
  editing: boolean
  desktop: boolean
  rowMargin: number
  heroId?: string
}

/**
 * The rows each widget is drawn at. Editing always gets the saved height, so
 * a drag or resize can only ever commit the operator's own number.
 */
export function effectiveRowsById({ widgets, needed, editing, desktop, rowMargin, heroId }: EffectiveRowsInput): Record<string, number> {
  const shrinkable = editing
    ? new Set<string>()
    : desktop
      ? bottomOfColumnIds(widgets, heroId)
      : new Set<string>([lastInStackId(widgets, heroId)].filter((id): id is string => id !== null))

  const out: Record<string, number> = {}
  for (const w of widgets) {
    out[w.id] = shrinkable.has(w.id) ? effectiveRows(w.h, needed[w.id], rowMargin) : w.h
  }
  return out
}
