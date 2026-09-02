import { destinationPoint } from '@/lib/geo'

/**
 * Reverse look-up from canvas pixel to spoke row and range bin.
 *
 * Forward ray-writing (walk each spoke, paint a wedge) leaves radial gaps
 * at the rim -- about half a pixel of hole between adjacent spokes at
 * 1024px -- and costs more exactly when there is more to see (tens of
 * thousands of `fill()` calls per revolution). The reverse LUT has no gaps
 * by construction, costs the same regardless of content, and the expensive
 * part (`atan2`/`hypot` over every pixel) depends only on
 * `(sizePx, spokeCount, binCount)`, so it is computed once and memoised for
 * the process lifetime rather than once per frame.
 */
export interface EchoLut {
  sizePx: number
  spokeCount: number
  binCount: number
  /** Which spoke row (bearing bucket) each pixel reads. */
  spokeIndex: Uint16Array
  /** Which range bin within that row each pixel reads. */
  rangeBin: Uint16Array
  /** 1 inside the inscribed circle, 0 outside it (the square's corners). */
  inside: Uint8Array
}

/**
 * Build the LUT for one (sizePx, spokeCount, binCount) triple. North-up,
 * per the Phase 0 measurement that `bearing` is populated on every spoke:
 * the pixel directly above centre is spoke 0, directly right is
 * spokeCount/4, directly below is spokeCount/2. Never rotated by heading.
 */
export function buildEchoLut(sizePx: number, spokeCount: number, binCount: number): EchoLut {
  const spokeIndex = new Uint16Array(sizePx * sizePx)
  const rangeBin = new Uint16Array(sizePx * sizePx)
  const inside = new Uint8Array(sizePx * sizePx)

  const centre = (sizePx - 1) / 2
  const halfSidePx = sizePx / 2
  const twoPi = Math.PI * 2

  for (let py = 0; py < sizePx; py++) {
    const dy = py - centre
    const rowOffset = py * sizePx
    for (let px = 0; px < sizePx; px++) {
      const i = rowOffset + px
      const dx = px - centre
      const distPx = Math.hypot(dx, dy)

      if (distPx > halfSidePx) {
        inside[i] = 0
        continue
      }
      inside[i] = 1

      // Image rows increase downward, so "up" (north) is -dy. Bearing is
      // clockwise from north, which is exactly what atan2(dx, -dy) gives:
      // straight up -> 0, right -> +pi/2 (east), down -> +pi (south).
      const bearing = Math.atan2(dx, -dy)
      const normalized = ((bearing % twoPi) + twoPi) % twoPi
      spokeIndex[i] = Math.round((normalized / twoPi) * spokeCount) % spokeCount
      rangeBin[i] = Math.min(binCount - 1, Math.floor((distPx / halfSidePx) * binCount))
    }
  }

  return { sizePx, spokeCount, binCount, spokeIndex, rangeBin, inside }
}

const lutCache = new Map<string, EchoLut>()

/**
 * Memoised buildEchoLut. This is up to ~823k atan2/hypot calls at the
 * production 1024px/4096-spoke size, and must happen once per process, not
 * once per mounted map or per frame.
 */
export function getEchoLut(sizePx: number, spokeCount: number, binCount: number): EchoLut {
  const key = `${sizePx}:${spokeCount}:${binCount}`
  let lut = lutCache.get(key)
  if (lut === undefined) {
    lut = buildEchoLut(sizePx, spokeCount, binCount)
    lutCache.set(key, lut)
  }
  return lut
}

/**
 * The four corners of the square the echo canvas covers, for MapLibre's
 * canvas source `coordinates`. An axis-aligned lat/lon box is correct here:
 * Mercator is conformal, so the residual error across an 11km box at 20°S
 * is about one pixel in a thousand.
 *
 * Returns MapLibre's corner order: [[lonW,latN],[lonE,latN],[lonE,latS],[lonW,latS]]
 * (NW, NE, SE, SW).
 */
export function echoCornerCoordinates(
  lat: number,
  lon: number,
  halfSideM: number,
): [[number, number], [number, number], [number, number], [number, number]] {
  const [latN] = destinationPoint(lat, lon, 0, halfSideM)
  const [, lonE] = destinationPoint(lat, lon, Math.PI / 2, halfSideM)
  const [latS] = destinationPoint(lat, lon, Math.PI, halfSideM)
  const [, lonW] = destinationPoint(lat, lon, (3 * Math.PI) / 2, halfSideM)

  return [
    [lonW, latN],
    [lonE, latN],
    [lonE, latS],
    [lonW, latS],
  ]
}
