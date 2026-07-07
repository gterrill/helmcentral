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

export function formatHeading(headingTrue: number | null) {
  if (headingTrue === null) {
    return '—'
  }

  const normalized = ((headingTrue % 360) + 360) % 360
  const directions = ['N', 'NNE', 'NE', 'ENE', 'E', 'ESE', 'SE', 'SSE', 'S', 'SSW', 'SW', 'WSW', 'W', 'WNW', 'NW', 'NNW']
  const direction = directions[Math.round(normalized / 22.5) % directions.length]

  return `${Math.round(normalized)}° ${direction}`
}
