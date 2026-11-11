// What the Inbox needs to know about a picture to help cull it, worked out on
// the phone while the photo is downscaled so the box does no image work: a
// perceptual hash that near-identical frames share (a burst, a retake) and a
// sharpness score that a blurred frame scores low on. Both only ever suggest.

/** The long edge the grey copy is drawn at: enough detail to judge focus, small to compute. */
export const FINGERPRINT_LONG_EDGE = 256

/** Luma (Rec. 601) of each pixel of an RGBA buffer. */
export function greyFromRgba(rgba: Uint8ClampedArray | Uint8Array): Uint8Array {
  const out = new Uint8Array(rgba.length / 4)
  for (let i = 0; i < out.length; i++) {
    out[i] = Math.round(0.299 * rgba[i * 4] + 0.587 * rgba[i * 4 + 1] + 0.114 * rgba[i * 4 + 2])
  }
  return out
}

/** A grey picture resized by averaging the source pixels each target pixel covers. */
export function resizeGrey(src: Uint8Array, w: number, h: number, tw: number, th: number): Uint8Array {
  const out = new Uint8Array(tw * th)
  for (let ty = 0; ty < th; ty++) {
    const y0 = Math.floor((ty * h) / th)
    const y1 = Math.max(y0 + 1, Math.floor(((ty + 1) * h) / th))
    for (let tx = 0; tx < tw; tx++) {
      const x0 = Math.floor((tx * w) / tw)
      const x1 = Math.max(x0 + 1, Math.floor(((tx + 1) * w) / tw))
      let sum = 0
      let n = 0
      for (let y = y0; y < y1; y++) {
        for (let x = x0; x < x1; x++) {
          sum += src[y * w + x]
          n++
        }
      }
      out[ty * tw + tx] = Math.round(sum / n)
    }
  }
  return out
}

/** Difference hash: 64 bits, each saying whether a pixel is brighter than its right neighbour on a 9x8 grey copy. */
export function dHash(grey: Uint8Array, w: number, h: number): string {
  const small = resizeGrey(grey, w, h, 9, 8)
  let hex = ''
  for (let y = 0; y < 8; y++) {
    let nibble = 0
    for (let x = 0; x < 8; x++) {
      nibble = (nibble << 1) | (small[y * 9 + x] > small[y * 9 + x + 1] ? 1 : 0)
      if (x % 4 === 3) {
        hex += nibble.toString(16)
        nibble = 0
      }
    }
  }
  return hex
}

const BITS = [0, 1, 1, 2, 1, 2, 2, 3, 1, 2, 2, 3, 2, 3, 3, 4]

/** The number of bits that differ between two hex fingerprints of the same length. */
export function hammingHex(a: string, b: string): number {
  if (a.length !== b.length) throw new Error(`fingerprints differ in length (${a.length} and ${b.length})`)
  let bits = 0
  for (let i = 0; i < a.length; i++) bits += BITS[parseInt(a[i], 16) ^ parseInt(b[i], 16)]
  return bits
}

/** Variance of the Laplacian over the interior: high for crisp edges, low for a soft or blurred picture. */
export function laplacianVariance(grey: Uint8Array, w: number, h: number): number {
  if (w < 3 || h < 3) throw new Error('the picture is too small to score')
  let sum = 0
  let sumSq = 0
  let n = 0
  for (let y = 1; y < h - 1; y++) {
    for (let x = 1; x < w - 1; x++) {
      const i = y * w + x
      const v = grey[i - w] + grey[i + w] + grey[i - 1] + grey[i + 1] - 4 * grey[i]
      sum += v
      sumSq += v * v
      n++
    }
  }
  const mean = sum / n
  return sumSq / n - mean * mean
}

export interface ImageFingerprint {
  /** 16 hex characters. */
  fingerprint: string
  sharpness: number
}

/**
 * Fingerprint and sharpness of a picture, from a grey copy drawn at
 * FINGERPRINT_LONG_EDGE. Throws when the browser cannot decode or draw it,
 * rather than uploading the photo with no score.
 */
export async function fingerprintImage(file: Blob): Promise<ImageFingerprint> {
  const bitmap = await createImageBitmap(file, { imageOrientation: 'from-image' })
  try {
    const scale = Math.min(1, FINGERPRINT_LONG_EDGE / Math.max(bitmap.width, bitmap.height))
    const w = Math.max(3, Math.round(bitmap.width * scale))
    const h = Math.max(3, Math.round(bitmap.height * scale))
    const canvas = document.createElement('canvas')
    canvas.width = w
    canvas.height = h
    const ctx = canvas.getContext('2d', { willReadFrequently: true })
    if (!ctx) throw new Error('could not get a 2d canvas context to fingerprint the photo')
    ctx.drawImage(bitmap, 0, 0, w, h)
    const grey = greyFromRgba(ctx.getImageData(0, 0, w, h).data)
    return { fingerprint: dHash(grey, w, h), sharpness: laplacianVariance(grey, w, h) }
  } finally {
    bitmap.close()
  }
}
