import { damagedExif, findExifTiff, guardExif, openTiff, type TiffReader } from '@/lib/exif-tiff'

// The moment a photo was taken, from its EXIF block. Only the two fields the
// replay needs are read, and only from JPEG, which is what cameras and phones
// write. The track says where the boat was, so a sign never takes a position
// from the photo; the Inbox reads the phone's position separately (exif-gps.ts)
// only to warn when its clock or zone looks wrong.

export interface ExifCapture {
  year: number
  month: number
  day: number
  hour: number
  minute: number
  second: number
  /** The photo's own offset from UTC in minutes (OffsetTimeOriginal), or null. */
  offsetMinutes: number | null
}

const TAG_EXIF_IFD = 0x8769
const TAG_DATE_TIME_ORIGINAL = 0x9003
const TAG_OFFSET_TIME_ORIGINAL = 0x9011

/**
 * DateTimeOriginal (and OffsetTimeOriginal when present) from a JPEG, or null
 * when the file is not a JPEG or carries no capture time. A block that is there
 * but cannot be read throws rather than guessing.
 */
export function readExifCapture(buffer: ArrayBuffer): ExifCapture | null {
  return guardExif(() => {
    const tiff = findExifTiff(buffer)
    return tiff ? readTiff(openTiff(tiff)) : null
  })
}

function readTiff({ u32, rootEntries, entries, ascii }: TiffReader): ExifCapture | null {
  const pointer = rootEntries().get(TAG_EXIF_IFD)
  if (pointer === undefined) return null
  const exif = entries(u32(pointer + 8))
  const dt = exif.get(TAG_DATE_TIME_ORIGINAL)
  if (dt === undefined) return null
  const text = ascii(dt).trim()
  if (text === '' || /^[\s:]*$/.test(text)) return null
  const m = /^(\d{4}):(\d{2}):(\d{2}) (\d{2}):(\d{2}):(\d{2})$/.exec(text)
  if (!m) throw damagedExif()
  const [year, month, day, hour, minute, second] = m.slice(1).map(Number)
  // The date must exist: Date.UTC rolls 30 Feb over to March, so the fields
  // must come back the same.
  const back = new Date(Date.UTC(year, month - 1, day, hour, minute, second))
  if (
    month < 1 || month > 12 || day < 1 || hour > 23 || minute > 59 || second > 59 ||
    back.getUTCFullYear() !== year || back.getUTCMonth() !== month - 1 || back.getUTCDate() !== day
  ) throw damagedExif()

  let offsetMinutes: number | null = null
  const off = exif.get(TAG_OFFSET_TIME_ORIGINAL)
  if (off !== undefined) {
    const raw = ascii(off).trim()
    // A blank placeholder means the camera did not record a zone. Anything
    // else must be a zone, or the block is damaged: no quiet fall back to boat time.
    if (raw !== '') {
      const om = /^([+-])(\d{2}):?(\d{2})$/.exec(raw)
      if (!om || Number(om[2]) > 14 || Number(om[3]) > 59) throw damagedExif()
      offsetMinutes = (om[1] === '-' ? -1 : 1) * (Number(om[2]) * 60 + Number(om[3]))
    }
  }
  return { year, month, day, hour, minute, second, offsetMinutes }
}

/**
 * The instant a capture time stands for, in Unix seconds. The photo's own
 * offset wins; without one the time is read in the boat's zone (whole hours,
 * the zone the replay bar labels its time with).
 */
export function captureUnix(c: ExifCapture, boatOffsetHours: number): { unix: number; zone: string; fromPhoto: boolean } {
  const local = Date.UTC(c.year, c.month - 1, c.day, c.hour, c.minute, c.second) / 1000
  if (c.offsetMinutes !== null) {
    const abs = Math.abs(c.offsetMinutes)
    const zone = `UTC${c.offsetMinutes < 0 ? '-' : '+'}${String(Math.floor(abs / 60)).padStart(2, '0')}:${String(abs % 60).padStart(2, '0')}`
    return { unix: local - c.offsetMinutes * 60, zone, fromPhoto: true }
  }
  const boatMinutes = Math.round(Math.abs(boatOffsetHours) * 60)
  const zone = boatOffsetHours === 0
    ? 'UTC'
    : `UTC${boatOffsetHours > 0 ? '+' : '-'}${Math.floor(boatMinutes / 60)}${boatMinutes % 60 ? `:${String(boatMinutes % 60).padStart(2, '0')}` : ''}`
  return { unix: local - boatOffsetHours * 3600, zone, fromPhoto: false }
}
