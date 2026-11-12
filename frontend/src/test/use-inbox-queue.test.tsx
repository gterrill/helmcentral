import { act, renderHook, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { resetInboxQueue, useInboxQueue } from '@/hooks/use-inbox-queue'
import { resetUploadRun, runQueue } from '@/lib/inbox-queue-runner'
import { FakeIdbFactory } from './helpers/fake-idb'
import { seedableQueue } from './helpers/seed-queue'

// One store per window: every reader shares one IndexedDB connection and one
// BroadcastChannel, holds metadata only, and reloads at most once per tick.

class FakeChannel {
  static all: FakeChannel[] = []
  onmessage: ((e: unknown) => void) | null = null
  closed = false
  posted: unknown[] = []
  constructor(readonly name: string) { FakeChannel.all.push(this) }
  postMessage(m: unknown) { this.posted.push(m) }
  close() { this.closed = true }
}

let idb: FakeIdbFactory
const file = (n: string) => new File([n], n, { type: 'image/jpeg' })
// Only the service worker adds rows; a test seeds them and tells the hook, as another window's message would.
const seed = (files: File[]) => seedableQueue(idb).add(files)
const refresh = () => act(async () => { FakeChannel.all[0].onmessage?.({ data: 'changed' }) })

beforeEach(() => {
  idb = new FakeIdbFactory()
  FakeChannel.all = []
  resetUploadRun()
  vi.stubGlobal('indexedDB', idb)
  vi.stubGlobal('BroadcastChannel', FakeChannel)
  resetInboxQueue()
})
afterEach(() => {
  resetInboxQueue()
  vi.unstubAllGlobals()
})

describe('useInboxQueue', () => {
  it('shares one connection and one channel between readers, and holds metadata only', async () => {
    const a = renderHook(() => useInboxQueue())
    const b = renderHook(() => useInboxQueue())
    await seed([file('x.jpg')])
    await refresh()
    await waitFor(() => expect(b.result.current.entries.map((e) => e.name)).toEqual(['x.jpg']))
    expect(a.result.current.entries).toBe(b.result.current.entries)
    expect(Object.values(b.result.current.entries[0]).some((v) => v instanceof Blob)).toBe(false)
    expect(idb.opened).toHaveLength(1)
    expect(FakeChannel.all).toHaveLength(1)
  })

  it('closes its channel when reset, so none is left open', () => {
    renderHook(() => useInboxQueue())
    resetInboxQueue()
    expect(FakeChannel.all.every((c) => c.closed)).toBe(true)
  })

  it('reads again when another window says it changed', async () => {
    const { result } = renderHook(() => useInboxQueue())
    await waitFor(() => expect(result.current.store).not.toBeNull())
    await seed([file('shared.jpg')])
    await act(async () => { FakeChannel.all[0].onmessage?.({ data: 'changed' }) })
    await waitFor(() => expect(result.current.entries.map((e) => e.name)).toEqual(['shared.jpg']))
  })

  it('reads the list once for a burst of changes', async () => {
    const { result } = renderHook(() => useInboxQueue())
    await waitFor(() => expect(result.current.store).not.toBeNull())
    await act(async () => { await new Promise((r) => setTimeout(r, 10)) })
    const store = result.current.store!
    const inner = vi.spyOn(store, 'list')
    await seed([file('a.jpg'), file('b.jpg'), file('c.jpg')])
    await act(async () => {
      for (let i = 0; i < 3; i++) FakeChannel.all[0].onmessage?.({ data: 'changed' })
      await new Promise((r) => setTimeout(r, 20))
    })
    expect(result.current.entries).toHaveLength(3)
    expect(inner.mock.calls.length).toBeLessThanOrEqual(2)
  })

  it('keeps the same entries object when a reload finds nothing changed', async () => {
    const { result } = renderHook(() => useInboxQueue())
    await seed([file('a.jpg')])
    await refresh()
    await waitFor(() => expect(result.current.entries).toHaveLength(1))
    const before = result.current.entries
    await act(async () => { FakeChannel.all[0].onmessage?.({ data: 'changed' }); await new Promise((r) => setTimeout(r, 20)) })
    expect(result.current.entries).toBe(before)
  })

  it('tells the other windows once at the end of a run, not once per file, and does not re-read the list per file', async () => {
    const { result } = renderHook(() => useInboxQueue())
    await seed(['a', 'b', 'c', 'd', 'e'].map((n) => file(`${n}.jpg`)))
    await refresh()
    await waitFor(() => expect(result.current.entries).toHaveLength(5))
    FakeChannel.all[0].posted.length = 0
    const store = result.current.store!
    const list = vi.spyOn(store, 'list')
    const NO_META = { captureMs: null, bearingDeg: null, phoneLat: null, phoneLon: null, fingerprint: '', sharpness: 0, sourceSha256: '' }
    await act(async () => {
      await runQueue(store, {
        hash: async (f) => `h-${f.name}`,
        known: async () => [],
        prepare: async (f) => ({ blob: new Blob([f.name]), thumb: null, filename: f.name, meta: NO_META, warnings: [] }),
        upload: async () => ({ status: 'stored', item: {} as never }),
      })
      await new Promise((r) => setTimeout(r, 20))
    })
    expect(result.current.entries).toEqual([])
    expect(FakeChannel.all[0].posted).toEqual(['changed'])
    // The run reads the list once to learn what to send; nothing reads it again per file.
    expect(list.mock.calls.length).toBeLessThanOrEqual(1)
  })

  it('drops a removed file and marks a failed one in the snapshot without reading the list', async () => {
    const { result } = renderHook(() => useInboxQueue())
    await seed([file('a.jpg'), file('b.jpg')])
    await refresh()
    await waitFor(() => expect(result.current.entries).toHaveLength(2))
    const store = result.current.store!
    const list = vi.spyOn(store, 'list')
    const [a, b] = result.current.entries
    await act(async () => { await store.setError(a.id, 'HTTP 503') })
    expect(result.current.entries[0].error).toBe('HTTP 503')
    await act(async () => { await store.remove([b.id]) })
    expect(result.current.entries.map((e) => e.name)).toEqual(['a.jpg'])
    expect(list).not.toHaveBeenCalled()
  })

  it('does not let a slow reload bring back a file removed while it was reading', async () => {
    const { result } = renderHook(() => useInboxQueue())
    await seed([file('a.jpg'), file('b.jpg')])
    await refresh()
    await waitFor(() => expect(result.current.entries).toHaveLength(2))
    const store = result.current.store!
    const stale = await store.list()
    let release!: () => void
    const gate = new Promise<void>((r) => { release = r })
    const list = vi.spyOn(store, 'list').mockImplementationOnce(async () => { await gate; return stale })
    await refresh()
    await waitFor(() => expect(list).toHaveBeenCalled())

    await act(async () => { await store.remove([stale[0].id]) })
    expect(result.current.entries.map((e) => e.name)).toEqual(['b.jpg'])
    await act(async () => { release(); await new Promise((r) => setTimeout(r, 30)) })
    expect(result.current.entries.map((e) => e.name)).toEqual(['b.jpg'])
  })

  it('reports one error, and no store, when the browser has no IndexedDB', () => {
    vi.stubGlobal('indexedDB', undefined)
    resetInboxQueue()
    const { result } = renderHook(() => useInboxQueue())
    expect(result.current.store).toBeNull()
    expect(result.current.error).toMatch(/cannot keep a waiting list/i)
  })
})
