import { TriangleAlert } from 'lucide-react'
import { memo } from 'react'

import { Button } from '@/components/ui/button'
import { ALARM_STATES, type ActiveAlarm, type AlarmState } from '@/hooks/use-alarms'
import { alarmConditionSentence } from '@/lib/alarm-display'
import { cn } from '@/lib/utils'

interface AlarmBannerProps {
  alarms: ActiveAlarm[]
  onOpen: () => void
}

/**
 * Worst-first, stable within a severity (ADR 0082 decision 8: the ribbon
 * never reorders, so triage is this banner's job instead). ALARM_STATES is
 * ordered worst-last, so a descending index sort ranks worst-first; the
 * original array index breaks ties so alarms sharing a severity keep the
 * order they arrived in rather than reshuffling on every render.
 */
function worstFirst(alarms: ActiveAlarm[]): ActiveAlarm[] {
  return alarms
    .map((alarm, index) => ({ alarm, index }))
    .sort((a, b) => ALARM_STATES.indexOf(b.alarm.state) - ALARM_STATES.indexOf(a.alarm.state) || a.index - b.index)
    .map(({ alarm }) => alarm)
}

/** One entry per state actually present among `sorted`, worst first. */
function countsByState(sorted: ActiveAlarm[]): { state: AlarmState; count: number }[] {
  const counts: { state: AlarmState; count: number }[] = []
  for (const alarm of sorted) {
    const last = counts[counts.length - 1]
    if (last && last.state === alarm.state) last.count += 1
    else counts.push({ state: alarm.state, count: 1 })
  }
  return counts
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
  const shown = worstFirst(loud ? unacknowledged : alarms)

  const [first] = shown
  const counts = countsByState(shown)
  // e.g. "1 ALARM · 2 WARN" — worst state first, matching the order `shown`
  // and its labels below are already sorted into.
  const headline = counts.map(({ state, count }) => `${count} ${state}`).join(' · ')
  const labels = shown.map((alarm) => alarm.label).join(', ')

  return (
    <div
      role="alert"
      className={cn(
        'flex min-w-0 items-center gap-3 rounded-lg border px-4 py-3 shadow-xs',
        loud
          ? 'border-destructive bg-destructive/10 text-destructive'
          : 'border-border bg-muted text-muted-foreground',
      )}
    >
      <TriangleAlert className="size-5 shrink-0" aria-hidden="true" />
      <div className="min-w-0 flex-1">
        {/* The counts keep the tracked uppercase idiom; the labels do not. An
            alarm's label is a SignalK path (`radar.fur6424a.guardzone.1`),
            and 44 characters of shouted dotted identifier is slower to read at
            arm's length than the same string in its own case. Any overflow is
            the plain `truncate` clip below, same as a single-alarm headline
            always had — there is no separate "and N more" count anymore now
            that every shown alarm's label is already listed. */}
        <p className="truncate text-sm font-semibold">
          <span className="uppercase tracking-[0.08em]">{headline}</span> — {labels}
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
