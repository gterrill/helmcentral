export function formatCoordinate(value: number | null, latitude: boolean) {
  if (value === null) {
    return '—'
  }

  const absolute = Math.abs(value)
  const degrees = Math.floor(absolute)
  const minutesFloat = (absolute - degrees) * 60
  const minutes = Math.floor(minutesFloat)
  const seconds = (minutesFloat - minutes) * 60
  const hemisphere = latitude ? (value >= 0 ? 'N' : 'S') : value >= 0 ? 'E' : 'W'

  return `${degrees}° ${String(minutes).padStart(2, '0')}' ${seconds.toFixed(1).padStart(4, '0')}" ${hemisphere}`
}

const COMPASS_POINTS = ['N', 'NNE', 'NE', 'ENE', 'E', 'ESE', 'SE', 'SSE', 'S', 'SSW', 'SW', 'WSW', 'W', 'WNW', 'NW', 'NNW']

// Converts an already-normalized 0-360 bearing to its nearest 16-point
// compass label (e.g. 0 -> 'N', 100 -> 'ESE'). Shared by formatHeading below
// and forecast-drawer.tsx's compassLabel, which use it for two different
// output formats (one includes the degree number, one doesn't).
export function compassPointFor(normalizedDegrees: number): string {
  return COMPASS_POINTS[Math.round(normalizedDegrees / 22.5) % COMPASS_POINTS.length]
}

export function formatHeading(headingTrue: number | null) {
  if (headingTrue === null) {
    return '—'
  }

  const normalized = ((headingTrue % 360) + 360) % 360
  const direction = compassPointFor(normalized)

  return `${Math.round(normalized)}° ${direction}`
}
