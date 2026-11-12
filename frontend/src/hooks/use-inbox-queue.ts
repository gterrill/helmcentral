import { useSyncExternalStore } from 'react'

import { errorMessage } from '@/lib/errors'
import { createIdbQueueStore, withQueuedError, type InboxQueueStore, type QueuedMeta } from '@/lib/inbox-queue'
import { getUploadRun, subscribeUploadRun, type UploadRunState } from '@/lib/inbox-queue-runner'

// The phone's waiting list as the app sees it (ADR 0280): one store per window,
// shared by every component that reads it, in the style of
// lib/boolean-preference.ts. It holds metadata only; a file's data is read from
// IndexedDB one file at a time by the runner.
//
// One IndexedDB connection and one BroadcastChannel are opened the first time
// anything asks and live as long as the page. A change made here is applied
// directly and posted to the other windows; a change another window posts is
// read when it arrives. Reloads are coalesced to one per tick.

const CHANNEL = 'helmcentral-inbox-queue'

export interface InboxQueueState {
  /** Oldest first; empty until read. */
  readonly entries: readonly QueuedMeta[]
  /** Why the list cannot be used or read, such as a browser with no IndexedDB. */
  readonly error: string | null
  /** Null when the browser cannot keep a list. Its changes tell every reader. */
  readonly store: InboxQueueStore | null
}

let state: InboxQueueState | null = null
let channel: BroadcastChannel | null = null
const listeners = new Set<() => void>()
let scheduled = false
let loading = false
let again = false
// Counts the changes made from this window. A reload that began before one is stale.
let generation = 0
// A run changes the list once per file; the other windows hear of it once, at the end.
let wasRunning = false
let ranWithChanges = false
let stopWatchingRun: (() => void) | null = null

function sameEntries(a: readonly QueuedMeta[], b: readonly QueuedMeta[]): boolean {
  return a.length === b.length && a.every((q, i) => q.id === b[i].id && q.error === b[i].error)
}

function set(next: InboxQueueState) {
  state = next
  for (const l of [...listeners]) l()
}

function scheduleReload() {
  if (scheduled) return
  scheduled = true
  setTimeout(() => {
    scheduled = false
    void reload()
  }, 0)
}

async function reload() {
  const current = ensure()
  const store = current.store
  if (!store) return
  if (loading) {
    again = true
    return
  }
  loading = true
  const startedAt = generation
  try {
    // The count is cheap; the list is read only when something waits.
    const entries = (await store.count()) === 0 ? [] : await store.list()
    // A change made here while this read was out has already edited the
    // snapshot; what was read may predate it. Throw it away and read again.
    if (generation !== startedAt) {
      again = true
      return
    }
    const latest = ensure()
    if (latest.error !== null || !sameEntries(latest.entries, entries)) {
      set({ ...latest, entries: sameEntries(latest.entries, entries) ? latest.entries : entries, error: null })
    }
  } catch (err) {
    const latest = ensure()
    const message = errorMessage(err)
    if (latest.error !== message) set({ ...latest, error: message })
  } finally {
    loading = false
    if (again) {
      again = false
      scheduleReload()
    }
  }
}

/** Tells the other windows, unless a run is going: then once, when it ends. */
function announce() {
  if (getUploadRun().running) ranWithChanges = true
  else channel?.postMessage('changed')
}

function onRunChange() {
  const running = getUploadRun().running
  if (wasRunning && !running && ranWithChanges) {
    ranWithChanges = false
    channel?.postMessage('changed')
    scheduleReload()
  }
  wasRunning = running
}

/**
 * The inner store, with each change applied here at once and posted to the
 * other windows. A remove or a recorded failure edits the snapshot directly: a
 * run makes one of these per file, and reading the whole list back each time
 * would cost more than the upload.
 */
function announcing(inner: InboxQueueStore): InboxQueueStore {
  const changed = <A extends unknown[], R>(fn: (...args: A) => Promise<R>, patch?: (entries: readonly QueuedMeta[], ...args: A) => readonly QueuedMeta[]) => async (...args: A): Promise<R> => {
    let done = false
    try {
      const result = await fn(...args)
      done = true
      return result
    } finally {
      generation += 1
      const current = state
      if (done && patch && current) {
        const next = patch(current.entries, ...args)
        if (!sameEntries(current.entries, next)) set({ ...current, entries: next })
      } else {
        scheduleReload()
      }
      announce()
    }
  }
  return {
    list: inner.list,
    count: inner.count,
    getBlob: inner.getBlob,
    remove: changed(inner.remove, (entries, ids) => entries.filter((q) => !ids.includes(q.id))),
    setError: changed(inner.setError, (entries, id, error) => entries.map((q) => (q.id === id ? withQueuedError(q, error) : q))),
    clear: changed(inner.clear),
  }
}

function ensure(): InboxQueueState {
  if (state) return state
  try {
    const inner = createIdbQueueStore()
    if (typeof BroadcastChannel !== 'undefined') {
      channel = new BroadcastChannel(CHANNEL)
      channel.onmessage = scheduleReload
    }
    state = { entries: [], error: null, store: announcing(inner) }
    wasRunning = getUploadRun().running
    stopWatchingRun = subscribeUploadRun(onRunChange)
  } catch (err) {
    state = { entries: [], error: errorMessage(err), store: null }
  }
  return state
}

function subscribe(onChange: () => void): () => void {
  ensure()
  listeners.add(onChange)
  scheduleReload()
  return () => { listeners.delete(onChange) }
}

/** For tests: forgets the store, closes the channel and stops pending reloads. */
export function resetInboxQueue(): void {
  channel?.close()
  channel = null
  stopWatchingRun?.()
  stopWatchingRun = null
  wasRunning = false
  ranWithChanges = false
  state = null
  listeners.clear()
  scheduled = false
  loading = false
  again = false
  generation = 0
}

export function useInboxQueue(): InboxQueueState {
  return useSyncExternalStore(subscribe, ensure, ensure)
}

/** This window's upload run: whether one is going, how far, how it ended. It outlives any page that started it. */
export function useUploadRun(): UploadRunState {
  return useSyncExternalStore(subscribeUploadRun, getUploadRun, getUploadRun)
}
