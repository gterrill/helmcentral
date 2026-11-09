// The Anchor Watch map tints the ground the boat has crossed inside the swing
// circle, and leaves the rest untouched. The server sends the crossed 2.5 m cells as integer
// indices in a local east/north frame at the anchor (docs/adr/0274); this turns
// them back into map polygons.

// The same constant the backend's localENMeters uses.
export const METERS_PER_DEGREE_LAT = 111320

export interface SoundedFrame {
  originLat: number
  originLon: number
  cellM: number
}

export type SoundedCell = readonly [number, number]
type Ring = [number, number][] // [lon, lat], closed

/** Inverse of the backend's localENMeters. */
export function localToLonLat(frame: SoundedFrame, eastM: number, northM: number): [number, number] {
  const lat = frame.originLat + northM / METERS_PER_DEGREE_LAT
  const lon = frame.originLon + eastM / (METERS_PER_DEGREE_LAT * Math.cos((frame.originLat * Math.PI) / 180))
  return [lon, lat]
}

/** The closed ring of one cell, as the backend indexes it: east i*cellM to (i+1)*cellM, north j*cellM to (j+1)*cellM. */
export function cellRing(frame: SoundedFrame, cell: SoundedCell): Ring {
  const [i, j] = cell
  const e0 = i * frame.cellM
  const e1 = (i + 1) * frame.cellM
  const n0 = j * frame.cellM
  const n1 = (j + 1) * frame.cellM
  const ring: Ring = [
    localToLonLat(frame, e0, n0),
    localToLonLat(frame, e1, n0),
    localToLonLat(frame, e1, n1),
    localToLonLat(frame, e0, n1),
  ]
  ring.push(ring[0])
  return ring
}

type Pt = [number, number] // [east, north] metres

// Sutherland-Hodgman against a convex, counter-clockwise polygon.
function clipToConvex(subject: Pt[], clip: Pt[]): Pt[] {
  let out = subject
  for (let k = 0; k < clip.length && out.length > 0; k += 1) {
    const a = clip[k]
    const b = clip[(k + 1) % clip.length]
    const inside = (p: Pt) => (b[0] - a[0]) * (p[1] - a[1]) - (b[1] - a[1]) * (p[0] - a[0]) >= 0
    const cross = (p: Pt, q: Pt): Pt => {
      const dx1 = q[0] - p[0]
      const dy1 = q[1] - p[1]
      const dx2 = b[0] - a[0]
      const dy2 = b[1] - a[1]
      const t = ((a[0] - p[0]) * dy2 - (a[1] - p[1]) * dx2) / (dx1 * dy2 - dy1 * dx2)
      return [p[0] + t * dx1, p[1] + t * dy1]
    }
    const input = out
    out = []
    for (let m = 0; m < input.length; m += 1) {
      const cur = input[m]
      const prev = input[(m + input.length - 1) % input.length]
      if (inside(cur)) {
        if (!inside(prev)) out.push(cross(prev, cur))
        out.push(cur)
      } else if (inside(prev)) {
        out.push(cross(prev, cur))
      }
    }
  }
  return out
}

const CIRCLE_STEPS = 64

/**
 * The cells to tint: the crossed ones, and each uncrossed cell that sits
 * between crossed cells (opposite neighbours N and S, E and W, or on either
 * diagonal). Small holes in the swing band fill in; the band does not grow.
 * For display only; the server's cells are what was actually crossed.
 */
export function withSmallGapsFilled(cells: readonly SoundedCell[]): SoundedCell[] {
  const key = (i: number, j: number) => `${i},${j}`
  const crossed = new Set(cells.map(([i, j]) => key(i, j)))
  const has = (i: number, j: number) => crossed.has(key(i, j))
  const out = new Map<string, SoundedCell>(cells.map(([i, j]) => [key(i, j), [i, j] as const]))
  for (const [ci, cj] of cells) {
    for (let i = ci - 1; i <= ci + 1; i += 1) {
      for (let j = cj - 1; j <= cj + 1; j += 1) {
        if (has(i, j) || out.has(key(i, j))) continue
        if (
          (has(i - 1, j) && has(i + 1, j)) ||
          (has(i, j - 1) && has(i, j + 1)) ||
          (has(i - 1, j - 1) && has(i + 1, j + 1)) ||
          (has(i - 1, j + 1) && has(i + 1, j - 1))
        ) {
          out.set(key(i, j), [i, j] as const)
        }
      }
    }
  }
  return [...out.values()]
}

/**
 * The part of the swing circle the boat has crossed, as a MultiPolygon. Small
 * gaps between crossed cells count as crossed. Each row of cells keeps its runs
 * of crossed cells; a run is cut to the circle, and rows whose runs are the same
 * join into one taller block. There are no per-cell edges, so the tint reads as
 * one area.
 */
