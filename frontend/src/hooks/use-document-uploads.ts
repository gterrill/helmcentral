import { useCallback, useEffect, useRef, useState } from 'react'
import { apiBaseUrl } from '@/config/api'

// ADR 0106 F2: staged attachments for one Mate composer. Each file uploads
// immediately through POST /api/documents (multipart), then - unless it
// came back already indexed (a duplicate of something already processed) -
// is polled every 3s until the background indexer (B4) finishes with it.
// One instance of this hook belongs to one AssistantThread composer; it is
// not a shared store, unlike e.g. use-vessel-identity.ts.

/** At most this many documents may be staged on one message - mirrors the
 * backend's own cap (documentAttachmentsPerMessageCap, assistant_handlers.go)
 * on how many attachment ids POST .../messages accepts. */
const MAX_ATTACHMENTS = 10

/** Matches documentMaxUploadBytes (backend/documents_handlers.go) - checked
 * client-side before ever opening a connection, so an oversized file never
 * ties up the upload path only to be told the same thing by a 413. */
const MAX_UPLOAD_BYTES = 100 * 1024 * 1024

const POLL_INTERVAL_MS = 3000

export type DocumentUploadStatus = 'uploading' | 'pending' | 'indexed' | 'failed'

export interface StagedDocument {
  /** Local-only identity for this staged item - stable across the upload
   * and poll lifecycle, used for remove()/React keys. Not the document id
   * the backend assigns (that arrives once the upload response lands, as
   * `documentId` below). */
  key: string
  filename: string
  size: number
  status: DocumentUploadStatus
  /** 0-100, upload progress only - polling for indexing doesn't move this
   * further, since the backend reports no fractional progress for extract
   * or enrich, only pending/indexed/failed. */
  progress: number
  documentId: string | null
  error: string | null
  duplicate: boolean
}

interface DocumentApi {
  id: string
  filename: string
  status: 'pending' | 'indexed' | 'failed'
  error?: string
}

interface UploadResponseApi {
  document?: DocumentApi
  duplicate?: boolean
  error?: string
}

function statusFromDocument(doc: DocumentApi): DocumentUploadStatus {
  return doc.status === 'indexed' || doc.status === 'failed' ? doc.status : 'pending'
}

let keyCounter = 0
function nextKey(): string {
  keyCounter += 1
  return `doc-upload-${keyCounter}`
}

function parseJSON<T>(text: string): T | null {
  try {
    return JSON.parse(text) as T
  } catch {
    return null
  }
}

/**
 * `folderId` (ADR 0106 F1): the folder a fresh upload files into. Read
 * through a ref, not closed over directly by startUpload, so the Documents
 * panel can navigate to a different folder mid-upload (a slow file still in
 * flight) without that change retroactively altering which request is about
 * to fire for the NEXT file - each startUpload call reads whatever folder is
 * current at the moment it fires. Left undefined/null (the composer's own
 * call site, assistant-thread.tsx, never passes one), uploads land in the
 * root exactly as before this parameter existed - documentsRootFolderSentinel
 * is the server's own default for an absent folder_id (documents_store.go).
 */
