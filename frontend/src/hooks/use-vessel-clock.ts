import { useCallback, useMemo } from 'react'

import { useVesselStateSelector } from '@/hooks/use-vessel-state'
import { readServerClock, subscribeServerClock } from '@/lib/server-clock'
import { useSelection } from '@/lib/selection'
import { formatClock, formatDate, formatDayKey, selectTimeZone, selectTimeZoneSource, unknownZoneMessage } from '@/lib/vessel-time'

/**
 * The boat's calendar day (YYYY-MM-DD) on the server clock in the boat's zone,
 * or null until both are known. Before the first fix the server sends UTC as a
 * stand-in zone with source "none"; that is no boat day, so this is null then
 * too. It re-renders the caller only when the day turns. A refetch key, never
 * a value sent to the server: the server works out the day a due date is
 * judged on for itself (ADR 0230).
 */
export function useBoatDayKey(): string | null {
  const timeZone = useVesselStateSelector(selectTimeZone)
  const timeZoneSource = useVesselStateSelector(selectTimeZoneSource)
  const known = timeZone !== undefined && timeZoneSource !== 'none' && !unknownZoneMessage(timeZone)
  const select = useCallback(
    (now: Date | null) => (now === null || !known || timeZone === undefined ? null : formatDayKey(now, timeZone)),
    [known, timeZone],
  )
  return useSelection({ subscribe: subscribeServerClock, getSnapshot: readServerClock, select })
}

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
  const timeZoneSource = useVesselStateSelector(selectTimeZoneSource)
  const now = useSelection({ subscribe: subscribeServerClock, getSnapshot: readServerClock })

  // A zone this browser's ICU does not know is shown as an error where the
  // time goes (the header sits outside any tile boundary), never as a throw
  // and never as another zone's clock.
  const zoneError = unknownZoneMessage(timeZone)
  const currentDate = useMemo(() => (now === null || zoneError ? null : formatDate(now, { timeZone }).toUpperCase()), [now, timeZone, zoneError])
  const clock = useMemo(() => (now === null || zoneError ? null : formatClock(now, timeZone)), [now, timeZone, zoneError])

  return { now: zoneError ? null : now, currentDate, clock, timeZone, timeZoneSource, zoneError }
}
