// ADR 0127: photos are downscaled on the phone before they ever leave it,
// so a bin of twenty items uploads over a boat's tailscale connection in a
// reasonable time rather than choking on full-resolution phone camera
// output. Re-encoding through a canvas also turns HEIC camera output into
// JPEG on iOS along the way - Safari decodes a HEIC <input type="file">
// capture into an ImageBitmap just fine, it just never becomes a JPEG file
// on its own, and drawing it into a canvas and re-exporting is what does
// that without asking the operator to convert anything themselves.
//
// Used by photo-strip-editor.tsx (the equipment editor's photo row) and
// bin-quick-add.tsx, both of which downscale on pick/capture, before
// anything reaches the network.

const MAX_DIMENSION = 1600
/** ADR 0156: a deck plan has fine detail (small labels on a builder's
 * drawing), so it keeps more of its resolution than a photo does. */
export const PLAN_MAX_DIMENSION = 3000
const JPEG_QUALITY = 0.85

/**
 * Downscales file to at most maxDimension px (default MAX_DIMENSION) on its long edge and
 * re-encodes it as JPEG at JPEG_QUALITY. A file already smaller than
 * MAX_DIMENSION is still re-encoded (never upscaled - the scale factor is
 * clamped to 1), which is what performs the HEIC-to-JPEG conversion above
 * for a small photo too.
 *
 * Throws rather than falling back to the original bytes on any failure
 * (AGENTS.md fallback policy) - a canvas that can't be drawn to, or an
 * encoder that returns no blob, is a real failure the caller has to show,
 * not a reason to silently upload an oversized or unconverted original.
 */
export interface DownscaleOptions {
  /** JPEG quality, 0 to 1. Defaults to the equipment photos' 0.85. */
  quality?: number
  /** Paint white first, so a picture with transparency does not come out black. */
  opaque?: boolean
  /** Decode at about the target size where the browser can, with the photo's
   * orientation applied, so a 48 MP photo is not held at full size. For a
   * photo whose Exif is about to be thrown away with the re-encode. */
  reduceDecode?: boolean
}

/** The stored size and Exif orientation of a JPEG, read from its first bytes,
 * or null for anything else. Orientations 5 to 8 swap width and height. */
export async function probeJpeg(file: Blob): Promise<{ width: number; height: number } | null> {
  const bytes = new Uint8Array(await file.slice(0, 512 * 1024).arrayBuffer())
  if (bytes.length < 4 || bytes[0] !== 0xff || bytes[1] !== 0xd8) return null
  let i = 2
  let orientation = 1
  while (i + 3 < bytes.length && bytes[i] === 0xff) {
    const marker = bytes[i + 1]
    const len = (bytes[i + 2] << 8) | bytes[i + 3]
    if (marker >= 0xc0 && marker <= 0xcf && marker !== 0xc4 && marker !== 0xc8 && marker !== 0xcc) {
      if (i + 8 >= bytes.length) return null
      const height = (bytes[i + 5] << 8) | bytes[i + 6]
      const width = (bytes[i + 7] << 8) | bytes[i + 8]
      return orientation >= 5 ? { width: height, height: width } : { width, height }
    }
    if (marker === 0xe1 && bytes[i + 4] === 0x45 && bytes[i + 5] === 0x78) orientation = exifOrientation(bytes, i + 10, i + 2 + len) ?? 1
    if (marker === 0xda) return null
    i += 2 + len
  }
  return null
}

function exifOrientation(b: Uint8Array, tiff: number, end: number): number | null {
  if (tiff + 8 > end) return null
  const le = b[tiff] === 0x49
  const u16 = (o: number) => (le ? b[o] | (b[o + 1] << 8) : (b[o] << 8) | b[o + 1])
  const u32 = (o: number) => (le ? (b[o] | (b[o + 1] << 8) | (b[o + 2] << 16) | (b[o + 3] << 24)) >>> 0 : ((b[o] << 24) | (b[o + 1] << 16) | (b[o + 2] << 8) | b[o + 3]) >>> 0)
  const ifd = tiff + u32(tiff + 4)
  if (ifd + 2 > end) return null
  const n = u16(ifd)
  for (let k = 0; k < n; k++) {
    const e = ifd + 2 + k * 12
    if (e + 12 > end) return null
    if (u16(e) === 0x0112) return u16(e + 8)
  }
  return null
}

