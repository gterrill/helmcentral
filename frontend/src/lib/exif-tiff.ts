// The reading of a JPEG's Exif block that exif-time.ts and exif-gps.ts share:
// finding the TIFF structure inside the APP1 segment, and walking its
// directories with every read checked against the end of the block. A block
// that is there but cannot be read throws the damaged error, never a guess.

const HEAD_BYTES = 256 * 1024
const TYPE_ASCII = 2

/** The error for an Exif block that is there but cannot be read. */
export const damagedExif = () => new Error("The photo's EXIF block is damaged")

/** Runs an Exif read, turning a read past the end of the buffer into the damaged error. */
export function guardExif<T>(read: () => T): T {
  try {
    return read()
  } catch (err) {
    // A read past the end of the buffer means the file stops short.
    if (err instanceof RangeError) throw damagedExif()
    throw err
  }
}

/** Where the TIFF structure of a JPEG's Exif APP1 sits: `base` is its first byte, `end` the end of the block. */
export interface ExifTiff {
  view: DataView
  base: number
  end: number
}

/** The Exif TIFF block of a JPEG, or null for a file that is not a JPEG or has none. Call inside guardExif. */
export function findExifTiff(buffer: ArrayBuffer): ExifTiff | null {
  const view = new DataView(buffer, 0, Math.min(buffer.byteLength, HEAD_BYTES))
  if (view.byteLength < 4 || view.getUint16(0) !== 0xffd8) return null
  let pos = 2
  while (pos + 2 <= view.byteLength) {
    if (view.getUint8(pos) !== 0xff) throw damagedExif()
    const marker = view.getUint8(pos + 1)
    if (marker === 0xff) { pos += 1; continue }
    if (marker === 0xda || marker === 0xd9) return null // image data: no Exif before it
    if (pos + 4 > view.byteLength) throw damagedExif()
    const length = view.getUint16(pos + 2)
    if (length < 2) throw damagedExif()
    if (marker === 0xe1) {
      if (pos + 10 > view.byteLength) throw damagedExif()
      if (length >= 8 && view.getUint32(pos + 4) === 0x45786966 && view.getUint16(pos + 8) === 0) {
        if (pos + 2 + length > view.byteLength) throw damagedExif()
        return { view, base: pos + 10, end: pos + 2 + length }
      }
    }
    pos += 2 + length
  }
  return null
}

/** Bounds-checked reads of one TIFF block. An entry is the offset where its 12 bytes start. */
export interface TiffReader {
  u16: (at: number) => number
  u32: (at: number) => number
  /** The entries of the IFD at `ifdOffset` (from the start of the TIFF block) as tag -> entry. */
  entries: (ifdOffset: number) => Map<number, number>
  /** The entries of the first directory. */
  rootEntries: () => Map<number, number>
  /** An ASCII entry's text, up to its first NUL. */
  ascii: (entry: number) => string
  /** Where an entry's data starts when it is too long to sit in the entry itself. */
  dataAt: (entry: number) => number
  /** Throws the damaged error unless `at + length` is inside the block. */
  within: (at: number, length: number) => void
}

/** Checks the byte order and the magic number 42, then gives bounds-checked reads. */
export function openTiff({ view, base, end }: ExifTiff): TiffReader {
  if (base + 8 > end) throw damagedExif()
  const order = view.getUint16(base)
  if (order !== 0x4949 && order !== 0x4d4d) throw damagedExif()
  const le = order === 0x4949
  const u16 = (at: number) => { if (at + 2 > end) throw damagedExif(); return view.getUint16(at, le) }
  const u32 = (at: number) => { if (at + 4 > end) throw damagedExif(); return view.getUint32(at, le) }
  if (u16(base + 2) !== 42) throw damagedExif()

  const entries = (ifdOffset: number): Map<number, number> => {
    const at = base + ifdOffset
    const n = u16(at)
    const out = new Map<number, number>()
    for (let i = 0; i < n; i++) out.set(u16(at + 2 + i * 12), at + 2 + i * 12)
    return out
  }
  const within = (at: number, length: number) => { if (at + length > end) throw damagedExif() }
  const dataAt = (entry: number) => base + u32(entry + 8)
  const ascii = (entry: number): string => {
    if (u16(entry + 2) !== TYPE_ASCII) throw damagedExif()
    const count = u32(entry + 4)
    const from = count <= 4 ? entry + 8 : dataAt(entry)
    within(from, count)
    let s = ''
    for (let i = 0; i < count; i++) {
      const c = view.getUint8(from + i)
      if (c === 0) break
      s += String.fromCharCode(c)
    }
    return s
  }
  return { u16, u32, entries, rootEntries: () => entries(u32(base + 4)), ascii, dataAt, within }
}
