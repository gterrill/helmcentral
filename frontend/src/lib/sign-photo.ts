import { apiBaseUrl } from '@/config/api'
import { apiRequest } from '@/lib/api-query'
import { uploadDocument } from '@/lib/document-upload'
import { readExifCapture, type ExifCapture } from '@/lib/exif-time'
import { downscaleForUpload, probeJpeg, UPLOAD_MAX_DIMENSION } from '@/lib/image-downscale'

// Photos for signs live in one Documents folder at the root. The box finds or
// makes it, so the Inbox and this editor share one rule (ADR 0283); the browser
// no longer reads the folder tree or creates folders of its own.

export const PASSAGE_PHOTOS_FOLDER = 'Passage photos'

/** The picture types a photo already in the library can have and still be a sign. */
export const SIGN_PHOTO_TYPES = ['image/png', 'image/jpeg', 'image/gif', 'image/webp'] as const
export const isSignPhotoType = (mime: string) => (SIGN_PHOTO_TYPES as readonly string[]).includes(mime)

/**
 * What the file picker offers: the types above and HEIC/HEIF, which the phone
 * shrinks to JPEG before anything is sent. Phones often give a HEIC file no
 * type, so the picker is told the extensions too.
 */
export const SIGN_UPLOAD_ACCEPT = [...SIGN_PHOTO_TYPES, 'image/heic', 'image/heif', '.heic', '.heif'].join(',')

const isHeic = (file: Pick<File, 'type' | 'name'>) => /^image\/hei[cf]$/.test(file.type) || /\.hei[cf]$/i.test(file.name)

/** Whether a picked file is a picture the editor takes, by its type or, failing that, its name. */
export const isSignUploadFile = (file: Pick<File, 'type' | 'name'>): boolean => isSignPhotoType(file.type) || isHeic(file)

/** The id of the Passage photos folder, made by the box if it does not exist. */
export async function ensurePassagePhotosFolder(): Promise<string> {
  const { id } = await apiRequest<{ id: string }>('/api/passage-photos-folder', { method: 'POST' })
  return id
}

/** A picture over this many bytes is shrunk even when its edges are within the limit. */
export const SIGN_PHOTO_MAX_BYTES = 4 * 1024 * 1024

/** The pixel size of a JPEG, PNG, GIF or WebP, read from its header; null when it cannot be told. */
export async function probePictureSize(file: Blob): Promise<{ width: number; height: number } | null> {
  const b = new Uint8Array(await file.slice(0, 64).arrayBuffer())
  const text = (from: number, to: number) => String.fromCharCode(...b.slice(from, to))
  const v = new DataView(b.buffer)
  if (b.length >= 24 && b[0] === 0x89 && text(1, 4) === 'PNG') return { width: v.getUint32(16), height: v.getUint32(20) }
  if (b.length >= 10 && text(0, 3) === 'GIF') return { width: v.getUint16(6, true), height: v.getUint16(8, true) }
  if (b.length >= 30 && text(0, 4) === 'RIFF' && text(8, 12) === 'WEBP') {
    const kind = text(12, 16)
    if (kind === 'VP8 ') return { width: v.getUint16(26, true) & 0x3fff, height: v.getUint16(28, true) & 0x3fff }
    if (kind === 'VP8L' && b[20] === 0x2f) {
      return { width: 1 + (b[21] | ((b[22] & 0x3f) << 8)), height: 1 + ((b[22] >> 6) | (b[23] << 2) | ((b[24] & 0x0f) << 10)) }
    }
    if (kind === 'VP8X') return { width: 1 + (b[24] | (b[25] << 8) | (b[26] << 16)), height: 1 + (b[27] | (b[28] << 8) | (b[29] << 16)) }
    return null
  }
  return probeJpeg(file)
}

/**
 * Whether a picture needs shrinking before it is sent: HEIC and HEIF (which
 * become JPEG), anything over SIGN_PHOTO_MAX_BYTES, anything longer than
 * UPLOAD_MAX_DIMENSION px on its long edge, and anything whose size cannot be
 * read. The rest goes as it is, so a transparent PNG or an animated GIF keeps
 * what a JPEG would lose.
 */
async function needsShrinking(file: File): Promise<boolean> {
  if (isHeic(file) || file.size > SIGN_PHOTO_MAX_BYTES) return true
  const size = await probePictureSize(file)
  return size === null || Math.max(size.width, size.height) > UPLOAD_MAX_DIMENSION
}

/**
 * Uploads a sign's photo into the Passage photos folder; the document id comes
 * back. A picture that needs shrinking (needsShrinking) is shrunk on the device
 * first (downscaleForUpload: 2560 px on the long edge, JPEG); one that cannot be
 * shrunk is an error: the original is never sent in its place. The capture time
 * is read from the original by `captureOfFile`, before this runs. `downscale` is
 * a seam for tests, as jsdom has no canvas.
 */
export async function uploadSignPhoto(file: File, downscale: (file: File) => Promise<File> = downscaleForUpload): Promise<string> {
  const ready = (await needsShrinking(file)) ? await downscale(file) : file
  const folder = await ensurePassagePhotosFolder()
  const { id } = await uploadDocument(ready, ready.name, undefined, folder)
  return id
}

/** When the photo was taken, or null when it says nothing; an Error when it is unreadable. */
export async function captureOfFile(file: Blob): Promise<ExifCapture | Error | null> {
  try {
    return readExifCapture(await file.slice(0, 256 * 1024).arrayBuffer())
  } catch (err) {
    return err instanceof Error ? err : new Error(String(err))
  }
}

/** The same for a picture already in the library: its first bytes are read.
 * A read that fails comes back as the Error, as for a file. */
export async function captureOfDocument(id: string): Promise<ExifCapture | Error | null> {
  try {
    const res = await fetch(`${apiBaseUrl}/api/documents/${encodeURIComponent(id)}/content`, { headers: { Range: 'bytes=0-262143' } })
    if (!res.ok) return new Error(`Could not read the photo, HTTP ${res.status}`)
    return await captureOfFile(await res.blob())
  } catch (err) {
    return err instanceof Error ? err : new Error(String(err))
  }
}
