import { describe, it, expect, beforeEach, vi } from 'vitest'

// Fresh module per test (vi.resetModules pattern - same as
// use-vessel-identity.test.ts) so the module-level `entries`/`listeners`
// state never leaks between assertions.
async function loadStoreModule() {
  vi.resetModules()
  return import('@/lib/mate-watch-store')
}

describe('mate-watch-store', () => {
  beforeEach(() => {
    vi.resetModules()
  })

  it('starts empty', async () => {
    const { getMateWatchSnapshot } = await loadStoreModule()
    expect(getMateWatchSnapshot()).toEqual([])
  })

  it('registers a conversation with its startedAt', async () => {
    const { registerMateWatch, getMateWatchSnapshot } = await loadStoreModule()

    registerMateWatch('c1', 1000)

    expect(getMateWatchSnapshot()).toEqual([{ conversationId: 'c1', startedAt: 1000 }])
  })

  it('defaults startedAt to Date.now() when omitted', async () => {
    const { registerMateWatch, getMateWatchSnapshot } = await loadStoreModule()
    const before = Date.now()

    registerMateWatch('c1')

    const [entry] = getMateWatchSnapshot()
    expect(entry.conversationId).toBe('c1')
    expect(entry.startedAt).toBeGreaterThanOrEqual(before)
    expect(entry.startedAt).toBeLessThanOrEqual(Date.now())
  })

  it('registering the same conversation again replaces its startedAt rather than duplicating the entry', async () => {
    const { registerMateWatch, getMateWatchSnapshot } = await loadStoreModule()

    registerMateWatch('c1', 1000)
    registerMateWatch('c1', 2000)

    expect(getMateWatchSnapshot()).toEqual([{ conversationId: 'c1', startedAt: 2000 }])
  })

  it('tracks more than one watched conversation independently', async () => {
    const { registerMateWatch, getMateWatchSnapshot } = await loadStoreModule()

    registerMateWatch('c1', 1000)
    registerMateWatch('c2', 1500)

    expect(getMateWatchSnapshot()).toEqual([
      { conversationId: 'c1', startedAt: 1000 },
      { conversationId: 'c2', startedAt: 1500 },
    ])
  })

  it('removes a watched conversation', async () => {
    const { registerMateWatch, removeMateWatch, getMateWatchSnapshot } = await loadStoreModule()

    registerMateWatch('c1', 1000)
    registerMateWatch('c2', 1500)
    removeMateWatch('c1')

    expect(getMateWatchSnapshot()).toEqual([{ conversationId: 'c2', startedAt: 1500 }])
  })

  it('removing an unwatched conversation is a no-op', async () => {
    const { registerMateWatch, removeMateWatch, getMateWatchSnapshot } = await loadStoreModule()

    registerMateWatch('c1', 1000)
    removeMateWatch('does-not-exist')

    expect(getMateWatchSnapshot()).toEqual([{ conversationId: 'c1', startedAt: 1000 }])
  })

  it('notifies subscribers on register', async () => {
    const { registerMateWatch, subscribeMateWatch } = await loadStoreModule()
    let calls = 0
    subscribeMateWatch(() => { calls += 1 })

    registerMateWatch('c1', 1000)

    expect(calls).toBe(1)
  })

  it('notifies subscribers on remove', async () => {
    const { registerMateWatch, removeMateWatch, subscribeMateWatch } = await loadStoreModule()
    registerMateWatch('c1', 1000)
    let calls = 0
    subscribeMateWatch(() => { calls += 1 })

    removeMateWatch('c1')

    expect(calls).toBe(1)
  })

  it('does not notify subscribers when removing an unwatched conversation', async () => {
    const { removeMateWatch, subscribeMateWatch } = await loadStoreModule()
    let calls = 0
    subscribeMateWatch(() => { calls += 1 })

    removeMateWatch('does-not-exist')

    expect(calls).toBe(0)
  })

  it('stops notifying once unsubscribed', async () => {
    const { registerMateWatch, subscribeMateWatch } = await loadStoreModule()
    let calls = 0
    const unsubscribe = subscribeMateWatch(() => { calls += 1 })

    unsubscribe()
    registerMateWatch('c1', 1000)

    expect(calls).toBe(0)
  })

  it('getMateWatchSnapshot returns the same reference when nothing has changed (useSyncExternalStore requirement)', async () => {
    const { registerMateWatch, getMateWatchSnapshot } = await loadStoreModule()
    registerMateWatch('c1', 1000)

    const first = getMateWatchSnapshot()
    const second = getMateWatchSnapshot()

    expect(first).toBe(second)
  })
})
