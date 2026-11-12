import { describe, expect, it } from 'vitest'

import {
  INBOX_QUEUE_DB,
  INBOX_QUEUE_DB_VERSION,
  INBOX_QUEUE_BLOB_STORE,
  INBOX_QUEUE_META_STORE,
  createIdbQueueStore,
  queuedToFile,
} from '@/lib/inbox-queue'
import { FakeIdbFactory } from './helpers/fake-idb'
import { seedableQueue } from './helpers/seed-queue'

const file = (name: string, body = name, type = 'image/jpeg', lastModified = 1_700_000_000_000) =>
  new File([body], name, { type, lastModified })

function newStore() {
  const idb = new FakeIdbFactory()
  return { idb, store: seedableQueue(idb) }
}

describe('the phone-side waiting list', () => {
  it('keeps each file with its name, type, modified time and when it was added', async () => {
    const { idb, store } = newStore()
    const added = await store.add([file('a.jpg', 'AAA', 'image/jpeg', 123)])
    expect(added).toHaveLength(1)

    const [row] = await store.list()
    expect(row).toMatchObject({ id: added[0].id, name: 'a.jpg', type: 'image/jpeg', lastModified: 123 })
    expect(row.addedMs).toBeGreaterThan(0)
    expect(row.error).toBeUndefined()
    expect(idb.opened[0]).toEqual({ name: INBOX_QUEUE_DB, version: INBOX_QUEUE_DB_VERSION })

    const back = queuedToFile(row, (await store.getBlob(row.id))!)
    expect(back).toBeInstanceOf(File)
    expect([back.name, back.type, back.lastModified]).toEqual(['a.jpg', 'image/jpeg', 123])
    expect(await back.text()).toBe('AAA')
  })

  it('lists the oldest first, in the order a batch was added', async () => {
    const { store } = newStore()
    await store.add([file('1.jpg'), file('2.jpg'), file('3.jpg')])
    await store.add([file('4.jpg')])
    expect((await store.list()).map((q) => q.name)).toEqual(['1.jpg', '2.jpg', '3.jpg', '4.jpg'])
  })

  it('removes only the files named', async () => {
    const { store } = newStore()
    const [a, b, c] = await store.add([file('a.jpg'), file('b.jpg'), file('c.jpg')])
    await store.remove([a.id, c.id])
    expect((await store.list()).map((q) => q.id)).toEqual([b.id])
  })

  it('keeps a failed file queued with its error, until a later try clears it', async () => {
    const { store } = newStore()
    const [a] = await store.add([file('a.jpg')])
    await store.setError(a.id, 'HTTP 500')
    expect((await store.list())[0].error).toBe('HTTP 500')
    await store.setError(a.id, null)
    expect((await store.list())[0].error).toBeUndefined()
  })

  it('clears everything', async () => {
    const { store } = newStore()
    await store.add([file('a.jpg'), file('b.jpg')])
    await store.clear()
    expect(await store.list()).toEqual([])
  })

  it('lists metadata only, never the file data, and counts without reading it', async () => {
    const { store } = newStore()
    await store.add([file('a.jpg', 'AAA'), file('b.jpg', 'BB')])
    const rows = await store.list()
    for (const row of rows) {
      expect(Object.values(row).some((v) => v instanceof Blob)).toBe(false)
      expect('blob' in row).toBe(false)
    }
    expect(rows.map((r) => [r.name, r.size])).toEqual([['a.jpg', 3], ['b.jpg', 2]])
    expect(await store.count()).toBe(rows.length)
    await store.remove([rows[0].id])
    expect(await store.count()).toBe(1)
  })

  it('reads one file back by id, and says nothing is there for an id that is gone', async () => {
    const { store } = newStore()
    const [a, b] = await store.add([file('a.jpg', 'AAA'), file('b.jpg', 'BB')])
    expect(await (await store.getBlob(b.id))!.text()).toBe('BB')
    await store.remove([a.id])
    expect(await store.getBlob(a.id)).toBeNull()
  })

  it('removes the metadata and the data together, and clear empties both', async () => {
    const { idb, store } = newStore()
    const [a, b] = await store.add([file('a.jpg'), file('b.jpg')])
    await store.remove([a.id])
    expect(idb.rows(INBOX_QUEUE_DB, INBOX_QUEUE_META_STORE).map((r) => r.id)).toEqual([b.id])
    expect(idb.rows(INBOX_QUEUE_DB, INBOX_QUEUE_BLOB_STORE).map((r) => r.id)).toEqual([b.id])
    await store.clear()
    expect(idb.rows(INBOX_QUEUE_DB, INBOX_QUEUE_META_STORE)).toEqual([])
    expect(idb.rows(INBOX_QUEUE_DB, INBOX_QUEUE_BLOB_STORE)).toEqual([])
  })

  it('names the problem when the browser has no IndexedDB', () => {
    expect(() => createIdbQueueStore(undefined)).toThrow(/cannot keep a waiting list/i)
  })

  it('pins the schema the service worker writes to', () => {
    expect([INBOX_QUEUE_DB, INBOX_QUEUE_DB_VERSION, INBOX_QUEUE_META_STORE, INBOX_QUEUE_BLOB_STORE]).toEqual(['helmcentral-inbox-queue', 2, 'meta', 'blobs'])
  })
})

describe('a dead connection to the waiting list', () => {
  it('reopens on the next call after the browser drops the connection', async () => {
    const { idb, store } = newStore()
    await store.add([file('a.jpg')])
    await store.count()
    expect(idb.opened).toHaveLength(1)
    idb.forceClose(INBOX_QUEUE_DB)
    expect((await store.list()).map((q) => q.name)).toEqual(['a.jpg'])
    expect(idb.opened).toHaveLength(2)
  })

  it('lets go when another tab upgrades the database, and reopens on the next call', async () => {
    const { idb, store } = newStore()
    await store.add([file('a.jpg')])
    await store.count()
    idb.versionChange(INBOX_QUEUE_DB)
    expect(idb.databases.get(INBOX_QUEUE_DB)!.closed).toBe(true)
    expect(await store.count()).toBe(1)
    expect(idb.opened).toHaveLength(2)
  })
})

