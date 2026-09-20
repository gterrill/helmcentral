import { Lock, TriangleAlert, WifiOff } from 'lucide-react'
import { useTelemetryStatus } from '@/hooks/use-telemetry-stream'
import type { ActiveAlarm } from '@/hooks/use-alarms'
import type { ScreenWakeLockStatus } from '@/hooks/use-screen-wake-lock'
import { severityBorderClass, severityFieldClass, worstZoneState } from '@/lib/severity'
import { cn } from '@/lib/utils'

interface DisplayStatusBadgeProps {
  alarms: ActiveAlarm[]
  /** useScreenWakeLock's status (ADR 0110 §5c). Anything other than
   * `'off'`/`'held'` means the best-effort wake lock isn't actually holding -
   * shown here per AGENTS.md's fallback policy: a best-effort feature has to
   * say when it isn't working rather than quietly doing nothing. Optional so
   * this stays usable without a display record (e.g. this component's own
   * unit tests, and any future non-wall caller). */
  wakeLockStatus?: ScreenWakeLockStatus
}

/**
 * The wall display has no room for the full ConnectionBanner (about 40px of
 * a 344px fold budget) or the full AlarmBanner, but hiding either outright
 * is wrong for an alarm product: a crew glancing at the wall has to be able
 * to tell "the feed is stale" or "something needs attention" without
 * walking to a screen with more room. Compact pills instead, bottom right,
 * absent while everything is fine.
 *
 * Rendered inside display-shell.tsx's inner box, not as a sibling of it, so
 * it rotates, scales and pixel-shifts along with the rest of the board.
 */
export function DisplayStatusBadge({ alarms, wakeLockStatus }: DisplayStatusBadgeProps) {
  const status = useTelemetryStatus()
  const connected = status === 'connected'
  const unacknowledged = alarms.filter((alarm) => alarm.phase !== 'acknowledged')
  const wakeLockTrouble = wakeLockStatus === 'unsupported' || wakeLockStatus === 'denied'
  // Same worst-rung question AlarmBanner asks of its own `shown` set: while
  // anything is unacknowledged the pill only has to answer for those, once
  // everything has been acked the muted variant still needs a rung to draw
  // its border from.
  const worst = worstZoneState((unacknowledged.length > 0 ? unacknowledged : alarms).map((alarm) => alarm.state))

  if (connected && alarms.length === 0 && !wakeLockTrouble) return null

  return (
    <div className="pointer-events-none fixed bottom-2 right-2 z-10 flex items-center gap-1.5">
      {!connected && (
        // Same vocabulary as connection-banner.tsx (WifiOff, "reconnecting
        // automatically"), condensed to a pill: amber, not destructive red,
        // because a dropped feed is not itself an alarm condition.
        <span
          data-testid="display-signal-pill"
          className="inline-flex items-center gap-1 rounded-md border border-amber-500/40 bg-amber-500/10 px-2 py-1 text-[10px] font-semibold uppercase tracking-[0.08em] text-amber-500"
        >
          <WifiOff className="h-3 w-3" aria-hidden="true" />
          {status === 'disconnected' ? 'No signal' : 'Reconnecting'}
        </span>
      )}
      {alarms.length > 0 && (
        // ADR 0080 rations alarm colour to state that actually needs looking
        // at; ADR 0116 makes the colour itself say how bad that state is,
        // the same severity ladder AlarmBanner now paints, so a warn-only
        // alarm on the wall doesn't shout the same red an emergency does.
        // Loud (worst rung's tinted triple) while anything is unacknowledged,
        // muted once everything showing has been acked but is still live —
        // the same "acknowledged, still live" distinction AlarmBanner draws,
        // with the border alone keeping the worst rung's colour there too.
        <span
          data-testid="display-alarm-pill"
          className={cn(
            'inline-flex items-center gap-1 rounded-md border px-2 py-1 text-[10px] font-semibold uppercase tracking-[0.08em]',
            unacknowledged.length > 0
              ? severityFieldClass(worst)
              : cn(severityBorderClass(worst) || 'border-border', 'bg-muted text-muted-foreground'),
          )}
        >
          <TriangleAlert className="h-3 w-3" aria-hidden="true" />
          {alarms.length} {alarms.length === 1 ? 'alarm' : 'alarms'}
        </span>
      )}
      {wakeLockTrouble && (
        <span
          data-testid="display-wake-lock-pill"
          className="inline-flex items-center gap-1 rounded-md border border-amber-500/40 bg-amber-500/10 px-2 py-1 text-[10px] font-semibold uppercase tracking-[0.08em] text-amber-500"
        >
          <Lock className="h-3 w-3" aria-hidden="true" />
          {wakeLockStatus === 'unsupported' ? 'Wake lock unsupported' : 'Wake lock denied'}
        </span>
      )}
    </div>
  )
}