export function useDocumentUploads(folderId?: string | null) {
  const [items, setItems] = useState<StagedDocument[]>([])
  const [error, setError] = useState<string | null>(null)
  // Keyed by the local staged key, not the document id - an item still
  // `uploading` has no document id yet, and this is exactly the set of
  // requests remove()/clear() need to be able to abort.
  const xhrsRef = useRef(new Map<string, XMLHttpRequest>())
  // read inside the poll's setInterval callback so it always sees the
  // latest staged list without re-subscribing the effect on every update.
  const itemsRef = useRef<StagedDocument[]>(items)
  itemsRef.current = items
  const folderIdRef = useRef(folderId)
  folderIdRef.current = folderId

  const updateItem = useCallback((key: string, patch: Partial<StagedDocument>) => {
    setItems((previous) => previous.map((item) => (item.key === key ? { ...item, ...patch } : item)))
  }, [])

  const startUpload = useCallback((file: File) => {
    const key = nextKey()
    setItems((previous) => [
      ...previous,
      { key, filename: file.name, size: file.size, status: 'uploading', progress: 0, documentId: null, error: null, duplicate: false },
    ])

    const xhr = new XMLHttpRequest()
    xhrsRef.current.set(key, xhr)

    xhr.upload.onprogress = (event) => {
      if (!event.lengthComputable || event.total === 0) return
      updateItem(key, { progress: Math.round((event.loaded / event.total) * 100) })
    }

    xhr.onload = () => {
      xhrsRef.current.delete(key)
      const body = parseJSON<UploadResponseApi>(xhr.responseText)

      if ((xhr.status === 200 || xhr.status === 201) && body?.document) {
        const doc = body.document

        // Uploads dedupe by sha256 server-side, so two chips can resolve to
        // the same document id (the same file attached twice, or two files
        // with identical content). Rather than filter at send time, collapse
        // it here: drop this chip and tell the operator plainly, instead of
        // leaving a second chip that would make handleSend post a duplicate
        // id the backend rejects with 400.
        const alreadyStaged = itemsRef.current.some((item) => item.key !== key && item.documentId === doc.id)
        if (alreadyStaged) {
          setError(`"${file.name}" is already attached.`)
          setItems((previous) => previous.filter((item) => item.key !== key))
          return
        }

        updateItem(key, {
          status: statusFromDocument(doc),
          progress: 100,
          documentId: doc.id,
          duplicate: body.duplicate === true,
          error: doc.status === 'failed' ? (doc.error ?? null) : null,
        })
        return
      }

      // Upload itself was refused (a validation failure, an over-limit body
      // that slipped past the client-side check below, or anything else the
      // backend rejected before a document ever existed) - fail-fast per
      // AGENTS.md: no document was created, so no chip stays staged for one.
      setError(body?.error && body.error !== '' ? body.error : `Upload failed (HTTP ${xhr.status})`)
      setItems((previous) => previous.filter((item) => item.key !== key))
    }

    xhr.onerror = () => {
      xhrsRef.current.delete(key)
      setError(`Upload of "${file.name}" failed: network error.`)
      setItems((previous) => previous.filter((item) => item.key !== key))
    }

    const formData = new FormData()
    formData.append('file', file)
    if (folderIdRef.current) formData.append('folder_id', folderIdRef.current)
    xhr.open('POST', `${apiBaseUrl}/api/documents`)
    xhr.send(formData)
  }, [updateItem])

  const add = useCallback((files: FileList | File[]) => {
    const incoming = Array.from(files)
    if (incoming.length === 0) return

    if (itemsRef.current.length + incoming.length > MAX_ATTACHMENTS) {
      setError(`At most ${MAX_ATTACHMENTS} attachments are allowed on one message.`)
      return
    }

    setError(null)
    for (const file of incoming) {
      if (file.size === 0) {
        setError(`"${file.name}" is empty.`)
        continue
      }
      if (file.size > MAX_UPLOAD_BYTES) {
        setError(`"${file.name}" is over the 100 MB upload limit.`)
        continue
      }
      startUpload(file)
    }
  }, [startUpload])

  const remove = useCallback((key: string) => {
    xhrsRef.current.get(key)?.abort()
    xhrsRef.current.delete(key)
    setItems((previous) => previous.filter((item) => item.key !== key))
  }, [])

  const clear = useCallback(() => {
    for (const xhr of xhrsRef.current.values()) xhr.abort()
    xhrsRef.current.clear()
    setItems([])
    setError(null)
  }, [])

  // Aborts anything still in flight on unmount - a composer that unmounts
  // mid-upload (the sheet closes, the operator navigates away) must not
  // leave a dangling request updating state nobody is listening to any
  // more.
  useEffect(() => () => {
    for (const xhr of xhrsRef.current.values()) xhr.abort()
    xhrsRef.current.clear()
  }, [])

  const pollOnce = useCallback(async () => {
    const pending = itemsRef.current.filter((item) => item.status === 'pending' && item.documentId !== null)
    await Promise.all(pending.map(async (item) => {
      // A poll that fails - the document was deleted, the backend
      // restarted, the network dropped - must not leave this chip `pending`
      // forever: that holds `ready` false and disables Send with no way out
      // short of removing the chip. Mark it `failed` instead, same outcome
      // as a failed extract/enrich, so the operator can send or remove it.
      try {
        const response = await fetch(`${apiBaseUrl}/api/documents/${encodeURIComponent(item.documentId as string)}`)
        if (!response.ok) {
          if (response.status === 404) {
            updateItem(item.key, { status: 'failed', error: 'This document is no longer in the library.' })
            return
          }
          const body = parseJSON<{ error?: string }>(await response.text())
          updateItem(item.key, {
            status: 'failed',
            error: body?.error && body.error !== '' ? body.error : `Poll failed (HTTP ${response.status})`,
          })
          return
        }
        const doc = (await response.json()) as DocumentApi
        updateItem(item.key, { status: statusFromDocument(doc), error: doc.status === 'failed' ? (doc.error ?? null) : null })
      } catch (err) {
        updateItem(item.key, { status: 'failed', error: err instanceof Error ? err.message : String(err) })
      }
    }))
  }, [updateItem])

  const hasPending = items.some((item) => item.status === 'pending')

  // The timer exists only while something is pending: this effect's own
  // cleanup - which React runs both when hasPending flips back to false and
  // on unmount - is what stops it, rather than a running interval that
  // simply no-ops on an empty pending list.
  useEffect(() => {
    if (!hasPending) return
    const id = setInterval(() => { void pollOnce() }, POLL_INTERVAL_MS)
    return () => clearInterval(id)
  }, [hasPending, pollOnce])

  // Ready once nothing is still uploading or being read/enriched - a failed
  // document is still attachable (it has a real document id, and Mate is
  // told it failed), so only `uploading`/`pending` hold Send back.
  const ready = items.every((item) => item.status === 'indexed' || item.status === 'failed')

  return { items, add, remove, clear, ready, error }
}
