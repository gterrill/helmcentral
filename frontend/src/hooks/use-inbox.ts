import { useEffect, useRef } from 'react'

import { useApiQuery } from '@/lib/api-query'
import { INBOX_COUNT_KEY, INBOX_COUNT_PATH, INBOX_KEY, INBOX_PATH, type InboxItem, type InboxList } from '@/lib/inbox-api'

const NO_ITEMS: InboxItem[] = []
/** How often the badge re-reads the number waiting, for items shared from a phone meanwhile. Uploads, filing and deleting refresh it at once (inboxApi). */
const COUNT_POLL_MS = 5 * 60_000
/** How long between re-reads of the list while Mate is still sorting what arrived: quick at first, then slower. The last step repeats. */
const SORTING_POLL_STEPS_MS = [4_000, 8_000, 15_000]
/** Polling stops once this long has passed with no item sorted and none new. Reopening the Inbox starts it again. */
const SORTING_POLL_GIVE_UP_MS = 10 * 60_000

/** Whether Mate is set up and some item has neither a kind nor a recorded failure yet. */
export function inboxWaitingOnMate(list: InboxList | undefined): boolean {
  return list?.mate_sorting === true && (list.items ?? NO_ITEMS).some((i) => !i.suggested_kind && !i.classify_error)
}

/** How often the list is re-read while a failed sort waits for its next try. No give-up: Mate keeps trying, hourly at the slowest. */
const RETRY_POLL_MS = 60_000
/** How long after a try is due the list is read again, to let the box make it. */
const RETRY_SETTLE_MS = 3_000

/** When the earliest failed sort is tried again (Unix ms), or null when none waits on a retry. */
export function inboxRetryAtMs(list: InboxList | undefined): number | null {
  if (list?.mate_sorting !== true) return null
  const due = (list.items ?? NO_ITEMS).filter((i) => !i.suggested_kind && i.classify_error && i.next_try_ms).map((i) => i.next_try_ms!)
  return due.length === 0 ? null : Math.min(...due)
}

/** Everything waiting in the Inbox, with the group each item sits in. */
export function useInbox() {
  const query = useApiQuery({
    queryKey: INBOX_KEY,
    path: INBOX_PATH,
    select: (raw: InboxList) => raw,
  })
  // Re-reads the list while Mate has items to sort, so the groups settle by
  // themselves; backs off as nothing changes and stops after 10 minutes. Any
  // change (an item sorted, a new one arrived) is a new signature and starts the
  // steps again.
  const waiting = inboxWaitingOnMate(query.data)
  const signature = waiting ? (query.data?.items ?? NO_ITEMS).map((i) => `${i.id}:${i.suggested_kind ?? ''}:${i.classify_error ?? ''}`).join('|') : ''
  const refetch = useRef(query.refetch)
  useEffect(() => { refetch.current = query.refetch })
  useEffect(() => {
    if (!waiting) return
    let waited = 0
    let step = 0
    let timer: ReturnType<typeof setTimeout> | undefined
    const next = () => {
      const delay = SORTING_POLL_STEPS_MS[Math.min(step++, SORTING_POLL_STEPS_MS.length - 1)]
      if (waited + delay > SORTING_POLL_GIVE_UP_MS) return
      timer = setTimeout(() => {
        waited += delay
        if (document.visibilityState !== 'hidden') void refetch.current()
        next()
      }, delay)
    }
    next()
    return () => clearTimeout(timer)
  }, [waiting, signature])
  // While a failed sort waits for its next try, read slowly and once more just
  // after the try is due, so the tile settles by itself. A new next try (another
  // failure) is a new time and sets the one-off read again.
  const retryAt = inboxRetryAtMs(query.data)
  useEffect(() => {
    if (retryAt === null) return
    const read = () => { if (document.visibilityState !== 'hidden') void refetch.current() }
    const slow = setInterval(read, RETRY_POLL_MS)
    const due = setTimeout(read, Math.max(RETRY_SETTLE_MS, retryAt - Date.now() + RETRY_SETTLE_MS))
    return () => { clearInterval(slow); clearTimeout(due) }
  }, [retryAt])
  return {
    items: query.data?.items ?? NO_ITEMS,
    mateSorting: query.data?.mate_sorting === true,
    /** Why Mate's state is unknown (its settings could not be read), or null. */
    mateError: query.data?.mate_error ?? null,
    loading: query.isLoading,
    error: query.error?.message ?? null,
    refresh: query.refetch,
  }
}

/** How many items wait, for the badge. Null until the server has answered or when it could not. */
export function useInboxCount(): number | null {
  const query = useApiQuery({
    queryKey: INBOX_COUNT_KEY,
    path: INBOX_COUNT_PATH,
    refetchInterval: COUNT_POLL_MS,
    pauseWhenHidden: true,
    refetchOnFocus: true,
    select: (raw: { count: number }) => raw.count,
  })
  return query.data ?? null
}
