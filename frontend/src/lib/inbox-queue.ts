// The waiting list on the phone (ADR 0280): files shared to Helmcentral wait in
// IndexedDB until the box has answered for each one. A file leaves the list only
// when the box has stored it or already holds it, or the operator removes it.
// Only the service worker adds to the list, when a share arrives; the app reads
// it, marks failures and removes. Files picked on the upload page do not wait
// here: they upload directly.
//
// Two object stores keep the list. `meta` holds one small row per file (name,
// type, size, times, the last error) and is what the page reads and lists.
// `blobs` holds the file data under the same id and is read one file at a time,
// only when that file is about to be sent, so a long list never sits in memory.
//
// The service worker writes to the same database when a share arrives, with its
// own few lines in public/sw.js (it is served as it is and cannot import this).
// The names below are the one definition of the schema; the worker's copies are
// pinned to them by src/test/service-worker-share.test.ts.

export const INBOX_QUEUE_DB = 'helmcentral-inbox-queue'
export const INBOX_QUEUE_DB_VERSION = 2
export const INBOX_QUEUE_META_STORE = 'meta'
export const INBOX_QUEUE_BLOB_STORE = 'blobs'

/** A file on the waiting list, without its data. `error` is why the last try failed, kept so the page can say. */
export interface QueuedMeta {
  /** Sorts oldest first: the time added, then the place in its batch. */
  id: string
  name: string
  type: string
  size: number
  lastModified: number
  addedMs: number
  error?: string
}

export interface InboxQueueStore {
  /** Metadata only, oldest first. */
  list(): Promise<QueuedMeta[]>
  /** How many files wait, without reading any of them. */
  count(): Promise<number>
  /** One file's data, or null when it is no longer on the list. */
  getBlob(id: string): Promise<Blob | null>
  /** Takes the files off the list, data and all, in one transaction. */
  remove(ids: readonly string[]): Promise<void>
  /** Records why a file failed, or clears the reason with null. */
  setError(id: string, error: string | null): Promise<void>
  clear(): Promise<void>
}

/** The file as the upload pipeline wants it, with the name, type and time it had. */
export function queuedToFile(q: QueuedMeta, blob: Blob): File {
  return new File([blob], q.name, { type: q.type, lastModified: q.lastModified })
}

/** "7 photos", "1 file": photos when every file is one, files otherwise. */
export function waitingLabel(entries: readonly Pick<QueuedMeta, 'type'>[]): string {
  const allPhotos = entries.every((q) => q.type.startsWith('image/'))
  const noun = allPhotos ? 'photo' : 'file'
  return `${entries.length} ${entries.length === 1 ? noun : `${noun}s`}`
}

function request<T>(req: IDBRequest<T>): Promise<T> {
  return new Promise((resolve, reject) => {
    req.onsuccess = () => resolve(req.result)
    req.onerror = () => reject(req.error ?? new Error('IndexedDB request failed'))
  })
}

function committed(tx: IDBTransaction): Promise<void> {
  return new Promise((resolve, reject) => {
    tx.oncomplete = () => resolve()
    tx.onerror = () => reject(tx.error ?? new Error('IndexedDB transaction failed'))
    tx.onabort = () => reject(tx.error ?? new Error('IndexedDB transaction was aborted'))
  })
}

/** The row with its last error set, or cleared with null. */
export function withQueuedError(q: QueuedMeta, error: string | null): QueuedMeta {
  const next: QueuedMeta = { ...q }
  if (error === null) delete next.error
  else next.error = error
  return next
}

/** Keeps the list in the browser's IndexedDB. Throws at once when there is none. */
export function createIdbQueueStore(factory: IDBFactory | undefined = globalThis.indexedDB): InboxQueueStore {
  if (!factory) throw new Error('This browser cannot keep a waiting list of files (IndexedDB is not available)')
  const idb = factory

  let opened: Promise<IDBDatabase> | null = null
  const db = () => {
    if (opened) return opened
    const thisOpen: Promise<IDBDatabase> = opened = new Promise<IDBDatabase>((resolve, reject) => {
      const req = idb.open(INBOX_QUEUE_DB, INBOX_QUEUE_DB_VERSION)
      req.onupgradeneeded = () => {
        for (const name of [INBOX_QUEUE_META_STORE, INBOX_QUEUE_BLOB_STORE]) {
          if (!req.result.objectStoreNames.contains(name)) req.result.createObjectStore(name, { keyPath: 'id' })
        }
      }
      req.onsuccess = () => {
        const conn = req.result
        // Another tab upgrading the database needs this connection gone, and the
        // browser may drop it on its own; either way the next call opens afresh.
        conn.onversionchange = () => {
          conn.close()
          if (opened === thisOpen) opened = null
        }
        conn.onclose = () => {
          if (opened === thisOpen) opened = null
        }
        resolve(conn)
      }
      req.onerror = () => reject(req.error ?? new Error('Could not open the waiting list'))
    }).catch((err: unknown) => {
      if (opened === thisOpen) opened = null
      throw err
    })
    return thisOpen
  }
  const open = async (mode: IDBTransactionMode) => {
    const tx = (await db()).transaction([INBOX_QUEUE_META_STORE, INBOX_QUEUE_BLOB_STORE], mode)
    return { tx, meta: tx.objectStore(INBOX_QUEUE_META_STORE), blobs: tx.objectStore(INBOX_QUEUE_BLOB_STORE) }
  }

  return {
    async list() {
      const { tx, meta } = await open('readonly')
      const done = committed(tx)
      const rows = await request(meta.getAll() as IDBRequest<QueuedMeta[]>)
      await done
      return rows.sort((a, b) => (a.id < b.id ? -1 : a.id > b.id ? 1 : 0))
    },
    async count() {
      const { tx, meta } = await open('readonly')
      const done = committed(tx)
      const n = await request(meta.count())
      await done
      return n
    },
    async getBlob(id) {
      const { tx, blobs } = await open('readonly')
      const done = committed(tx)
      const row = await request(blobs.get(id) as IDBRequest<{ id: string; blob: Blob } | undefined>)
      await done
      return row?.blob ?? null
    },
    async remove(ids) {
      const { tx, meta, blobs } = await open('readwrite')
      const done = committed(tx)
      for (const id of ids) {
        meta.delete(id)
        blobs.delete(id)
      }
      await done
    },
    async setError(id, error) {
      const { tx, meta } = await open('readwrite')
      const done = committed(tx)
      const row = await request(meta.get(id) as IDBRequest<QueuedMeta | undefined>)
      // A file removed meanwhile (the operator, or another window) has nothing to mark.
      if (row) meta.put(withQueuedError(row, error))
      await done
    },
    async clear() {
      const { tx, meta, blobs } = await open('readwrite')
      const done = committed(tx)
      meta.clear()
      blobs.clear()
      await done
    },
  }
}
