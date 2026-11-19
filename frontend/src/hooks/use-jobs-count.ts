import { useApiQuerySelector } from '@/lib/api-query'
import { useRereadOnBoatDay } from '@/hooks/use-maintenance'
import { JOBS_COUNT_QUERY } from '@/hooks/use-open-jobs'

/** How often the badge asks again, for work added from another screen or device meanwhile. Writes made here refresh it at once (their own invalidations). */
const COUNT_POLL_MS = 5 * 60_000

/** What the server says is waiting: the open jobs plus the services overdue or due soon and not acknowledged. `incomplete` means some item's schedule could not be read, so the count may be short. */
export interface JobsCount {
  count: number
  incomplete: boolean
}

interface Reading {
  answer: JobsCount | null
  failed: boolean
}

const sameReading = (a: Reading, b: Reading) =>
  a.failed === b.failed && a.answer?.count === b.answer?.count && a.answer?.incomplete === b.answer?.incomplete

function readCount(raw: { count?: unknown; incomplete?: unknown } | undefined, error: Error | null): Reading {
  if (error !== null) return { answer: null, failed: true }
  if (raw === undefined) return { answer: null, failed: false }
  // An answer that is not a count is a failed read, never a 0.
  if (typeof raw.count !== 'number' || typeof raw.incomplete !== 'boolean') throw new Error('the Jobs count was not a number')
  return { answer: { count: raw.count, incomplete: raw.incomplete }, failed: false }
}

/**
 * What needs doing on the boat, for the Jobs row's badge (ADR 0293). The server
 * does the counting and the judging of what is due, on the boat's day, and
 * answers with one small object, so no tab downloads the lists to count them.
 * Null until it has answered, or while its latest read has failed: the badge
 * then says nothing rather than an old number.
 *
 * The count is read again on focus, on a slow poll, after any open-job or
 * service write (their invalidations reach this key), and when the boat's day
 * arrives or turns.
 */
export function useJobsCount(): JobsCount | null {
  const reading = useApiQuerySelector(
    { ...JOBS_COUNT_QUERY, refetchInterval: COUNT_POLL_MS, pauseWhenHidden: true, refetchOnFocus: true },
    readCount,
    sameReading,
  )
  useRereadOnBoatDay(JOBS_COUNT_QUERY.queryKey, JOBS_COUNT_QUERY.path, true, reading.failed)
  return reading.answer
}
