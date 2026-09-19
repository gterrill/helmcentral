import { TriangleAlert } from 'lucide-react'
import { memo, useEffect, useRef, useState } from 'react'

import { Button } from '@/components/ui/button'
import { ALARM_STATES, type ActiveAlarm, type AlarmState } from '@/hooks/use-alarms'
import { forecastWarningDetailsUrl, type ForecastWarnings } from '@/hooks/use-forecast-warnings'
import { alarmConditionSentence } from '@/lib/alarm-display'
import { cn } from '@/lib/utils'

interface AlarmBannerProps {
  alarms: ActiveAlarm[]
  onOpen: () => void
  // Same signature the AlarmsDrawer already takes; acknowledging is
  // server-side (ADR 0038), so the banner can call it directly rather than
  // sending the operator to the drawer to silence a live alarm.
  onAcknowledge: (ruleId: string) => Promise<void>
  // The forecast-warnings payload App already holds (ADR 0087), read only to
  // resolve the worst shown alarm's own bulletin link (ADR 0114); the
  // banner never fetches it itself.
  forecastWarnings: ForecastWarnings | null
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
 *
 * The banner carries its own Acknowledge button (ADR 0114) rather than
 * making that mean a trip to the Alarms drawer, because acknowledging costs
 * the banner nothing extra to offer: it is a single server call, the same
 * one the drawer already makes, and a wall display or a phone at the helm
 * has no faster way to reach the drawer than the View button already sitting
 * beside it. Acknowledging still only silences the sound — the banner drops
 * back into its muted "all acknowledged, still live" variant afterwards, it
 * does not disappear.
 */
export const AlarmBanner = memo(function AlarmBanner({ alarms, onOpen, onAcknowledge, forecastWarnings }: AlarmBannerProps) {
  // Hooks run on every render regardless of the empty-alarms early return
  // below, which alarms.length can flip on the very next push.
  const [acking, setAcking] = useState(false)
  const [ackError, setAckError] = useState<string | null>(null)

  // The live alarm set as of the latest render. The acknowledge loop below
  // awaits between calls and closes over the alarms of the render the click
  // happened on, so it reads the current set through this ref instead of
  // acting on a snapshot the server has already moved past.
  const alarmsRef = useRef(alarms)
  alarmsRef.current = alarms

  // A refusal describes the alarm set it was raised against. Once that set
  // has moved on - the alarm cleared itself, or a different one replaced it
  // - the sentence is stale, and a stale refusal sitting under a live alarm
  // reads as a refusal of that alarm. Keyed on rule id and phase, not on the
  // array identity, which changes on every telemetry push regardless.
  const alarmsKey = alarms.map((alarm) => `${alarm.rule_id}:${alarm.phase}`).join('|')
  useEffect(() => {
    setAckError(null)
  }, [alarmsKey])

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

  // Everything shown that this banner can offer to acknowledge. In the
  // muted "all acknowledged, still live" variant every shown alarm already
  // has phase 'acknowledged', so this is always empty there and no button
  // renders — the same test the drawer's own per-card button uses.
  const acknowledgeable = shown.filter((alarm) => alarm.can_acknowledge && alarm.phase !== 'acknowledged')

  const acknowledge = async () => {
    setAckError(null)
    setAcking(true)
    try {
      // Sequential and fail-fast: stop at the first rejection rather than
      // racing every call, so a partial failure leaves the board in a state
      // the operator can reason about (these acknowledged, that one didn't).
      for (const alarm of acknowledgeable) {
        // Between the click and this alarm's turn the server may have
        // cleared it (a forecast warning that lapsed, a bus notification
        // withdrawn), and acknowledging one that is no longer live answers
        // 409 (alarm_service.go's errNotificationNotLive) - which in a
        // fail-fast loop would abandon the alarms that are still sounding
        // over an alarm that has already stopped. Nothing is swallowed
        // here: a refusal for an alarm that *is* still live still stops the
        // loop and surfaces below.
        const live = alarmsRef.current.find((current) => current.rule_id === alarm.rule_id)
        if (!live || !live.can_acknowledge || live.phase === 'acknowledged') continue
        await onAcknowledge(alarm.rule_id)
      }
    } catch (err) {
      setAckError(err instanceof Error ? err.message : String(err))
    } finally {
      setAcking(false)
    }
  }

  // The URL lives on the forecast-warnings payload, not on the alarm (ADR
  // 0114); null for every non-forecast alarm and for a forecast alarm with
  // no currently-active matching bulletin, in which case the sentence keeps
  // its own "Details on the Forecast page." clause instead.
  const forecastDetailsUrl = forecastWarningDetailsUrl(forecastWarnings, first.path)
  const sentence = alarmConditionSentence(first, { forecastDetailsLinked: forecastDetailsUrl !== null })

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
        {/* The sentence truncates, the link does not. `truncate` sets
            white-space: nowrap and overflow: hidden, so a link inside the
            same clipped line is simply gone at phone or kiosk width - with
            no horizontal scroll to reach it - which is exactly where this
            banner is read. Clipping the sentence and keeping the link is
            the one arrangement where both stay usable. */}
        <div className="flex min-w-0 items-baseline gap-1 text-xs">
          <span className="min-w-0 truncate">{sentence}</span>
          {forecastDetailsUrl && (
            // Text colour is inherited from the container above (destructive
            // while loud, muted-foreground once acknowledged), so only the
            // underline needs adding here to read as a link on either
            // background - same idiom as forecast-warning-notice.tsx's own
            // link, which sets the colour itself only because that component
            // has no equivalent tinted container to inherit from.
            <a
              href={forecastDetailsUrl}
              target="_blank"
              rel="noopener noreferrer"
              className="shrink-0 underline underline-offset-2 hover:opacity-80"
            >
              View details →
            </a>
          )}
        </div>
        {/* Wraps rather than truncates: this is the server's own refusal
            sentence ("an emergency cannot be acknowledged"), the whole
            reason the banner surfaces a failure at all, and half of it
            clipped is worse than the extra line it costs. */}
        {ackError && <p className="mt-0.5 wrap-break-word text-xs">{ackError}</p>}
      </div>
      <div className="flex shrink-0 items-center gap-2">
        {acknowledgeable.length > 0 && (
          <Button size="sm" variant="outline" className="shrink-0" onClick={() => void acknowledge()} disabled={acking}>
            {acknowledgeable.length === 1 ? 'Acknowledge' : 'Acknowledge all'}
          </Button>
        )}
        <Button size="sm" variant="outline" className="shrink-0" onClick={onOpen}>
          View
        </Button>
      </div>
    </div>
  )
})
