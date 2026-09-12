import { memo } from 'react'
import { Sunrise, Sunset } from 'lucide-react'

import { Tile } from '@/components/ui/tile'
import { useVesselIdentity, formatClock, formatDate } from '@/hooks/use-vessel-identity'
import { moonPhaseEmoji, moonPhaseLabel } from '@/lib/moon-phase'

export interface ClockTileNextWaypoint {
  label: string
  /** null when SOG and planning speed can't produce an honest ETA (e.g. becalmed). */
  etaAt: Date | null
  basis: 'sog' | 'plan'
}

export interface ClockTileProps {
  /** Backend-formatted, e.g. "6:02AM" (forecast[0].sunriseTime). Null when the forecast has no fix for it. */
  sunriseTime: string | null
  sunsetTime: string | null
  moonPhase: string | null
  placeName: string | null
  nextWaypoint: ClockTileNextWaypoint | null
}

/** Drops :SS from a formatClock timePart ("14:32:07" -> "14:32"). */
function hhmm(timePart: string) {
  const [hh = '--', mm = '--'] = timePart.split(':')
  return `${hh}:${mm}`
}

/**
 * The wall display's clock page (ADR 0092): time, date, sun and moon, and
 * where the boat is and where it's headed. Ticks its own clock from
 * useVesselIdentity() exactly like vessel-status-bar.tsx does, rather than
 * threading the vessel clock down as a prop - this is the one tile on the
 * wall that owns a per-second re-render, and every other tile stays clear
 * of it.
 */
export const ClockTile = memo(function ClockTile({
  sunriseTime,
  sunsetTime,
  moonPhase,
  placeName,
  nextWaypoint,
}: ClockTileProps) {
  const { now, clock, timeZone } = useVesselIdentity()
  // The header (vessel-status-bar.tsx) shares formatDate's full-weekday output via
  // currentDate; this tile is two grid columns wide (~300px) and a long weekday
  // ("SATURDAY, SEP 12, 2026") wraps to a second line there, pushing the place
  // chip below the tile's bottom edge. The compact option keeps formatDate's
  // weekday-first ordering but stays short enough to hold one line at minW.
  const compactDate = formatDate(now, { compact: true, timeZone }).toUpperCase()
  const etaLabel = nextWaypoint
    ? `ETA ${nextWaypoint.label} ${nextWaypoint.etaAt ? hhmm(formatClock(nextWaypoint.etaAt).timePart) : '—'}${nextWaypoint.basis === 'plan' ? ' (plan)' : ''}`
    : '—'

  return (
    <Tile title="Clock">
      <div className="mt-1 flex h-full flex-col justify-between gap-2">
        <div>
          <time className="flex items-baseline gap-1 font-display leading-none tabular-nums text-gauge-secondary">
            <span className="text-7xl">{hhmm(clock.timePart)}</span>
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

        <div>
          <div className="truncate rounded-md bg-gauge-secondary/10 px-3 py-1.5 font-display text-lg text-gauge-secondary">
            {placeName ?? '—'}
          </div>
          <p data-testid="clock-eta" className="mt-1 truncate text-[11px] text-muted-foreground">
            {etaLabel}
          </p>
        </div>
      </div>
    </Tile>
  )
})
