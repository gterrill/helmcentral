import type { InboxQueueStore } from '@/lib/inbox-queue'
import { queuedToFile } from '@/lib/inbox-queue'
import { errorMessage } from '@/lib/errors'
import { FileGoneError, uploadFiles, type FileSource, type UploadDeps, type UploadTally } from '@/lib/inbox-upload'

// Sends what is on the phone's waiting list (ADR 0280), through the same
// uploadFiles that sends a batch picked on the upload page. A file leaves
// the list only when the box has stored it or already holds it; a file that
// failed stays, with its error. Files picked on the page do not use the list:
// runPicked sends them as they are.
//
// One pass: the ids are taken once, and each file's data is fetched from the
// phone's storage just before it is hashed or prepared and let go after, so a
// run holds one file at a time. A file added while the run goes stays waiting
// for the next one; the banner and the upload page show it.
//
// A run outlives the page that started it, so what a page needs to know about
// it (is one going, how far, how it ended) lives here, not in the page.

const LOCK_NAME = 'helmcentral-inbox-upload'

/** The most often the progress is published while files go. */
const PROGRESS_INTERVAL_MS = 250

export type QueueRunResult =
  /** Another run holds the guard, in this window or another. */
  | { busy: true }
  | { busy: false; tally: UploadTally }

export type QueueRunDeps = Omit<UploadDeps, 'onFileDone' | 'signal'>

/** What this window knows of its upload run, for any page to show. */
export interface UploadRunState {
  readonly running: boolean
  /** The latest progress, or the end of the last run; kept after the run so the page can say how it went. */
  readonly tally: UploadTally | null
  /** Why the last run stopped part way, in words for the operator; null when it did not. */
  readonly error: string | null
  /** The last start found a run in another window of the app holding the guard. */
  readonly busyElsewhere: boolean
}

const IDLE: UploadRunState = { running: false, tally: null, error: null, busyElsewhere: false }
let current: UploadRunState = IDLE
let lastProgressAt = 0
let trailing: ReturnType<typeof setTimeout> | null = null
const listeners = new Set<() => void>()

function setRun(next: UploadRunState) {
  current = next
  for (const l of [...listeners]) l()
}

export function getUploadRun(): UploadRunState {
  return current
}

export function subscribeUploadRun(onChange: () => void): () => void {
  listeners.add(onChange)
  return () => { listeners.delete(onChange) }
}

/** For tests: forgets the last run. */
export function resetUploadRun(): void {
  current = IDLE
  lastProgressAt = 0
  stopTrailing()
  listeners.clear()
}

function stopTrailing() {
  if (trailing !== null) clearTimeout(trailing)
  trailing = null
}

/** What a run got through before it stopped, to say so truthfully. */
interface RunCount {
  /** Files in the run. */
  total: number
  /** Files the box stored or already held, counted when it answered, before they came off the list. */
  held: number
}

function stoppedMessage(err: unknown, count: RunCount, listed: boolean): string {
  const why = errorMessage(err)
  if (count.held === 0) return listed ? `Nothing was sent: ${why}. The files are still on the waiting list.` : `Nothing was sent: ${why}.`
  return `${count.held} of ${count.total} were stored or already held before this stopped: ${why}. Everything not yet taken off the waiting list is still waiting, and the box skips any it already has.`
}

/**
 * Runs `go` as this window's one upload run. A start while one is going does
 * nothing and says so; the run's progress and end are published for any page.
 */
async function tracked(
  deps: QueueRunDeps,
  listed: boolean,
  go: (deps: QueueRunDeps, count: RunCount) => Promise<QueueRunResult>,
): Promise<QueueRunResult> {
  if (current.running) return { busy: true }
  lastProgressAt = 0
  stopTrailing()
  // The last run's numbers go: until this run reports, there is nothing to show.
  setRun({ running: true, tally: null, error: null, busyElsewhere: false })
  const count: RunCount = { total: 0, held: 0 }
  try {
    const result = await go({
      ...deps,
      // A big batch reports after every file; the screen needs about four a second.
      // The latest report is held back and published when the interval is up, so a
      // slow upload after a burst does not leave the line showing an old count.
      onProgress: (t) => {
        deps.onProgress?.(t)
        const publish = () => {
          trailing = null
          lastProgressAt = Date.now()
          setRun({ ...current, tally: t })
        }
        stopTrailing()
        const wait = PROGRESS_INTERVAL_MS - (Date.now() - lastProgressAt)
        if (wait <= 0) publish()
        else trailing = setTimeout(publish, wait)
      },
    }, count)
    stopTrailing()
    setRun(result.busy ? { ...current, running: false, busyElsewhere: true } : { ...current, running: false, tally: result.tally })
    return result
  } catch (err) {
    stopTrailing()
    setRun({ ...current, running: false, error: stoppedMessage(err, count, listed) })
    throw err
  }
}

/**
 * Uploads the waiting list in order. Returns `{ busy: true }` and sends nothing
 * when another window of the app holds the Web Lock or this window's own run is
 * already going. Web Locks need a secure address; on a
 * plain-http LAN address there are none, and the run goes ahead with no guard
 * across windows. That is harmless: the box checks every file's hash and
 * refuses a duplicate, so two runs at once only repeat each other's work.
 * Throws, leaving the list as it was, when the box cannot be asked.
 */
export function runQueue(store: InboxQueueStore, deps: QueueRunDeps): Promise<QueueRunResult> {
  return tracked(deps, true, async (runDeps, count) => {
    const locks = globalThis.navigator?.locks
    if (!locks) return { busy: false, tally: await pass(store, runDeps, count) }
    return locks.request(LOCK_NAME, { ifAvailable: true }, async (lock): Promise<QueueRunResult> => {
      if (!lock) return { busy: true }
      return { busy: false, tally: await pass(store, runDeps, count) }
    })
  })
}

/** Uploads files picked on the page as they are: no list, no copy, nothing kept if it fails. */
export function runPicked(files: readonly File[], deps: QueueRunDeps): Promise<QueueRunResult> {
  return tracked(deps, false, async (runDeps) => ({ busy: false, tally: await uploadFiles(files, runDeps) }))
}

async function pass(store: InboxQueueStore, deps: QueueRunDeps, count: RunCount): Promise<UploadTally> {
  const waiting = await store.list()
  count.total = waiting.length
  const source: FileSource = {
    length: waiting.length,
    name: (i) => waiting[i].name,
    read: async (i) => {
      const blob = await store.getBlob(waiting[i].id)
      if (!blob) {
        // Discarded after the list was read: nothing to send, and not a failure.
        count.total -= 1
        throw new FileGoneError()
      }
      return queuedToFile(waiting[i], blob)
    },
  }
  return uploadFiles(source, {
    ...deps,
    onFileDone: async (_file, outcome, i) => {
      if (outcome.kind === 'failed') {
        await store.setError(waiting[i].id, outcome.error)
        return
      }
      count.held += 1
      await store.remove([waiting[i].id])
    },
  })
}
