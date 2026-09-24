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
const JPEG_QUALITY = 0.85

/**
 * Downscales file to at most MAX_DIMENSION px on its long edge and
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
export async function downscaleImage(file: Blob): Promise<Blob> {
  const bitmap = await createImageBitmap(file)
  try {
    const scale = Math.min(1, MAX_DIMENSION / Math.max(bitmap.width, bitmap.height))
    const width = Math.max(1, Math.round(bitmap.width * scale))
    const height = Math.max(1, Math.round(bitmap.height * scale))

    const canvas = document.createElement('canvas')
    canvas.width = width
    canvas.height = height
    const ctx = canvas.getContext('2d')
    if (!ctx) {
      throw new Error('could not get a 2d canvas context to downscale the photo')
    }
    ctx.drawImage(bitmap, 0, 0, width, height)

    const blob = await new Promise<Blob | null>((resolve) => {
      canvas.toBlob(resolve, 'image/jpeg', JPEG_QUALITY)
    })
    if (!blob) {
      throw new Error('could not encode the downscaled photo')
    }
    return blob
  } finally {
    bitmap.close()
  }
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
