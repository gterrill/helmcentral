import { TriangleAlert } from 'lucide-react'
import { memo } from 'react'

import { Button } from '@/components/ui/button'
import type { ActiveAlarm } from '@/hooks/use-alarms'

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
 */
export const AlarmBanner = memo(function AlarmBanner({ alarms, onOpen }: AlarmBannerProps) {
  const unacknowledged = alarms.filter((alarm) => alarm.phase !== 'acknowledged')
  if (unacknowledged.length === 0) return null

  const [first] = unacknowledged
  const others = unacknowledged.length - 1

  return (
    <div
      role="alert"
      className="flex min-w-0 items-center gap-3 rounded-lg border border-destructive bg-destructive/10 px-4 py-3 text-destructive shadow-sm"
    >
      <TriangleAlert className="size-5 shrink-0" aria-hidden="true" />
      <div className="min-w-0 flex-1">
        <p className="truncate text-sm font-semibold uppercase tracking-[0.08em]">
          {first.state} — {first.label}
          {others > 0 && <span className="ml-2 font-normal">and {others} more</span>}
        </p>
        <p className="truncate text-xs">{first.message}</p>
      </div>
      <Button size="sm" variant="outline" className="shrink-0" onClick={onOpen}>
        View
      </Button>
    </div>
  )
})
