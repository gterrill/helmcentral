// Positions as the operator writes them: degrees and decimal minutes
// ("020º32.676' S"), the way a chart plotter shows them. Saved places are
// stored in signed decimal degrees; these two functions are the only
// translation between that and what the operator reads and types.

export type CoordinateAxis = 'lat' | 'lon'

const HEMISPHERES: Record<CoordinateAxis, { positive: string; negative: string }> = {
  lat: { positive: 'N', negative: 'S' },
  lon: { positive: 'E', negative: 'W' },
}

/** "20°32.676' S" / "149°07.136' E". Minutes are rounded to the thousandth. */
export function formatCoordinateDM(value: number, axis: CoordinateAxis): string {
  const { positive, negative } = HEMISPHERES[axis]
  const hemisphere = value < 0 ? negative : positive
  const absolute = Math.abs(value)
  let degrees = Math.floor(absolute)
  let minutes = Math.round((absolute - degrees) * 60 * 1000) / 1000
  if (minutes >= 60) {
    degrees += 1
    minutes = 0
  }
  const degreeText = String(degrees).padStart(axis === 'lat' ? 2 : 3, '0')
  return `${degreeText}°${minutes.toFixed(3).padStart(6, '0')}' ${hemisphere}`
}

export function formatPositionDM(lat: number, lon: number): string {
  return `${formatCoordinateDM(lat, 'lat')} ${formatCoordinateDM(lon, 'lon')}`
}

const NUMBER = /\d+(?:\.\d+)?/g
const SYMBOLS = /[°º˚'′’"″\s,]/g

/**
 * Reads one coordinate as the operator types it: degrees and decimal minutes
 * (`020º32.676' S`, `20 32.676 S`), degrees, minutes and seconds, or decimal
 * degrees (`-20.5446`, `20.5446 S`). A hemisphere letter or a leading minus
 * makes it negative, never both. Returns null for anything it cannot read
 * exactly, including a hemisphere of the wrong axis or a value out of range.
 */
export function parseCoordinate(text: string, axis: CoordinateAxis): number | null {
  const input = text.trim()
  if (input === '') return null
  const numbers = input.match(NUMBER)
  if (!numbers || numbers.length > 3) return null
  const rest = input.replace(NUMBER, '').replace(SYMBOLS, '')
  const minus = rest.includes('-')
  const letters = rest.replace(/[-+]/g, '').toUpperCase()
  if (letters.length > 1) return null
  const { positive, negative } = HEMISPHERES[axis]
  if (letters !== '' && letters !== positive && letters !== negative) return null
  if (minus && letters !== '') return null

  const [degrees, minutes = 0, seconds = 0] = numbers.map(Number)
  if (numbers.length > 1 && minutes >= 60) return null
  if (numbers.length > 2 && seconds >= 60) return null
  if (numbers.length > 1 && !Number.isInteger(degrees)) return null
  if (numbers.length > 2 && !Number.isInteger(minutes)) return null
  const magnitude = degrees + minutes / 60 + seconds / 3600
  const limit = axis === 'lat' ? 90 : 180
  if (magnitude > limit) return null
  const negativeValue = minus || letters === negative
  return negativeValue ? -magnitude : magnitude
}
