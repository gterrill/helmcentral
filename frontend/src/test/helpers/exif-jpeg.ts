/** A JPEG with just an Exif APP1 holding DateTimeOriginal and, optionally, OffsetTimeOriginal. */
export function exifJpeg(opts: { dt?: string; offset?: string; bigEndian?: boolean; skipExifPointer?: boolean; truncate?: boolean }): Uint8Array {
  const be = opts.bigEndian ?? false
  const bytes: number[] = []
  const u16 = (v: number) => (be ? [v >> 8, v & 255] : [v & 255, v >> 8])
  const u32 = (v: number) => (be ? [(v >>> 24) & 255, (v >>> 16) & 255, (v >>> 8) & 255, v & 255] : [v & 255, (v >>> 8) & 255, (v >>> 16) & 255, (v >>> 24) & 255])
  const ascii = (s: string) => [...s].map((c) => c.charCodeAt(0)).concat(0)
  // TIFF: header(8) + IFD0 (1 entry: ExifIFD pointer) + ExifIFD + strings.
  const tiff: number[] = [...(be ? [0x4d, 0x4d] : [0x49, 0x49]), ...u16(42), ...u32(8)]
  const exifIfdAt = 8 + 2 + 12 + 4
  tiff.push(...u16(1), ...u16(0x8769), ...u16(4), ...u32(1), ...u32(opts.skipExifPointer ? 0 : exifIfdAt), ...u32(0))
  const entries: { tag: number; data: number[] }[] = []
  if (opts.dt !== undefined) entries.push({ tag: 0x9003, data: ascii(opts.dt) })
  if (opts.offset !== undefined) entries.push({ tag: 0x9011, data: ascii(opts.offset) })
  let strAt = exifIfdAt + 2 + entries.length * 12 + 4
  const body: number[] = []
  const ifd: number[] = [...u16(entries.length)]
  for (const e of entries) {
    ifd.push(...u16(e.tag), ...u16(2), ...u32(e.data.length), ...u32(strAt))
    body.push(...e.data)
    strAt += e.data.length
  }
  ifd.push(...u32(0))
  tiff.push(...ifd, ...body)
  const payload = [...ascii('Exif').slice(0, 4), 0, 0, ...tiff]
  bytes.push(0xff, 0xd8, 0xff, 0xe1, ((payload.length + 2) >> 8) & 255, (payload.length + 2) & 255, ...payload, 0xff, 0xda, 0, 2, 0xff, 0xd9)
  const out = new Uint8Array(bytes)
  return opts.truncate ? out.slice(0, 40) : out
}
