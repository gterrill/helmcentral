import { useApiQuery } from '@/lib/api-query'
import { INBOX_COUNT_KEY, INBOX_COUNT_PATH, INBOX_KEY, INBOX_PATH, type InboxItem } from '@/lib/inbox-api'

const NO_ITEMS: InboxItem[] = []
/** How often the badge re-reads the number waiting, for items shared from a phone meanwhile. Uploads, filing and deleting refresh it at once (inboxApi). */
const COUNT_POLL_MS = 5 * 60_000

/** Everything waiting in the Inbox, with the group each item sits in. */
export function useInbox() {
  const query = useApiQuery({
    queryKey: INBOX_KEY,
    path: INBOX_PATH,
    select: (raw: { items?: InboxItem[] }) => raw.items ?? NO_ITEMS,
  })
  return {
    items: query.data ?? NO_ITEMS,
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
