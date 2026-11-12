import {
  createIdbQueueStore,
  INBOX_QUEUE_BLOB_STORE,
  INBOX_QUEUE_DB,
  INBOX_QUEUE_DB_VERSION,
  INBOX_QUEUE_META_STORE,
  type InboxQueueStore,
  type QueuedMeta,
} from '@/lib/inbox-queue'
import type { FakeIdbFactory } from './fake-idb'

// The app only reads and edits the waiting list; the service worker is what adds
// rows (public/sw.js). Tests seed rows the way the worker leaves them: straight
// into the two object stores, without a connection of the app's own.

export interface SeedableQueueStore extends InboxQueueStore {
  /** Puts a batch on the list, as the service worker does when a share arrives. */
  add(files: readonly File[]): Promise<QueuedMeta[]>
}

let seq = 0

export function seedableQueue(idb: FakeIdbFactory): SeedableQueueStore {
  const store = createIdbQueueStore(idb as unknown as IDBFactory)
  const stores = [INBOX_QUEUE_META_STORE, INBOX_QUEUE_BLOB_STORE]

  async function add(files: readonly File[]): Promise<QueuedMeta[]> {
    const now = Date.now()
    const metas = files.map((f): QueuedMeta => ({
      // A running count keeps ids in the order they were added, even within one millisecond.
      id: `${now.toString(36).padStart(9, '0')}-${String(seq++).padStart(8, '0')}`,
      name: f.name,
      type: f.type,
      size: f.size,
      lastModified: f.lastModified,
      addedMs: now,
    }))
    idb.seed(INBOX_QUEUE_DB, INBOX_QUEUE_DB_VERSION, stores, INBOX_QUEUE_META_STORE, metas as unknown as Record<string, unknown>[])
    idb.seed(INBOX_QUEUE_DB, INBOX_QUEUE_DB_VERSION, stores, INBOX_QUEUE_BLOB_STORE, metas.map((m, i) => ({ id: m.id, blob: files[i] })))
    return metas
  }

  return { ...store, add }
}
