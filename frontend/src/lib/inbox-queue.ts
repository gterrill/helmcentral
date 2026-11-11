// The waiting list on the phone (ADR 0280): files shared to Helmcentral, or
// picked on the upload page, wait in IndexedDB until the box has answered for
// each one. A file leaves the list only when the box has stored it or already
// holds it, or the operator removes it.
//
// The service worker writes to the same database when a share arrives, with its
// own few lines in public/sw.js (it is served as it is and cannot import this).
// The names below are the one definition of the schema; the worker's copies are
// pinned to them by src/test/service-worker-share.test.ts.

export const INBOX_QUEUE_DB = 'helmcentral-inbox-queue'
export const INBOX_QUEUE_DB_VERSION = 1
export const INBOX_QUEUE_STORE = 'pending'

/** A file on the waiting list. `error` is why the last try failed, kept so the page can say. */
export interface QueuedFile {
  /** Sorts oldest first: the time added, then the place in its batch. */
  id: string
  blob: Blob
  name: string
  type: string
  lastModified: number
  addedAt: number
  error?: string
}

export interface InboxQueueStore {
  /** Adds the whole batch or none of it. */
  add(files: readonly File[]): Promise<QueuedFile[]>
  /** Oldest first. */
  list(): Promise<QueuedFile[]>
  remove(ids: readonly string[]): Promise<void>
  /** Records why a file failed, or clears the reason with null. */
  setError(id: string, error: string | null): Promise<void>
  clear(): Promise<void>
}

/** The file as the upload pipeline wants it, with the name, type and time it had. */
export function queuedToFile(q: QueuedFile): File {
  return new File([q.blob], q.name, { type: q.type, lastModified: q.lastModified })
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

function idString(now: number, index: number): string {
  return `${now.toString(36).padStart(9, '0')}-${String(index).padStart(5, '0')}-${Math.random().toString(36).slice(2, 8)}`
}

/** Keeps the list in the browser's IndexedDB. Throws at once when there is none. */
export function createIdbQueueStore(factory: IDBFactory | undefined = globalThis.indexedDB): InboxQueueStore {
  if (!factory) throw new Error('This browser cannot keep a waiting list of files (IndexedDB is not available)')
  const idb = factory

  let opened: Promise<IDBDatabase> | null = null
  const db = () => {
    opened ??= new Promise<IDBDatabase>((resolve, reject) => {
      const req = idb.open(INBOX_QUEUE_DB, INBOX_QUEUE_DB_VERSION)
      req.onupgradeneeded = () => {
        if (!req.result.objectStoreNames.contains(INBOX_QUEUE_STORE)) {
          req.result.createObjectStore(INBOX_QUEUE_STORE, { keyPath: 'id' })
        }
      }
      req.onsuccess = () => resolve(req.result)
      req.onerror = () => reject(req.error ?? new Error('Could not open the waiting list'))
    }).catch((err: unknown) => {
      opened = null
      throw err
    })
    return opened
  }
  const store = async (mode: IDBTransactionMode) => {
    const tx = (await db()).transaction(INBOX_QUEUE_STORE, mode)
    return { tx, objects: tx.objectStore(INBOX_QUEUE_STORE) }
  }

  return {
    async add(files) {
      const now = Date.now()
      const rows: QueuedFile[] = files.map((f, i) => ({
        id: idString(now, i),
        blob: f,
        name: f.name,
        type: f.type,
        lastModified: f.lastModified,
        addedAt: now,
      }))
      const { tx, objects } = await store('readwrite')
      const done = committed(tx)
      for (const row of rows) objects.add(row)
      await done
      return rows
    },
    async list() {
      const { tx, objects } = await store('readonly')
      const done = committed(tx)
      const rows = await request(objects.getAll() as IDBRequest<QueuedFile[]>)
      await done
      return rows.sort((a, b) => (a.id < b.id ? -1 : a.id > b.id ? 1 : 0))
    },
    async remove(ids) {
      const { tx, objects } = await store('readwrite')
      const done = committed(tx)
      for (const id of ids) objects.delete(id)
      await done
    },
    async setError(id, error) {
      const { tx, objects } = await store('readwrite')
      const done = committed(tx)
      const row = await request(objects.get(id) as IDBRequest<QueuedFile | undefined>)
      // A file removed meanwhile (the operator, or another window) has nothing to mark.
      if (row) {
        const next = { ...row }
        if (error === null) delete next.error
        else next.error = error
        objects.put(next)
      }
      await done
    },
    async clear() {
      const { tx, objects } = await store('readwrite')
      const done = committed(tx)
      objects.clear()
      await done
    },
  }
}
