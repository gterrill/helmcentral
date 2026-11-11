import type { InboxQueueStore } from '@/lib/inbox-queue'
import { queuedToFile } from '@/lib/inbox-queue'
import { uploadFiles, type UploadDeps, type UploadTally } from '@/lib/inbox-upload'

// Sends what is on the phone's waiting list (ADR 0280), through the same
// uploadFiles that sent a picked batch before there was a list. A file leaves
// the list only when the box has stored it or already holds it; a file that
// failed stays, with its error. One loop runs at a time per device.

const LOCK_NAME = 'helmcentral-inbox-upload'

/** The part of navigator.locks the loop uses. */
export interface LockLike {
  request<T>(name: string, options: { ifAvailable: boolean }, callback: (lock: { name: string } | null) => Promise<T>): Promise<T>
}

export type QueueRunResult = { busy: true } | { busy: false; tally: UploadTally }

/** Held for the length of a run in this window, whether or not Web Locks can span windows. */
let runningHere = false

export type QueueRunDeps = Omit<UploadDeps, 'onFileDone' | 'onProgress'> & {
  /** Progress across the whole run, including files added while it ran. */
  onProgress?: (tally: UploadTally) => void
}

function combine(done: UploadTally, current: UploadTally): UploadTally {
  return {
    stage: current.stage,
    total: done.total + current.total,
    checked: done.checked + current.checked,
    done: done.done + current.done,
    uploaded: done.uploaded + current.uploaded,
    skipped: done.skipped + current.skipped,
    failed: [...done.failed, ...current.failed],
    noTime: done.noTime + current.noTime,
    warnings: [...done.warnings, ...current.warnings],
  }
}

const EMPTY: UploadTally = { stage: 'checking', total: 0, checked: 0, done: 0, uploaded: 0, skipped: 0, failed: [], noTime: 0, warnings: [] }

/**
 * Uploads the waiting list in order. Returns `{ busy: true }` and sends nothing
 * when another loop holds the device: another window of the app (a Web Lock,
 * which needs a secure address) or this one. A file added while the loop runs
 * is taken up before it ends. A file that failed is not tried again in the same
 * run. Throws, leaving the list as it was, when the box cannot be asked.
 */
export async function runQueue(
  store: InboxQueueStore,
  deps: QueueRunDeps,
  locks: LockLike | undefined = (globalThis.navigator?.locks as LockLike | undefined),
): Promise<QueueRunResult> {
  if (runningHere) return { busy: true }
  runningHere = true
  try {
    const work = () => loop(store, deps)
    if (!locks) return { busy: false, tally: await work() }
    return await locks.request<QueueRunResult>(LOCK_NAME, { ifAvailable: true }, async (lock) => {
      if (!lock) return { busy: true }
      return { busy: false, tally: await work() }
    })
  } finally {
    runningHere = false
  }
}

async function loop(store: InboxQueueStore, deps: QueueRunDeps): Promise<UploadTally> {
  let total = EMPTY
  const tried = new Set<string>()
  for (;;) {
    if (deps.signal?.aborted) return total
    const waiting = (await store.list()).filter((q) => !tried.has(q.id))
    if (waiting.length === 0) return total

    const ids = new Map<File, string>()
    const files = waiting.map((q) => {
      tried.add(q.id)
      const f = queuedToFile(q)
      ids.set(f, q.id)
      return f
    })
    const before = total
    const round = await uploadFiles(files, {
      ...deps,
      onProgress: (t) => deps.onProgress?.(combine(before, t)),
      onFileDone: async (f, outcome) => {
        const id = ids.get(f)
        if (id === undefined) throw new Error(`${f.name} is not on the waiting list`)
        if (outcome.kind === 'failed') await store.setError(id, outcome.error)
        else await store.remove([id])
      },
    })
    total = combine(before, round)
  }
}
