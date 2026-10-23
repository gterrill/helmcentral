// The moment a photo was taken, from its EXIF block. Only the two fields the
// replay needs are read, and only from JPEG, which is what cameras and phones
// write. GPS is deliberately not read: the track says where the boat was.

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

const damaged = () => new Error('The photo\'s EXIF block is damaged')

const TAG_EXIF_IFD = 0x8769
const TAG_DATE_TIME_ORIGINAL = 0x9003
const TAG_OFFSET_TIME_ORIGINAL = 0x9011
const TYPE_ASCII = 2
const HEAD_BYTES = 256 * 1024

/**
 * DateTimeOriginal (and OffsetTimeOriginal when present) from a JPEG, or null
 * when the file is not a JPEG or carries no capture time. A block that is there
 * but cannot be read throws rather than guessing.
 */
export function readExifCapture(buffer: ArrayBuffer): ExifCapture | null {
  try {
    return parse(buffer)
  } catch (err) {
    // A read past the end of the buffer means the file stops short.
    if (err instanceof RangeError) throw damaged()
    throw err
  }
}

function parse(buffer: ArrayBuffer): ExifCapture | null {
  const view = new DataView(buffer, 0, Math.min(buffer.byteLength, HEAD_BYTES))
  if (view.byteLength < 4 || view.getUint16(0) !== 0xffd8) return null
  let pos = 2
  while (pos + 2 <= view.byteLength) {
    if (view.getUint8(pos) !== 0xff) throw damaged()
    const marker = view.getUint8(pos + 1)
    if (marker === 0xff) { pos += 1; continue }
    if (marker === 0xda || marker === 0xd9) return null // image data: no Exif before it
    if (pos + 4 > view.byteLength) throw damaged()
    const length = view.getUint16(pos + 2)
    if (length < 2) throw damaged()
    if (marker === 0xe1) {
      if (pos + 10 > view.byteLength) throw damaged()
      if (length >= 8 && view.getUint32(pos + 4) === 0x45786966 && view.getUint16(pos + 8) === 0) {
        if (pos + 2 + length > view.byteLength) throw damaged()
        return readTiff(view, pos + 10, pos + 2 + length)
      }
    }
    pos += 2 + length
  }
  return null
}

function readTiff(view: DataView, base: number, end: number): ExifCapture | null {
  if (base + 8 > end) throw damaged()
  const order = view.getUint16(base)
  if (order !== 0x4949 && order !== 0x4d4d) throw damaged()
  const le = order === 0x4949
  const u16 = (at: number) => { if (at + 2 > end) throw damaged(); return view.getUint16(at, le) }
  const u32 = (at: number) => { if (at + 4 > end) throw damaged(); return view.getUint32(at, le) }
  if (u16(base + 2) !== 42) throw damaged()

  // The entries of one IFD as tag -> where its 12 bytes start.
  const entries = (ifdOffset: number): Map<number, number> => {
    const at = base + ifdOffset
    const n = u16(at)
    const out = new Map<number, number>()
    for (let i = 0; i < n; i++) out.set(u16(at + 2 + i * 12), at + 2 + i * 12)
    return out
  }
  const ascii = (entry: number): string => {
    if (u16(entry + 2) !== TYPE_ASCII) throw damaged()
    const count = u32(entry + 4)
    const from = count <= 4 ? entry + 8 : base + u32(entry + 8)
    if (from + count > end) throw damaged()
    let s = ''
    for (let i = 0; i < count; i++) {
      const c = view.getUint8(from + i)
      if (c === 0) break
      s += String.fromCharCode(c)
    }
    return s
  }

  const root = entries(u32(base + 4))
  const pointer = root.get(TAG_EXIF_IFD)
  if (pointer === undefined) return null
  const exif = entries(u32(pointer + 8))
  const dt = exif.get(TAG_DATE_TIME_ORIGINAL)
  if (dt === undefined) return null
  const text = ascii(dt).trim()
  if (text === '' || /^[\s:]*$/.test(text)) return null
  const m = /^(\d{4}):(\d{2}):(\d{2}) (\d{2}):(\d{2}):(\d{2})$/.exec(text)
  if (!m) throw damaged()
  const [year, month, day, hour, minute, second] = m.slice(1).map(Number)
  // The date must exist: Date.UTC rolls 30 Feb over to March, so the fields
  // must come back the same.
  const back = new Date(Date.UTC(year, month - 1, day, hour, minute, second))
  if (
    month < 1 || month > 12 || day < 1 || hour > 23 || minute > 59 || second > 59 ||
    back.getUTCFullYear() !== year || back.getUTCMonth() !== month - 1 || back.getUTCDate() !== day
  ) throw damaged()

  let offsetMinutes: number | null = null
  const off = exif.get(TAG_OFFSET_TIME_ORIGINAL)
  if (off !== undefined) {
    const raw = ascii(off).trim()
    // A blank placeholder means the camera did not record a zone. Anything
    // else must be a zone, or the block is damaged: no quiet fall back to boat time.
    if (raw !== '') {
      const om = /^([+-])(\d{2}):?(\d{2})$/.exec(raw)
      if (!om || Number(om[2]) > 14 || Number(om[3]) > 59) throw damaged()
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
  const zone = boatOffsetHours === 0 ? 'UTC' : `UTC${boatOffsetHours > 0 ? '+' : ''}${boatOffsetHours}`
  return { unix: local - boatOffsetHours * 3600, zone, fromPhoto: false }
}
