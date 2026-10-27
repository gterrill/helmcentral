import { useMemo, useSyncExternalStore } from 'react'

import { useVesselStateSelector } from '@/hooks/use-vessel-state'
import { readServerClock, subscribeServerClock } from '@/lib/server-clock'
import { formatClock, formatDate, selectTimeZone } from '@/lib/vessel-time'

/**
 * The boat's time as the clock surfaces show it (ADR 0218), re-rendering the
 * caller every second. This is the only reader of the boat's clock. It reads
 * the zone from the vessel-state store, which starts that store, and the store
 * is what feeds the clock, so a clock reader with no other vessel-state reader
 * still gets a time. Only the clock surfaces call this: the header's vessel
 * name and status read the store's fields and do not tick.
 * Everything is null until the server has sent a datetime, and the zone is
 * undefined until it has sent a timezone; nothing is read from the browser's
 * own clock.
 */
export function useVesselClock() {
  const timeZone = useVesselStateSelector(selectTimeZone)
  const now = useSyncExternalStore(subscribeServerClock, readServerClock)

  const currentDate = useMemo(() => (now === null ? null : formatDate(now, { timeZone }).toUpperCase()), [now, timeZone])
  const clock = useMemo(() => (now === null ? null : formatClock(now, timeZone)), [now, timeZone])

  return { now, currentDate, clock, timeZone }
}
