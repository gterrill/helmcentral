import { apiBaseUrl } from '@/config/api'
import { apiRequest } from '@/lib/api-query'
import { uploadDocument } from '@/lib/document-upload'
import { readExifCapture, type ExifCapture } from '@/lib/exif-time'
import { downscaleImage, photoFilename } from '@/lib/image-downscale'
import { INBOX_MAX_DIMENSION } from '@/lib/inbox-image'

// Photos for signs live in one Documents folder at the root. The box finds or
// makes it, so the Inbox and this editor share one rule (ADR 0282); the browser
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

/** Whether a picked file is a picture the editor takes, by its type or, failing that, its name. */
export const isSignUploadFile = (file: Pick<File, 'type' | 'name'>): boolean =>
  isSignPhotoType(file.type) || /^image\/hei[cf]$/.test(file.type) || /\.hei[cf]$/i.test(file.name)

/** The id of the Passage photos folder, made by the box if it does not exist. */
export async function ensurePassagePhotosFolder(): Promise<string> {
  const { id } = await apiRequest<{ id: string }>('/api/passage-photos-folder', { method: 'POST' })
  return id
}

/**
 * Shrinks the picture on the device to the Inbox's size (2560 px on the long
 * edge, JPEG at the same quality; HEIC becomes JPEG), then uploads that copy
 * into the Passage photos folder; the document id comes back. A picture that
 * cannot be shrunk is an error: the original is never sent in its place. The
 * capture time is read from the original by `captureOfFile`, before this runs.
 * `downscale` is a seam for tests, as jsdom has no canvas.
 */
export async function uploadSignPhoto(file: File, downscale: typeof downscaleImage = downscaleImage): Promise<string> {
  const shrunk = await downscale(file, INBOX_MAX_DIMENSION, { reduceDecode: true, opaque: true })
  const name = photoFilename(file.name)
  const folder = await ensurePassagePhotosFolder()
  const { id } = await uploadDocument(new File([shrunk], name, { type: 'image/jpeg' }), name, undefined, folder)
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
