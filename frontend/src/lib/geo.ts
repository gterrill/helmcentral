// ── Haversine distance (m) ──────────────────────────────────────────────────
export function haversineMeters(lat1: number, lon1: number, lat2: number, lon2: number): number {
  const R = 6371000
  const toRad = (d: number) => (d * Math.PI) / 180
  const dLat = toRad(lat2 - lat1)
  const dLon = toRad(lon2 - lon1)
  const a =
    Math.sin(dLat / 2) ** 2 +
    Math.cos(toRad(lat1)) * Math.cos(toRad(lat2)) * Math.sin(dLon / 2) ** 2
  return R * 2 * Math.atan2(Math.sqrt(a), Math.sqrt(1 - a))
}

// ── Initial bearing (degrees, 0-360) ────────────────────────────────────────
export function bearingDeg(lat1: number, lon1: number, lat2: number, lon2: number): number {
  const toRad = (d: number) => (d * Math.PI) / 180
  const toDeg = (r: number) => (r * 180) / Math.PI
  const dLon = toRad(lon2 - lon1)
  const y = Math.sin(dLon) * Math.cos(toRad(lat2))
  const x =
    Math.cos(toRad(lat1)) * Math.sin(toRad(lat2)) -
    Math.sin(toRad(lat1)) * Math.cos(toRad(lat2)) * Math.cos(dLon)
  return ((toDeg(Math.atan2(y, x)) + 360) % 360)
}

// ── Destination point given start + bearing + distance ──────────────────────
export function destinationPoint(lat: number, lon: number, bearingRad: number, distanceM: number): [number, number] {
  const R = 6371000
  const lat1 = (lat * Math.PI) / 180
  const lon1 = (lon * Math.PI) / 180
  const lat2 = Math.asin(
    Math.sin(lat1) * Math.cos(distanceM / R) +
    Math.cos(lat1) * Math.sin(distanceM / R) * Math.cos(bearingRad),
  )
  const lon2 =
    lon1 +
    Math.atan2(
      Math.sin(bearingRad) * Math.sin(distanceM / R) * Math.cos(lat1),
      Math.cos(distanceM / R) - Math.sin(lat1) * Math.sin(lat2),
    )
  return [(lat2 * 180) / Math.PI, ((lon2 * 180) / Math.PI + 540) % 360 - 180]
}

// ── Circle round a point ────────────────────────────────────────────────────
/** A closed ring of [lon, lat] points `radiusM` from (lat, lon): `steps` points round, then the first again. */
export function circlePolygon(lat: number, lon: number, radiusM: number, steps = 64): [number, number][] {
  const ring: [number, number][] = []
  for (let i = 0; i < steps; i += 1) {
    const [pLat, pLon] = destinationPoint(lat, lon, (2 * Math.PI * i) / steps, radiusM)
    ring.push([pLon, pLat])
  }
  ring.push(ring[0])
  return ring
}

// ── Web Mercator as fractions of the world: x from 0 (antimeridian, west) to 1
// (east), y from 0 (north) to 1 (south). Multiply by the world's size in
// pixels at a zoom (512 * 2^zoom) for screen units.

/** Fraction of the world's width. */
export function mercatorX(lon: number): number {
  return (lon + 180) / 360
}

/** Fraction of the world's height: Web Mercator's characteristic latitude stretch. */
export function mercatorY(lat: number): number {
  const latRad = (lat * Math.PI) / 180
  return 0.5 - Math.log(Math.tan(Math.PI / 4 + latRad / 2)) / (2 * Math.PI)
}

/** The longitude at a fraction of the world's width. */
export function lonOfMercatorX(x: number): number {
  return x * 360 - 180
}

/** The latitude at a fraction of the world's height. */
export function latOfMercatorY(y: number): number {
  return (360 / Math.PI) * Math.atan(Math.exp((0.5 - y) * 2 * Math.PI)) - 90
}