export function soundedAreaGeoJSON(
  frame: SoundedFrame,
  cells: readonly SoundedCell[],
  radiusM: number,
): GeoJSON.Feature<GeoJSON.MultiPolygon> {
  const c = frame.cellM
  const circle: Pt[] = []
  for (let s = 0; s < CIRCLE_STEPS; s += 1) {
    const th = (2 * Math.PI * s) / CIRCLE_STEPS
    circle.push([radiusM * Math.cos(th), radiusM * Math.sin(th)]) // counter-clockwise
  }

  const rows = new Map<number, number[]>()
  for (const [i, j] of withSmallGapsFilled(cells)) {
    const row = rows.get(j)
    if (row) row.push(i)
    else rows.set(j, [i])
  }

  // Row j covers north j*c..(j+1)*c. Its crossed runs are in east metres, with
  // neighbouring cells joined.
  const lo = Math.floor(-radiusM / c)
  const hi = Math.ceil(radiusM / c)
  const blocks: { key: string; runs: [number, number][]; n0: number; n1: number }[] = []
  for (let j = lo; j < hi; j += 1) {
    const eastIdx = (rows.get(j) ?? []).slice().sort((a, b) => a - b)
    const runs: [number, number][] = []
    for (const i of eastIdx) {
      const last = runs[runs.length - 1]
      if (last && last[1] === i * c) last[1] = (i + 1) * c
      else runs.push([i * c, (i + 1) * c])
    }
    const key = runs.map((r) => `${r[0]}:${r[1]}`).join('|')
    const last = blocks[blocks.length - 1]
    if (last && last.key === key && last.n1 === j * c) last.n1 = (j + 1) * c
    else blocks.push({ key, runs, n0: j * c, n1: (j + 1) * c })
  }

  // Neighbouring blocks share a horizontal edge. Each puts a vertex at every
  // place the other's runs start or end, so the edges match point for point
  // and no seam shows between them when the tile is cut.
  const breaks = (b: { runs: [number, number][] } | undefined): number[] => (b ? b.runs.flat() : [])
  const between = (xs: number[], a: number, b: number) => xs.filter((x) => x > a && x < b)
  const polygons: [number, number][][][] = []
  blocks.forEach((block, k) => {
    const below = breaks(blocks[k - 1])
    const above = breaks(blocks[k + 1])
    for (const [e0, e1] of block.runs) {
      const bottom = between(below, e0, e1).sort((a, b) => a - b)
      const top = between(above, e0, e1).sort((a, b) => b - a)
      const outline: Pt[] = [
        [e0, block.n0],
        ...bottom.map((e): Pt => [e, block.n0]),
        [e1, block.n0],
        [e1, block.n1],
        ...top.map((e): Pt => [e, block.n1]),
        [e0, block.n1],
      ]
      const clipped = clipToConvex(outline, circle)
      if (clipped.length < 3) continue
      const ring = clipped.map(([e, n]) => localToLonLat(frame, e, n))
      ring.push(ring[0])
      polygons.push([ring])
    }
  })
  return { type: 'Feature', properties: {}, geometry: { type: 'MultiPolygon', coordinates: polygons } }
}

// ---------------------------------------------------------------- wire

export type SoundedCellsAnswer =
  | { status: 'no_watch' | 'not_ready' }
  | { status: 'ready'; frame: SoundedFrame; cells: SoundedCell[] }

interface SoundedCellsWire {
  status?: unknown
  cell_m?: unknown
  origin_lat?: unknown
  origin_lon?: unknown
  cells?: unknown
}

const isNum = (v: unknown): v is number => typeof v === 'number' && Number.isFinite(v)

/** An answer that is not one of the server's shapes is an error, not an empty map. */
export function parseSoundedCells(wire: SoundedCellsWire): SoundedCellsAnswer {
  if (wire.status === 'no_watch' || wire.status === 'not_ready') return { status: wire.status }
  if (wire.status !== 'ready') throw new Error(`sounded cells: unknown status ${JSON.stringify(wire.status)}`)
  if (!isNum(wire.cell_m) || wire.cell_m <= 0 || !isNum(wire.origin_lat) || !isNum(wire.origin_lon) || !Array.isArray(wire.cells)) {
    throw new Error('sounded cells: a ready answer without its frame or cells')
  }
  const cells: SoundedCell[] = wire.cells.map((cell) => {
    if (!Array.isArray(cell) || cell.length !== 2 || !Number.isInteger(cell[0]) || !Number.isInteger(cell[1])) {
      throw new Error('sounded cells: a cell that is not an [i, j] pair')
    }
    return [cell[0], cell[1]] as const
  })
  return { status: 'ready', frame: { originLat: wire.origin_lat, originLon: wire.origin_lon, cellM: wire.cell_m }, cells }
}
