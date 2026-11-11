import { exifDamaged, findExifTiff, guardExif } from '@/lib/exif-time'

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
}

const NONE: ExifGps = { lat: null, lon: null, bearingDeg: null }

const TAG_GPS_IFD = 0x8825
const TAG_LAT_REF = 1
const TAG_LAT = 2
const TAG_LON_REF = 3
const TAG_LON = 4
const TAG_DIR_REF = 16
const TAG_DIR = 17
const TYPE_ASCII = 2
const TYPE_RATIONAL = 5

/**
 * The GPS block of a JPEG. A photo with no Exif, no GPS block or a zeroed one
 * answers all-null; a block that is there but cannot be read throws.
 */
export function readExifGps(buffer: ArrayBuffer): ExifGps {
  return guardExif(() => {
    const tiff = findExifTiff(buffer)
    if (!tiff) return NONE
    const { view, base, end } = tiff
    if (base + 8 > end) throw exifDamaged()
    const order = view.getUint16(base)
    if (order !== 0x4949 && order !== 0x4d4d) throw exifDamaged()
    const le = order === 0x4949
    const u16 = (at: number) => { if (at + 2 > end) throw exifDamaged(); return view.getUint16(at, le) }
    const u32 = (at: number) => { if (at + 4 > end) throw exifDamaged(); return view.getUint32(at, le) }
    if (u16(base + 2) !== 42) throw exifDamaged()

    const entries = (ifdOffset: number): Map<number, number> => {
      const at = base + ifdOffset
      const n = u16(at)
      const out = new Map<number, number>()
      for (let i = 0; i < n; i++) out.set(u16(at + 2 + i * 12), at + 2 + i * 12)
      return out
    }
    const root = entries(u32(base + 4))
    const pointer = root.get(TAG_GPS_IFD)
    if (pointer === undefined) return NONE
    const gpsAt = u32(pointer + 8)
    if (gpsAt === 0) return NONE
    const gps = entries(gpsAt)

    const letter = (tag: number): string => {
      const entry = gps.get(tag)
      if (entry === undefined) return ''
      if (u16(entry + 2) !== TYPE_ASCII) throw exifDamaged()
      const count = u32(entry + 4)
      const from = count <= 4 ? entry + 8 : base + u32(entry + 8)
      if (from + count > end) throw exifDamaged()
      let s = ''
      for (let i = 0; i < count; i++) {
        const c = view.getUint8(from + i)
        if (c === 0) break
        s += String.fromCharCode(c)
      }
      return s.trim().toUpperCase()
    }
    // The rationals of an entry as numbers, or null when any has a zero denominator.
    const rationals = (tag: number, want: number): number[] | null => {
      const entry = gps.get(tag)
      if (entry === undefined) return null
      if (u16(entry + 2) !== TYPE_RATIONAL || u32(entry + 4) !== want) throw exifDamaged()
      const from = base + u32(entry + 8)
      if (from + want * 8 > end) throw exifDamaged()
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
    const dirRef = letter(TAG_DIR_REF)
    const dir = rationals(TAG_DIR, 1)
    if (dir !== null && (dirRef === 'T' || dirRef === 'M') && dir[0] >= 0 && dir[0] < 360) bearingDeg = dir[0]

    return { lat, lon, bearingDeg }
  })
}
