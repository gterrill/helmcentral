import { useCallback, useEffect, useState } from 'react'

import { errorMessage } from '@/lib/errors'
import { createIdbQueueStore, type InboxQueueStore, type QueuedFile } from '@/lib/inbox-queue'

// The phone's waiting list as the app sees it (ADR 0280): a store that tells
// every window and every component when it changed, and a hook that reads it.

const CHANGED = 'helmcentral:inbox-queue-changed'
const CHANNEL = 'helmcentral-inbox-queue'

function announce(channel: BroadcastChannel | null) {
  window.dispatchEvent(new Event(CHANGED))
  channel?.postMessage('changed')
}

/** The waiting list in IndexedDB, announcing each change. Throws when the browser has no IndexedDB. */
export function openInboxQueue(): InboxQueueStore {
  const inner = createIdbQueueStore()
  const channel = typeof BroadcastChannel === 'undefined' ? null : new BroadcastChannel(CHANNEL)
  const changed = <A extends unknown[], R>(fn: (...args: A) => Promise<R>) => async (...args: A): Promise<R> => {
    try {
      return await fn(...args)
    } finally {
      announce(channel)
    }
  }
  return {
    add: changed(inner.add),
    list: inner.list,
    remove: changed(inner.remove),
    setError: changed(inner.setError),
    clear: changed(inner.clear),
  }
}

export interface InboxQueueState {
  /** Oldest first; empty until read. */
  entries: QueuedFile[]
  /** Why the list cannot be used, such as a browser with no IndexedDB. */
  error: string | null
  /** Null when the browser cannot keep a list. */
  store: InboxQueueStore | null
}

export function useInboxQueue(): InboxQueueState {
  const [entries, setEntries] = useState<QueuedFile[]>([])
  const [error, setError] = useState<string | null>(null)
  const [{ store, initError }] = useState<{ store: InboxQueueStore | null; initError: string | null }>(() => {
    try {
      return { store: openInboxQueue(), initError: null }
    } catch (err) {
      return { store: null, initError: errorMessage(err) }
    }
  })

  const reload = useCallback(async () => {
    if (!store) return
    try {
      setEntries(await store.list())
      setError(null)
    } catch (err) {
      setError(errorMessage(err))
    }
  }, [store])

  useEffect(() => {
    void reload()
    const onChange = () => { void reload() }
    window.addEventListener(CHANGED, onChange)
    const channel = typeof BroadcastChannel === 'undefined' ? null : new BroadcastChannel(CHANNEL)
    if (channel) channel.onmessage = onChange
    return () => {
      window.removeEventListener(CHANGED, onChange)
      channel?.close()
    }
  }, [reload])

  return { entries, error: initError ?? error, store }
}
