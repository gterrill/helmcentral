import { describe, expect, it } from 'vitest'

import {
  INBOX_QUEUE_DB,
  INBOX_QUEUE_DB_VERSION,
  INBOX_QUEUE_STORE,
  createIdbQueueStore,
  queuedToFile,
} from '@/lib/inbox-queue'
import { FakeIdbFactory } from './helpers/fake-idb'

const file = (name: string, body = name, type = 'image/jpeg', lastModified = 1_700_000_000_000) =>
  new File([body], name, { type, lastModified })

function newStore() {
  const idb = new FakeIdbFactory()
  return { idb, store: createIdbQueueStore(idb as unknown as IDBFactory) }
}

describe('the phone-side waiting list', () => {
  it('keeps each file with its name, type, modified time and when it was added', async () => {
    const { idb, store } = newStore()
    const added = await store.add([file('a.jpg', 'AAA', 'image/jpeg', 123)])
    expect(added).toHaveLength(1)

    const [row] = await store.list()
    expect(row).toMatchObject({ id: added[0].id, name: 'a.jpg', type: 'image/jpeg', lastModified: 123 })
    expect(row.addedAt).toBeGreaterThan(0)
    expect(row.error).toBeUndefined()
    expect(idb.opened[0]).toEqual({ name: INBOX_QUEUE_DB, version: INBOX_QUEUE_DB_VERSION })

    const back = queuedToFile(row)
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

  it('adds a whole batch or none of it, and says why when it cannot', async () => {
    const { idb, store } = newStore()
    await store.add([file('keep.jpg')])
    idb.options.failWrite = { n: 2, error: new Error('QuotaExceededError') }
    await expect(store.add([file('a.jpg'), file('b.jpg'), file('c.jpg')])).rejects.toThrow('QuotaExceededError')
    expect((await store.list()).map((q) => q.name)).toEqual(['keep.jpg'])
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

  it('names the problem when the browser has no IndexedDB', () => {
    expect(() => createIdbQueueStore(undefined)).toThrow(/cannot keep a waiting list/i)
  })

  it('pins the schema the service worker writes to', () => {
    expect([INBOX_QUEUE_DB, INBOX_QUEUE_DB_VERSION, INBOX_QUEUE_STORE]).toEqual(['helmcentral-inbox-queue', 1, 'pending'])
  })
})
