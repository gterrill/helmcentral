import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

import { afterEach, describe, expect, it, vi } from 'vitest'

import { createIdbQueueStore } from '@/lib/inbox-queue'
import { FakeIdbFactory } from './helpers/fake-idb'

// The worker is plain JS served from public/, so this loads the real file
// against a fake `self` (see service-worker-push.test.ts). The queue it writes
// is read back with the app's own store, which pins the schema both share.
const swSource = readFileSync(resolve(dirname(fileURLToPath(import.meta.url)), '../../public/sw.js'), 'utf-8')

type Handler = (event: unknown) => void

class FakeChannel {
  static all: FakeChannel[] = []
  posted: unknown[] = []
  closed = false
  constructor(readonly name: string) { FakeChannel.all.push(this) }
  postMessage(m: unknown) { this.posted.push(m) }
  close() { this.closed = true }
}

afterEach(() => { FakeChannel.all = []; vi.restoreAllMocks() })

function load(idb = new FakeIdbFactory()) {
  const handlers: Record<string, Handler> = {}
  const fakeSelf = {
    addEventListener: (type: string, h: Handler) => { handlers[type] = h },
    registration: { showNotification: vi.fn() },
    clients: { matchAll: vi.fn(), openWindow: vi.fn(), claim: vi.fn() },
    skipWaiting: vi.fn(),
    indexedDB: idb,
    BroadcastChannel: FakeChannel,
  }
  new Function('self', swSource)(fakeSelf)
  return { handlers, idb }
}

const ORIGIN = 'https://boat.example.ts.net'

function shareEvent(form: FormData | Error, method = 'POST', path = '/share-target') {
  let answer: Promise<Response> | undefined
  const request = {
    method,
    url: `${ORIGIN}${path}`,
    formData: async () => { if (form instanceof Error) throw form; return form },
  }
  return {
    event: { request, respondWith: (p: Promise<Response>) => { answer = p } },
    answered: () => answer,
  }
}

const formOf = (...files: File[]) => {
  const form = new FormData()
  for (const f of files) form.append('files', f)
  return form
}

describe('manifest share target', () => {
  const manifest = JSON.parse(readFileSync(resolve(dirname(fileURLToPath(import.meta.url)), '../../public/manifest.webmanifest'), 'utf-8'))

  it('posts photos and PDFs to the address and field the worker listens for', () => {
    const target = manifest.share_target
    expect(target.action).toBe('/share-target')
    expect(target.method).toBe('POST')
    expect(target.enctype).toBe('multipart/form-data')
    expect(target.params.files).toHaveLength(1)
    expect(target.params.files[0].name).toBe('files')
    expect(target.params.files[0].accept).toEqual(expect.arrayContaining(['image/*', '.heic', '.heif', 'application/pdf']))
    expect(swSource).toContain(`'${target.action}'`)
    expect(swSource).toContain(`'${target.params.files[0].name}'`)
  })
})

