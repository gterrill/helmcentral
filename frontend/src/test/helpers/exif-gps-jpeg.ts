/** One rational number, numerator over denominator. */
type Rational = [number, number]

export interface GpsFixture {
  latRef?: string
  lat?: [Rational, Rational, Rational]
  lonRef?: string
  lon?: [Rational, Rational, Rational]
  dirRef?: string
  dir?: Rational
  bigEndian?: boolean
  /** Leaves the GPS IFD pointer out of IFD0. */
  noGpsIfd?: boolean
  /** DateTimeOriginal and OffsetTimeOriginal, written in an Exif IFD beside the GPS one. */
  dt?: string
  offset?: string
}

/** Degrees, minutes and seconds as the rationals a camera writes. */
export const dms = (d: number, m: number, s: number): [Rational, Rational, Rational] => [[d, 1], [m, 1], [Math.round(s * 100), 100]]

interface Entry { tag: number; type: number; count: number; inline?: number[]; data?: number[] }

/**
 * A JPEG whose Exif APP1 holds a GPS IFD and, when `dt` is given, a capture
 * time. A GPS tag is written when its field is given, so `{ latRef: '', lat:
 * dms(0,0,0) ... }` builds the zeroed block a phone writes when it hides the
 * location: empty reference letters, zero values.
 */
export function gpsJpeg(f: GpsFixture): Uint8Array {
  const be = f.bigEndian ?? false
  const u16 = (v: number) => (be ? [v >> 8, v & 255] : [v & 255, v >> 8])
  const u32 = (v: number) => (be ? [(v >>> 24) & 255, (v >>> 16) & 255, (v >>> 8) & 255, v & 255] : [v & 255, (v >>> 8) & 255, (v >>> 16) & 255, (v >>> 24) & 255])
  const asciiBytes = (s: string) => [...s].map((c) => c.charCodeAt(0)).concat(0)

  // An IFD laid out at absolute offset `at`: its entries, then the data they point to.
  const layout = (at: number, entries: Entry[]): number[] => {
    let dataAt = at + 2 + entries.length * 12 + 4
    const ifd: number[] = [...u16(entries.length)]
    const body: number[] = []
    for (const e of entries) {
      ifd.push(...u16(e.tag), ...u16(e.type), ...u32(e.count))
      if (e.inline) {
        ifd.push(...e.inline, ...new Array(4 - e.inline.length).fill(0))
      } else {
        ifd.push(...u32(dataAt))
        body.push(...e.data!)
        dataAt += e.data!.length
      }
    }
    ifd.push(...u32(0))
    return [...ifd, ...body]
  }
  const text = (tag: number, s: string): Entry => {
    const bytes = asciiBytes(s)
    return bytes.length <= 4 ? { tag, type: 2, count: bytes.length, inline: bytes } : { tag, type: 2, count: bytes.length, data: bytes }
  }
  const rationals = (tag: number, vals: Rational[]): Entry => ({ tag, type: 5, count: vals.length, data: vals.flatMap(([n, d]) => [...u32(n), ...u32(d)]) })

  const gpsEntries: Entry[] = []
  if (f.latRef !== undefined) gpsEntries.push(text(1, f.latRef))
  if (f.lat) gpsEntries.push(rationals(2, f.lat))
  if (f.lonRef !== undefined) gpsEntries.push(text(3, f.lonRef))
  if (f.lon) gpsEntries.push(rationals(4, f.lon))
  if (f.dirRef !== undefined) gpsEntries.push(text(16, f.dirRef))
  if (f.dir) gpsEntries.push(rationals(17, [f.dir]))
  const exifEntries: Entry[] = []
  if (f.dt !== undefined) exifEntries.push(text(0x9003, f.dt))
  if (f.offset !== undefined) exifEntries.push(text(0x9011, f.offset))

  const pointer = (tag: number, to: number): Entry => ({ tag, type: 4, count: 1, inline: u32(to) })
  const pointers = (exifAt: number, gpsAt: number): Entry[] => [
    ...(exifEntries.length ? [pointer(0x8769, exifAt)] : []),
    pointer(0x8825, f.noGpsIfd ? 0 : gpsAt),
  ]
  const ifd0Size = layout(8, pointers(0, 0)).length
  const exifAt = 8 + ifd0Size
  const exifBlock = exifEntries.length ? layout(exifAt, exifEntries) : []
  const gpsAt = exifAt + exifBlock.length
  const tiff = [...(be ? [0x4d, 0x4d] : [0x49, 0x49]), ...u16(42), ...u32(8), ...layout(8, pointers(exifAt, gpsAt)), ...exifBlock, ...layout(gpsAt, gpsEntries)]
  const payload = [0x45, 0x78, 0x69, 0x66, 0, 0, ...tiff]
  const len = payload.length + 2
  return new Uint8Array([0xff, 0xd8, 0xff, 0xe1, (len >> 8) & 255, len & 255, ...payload, 0xff, 0xda, 0, 2, 0xff, 0xd9])
}
