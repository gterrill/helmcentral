import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

import { describe, expect, it, vi } from 'vitest'

import { createIdbQueueStore } from '@/lib/inbox-queue'
import { FakeIdbFactory } from './helpers/fake-idb'

// The worker is plain JS served from public/, so this loads the real file
// against a fake `self` (see service-worker-push.test.ts). The queue it writes
// is read back with the app's own store, which pins the schema both share.
const swSource = readFileSync(resolve(dirname(fileURLToPath(import.meta.url)), '../../public/sw.js'), 'utf-8')

type Handler = (event: unknown) => void

function load(idb = new FakeIdbFactory()) {
  const handlers: Record<string, Handler> = {}
  const fakeSelf = {
    addEventListener: (type: string, h: Handler) => { handlers[type] = h },
    registration: { showNotification: vi.fn() },
    clients: { matchAll: vi.fn(), openWindow: vi.fn(), claim: vi.fn() },
    skipWaiting: vi.fn(),
    indexedDB: idb,
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

    const rows = await createIdbQueueStore(idb as unknown as IDBFactory).list()
    expect(rows.map((r) => [r.name, r.type, r.lastModified])).toEqual([['a.jpg', 'image/jpeg', 5], ['r.pdf', 'application/pdf', 6]])
    expect(await rows[0].blob.text()).toBe('AAA')
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
    expect(res.headers.get('Location')).toBe(`${ORIGIN}/inbox/upload?share_error=1`)
    expect(idb.rows('helmcentral-inbox-queue', 'pending')).toEqual([])
  })

  it('says so when the share could not be read, or held no files', async () => {
    for (const form of [new Error('bad body'), new FormData()]) {
      const { handlers } = load()
      const sent = shareEvent(form)
      handlers.fetch(sent.event)
      const res = await sent.answered()!
      expect(res.headers.get('Location')).toBe(`${ORIGIN}/inbox/upload?share_error=1`)
    }
  })
})
