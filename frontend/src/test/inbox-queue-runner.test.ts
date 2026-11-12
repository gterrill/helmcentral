import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import type { InboxUploadMeta, InboxUploadResult } from '@/lib/inbox-api'
import type { InboxImage } from '@/lib/inbox-image'
import { getUploadRun, resetUploadRun, runPicked, runQueue } from '@/lib/inbox-queue-runner'
import { prepareFile, type PreparedFile } from '@/lib/inbox-upload'
import { FakeIdbFactory } from './helpers/fake-idb'
import { seedableQueue } from './helpers/seed-queue'
import { dms, gpsJpeg } from './helpers/exif-gps-jpeg'

const file = (name: string, body = name) => new File([body], name, { type: 'image/jpeg', lastModified: 9 })

function setup() {
  const idb = new FakeIdbFactory()
  const store = seedableQueue(idb)
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

/** Web Locks as the browser has them on a secure address: a name has one holder at a time. */
function stubLocks() {
  const held = new Set<string>()
  const request = vi.fn(async (name: string, options: { ifAvailable: boolean }, callback: (lock: { name: string } | null) => Promise<unknown>) => {
    if (options.ifAvailable && held.has(name)) return callback(null)
    held.add(name)
    try {
      return await callback({ name })
    } finally {
      held.delete(name)
    }
  })
  vi.stubGlobal('navigator', { locks: { request } })
  return request
}

afterEach(() => { vi.unstubAllGlobals(); resetUploadRun() })

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
    }))

    expect(result.busy).toBe(false)
    const left = await store.list()
    expect(left.map((q) => [q.name, q.error])).toEqual([['bad.jpg', 'HTTP 500']])
    if (!result.busy) expect(result.tally).toMatchObject({ uploaded: 1, skipped: 2, failed: [{ name: 'bad.jpg', error: 'HTTP 500' }] })
  })

  it('tries a file that failed before again, and clears its error once the box has it', async () => {
    const { store } = setup()
    const [a] = await store.add([file('a.jpg')])
    await store.setError(a.id, 'HTTP 500')
    await runQueue(store, deps())
    expect(await store.list()).toEqual([])
  })

  it('leaves everything queued when the box cannot be asked', async () => {
    const { store } = setup()
    await store.add([file('a.jpg'), file('b.jpg')])
    await expect(runQueue(store, deps({ known: async () => { throw new Error('offline') } }))).rejects.toThrow('offline')
    expect((await store.list()).map((q) => q.name)).toEqual(['a.jpg', 'b.jpg'])
  })

  it('leaves a file added while it runs on the list for the next run', async () => {
    const { store } = setup()
    await store.add([file('a.jpg')])
    const sent: string[] = []
    await runQueue(store, deps({
      upload: async (p) => {
        sent.push(p.filename)
        if (p.filename === 'a.jpg') await store.add([file('late.jpg')])
        return stored
      },
    }))
    expect(sent).toEqual(['a.jpg'])
    expect((await store.list()).map((q) => q.name)).toEqual(['late.jpg'])
  })

  it('takes the ids once and reads one file at a time, each just before it is used', async () => {
    const { store } = setup()
    await store.add([file('a.jpg'), file('b.jpg'), file('c.jpg')])
    const names = new Map((await store.list()).map((q) => [q.id, q.name]))
    const events: string[] = []
    const realGetBlob = store.getBlob.bind(store)
    const realList = store.list.bind(store)
    let lists = 0
    store.getBlob = async (id) => { events.push(`read ${names.get(id)}`); return realGetBlob(id) }
    store.list = async () => { lists += 1; return realList() }
    await runQueue(store, deps({
      hash: async (f) => { events.push(`hash ${f.name}`); return `h-${f.name}` },
      prepare: async (f) => { events.push(`prepare ${f.name}`); return prepared(f.name) },
      upload: async (p) => { events.push(`upload ${p.filename}`); return stored },
    }))
    expect(lists).toBe(1)
    // No read is followed by another read: a file is used, then let go, before the next is fetched.
    expect(events).toEqual([
      'read a.jpg', 'hash a.jpg', 'read b.jpg', 'hash b.jpg', 'read c.jpg', 'hash c.jpg',
      'read a.jpg', 'prepare a.jpg', 'upload a.jpg',
      'read b.jpg', 'prepare b.jpg', 'upload b.jpg',
      'read c.jpg', 'prepare c.jpg', 'upload c.jpg',
    ])
  })

  it('does not try a failed file twice in one run', async () => {
    const { store } = setup()
    await store.add([file('bad.jpg')])
    const upload = vi.fn(async (): Promise<InboxUploadResult> => { throw new Error('nope') })
    await runQueue(store, deps({ upload }))
    expect(upload).toHaveBeenCalledTimes(1)
  })

  it('runs one upload at a time where Web Locks exist: another window holding the lock means nothing is sent', async () => {
    const { store } = setup()
    await store.add([file('a.jpg')])
    const request = stubLocks()
    // Another window of the app: its run holds the lock under the same name.
    let release!: () => void
    const gate = new Promise<void>((r) => { release = r })
    const otherWindow = request('helmcentral-inbox-upload', { ifAvailable: true }, async () => { await gate })
    const sent: string[] = []

    expect(await runQueue(store, deps({ upload: async (p) => { sent.push(p.filename); return stored } }))).toEqual({ busy: true })
    expect(getUploadRun()).toMatchObject({ running: false, busyElsewhere: true })
    expect(sent).toEqual([])
    release()
    await otherWindow

    expect((await runQueue(store, deps({ upload: async (p) => { sent.push(p.filename); return stored } }))).busy).toBe(false)
    expect(sent).toEqual(['a.jpg'])
    expect(request).toHaveBeenCalledWith('helmcentral-inbox-upload', { ifAvailable: true }, expect.any(Function))
  })

  it('runs without a cross-run guard where the browser has no Web Locks (a plain-http address)', async () => {
    const { store } = setup()
    await store.add([file('a.jpg')])
    vi.stubGlobal('navigator', {})
    const result = await runQueue(store, deps())
    expect(result).toMatchObject({ busy: false })
    expect(await store.list()).toEqual([])
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
    }))
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
    }))
    expect(metas[0]).toMatchObject({ bearingDeg: null, phoneLat: null, phoneLon: null })
  })
})

