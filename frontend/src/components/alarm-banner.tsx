import { TriangleAlert } from 'lucide-react'
import { memo } from 'react'

import { Button } from '@/components/ui/button'
import type { ActiveAlarm } from '@/hooks/use-alarms'
import { alarmConditionSentence } from '@/lib/alarm-display'
import { cn } from '@/lib/utils'

interface AlarmBannerProps {
  alarms: ActiveAlarm[]
  onOpen: () => void
}

/**
 * A live alarm has to be visible from every page, not only the Alarms page.
 *
 * Unlike the forecast-warnings banner this one is deliberately not dismissible:
 * dismissal there is stored per-browser in localStorage, which is fine for an
 * informational bulletin and wrong for a live vessel condition. Acknowledging is
 * the equivalent action here, and it is server-side so every screen agrees.
 *
 * Acknowledging silences the sound, not the condition — the boat can still be
 * dragging with every alarm on the board acknowledged. Filtering those out
 * used to make the whole dashboard look calm while that held (the same P0 bug
 * as anchor-watch-tile.tsx's alarm strip): every live alarm renders here,
 * loud while anything is unacknowledged, and in a muted-but-present variant
 * once everything showing has been, labelled "all acknowledged, still live"
 * rather than "silenced", because ADR 0038 treats those as different states
 * and the drawer already uses the acknowledged word.
 */
export const AlarmBanner = memo(function AlarmBanner({ alarms, onOpen }: AlarmBannerProps) {
  if (alarms.length === 0) return null

  const unacknowledged = alarms.filter((alarm) => alarm.phase !== 'acknowledged')
  const loud = unacknowledged.length > 0
  const shown = loud ? unacknowledged : alarms

  const [first] = shown
  const others = shown.length - 1

  return (
    <div
      role="alert"
      className={cn(
        'flex min-w-0 items-center gap-3 rounded-lg border px-4 py-3 shadow-sm',
        loud
          ? 'border-destructive bg-destructive/10 text-destructive'
          : 'border-border bg-muted text-muted-foreground',
      )}
    >
      <TriangleAlert className="size-5 shrink-0" aria-hidden="true" />
      <div className="min-w-0 flex-1">
        {/* The severity word keeps the tracked uppercase idiom; the label does
            not. An alarm's label is a SignalK path (`radar.fur6424a.guardzone.1`),
            and 44 characters of shouted dotted identifier is slower to read at
            arm's length than the same string in its own case. */}
        <p className="truncate text-sm font-semibold">
          <span className="uppercase tracking-[0.08em]">{first.state}</span> — {first.label}
          {others > 0 && <span className="ml-2 font-normal">and {others} more</span>}
          {!loud && <span className="ml-2 font-normal">· all acknowledged, still live</span>}
        </p>
        <p className="truncate text-xs">{alarmConditionSentence(first)}</p>
      </div>
      <Button size="sm" variant="outline" className="shrink-0" onClick={onOpen}>
        View
      </Button>
    </div>
  )
})
