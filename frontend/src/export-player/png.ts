// A small PNG decoder for Terrarium elevation tiles. Elevation lives in the
// colour channels, so it must be read exactly: privacy modes in Brave, Firefox
// (resist fingerprinting) and Safari add noise to canvas readback, and one step
// in the red channel is 256 m. So the tile is never drawn to a canvas to be
// read. The chunks are parsed here, the IDAT data is inflated by the browser's
// own DecompressionStream and the row filters are undone by hand.
// Supports what Terrarium tiles are: 8-bit RGB or RGBA, not interlaced.

export interface Pixels {
  width: number
  height: number
  /** RGBA, not premultiplied. */
  data: Uint8ClampedArray
}

const SIGNATURE = [0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a]

async function inflate(zlib: Uint8Array): Promise<Uint8Array> {
  if (typeof DecompressionStream === 'undefined') throw new Error('This browser cannot unpack PNG data (no DecompressionStream)')
  const stream = new Blob([zlib as BlobPart]).stream().pipeThrough(new DecompressionStream('deflate'))
  return new Uint8Array(await new Response(stream).arrayBuffer())
}

const paeth = (a: number, b: number, c: number) => {
  const p = a + b - c
  const pa = Math.abs(p - a)
  const pb = Math.abs(p - b)
  const pc = Math.abs(p - c)
  return pa <= pb && pa <= pc ? a : pb <= pc ? b : c
}

/** Undoes the row filters in place over `raw` (each row a filter byte then
 * `stride` bytes) and returns the unfiltered rows packed without the filter
 * bytes. */
export function unfilter(raw: Uint8Array, width: number, height: number, bpp: number): Uint8Array {
  const stride = width * bpp
  if (raw.length !== height * (stride + 1)) throw new Error('PNG image data is the wrong length')
  const out = new Uint8Array(height * stride)
  for (let y = 0; y < height; y++) {
    const type = raw[y * (stride + 1)]
    const src = y * (stride + 1) + 1
    const row = y * stride
    const prev = row - stride
    for (let i = 0; i < stride; i++) {
      const a = i >= bpp ? out[row + i - bpp] : 0
      const b = y > 0 ? out[prev + i] : 0
      const c = y > 0 && i >= bpp ? out[prev + i - bpp] : 0
      const x = raw[src + i]
      let v: number
      switch (type) {
        case 0: v = x; break
        case 1: v = x + a; break
        case 2: v = x + b; break
        case 3: v = x + ((a + b) >> 1); break
        case 4: v = x + paeth(a, b, c); break
        default: throw new Error(`PNG row filter ${type} is not valid`)
      }
      out[row + i] = v & 255
    }
  }
  return out
}

export async function decodePng(bytes: Uint8Array): Promise<Pixels> {
  if (bytes.length < 8 || SIGNATURE.some((v, i) => bytes[i] !== v)) throw new Error('Not a PNG file')
  const view = new DataView(bytes.buffer, bytes.byteOffset, bytes.byteLength)
  let width = 0
  let height = 0
  let colourType = -1
  const parts: Uint8Array[] = []
  for (let p = 8; p + 8 <= bytes.length;) {
    const length = view.getUint32(p)
    const type = String.fromCharCode(bytes[p + 4], bytes[p + 5], bytes[p + 6], bytes[p + 7])
    const body = bytes.subarray(p + 8, p + 8 + length)
    if (body.length !== length) throw new Error('PNG is cut short')
    if (type === 'IHDR') {
      width = view.getUint32(p + 8)
      height = view.getUint32(p + 12)
      const depth = bytes[p + 16]
      colourType = bytes[p + 17]
      if (depth !== 8 || (colourType !== 2 && colourType !== 6)) throw new Error(`PNG with bit depth ${depth} and colour type ${colourType} is not a Terrarium tile`)
      if (bytes[p + 20] !== 0) throw new Error('Interlaced PNG is not a Terrarium tile')
    } else if (type === 'IDAT') {
      parts.push(body)
    } else if (type === 'IEND') {
      break
    }
    p += 12 + length
  }
  if (colourType < 0 || parts.length === 0) throw new Error('PNG has no image data')
  const joined = new Uint8Array(parts.reduce((n, c) => n + c.length, 0))
  let at = 0
  for (const c of parts) { joined.set(c, at); at += c.length }
  const bpp = colourType === 2 ? 3 : 4
  const rows = unfilter(await inflate(joined), width, height, bpp)
  const data = new Uint8ClampedArray(width * height * 4)
  for (let i = 0, o = 0; i < rows.length; i += bpp, o += 4) {
    data[o] = rows[i]
    data[o + 1] = rows[i + 1]
    data[o + 2] = rows[i + 2]
    data[o + 3] = bpp === 4 ? rows[i + 3] : 255
  }
  return { width, height, data }
}
