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
