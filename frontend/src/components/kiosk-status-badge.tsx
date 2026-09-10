import { TriangleAlert, WifiOff } from 'lucide-react'
import { useTelemetryStatus } from '@/hooks/use-telemetry-stream'
import type { ActiveAlarm } from '@/hooks/use-alarms'
import { cn } from '@/lib/utils'

interface KioskStatusBadgeProps {
  alarms: ActiveAlarm[]
}

/**
 * The wall display has no room for the full ConnectionBanner (about 40px of
 * the 344px fold budget) or the full AlarmBanner, but hiding either outright
 * is wrong for an alarm product: a crew glancing at the wall has to be able
 * to tell "the feed is stale" or "something needs attention" without
 * walking to a screen with more room. Two compact pills instead, bottom
 * right, absent while everything is fine.
 *
 * Rendered inside kiosk-shell.tsx's rotated root, not as a sibling of it, so
 * it flips upright along with the rest of the content under ?rotate=180.
 */
export function KioskStatusBadge({ alarms }: KioskStatusBadgeProps) {
  const status = useTelemetryStatus()
  const connected = status === 'connected'
  const unacknowledged = alarms.filter((alarm) => alarm.phase !== 'acknowledged')

  if (connected && alarms.length === 0) return null

  return (
    <div className="pointer-events-none fixed bottom-2 right-2 z-10 flex items-center gap-1.5">
      {!connected && (
        // Same vocabulary as connection-banner.tsx (WifiOff, "reconnecting
        // automatically"), condensed to a pill: amber, not destructive red,
        // because a dropped feed is not itself an alarm condition.
        <span
          data-testid="kiosk-signal-pill"
          className="inline-flex items-center gap-1 rounded-md border border-amber-500/40 bg-amber-500/10 px-2 py-1 text-[10px] font-semibold uppercase tracking-[0.08em] text-amber-500"
        >
          <WifiOff className="h-3 w-3" aria-hidden="true" />
          {status === 'disconnected' ? 'No signal' : 'Reconnecting'}
        </span>
      )}
      {alarms.length > 0 && (
        // ADR 0080: alarm colour is reserved for state that actually needs
        // looking at. Loud (destructive red) while anything is unacknowledged,
        // muted once everything showing has been acked but is still live —
        // the same "acknowledged, still live" distinction AlarmBanner draws.
        <span
          data-testid="kiosk-alarm-pill"
          className={cn(
            'inline-flex items-center gap-1 rounded-md border px-2 py-1 text-[10px] font-semibold uppercase tracking-[0.08em]',
            unacknowledged.length > 0
              ? 'border-destructive bg-destructive/10 text-destructive'
              : 'border-border bg-muted text-muted-foreground',
          )}
        >
          <TriangleAlert className="h-3 w-3" aria-hidden="true" />
          {alarms.length} {alarms.length === 1 ? 'alarm' : 'alarms'}
        </span>
      )}
    </div>
  )
}
