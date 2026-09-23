import { memo } from 'react'
import { Sunrise, Sunset } from 'lucide-react'

import { Tile } from '@/components/ui/tile'
import { useVesselIdentity, formatClock, formatDate, formatWeekday, isSameLocalDate } from '@/hooks/use-vessel-identity'
import { moonPhaseEmoji, moonPhaseLabel } from '@/lib/moon-phase'

/**
 * The clock tile's trip ETA (ADR 0092, ADR 0125): the whole trip's arrival,
 * not just the next leg. label is the FINAL waypoint's name for a
 * Helmcentral-activated route (etaToRouteEnd, lib/next-waypoint.ts), or a
 * bare chartplotter go-to destination's reverse-geocoded name
 * (etaToDestination) when no Helmcentral route is active - `null` when
 * that destination has no name yet (a fresh backend cache miss, or a
 * provider that never resolves one), in which case the ETA line shows just
 * the time rather than inventing a label.
 */
export interface ClockTileTripEta {
  label: string | null
  /** null when SOG (and, for a route, planning speed) can't produce an honest ETA (e.g. becalmed, or a bare destination with no SOG to steer by). */
  etaAt: Date | null
  basis: 'sog' | 'plan'
}

export interface ClockTileProps {
  /** Backend-formatted, e.g. "6:02AM" (forecast[0].sunriseTime). Null when the forecast has no fix for it. */
  sunriseTime: string | null
  sunsetTime: string | null
  moonPhase: string | null
  placeName: string | null
  tripEta: ClockTileTripEta | null
}

/** Drops :SS from a formatClock timePart ("14:32:07" -> "14:32"). */
function hhmm(timePart: string) {
  const [hh = '--', mm = '--'] = timePart.split(':')
  return `${hh}:${mm}`
}

/**
 * The wall display's clock page (ADR 0092, ADR 0125): time, date, sun and
 * moon, and where the boat is and where it's headed. Ticks its own clock
 * from useVesselIdentity() exactly like vessel-status-bar.tsx does, rather
 * than threading the vessel clock down as a prop - this is the one tile on
 * the wall that owns a per-second re-render, and every other tile stays
 * clear of it.
 *
 * The hero time is one step down from the drawer's own convention
 * (`text-6xl`, not `text-7xl`) and the place chip carries the ETA as a
 * second line rather than a separate margined block below it: at this
 * tile's placed height (h7 on the wall, minH 6 as its resize floor) the
 * original three-block, `text-7xl` layout measured taller than the tile
 * itself in a real browser (jsdom has no layout engine, so nothing in this
 * suite caught it) and clipped the ETA line off the bottom of the wall.
 * Both trims are votes for keeping the ETA genuinely visible over squeezing
 * out a few more pixels of hero digit.
 */
export const ClockTile = memo(function ClockTile({
  sunriseTime,
  sunsetTime,
  moonPhase,
  placeName,
  tripEta,
}: ClockTileProps) {
  const { now, clock, timeZone } = useVesselIdentity()
  // The header (vessel-status-bar.tsx) shares formatDate's full-weekday output via
  // currentDate; this tile is two grid columns wide (~300px) and a long weekday
  // ("SATURDAY, SEP 12, 2026") wraps to a second line there, pushing the place
  // chip below the tile's bottom edge. The compact option keeps formatDate's
  // weekday-first ordering but stays short enough to hold one line at minW.
  const compactDate = formatDate(now, { compact: true, timeZone }).toUpperCase()

  const etaLabel = (() => {
    if (!tripEta) return '—'
    if (!tripEta.etaAt) return tripEta.label ? `ETA ${tripEta.label} —` : 'ETA —'

    // HelmCast prefixed a multi-day passage's ETA with the arrival weekday
    // rather than leaving it looking like it lands today - carried forward
    // here now the trip ETA can span more than one day.
    const dayPrefix = isSameLocalDate(tripEta.etaAt, now, timeZone) ? '' : `${formatWeekday(tripEta.etaAt, timeZone)} `
    // Include the meridiem the same way the hero clock does (its own
    // `clock.meridiem` span) - a bare "09:40" can't be told from 9:40 PM,
    // and this line has no other AM/PM context of its own to lean on.
    const etaClock = formatClock(tripEta.etaAt, timeZone)
    const time = `${dayPrefix}${hhmm(etaClock.timePart)}${etaClock.meridiem ? ` ${etaClock.meridiem}` : ''}`
    const labelPart = tripEta.label ? `${tripEta.label} ` : ''
    const planSuffix = tripEta.basis === 'plan' ? ' (plan)' : ''
    return `ETA ${labelPart}${time}${planSuffix}`
  })()

  return (
    <Tile title="Clock">
      <div className="mt-1 flex h-full flex-col justify-between gap-1.5">
        <div>
          <time className="flex items-baseline gap-1 font-display leading-none tabular-nums text-gauge-secondary">
            <span className="text-6xl">{hhmm(clock.timePart)}</span>
            <span className="text-2xl">{clock.meridiem}</span>
            {/* UTC only ever means "no position fix yet" (vesselLocalTimezoneName's
                honest fallback, ADR 0035) — flagging it here explains an otherwise
                wrong-looking clock. A real vessel offset needs no such caveat. */}
            {timeZone === 'UTC' && (
              <span
                data-testid="clock-timezone-label"
                className="text-[10px] font-medium uppercase tracking-wider text-muted-foreground"
              >
                UTC
              </span>
            )}
          </time>
          <p
            data-testid="clock-date"
            className="mt-1 truncate whitespace-nowrap text-xl uppercase tracking-[0.08em] text-muted-foreground"
          >
            {compactDate}
          </p>
        </div>

        <div className="flex flex-wrap items-center gap-3 text-lg tabular-nums">
          <span className="flex items-center gap-1.5">
            <Sunrise className="h-4 w-4 text-gauge-primary" aria-hidden="true" />
            {sunriseTime ?? '—'}
          </span>
          <span className="flex items-center gap-1.5">
            <Sunset className="h-4 w-4 text-gauge-primary" aria-hidden="true" />
            {sunsetTime ?? '—'}
          </span>
          <span className="flex items-center gap-1.5">
            <span aria-hidden="true">{moonPhase ? moonPhaseEmoji(moonPhase) : '—'}</span>
            {moonPhase ? moonPhaseLabel(moonPhase) : null}
          </span>
        </div>

        {/* Place chip and trip ETA share one box (see the component doc
            comment above) rather than the ETA sitting in its own margined
            paragraph below it - the merge is what buys back the vertical
            room the layout fix needs. */}
        <div className="truncate rounded-md bg-gauge-secondary/10 px-3 py-1 leading-tight">
          <div className="truncate font-display text-lg text-gauge-secondary">{placeName ?? '—'}</div>
          <p data-testid="clock-eta" className="truncate text-[11px] text-muted-foreground">
            {etaLabel}
          </p>
        </div>
      </div>
    </Tile>
  )
})
