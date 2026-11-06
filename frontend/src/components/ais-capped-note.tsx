import { useNearbyVesselFeed } from '@/hooks/use-nearby-vessels'
import { cappedBoatsMessage } from '@/lib/nearby-vessel-display'

/**
 * Says on a map when the backend's cap left boats near the anchor out of the
 * feed, so a crowded anchorage does not read as fewer boats than there are.
 * Nothing when none were left out.
 */
export function AisCappedNote() {
  const feed = useNearbyVesselFeed()
  const message = cappedBoatsMessage(feed?.cappedCount, feed?.feedCap)
  if (message === null) return null
  return (
    <div
      data-testid="ais-capped-note"
      className="pointer-events-none absolute bottom-8 left-3 max-w-[70%] rounded bg-black/85 px-2 py-0.5 text-xs text-white"
      style={{ zIndex: 1700 }}
    >
      {message}
    </div>
  )
}
