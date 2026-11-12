import { useEffect, useState } from 'react'

import { useApiQuery } from '@/lib/api-query'
import { INBOX_COUNT_KEY, INBOX_COUNT_PATH, INBOX_KEY, INBOX_PATH, type InboxItem, type InboxList } from '@/lib/inbox-api'

const NO_ITEMS: InboxItem[] = []
/** How often the badge re-reads the number waiting, for items shared from a phone meanwhile. Uploads, filing and deleting refresh it at once (inboxApi). */
const COUNT_POLL_MS = 5 * 60_000
/** How often the list is re-read while Mate is still sorting what arrived. */
const SORTING_POLL_MS = 4_000

/** Whether Mate is set up and some item has neither a kind nor a recorded failure yet. */
export function inboxWaitingOnMate(list: InboxList | undefined): boolean {
  return list?.mate_sorting === true && (list.items ?? NO_ITEMS).some((i) => !i.suggested_kind && !i.classify_error)
}

/** Everything waiting in the Inbox, with the group each item sits in. */
export function useInbox() {
  // Polls only while Mate has items to sort, so the groups settle by themselves.
  const [sorting, setSorting] = useState(false)
  const query = useApiQuery({
    queryKey: INBOX_KEY,
    path: INBOX_PATH,
    refetchInterval: sorting ? SORTING_POLL_MS : undefined,
    pauseWhenHidden: true,
    select: (raw: InboxList) => raw,
  })
  const waiting = inboxWaitingOnMate(query.data)
  useEffect(() => setSorting(waiting), [waiting])
  return {
    items: query.data?.items ?? NO_ITEMS,
    mateSorting: query.data?.mate_sorting === true,
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
