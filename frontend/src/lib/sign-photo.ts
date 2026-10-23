import { apiBaseUrl } from '@/config/api'
import { apiRequest, ApiError } from '@/lib/api-query'
import { uploadDocument } from '@/lib/document-upload'
import { readExifCapture, type ExifCapture } from '@/lib/exif-time'

// Photos for signs live in one Documents folder at the root. It is looked up
// by name and made on first use; two uploads at once share one lookup so the
// folder is never made twice.

export const PASSAGE_PHOTOS_FOLDER = 'Passage photos'

/** Accepts the picture types the documents API takes. */
export const SIGN_PHOTO_TYPES = ['image/png', 'image/jpeg', 'image/gif', 'image/webp'] as const
export const isSignPhotoType = (mime: string) => (SIGN_PHOTO_TYPES as readonly string[]).includes(mime)

interface FolderList { folders?: { id: string; name: string; parent_id?: string | null }[] }

// The folder tree (folders only), matched by name at the root without regard
// to capitals: the server refuses a second folder whose name matches in lower
// case, so "Passage Photos" is the same folder.
async function findFolder(): Promise<string | null> {
  const { folders } = await apiRequest<FolderList>('/api/document-folders/tree')
  const want = PASSAGE_PHOTOS_FOLDER.toLowerCase()
  return (folders ?? []).find((f) => f.name.toLowerCase() === want && !f.parent_id)?.id ?? null
}

let pending: Promise<string> | null = null

export function ensurePassagePhotosFolder(): Promise<string> {
  if (pending) return pending
  const run = async (): Promise<string> => {
    const found = await findFolder()
    if (found) return found
    try {
      const created = await apiRequest<{ id: string }>('/api/document-folders', { method: 'POST', json: { name: PASSAGE_PHOTOS_FOLDER } })
      return created.id
    } catch (err) {
      // Someone else made it between the look and the create: use theirs.
      if (err instanceof ApiError && err.status === 409) {
        const theirs = await findFolder()
        if (theirs) return theirs
      }
      throw err
    }
  }
  pending = run().finally(() => { pending = null })
  return pending
}

/** Uploads the picture into the Passage photos folder; the document id comes back. */
export async function uploadSignPhoto(file: File): Promise<string> {
  const folder = await ensurePassagePhotosFolder()
  const { id } = await uploadDocument(file, file.name, undefined, folder)
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