describe('service worker share target', () => {
  it('keeps the shared files on the waiting list and sends the phone to the upload page', async () => {
    const { handlers, idb } = load()
    const sent = shareEvent(formOf(
      new File(['AAA'], 'a.jpg', { type: 'image/jpeg', lastModified: 5 }),
      new File(['%PDF'], 'r.pdf', { type: 'application/pdf', lastModified: 6 }),
    ))
    handlers.fetch(sent.event)
    const res = await sent.answered()!

    expect(res.status).toBe(303)
    expect(res.headers.get('Location')).toBe(`${ORIGIN}/inbox/upload?shared=1`)

    const store = createIdbQueueStore(idb as unknown as IDBFactory)
    const rows = await store.list()
    expect(rows.map((r) => [r.name, r.type, r.lastModified])).toEqual([['a.jpg', 'image/jpeg', 5], ['r.pdf', 'application/pdf', 6]])
    expect(await (await store.getBlob(rows[0].id))!.text()).toBe('AAA')
  })

  it('does not touch any other request', () => {
    const { handlers } = load()
    for (const [method, path] of [['GET', '/share-target'], ['POST', '/api/inbox'], ['GET', '/'], ['POST', '/inbox/upload']]) {
      const sent = shareEvent(formOf(), method, path)
      handlers.fetch(sent.event)
      expect(sent.answered(), `${method} ${path}`).toBeUndefined()
    }
  })

  it('says so on the upload page when the files cannot be kept', async () => {
    const idb = new FakeIdbFactory()
    idb.options.failWrite = { n: 1, error: new Error('QuotaExceededError') }
    const { handlers } = load(idb)
    const sent = shareEvent(formOf(new File(['A'], 'a.jpg', { type: 'image/jpeg' })))
    handlers.fetch(sent.event)
    const res = await sent.answered()!
    expect(res.status).toBe(303)
    expect(res.headers.get('Location')).toBe(`${ORIGIN}/inbox/upload?share_error=store-failed`)
    expect(idb.rows('helmcentral-inbox-queue', 'meta')).toEqual([])
    expect(idb.rows('helmcentral-inbox-queue', 'blobs')).toEqual([])
  })

  it('says so when the share could not be read', async () => {
    const error = vi.spyOn(console, 'error').mockImplementation(() => {})
    const { handlers } = load()
    const sent = shareEvent(new Error('bad body'))
    handlers.fetch(sent.event)
    const res = await sent.answered()!
    expect(res.headers.get('Location')).toBe(`${ORIGIN}/inbox/upload?share_error=store-failed`)
    expect(error).toHaveBeenCalledWith(expect.stringContaining('share'), expect.objectContaining({ message: 'bad body' }))
  })

  it('says a share that held no files is empty, not a storage failure, and logs why', async () => {
    const error = vi.spyOn(console, 'error').mockImplementation(() => {})
    const { handlers, idb } = load()
    const sent = shareEvent(new FormData())
    handlers.fetch(sent.event)
    const res = await sent.answered()!
    expect(res.headers.get('Location')).toBe(`${ORIGIN}/inbox/upload?share_error=no-files`)
    expect(error).toHaveBeenCalledTimes(1)
    expect(idb.rows('helmcentral-inbox-queue', 'meta')).toEqual([])
  })

  it('logs the real cause when the files cannot be kept', async () => {
    const error = vi.spyOn(console, 'error').mockImplementation(() => {})
    const idb = new FakeIdbFactory()
    idb.options.failWrite = { n: 1, error: new Error('QuotaExceededError') }
    const { handlers } = load(idb)
    const sent = shareEvent(formOf(new File(['A'], 'a.jpg', { type: 'image/jpeg' })))
    handlers.fetch(sent.event)
    await sent.answered()
    expect(error).toHaveBeenCalledWith(expect.stringContaining('share'), expect.objectContaining({ message: 'QuotaExceededError' }))
  })

  it('tells the other open windows a share arrived, in the message the app listens for', async () => {
    const { handlers } = load()
    const sent = shareEvent(formOf(new File(['A'], 'a.jpg', { type: 'image/jpeg' })))
    handlers.fetch(sent.event)
    await sent.answered()
    expect(FakeChannel.all.map((c) => c.name)).toEqual(['helmcentral-inbox-queue'])
    expect(FakeChannel.all[0].posted).toEqual(['changed'])
    expect(FakeChannel.all[0].closed).toBe(true)
  })

  it('tells nobody when nothing was written', async () => {
    vi.spyOn(console, 'error').mockImplementation(() => {})
    const { handlers } = load()
    const sent = shareEvent(new FormData())
    handlers.fetch(sent.event)
    await sent.answered()
    expect(FakeChannel.all.flatMap((c) => c.posted)).toEqual([])
  })

  it('sends the phone back with store-failed when the transaction cannot even start', async () => {
    const error = vi.spyOn(console, 'error').mockImplementation(() => {})
    const idb = new FakeIdbFactory()
    idb.options.transactionError = new Error('InvalidStateError: the connection is closing')
    const { handlers } = load(idb)
    const sent = shareEvent(formOf(new File(['A'], 'a.jpg', { type: 'image/jpeg' })))
    handlers.fetch(sent.event)
    const res = await sent.answered()!
    expect(res.headers.get('Location')).toBe(`${ORIGIN}/inbox/upload?share_error=store-failed`)
    expect(error).toHaveBeenCalledWith(expect.stringContaining('share'), expect.objectContaining({ message: expect.stringContaining('closing') }))
  })

  it('gives up on a waiting list that stays blocked for about five seconds, and says store-failed', async () => {
    vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'] })
    try {
      const error = vi.spyOn(console, 'error').mockImplementation(() => {})
      const idb = new FakeIdbFactory()
      idb.options.blockOpenForever = true
      const { handlers } = load(idb)
      const sent = shareEvent(formOf(new File(['A'], 'a.jpg', { type: 'image/jpeg' })))
      handlers.fetch(sent.event)
      await vi.advanceTimersByTimeAsync(4_000)
      let settled = false
      void sent.answered()!.then(() => { settled = true })
      await vi.advanceTimersByTimeAsync(0)
      expect(settled).toBe(false)
      await vi.advanceTimersByTimeAsync(2_000)
      const res = await sent.answered()!
      expect(res.headers.get('Location')).toBe(`${ORIGIN}/inbox/upload?share_error=store-failed`)
      expect(error).toHaveBeenCalledWith(expect.stringMatching(/blocked/i))
    } finally {
      vi.useRealTimers()
    }
  })

  it('logs when another window holds up opening the waiting list, instead of stalling silently', async () => {
    const error = vi.spyOn(console, 'error').mockImplementation(() => {})
    const idb = new FakeIdbFactory()
    idb.options.blockOpen = true
    const { handlers } = load(idb)
    const sent = shareEvent(formOf(new File(['A'], 'a.jpg', { type: 'image/jpeg' })))
    handlers.fetch(sent.event)
    const res = await sent.answered()!
    expect(res.headers.get('Location')).toBe(`${ORIGIN}/inbox/upload?shared=1`)
    expect(error).toHaveBeenCalledWith(expect.stringMatching(/blocked/i))
  })
})