async function decode(file: Blob, maxDimension: number, reduce: boolean): Promise<ImageBitmap> {
  if (!reduce) return createImageBitmap(file)
  const size = await probeJpeg(file)
  const longSide = size ? Math.max(size.width, size.height) : 0
  if (!size || longSide <= maxDimension * 1.5) return createImageBitmap(file, { imageOrientation: 'from-image' })
  // One side only, so the shape is kept whichever way a browser reads the
  // pair against the orientation; the canvas below does the exact fit.
  const resize = size.width >= size.height ? { resizeWidth: maxDimension } : { resizeHeight: maxDimension }
  return createImageBitmap(file, { imageOrientation: 'from-image', resizeQuality: 'high', ...resize })
}

export async function downscaleImage(file: Blob, maxDimension: number = MAX_DIMENSION, options: DownscaleOptions = {}): Promise<Blob> {
  const bitmap = await decode(file, maxDimension, options.reduceDecode === true)
  try {
    const scale = Math.min(1, maxDimension / Math.max(bitmap.width, bitmap.height))
    const width = Math.max(1, Math.round(bitmap.width * scale))
    const height = Math.max(1, Math.round(bitmap.height * scale))

    const canvas = document.createElement('canvas')
    canvas.width = width
    canvas.height = height
    const ctx = canvas.getContext('2d')
    if (!ctx) {
      throw new Error('could not get a 2d canvas context to downscale the photo')
    }
    if (options.opaque) {
      ctx.fillStyle = '#ffffff'
      ctx.fillRect(0, 0, width, height)
    }
    if (options.reduceDecode) ctx.imageSmoothingQuality = 'high'
    ctx.drawImage(bitmap, 0, 0, width, height)

    const blob = await new Promise<Blob | null>((resolve) => {
      canvas.toBlob(resolve, 'image/jpeg', options.quality ?? JPEG_QUALITY)
    })
    if (!blob) {
      throw new Error('could not encode the downscaled photo')
    }
    return blob
  } finally {
    bitmap.close()
  }
}

/** JPEG/PNG re-encoding always renames to .jpg (downscaleImage's own output
 * format) - a camera capture's filename is often meaningless anyway
 * ("image.heic", "blob"), and giving every upload a real extension that
 * matches its actual bytes is one less thing to get wrong. Shared by every
 * caller that uploads a downscaleImage/downscaleAll result: use-photo-
 * staging.ts's own add/upload path (re-exported there for its existing
 * import sites) and note-editor-impl.tsx's ImageButton (Take photo/Add from
 * library), which has no equipment-photo concept to pull in just for this
 * one pure rename. */
export function photoFilename(original: string): string {
  const base = original.replace(/\.[^.]+$/, '').trim()
  return `${base || 'photo'}.jpg`
}

/** One outcome from downscaleAll, at the same index as its input file. */
export interface DownscaleOutcome {
  file: File
  result: { ok: true; blob: Blob } | { ok: false; error: string }
}

/**
 * Downscales every file in files, at most `concurrency` running at once -
 * review finding: bin-quick-add.tsx's addFiles and equipment-editor.tsx's
 * addLocalPhotos/uploadPhotosToSavedItem each ran
 * Promise.all(files.map(downscaleImage)), decoding every picked photo into
 * memory at once. A bin's worth of full-resolution phone camera photos
 * picked in one go risks the tab running out of memory; a small worker
 * pool bounds how many are ever mid-decode at the same time.
 *
 * Results come back in the ORIGINAL file order, not completion order (each
 * call site applies them in pick order regardless of which file's canvas
 * work happened to finish first) - one outcome per file, ok+blob or
 * ok:false+error, so a failure on one file never loses the others'
 * results (AGENTS.md fallback policy: no failure is silently dropped).
 *
 * `downscale` defaults to downscaleImage above; every real caller leaves it
 * at the default - it exists as a parameter only so this function's own
 * concurrency/ordering logic can be tested against a controllable stand-in
 * rather than the real (jsdom-unavailable) canvas/createImageBitmap path.
 */
export async function downscaleAll(
  files: File[],
  concurrency = 3,
  downscale: (file: File) => Promise<Blob> = downscaleImage,
): Promise<DownscaleOutcome[]> {
  const outcomes: DownscaleOutcome[] = new Array(files.length)
  let next = 0

  const worker = async () => {
    while (next < files.length) {
      const i = next++
      const file = files[i]
      try {
        const blob = await downscale(file)
        outcomes[i] = { file, result: { ok: true, blob } }
      } catch (err) {
        outcomes[i] = { file, result: { ok: false, error: err instanceof Error ? err.message : String(err) } }
      }
    }
  }

  const workerCount = Math.max(1, Math.min(concurrency, files.length))
  await Promise.all(Array.from({ length: workerCount }, () => worker()))
  return outcomes
}
