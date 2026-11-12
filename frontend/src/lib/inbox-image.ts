import { decodeImage, encodeBitmap, UPLOAD_MAX_DIMENSION } from '@/lib/image-downscale'
import { fingerprintBitmap } from '@/lib/image-fingerprint'

// What the phone makes of a photo before it is sent: the shrunk copy, a small
// preview for the contact sheet and the fingerprint, all from one decode. A
// 12 MP photo is held in memory once, not once per thing made from it.

/** The preview the contact sheet and the assign dialog show. */
export const INBOX_THUMB_DIMENSION = 400
const THUMB_QUALITY = 0.7

export interface InboxImage {
  /** The photo shrunk to UPLOAD_MAX_DIMENSION and re-encoded as JPEG. */
  blob: Blob
  thumb: Blob
  /** 16 hex characters. */
  fingerprint: string
  sharpness: number
}

/**
 * Throws when the browser cannot decode or draw the photo, so that file is
 * reported rather than sent half-prepared.
 */
export async function processInboxImage(file: Blob): Promise<InboxImage> {
  const bitmap = await decodeImage(file, UPLOAD_MAX_DIMENSION, true)
  try {
    const blob = await encodeBitmap(bitmap, UPLOAD_MAX_DIMENSION, { reduceDecode: true })
    const thumb = await encodeBitmap(bitmap, INBOX_THUMB_DIMENSION, { reduceDecode: true, quality: THUMB_QUALITY })
    return { blob, thumb, ...fingerprintBitmap(bitmap) }
  } finally {
    bitmap.close()
  }
}
