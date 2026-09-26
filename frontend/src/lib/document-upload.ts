import { apiBaseUrl } from '@/config/api'
import { readErrorMessage } from '@/lib/api-error'

// The plain multipart POST every "upload a file, get a document id back"
// call site wants - uploadEquipmentPhoto (use-inventory.ts) is the same
// fetch+FormData+readErrorMessage shape, but it targets the equipment-photo
// route, which also links the upload to one equipment item. A note's
// "Take photo"/"Add from library" (note-editor-impl.tsx's ImageButton) has
// no equipment item to link to - it wants an ordinary library document, so
// it POSTs the general upload endpoint (documents_handlers.go's
// uploadDocumentHandler) directly instead.

export interface UploadedDocument {
  id: string
  duplicate: boolean
}

/**
 * POST /api/documents (multipart) - throws the server's own message on a
 * rejected upload (AGENTS.md fallback policy: no generic "upload failed"
 * swallowing a real reason). `tags` becomes the upload's comma-separated
 * `tags` field (documents_handlers.go's parseDocumentTagsField); omitted for
 * a caller that doesn't want one.
 *
 * Uploads dedupe by sha256 server-side (documents_store.go/storeUploadedFile):
 * identical bytes already on file come back as the EXISTING document, `id`
 * still points at a real, referenceable document either way - the caller
 * doesn't need to branch on `duplicate` to decide whether inserting a
 * reference to `id` is safe.
 */
export async function uploadDocument(file: Blob, filename: string, tags?: string[]): Promise<UploadedDocument> {
  const form = new FormData()
  form.append('file', file, filename)
  if (tags && tags.length > 0) form.append('tags', tags.join(','))
  const response = await fetch(`${apiBaseUrl}/api/documents`, { method: 'POST', body: form })
  if (!response.ok) throw new Error(await readErrorMessage(response))
  const data = (await response.json()) as { document: { id: string }; duplicate: boolean }
  return { id: data.document.id, duplicate: data.duplicate === true }
}
