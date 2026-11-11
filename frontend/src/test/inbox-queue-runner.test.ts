import { describe, expect, it, vi } from 'vitest'

import type { InboxUploadMeta, InboxUploadResult } from '@/lib/inbox-api'
import type { InboxImage } from '@/lib/inbox-image'
import { createIdbQueueStore } from '@/lib/inbox-queue'
import { runQueue, type LockLike } from '@/lib/inbox-queue-runner'
import { prepareFile, type PreparedFile } from '@/lib/inbox-upload'
import { FakeIdbFactory } from './helpers/fake-idb'
import { dms, gpsJpeg } from './helpers/exif-gps-jpeg'

const file = (name: string, body = name) => new File([body], name, { type: 'image/jpeg', lastModified: 9 })

function setup() {
  const idb = new FakeIdbFactory()
  const store = createIdbQueueStore(idb as unknown as IDBFactory)
  return { idb, store }
}

const prepared = (name: string): PreparedFile => ({
  blob: new Blob([name]),
  thumb: null,
  filename: name,
  meta: { captureMs: 1, bearingDeg: null, phoneLat: null, phoneLon: null, fingerprint: '', sharpness: 0, sourceSha256: '' },
  warnings: [],
})
const stored = { status: 'stored', item: {} as never } as const

const deps = (over: Partial<Parameters<typeof runQueue>[1]> = {}) => ({
  hash: async (f: File) => `h-${f.name}`,
  known: async () => [] as string[],
  prepare: async (f: File) => prepared(f.name),
  upload: async (): Promise<InboxUploadResult> => stored,
  ...over,
})

/** A Web Locks stand-in that grants a name to one holder at a time. */
function fakeLocks(): LockLike {
  const held = new Set<string>()
  return {
    request: async (name, options, callback) => {
      if (options.ifAvailable && held.has(name)) return callback(null)
      held.add(name)
      try {
        return await callback({ name })
      } finally {
        held.delete(name)
      }
    },
  }
}

