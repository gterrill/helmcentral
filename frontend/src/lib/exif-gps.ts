import { damagedExif, findExifTiff, guardExif, openTiff } from '@/lib/exif-tiff'

// The phone's position and facing from a photo's GPS block. Android hides the
// location of a photo picked in the browser by writing the block with every
// value zero and every reference letter empty. That is a missing position,
// never 0, 0 and never a bearing of due north, so a value counts only when its
// reference letter says it was really written.

export interface ExifGps {
  /** Degrees north, or null when the photo carries no real latitude and longitude. */
  lat: number | null
  lon: number | null
  /** Degrees clockwise from north (true or magnetic) the phone faced, or null. */
  bearingDeg: number | null
  /** Which north that bearing is measured from: T true, M magnetic. Null when there is no bearing. */
  bearingRef: 'T' | 'M' | null
}

const NONE: ExifGps = { lat: null, lon: null, bearingDeg: null, bearingRef: null }

const TAG_GPS_IFD = 0x8825
const TAG_LAT_REF = 1
const TAG_LAT = 2
const TAG_LON_REF = 3
const TAG_LON = 4
const TAG_DIR_REF = 16
const TAG_DIR = 17
const TYPE_RATIONAL = 5

/**
 * The GPS block of a JPEG. A photo with no Exif, no GPS block or a zeroed one
 * answers all-null; a block that is there but cannot be read throws.
 */
export function readExifGps(buffer: ArrayBuffer): ExifGps {
  return guardExif(() => {
    const tiff = findExifTiff(buffer)
    if (!tiff) return NONE
    const { u16, u32, entries, rootEntries, ascii, dataAt, within } = openTiff(tiff)

    const pointer = rootEntries().get(TAG_GPS_IFD)
    if (pointer === undefined) return NONE
    const gpsAt = u32(pointer + 8)
    if (gpsAt === 0) return NONE
    const gps = entries(gpsAt)

    const letter = (tag: number): string => {
      const entry = gps.get(tag)
      return entry === undefined ? '' : ascii(entry).trim().toUpperCase()
    }
    // The rationals of an entry as numbers, or null when any has a zero denominator.
    const rationals = (tag: number, want: number): number[] | null => {
      const entry = gps.get(tag)
      if (entry === undefined) return null
      if (u16(entry + 2) !== TYPE_RATIONAL || u32(entry + 4) !== want) throw damagedExif()
      const from = dataAt(entry)
      within(from, want * 8)
      const out: number[] = []
      for (let i = 0; i < want; i++) {
        const num = u32(from + i * 8)
        const den = u32(from + i * 8 + 4)
        if (den === 0) return null
        out.push(num / den)
      }
      return out
    }
    const degrees = (v: number[] | null): number | null => (v ? v[0] + v[1] / 60 + v[2] / 3600 : null)

    let lat = degrees(rationals(TAG_LAT, 3))
    let lon = degrees(rationals(TAG_LON, 3))
    const latRef = letter(TAG_LAT_REF)
    const lonRef = letter(TAG_LON_REF)
    if (lat !== null && latRef !== 'N' && latRef !== 'S') lat = null
    if (lon !== null && lonRef !== 'E' && lonRef !== 'W') lon = null
    if (lat !== null && latRef === 'S') lat = -lat
    if (lon !== null && lonRef === 'W') lon = -lon
    // Both or neither, in range, and not the 0, 0 of an unset block.
    if (lat === null || lon === null || Math.abs(lat) > 90 || Math.abs(lon) > 180 || (lat === 0 && lon === 0)) {
      lat = null
      lon = null
    }

    let bearingDeg: number | null = null
    let bearingRef: 'T' | 'M' | null = null
    const dirRef = letter(TAG_DIR_REF)
    const dir = rationals(TAG_DIR, 1)
    if (dir !== null && (dirRef === 'T' || dirRef === 'M') && dir[0] >= 0 && dir[0] < 360) {
      bearingDeg = dir[0]
      bearingRef = dirRef
    }

    return { lat, lon, bearingDeg, bearingRef }
  })
}