describe('the run the window is making', () => {
  beforeEach(() => resetUploadRun())

  it('makes a second start in the same window a no-op, even where there are no Web Locks', async () => {
    const { store } = setup()
    await store.add([file('a.jpg')])
    vi.stubGlobal('navigator', {})
    let release!: () => void
    const gate = new Promise<void>((r) => { release = r })
    const sent: string[] = []
    const first = runQueue(store, deps({ upload: async (p) => { sent.push(p.filename); await gate; return stored } }))
    await vi.waitFor(() => expect(sent).toEqual(['a.jpg']))
    expect(getUploadRun().running).toBe(true)

    const second = await runQueue(store, deps({ upload: async (p) => { sent.push('second ' + p.filename); return stored } }))
    expect(second).toEqual({ busy: true })
    expect(getUploadRun().busyElsewhere).toBe(false)
    release()
    await first
    expect(sent).toEqual(['a.jpg'])
    expect(getUploadRun()).toMatchObject({ running: false, error: null, tally: { uploaded: 1 } })
  })

  it('keeps the progress where a page that comes back can read it', async () => {
    const { store } = setup()
    await store.add([file('a.jpg'), file('b.jpg')])
    let release!: () => void
    const gate = new Promise<void>((r) => { release = r })
    const sent: string[] = []
    const run = runQueue(store, deps({ upload: async (p) => { sent.push(p.filename); await gate; return stored } }))
    await vi.waitFor(() => expect(sent).toEqual(['a.jpg']))
    expect(getUploadRun()).toMatchObject({ running: true, tally: { total: 2 } })
    release()
    await run
  })

  it('does not show the last run\'s numbers when a new run starts', async () => {
    const { store } = setup()
    await store.add([file('a.jpg')])
    await runQueue(store, deps())
    expect(getUploadRun().tally).toMatchObject({ uploaded: 1 })

    await store.add([file('b.jpg')])
    const next = runQueue(store, deps())
    expect(getUploadRun()).toMatchObject({ running: true, tally: null })
    await next
  })

  it('skips a file taken off the list before it was read: not a failure, not in the total', async () => {
    const { store } = setup()
    await store.add([file('a.jpg'), file('gone.jpg'), file('c.jpg')])
    const realGetBlob = store.getBlob.bind(store)
    const names = new Map((await store.list()).map((q) => [q.id, q.name]))
    store.getBlob = async (id) => (names.get(id) === 'gone.jpg' ? null : realGetBlob(id))
    const sent: string[] = []
    const result = await runQueue(store, deps({ upload: async (p) => { sent.push(p.filename); return stored } }))

    expect(sent).toEqual(['a.jpg', 'c.jpg'])
    if (result.busy) throw new Error('busy')
    expect(result.tally).toMatchObject({ total: 2, uploaded: 2, failed: [] })
    expect(getUploadRun().error).toBeNull()
  })

  it('says how many went before an error and that the rest are still waiting', async () => {
    const { store } = setup()
    await store.add([file('a.jpg'), file('b.jpg'), file('c.jpg')])
    const realRemove = store.remove.bind(store)
    let removes = 0
    store.remove = async (ids) => {
      removes += 1
      if (removes === 2) throw new Error('disk gone')
      return realRemove(ids)
    }
    await expect(runQueue(store, deps())).rejects.toThrow('disk gone')
    expect(getUploadRun().running).toBe(false)
    expect(getUploadRun().error).toMatch(/^2 of 3 were stored or already held before this stopped: disk gone\. .*still waiting/)
    expect((await store.list()).map((q) => q.name)).toEqual(['b.jpg', 'c.jpg'])
  })

  it('says nothing was sent when the box could not be asked', async () => {
    const { store } = setup()
    await store.add([file('a.jpg')])
    await expect(runQueue(store, deps({ known: async () => { throw new Error('offline') } }))).rejects.toThrow('offline')
    expect(getUploadRun().error).toBe('Nothing was sent: offline. The files are still on the waiting list.')
  })

  it('sends picked files straight away, with no list', async () => {
    const sent: string[] = []
    const result = await runPicked([file('a.jpg'), file('b.jpg')], deps({ upload: async (p) => { sent.push(p.filename); return stored } }))
    expect(sent).toEqual(['a.jpg', 'b.jpg'])
    expect(result).toMatchObject({ busy: false, tally: { uploaded: 2 } })
    expect(getUploadRun().tally).toMatchObject({ uploaded: 2 })
  })

  it('says nothing was sent when picked files cannot go because the box could not be asked', async () => {
    await expect(runPicked([file('a.jpg')], deps({ known: async () => { throw new Error('offline') } }))).rejects.toThrow('offline')
    expect(getUploadRun().error).toBe('Nothing was sent: offline.')
  })
})