describe('runQueue', () => {
  it('removes the files the box stored or already held, and keeps a failed one with its error', async () => {
    const { store } = setup()
    await store.add([file('stored.jpg'), file('held.jpg'), file('known.jpg'), file('bad.jpg')])
    const result = await runQueue(store, deps({
      known: async () => ['h-known.jpg'],
      upload: async (p) => {
        if (p.filename === 'bad.jpg') throw new Error('HTTP 500')
        return p.filename === 'held.jpg' ? { status: 'already_uploaded', where: 'documents' } : stored
      },
    }), fakeLocks())

    expect(result.busy).toBe(false)
    const left = await store.list()
    expect(left.map((q) => [q.name, q.error])).toEqual([['bad.jpg', 'HTTP 500']])
    if (!result.busy) expect(result.tally).toMatchObject({ uploaded: 1, skipped: 2, failed: [{ name: 'bad.jpg', error: 'HTTP 500' }] })
  })

  it('tries a file that failed before again, and clears its error once the box has it', async () => {
    const { store } = setup()
    const [a] = await store.add([file('a.jpg')])
    await store.setError(a.id, 'HTTP 500')
    await runQueue(store, deps(), fakeLocks())
    expect(await store.list()).toEqual([])
  })

  it('leaves everything queued when the box cannot be asked', async () => {
    const { store } = setup()
    await store.add([file('a.jpg'), file('b.jpg')])
    await expect(runQueue(store, deps({ known: async () => { throw new Error('offline') } }), fakeLocks())).rejects.toThrow('offline')
    expect((await store.list()).map((q) => q.name)).toEqual(['a.jpg', 'b.jpg'])
  })

  it('takes up files added while it runs, which another window may have queued', async () => {
    const { store } = setup()
    await store.add([file('a.jpg')])
    const sent: string[] = []
    await runQueue(store, deps({
      upload: async (p) => {
        sent.push(p.filename)
        if (p.filename === 'a.jpg') await store.add([file('late.jpg')])
        return stored
      },
    }), fakeLocks())
    expect(sent).toEqual(['a.jpg', 'late.jpg'])
    expect(await store.list()).toEqual([])
  })

  it('does not try a failed file twice in one run', async () => {
    const { store } = setup()
    await store.add([file('bad.jpg')])
    const upload = vi.fn(async (): Promise<InboxUploadResult> => { throw new Error('nope') })
    await runQueue(store, deps({ upload }), fakeLocks())
    expect(upload).toHaveBeenCalledTimes(1)
  })

  it('runs one upload loop at a time: a second is told another is working and sends nothing', async () => {
    const { store } = setup()
    await store.add([file('a.jpg')])
    const locks = fakeLocks()
    let release!: () => void
    const gate = new Promise<void>((r) => { release = r })
    const sent: string[] = []
    const first = runQueue(store, deps({ upload: async (p) => { sent.push(p.filename); await gate; return stored } }), locks)
    await vi.waitFor(() => expect(sent).toEqual(['a.jpg']))

    expect(await runQueue(store, deps({ upload: async (p) => { sent.push('second ' + p.filename); return stored } }), locks)).toEqual({ busy: true })
    release()
    expect((await first).busy).toBe(false)
    expect(sent).toEqual(['a.jpg'])
  })

  it('holds the same guard in one window when the browser has no Web Locks', async () => {
    const { store } = setup()
    await store.add([file('a.jpg')])
    let release!: () => void
    const gate = new Promise<void>((r) => { release = r })
    const first = runQueue(store, deps({ upload: async () => { await gate; return stored } }), undefined)
    await vi.waitFor(async () => expect(await runQueue(store, deps(), undefined)).toEqual({ busy: true }))
    release()
    expect((await first).busy).toBe(false)
    expect(await runQueue(store, deps(), undefined)).toMatchObject({ busy: false })
  })

  it('reads the bearing and position from a shared photo, the same as one picked on the page', async () => {
    const { store } = setup()
    const jpg = gpsJpeg({
      dt: '2026:10:22 11:30:15', offset: '+10:00',
      latRef: 'S', lat: dms(20, 15, 0), lonRef: 'E', lon: dms(149, 30, 0), dirRef: 'T', dir: [90, 1],
    })
    await store.add([new File([jpg as BlobPart], 'IMG_1.jpg', { type: 'image/jpeg' })])
    const image: InboxImage = { blob: new Blob(['s']), thumb: new Blob(['t']), fingerprint: '00ff00ff00ff00ff', sharpness: 1 }
    const metas: InboxUploadMeta[] = []
    await runQueue(store, deps({
      prepare: (f, sha) => prepareFile(f, sha, { boatTimeZone: 'Australia/Brisbane', process: async () => image }),
      upload: async (p) => { metas.push(p.meta); return stored },
    }), fakeLocks())
    expect(metas).toHaveLength(1)
    expect(metas[0]).toMatchObject({ bearingDeg: 90, phoneLat: -20.25, phoneLon: 149.5, captureMs: Date.UTC(2026, 9, 22, 1, 30, 15) })
  })

  it('treats the zeroed GPS block of a shared photo as no bearing, never due north', async () => {
    const { store } = setup()
    const jpg = gpsJpeg({ dt: '2026:10:22 11:30:15', offset: '+10:00', latRef: '', lat: dms(0, 0, 0), lonRef: '', lon: dms(0, 0, 0), dirRef: '', dir: [0, 1] })
    await store.add([new File([jpg as BlobPart], 'IMG_2.jpg', { type: 'image/jpeg' })])
    const image: InboxImage = { blob: new Blob(['s']), thumb: new Blob(['t']), fingerprint: '', sharpness: 1 }
    const metas: InboxUploadMeta[] = []
    await runQueue(store, deps({
      prepare: (f, sha) => prepareFile(f, sha, { process: async () => image }),
      upload: async (p) => { metas.push(p.meta); return stored },
    }), fakeLocks())
    expect(metas[0]).toMatchObject({ bearingDeg: null, phoneLat: null, phoneLon: null })
  })
})
