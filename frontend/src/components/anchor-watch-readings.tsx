import type { ReactNode } from 'react'
import { ArrowDown, ArrowUp } from 'lucide-react'
import type { TideToday } from '@/hooks/use-tide-today'
import { lowWaterClearanceReasonLabel, underKeelPhrase, type computeLowWaterClearance } from '@/lib/low-water-clearance'
import { formatDataAge, isStale } from '@/lib/staleness'
import { estimateDepthAtNextTurn, nextTideExtreme } from '@/lib/tide-estimate'
import { feetToMeters, metersToFeet } from '@/lib/units'
import { cn } from '@/lib/utils'

/** A tide height in feet, converted to the host's chosen unit — matches depth-tide-tile.tsx's own conversion. */
function tideValueDisplay(heightFt: number, isImperial: boolean): string {
  return isImperial ? heightFt.toFixed(1) : feetToMeters(heightFt).toFixed(2)
}

/**
 * A depth-shaped figure (the hero reading, or the Est. high/low projection)
 * in the host's chosen unit — matches depth-tide-tile.tsx's own conversion,
 * one decimal place either way.
 */
function depthValueDisplay(meters: number, isImperial: boolean): string {
  return (isImperial ? metersToFeet(meters) : meters).toFixed(1)
}

/** A tide time as the host reads it on a clock, e.g. "9:13 AM". */
function clockTime(iso: string | number): string {
  return new Date(iso).toLocaleTimeString('en-US', { hour: 'numeric', minute: '2-digit' })
}

interface AnchorWatchReadingsProps {
  /**
   * 'hud' is the compact version for the map's black top-left panel
   * (ADR 0212); 'card' is the standalone card shown when there is no map to
   * carry it. The figures and their wording come from the one place below;
   * only the layout classes differ.
   */
  variant: 'hud' | 'card'
  depthMeters: number | null
  depthLastUpdateAgeS: number | null
  tide: TideToday | null
  lowWaterClearance: ReturnType<typeof computeLowWaterClearance>
  isImperial: boolean
  /** Card only: the no-WebGL2 Adjust text button, beside the tide. */
  adjustAction?: ReactNode
}

/** Depth, tide and low-water clearance, as one presenter for both homes. */
export function AnchorWatchReadings({
  variant,
  depthMeters,
  depthLastUpdateAgeS,
  tide,
  lowWaterClearance,
  isImperial,
  adjustAction,
}: AnchorWatchReadingsProps) {
  const hud = variant === 'hud'
  const depthStale = isStale(depthLastUpdateAgeS)
  const depthStaleLabel = formatDataAge(depthLastUpdateAgeS)
  const nextTurn = tide !== null ? nextTideExtreme(tide) : null
  const estimatedDepthAtNextTurn = tide !== null ? estimateDepthAtNextTurn(depthMeters, tide) : null
  const tideIsRising = tide?.tide_direction === 'Rising'
  const lengthUnit = isImperial ? 'ft' : 'm'
  const tideLabel = `Tide${tide?.station_name ? ` · ${tide.station_name}` : ''}`

  const label = hud ? 'text-[10px] uppercase tracking-[0.16em] text-white/80' : 'text-[10px] uppercase tracking-[0.16em] text-muted-foreground'
  const quiet = hud ? 'text-white/80' : 'text-muted-foreground'
  const body = hud ? 'text-white' : 'text-foreground'

  const depthBlock = (
    <div className={cn(hud ? 'flex items-center justify-between gap-4 px-3 py-1.5' : 'min-w-0', depthStale && 'grayscale')}>
      <p className={cn('flex items-center gap-1', label)}>
        Depth
        {depthStale && (
          <span
            data-testid="anchor-watch-depth-stale-badge"
            title={depthStaleLabel ? `No update for ${depthStaleLabel}` : 'Source has stopped updating'}
            className={cn(
              'shrink-0 rounded-xs border border-amber-500/40 bg-amber-500/10 px-1.5 py-0.5 text-xs normal-case leading-none tracking-normal',
              hud ? 'text-amber-400' : 'text-amber-600 dark:text-amber-400',
            )}
          >
            Stale{depthStaleLabel ? ` ${depthStaleLabel}` : ''}
          </span>
        )}
      </p>
      <p className={cn('font-display leading-none tabular-nums text-gauge-primary', hud ? 'text-3xl' : 'text-4xl')}>
        {depthMeters !== null ? depthValueDisplay(depthMeters, isImperial) : '—'}
        <span className={cn(quiet, hud ? 'ml-1 text-sm' : 'ml-2 align-baseline text-lg')}>
          {depthMeters !== null ? lengthUnit : 'unavailable'}
        </span>
      </p>
    </div>
  )

  const direction = tide !== null && nextTurn !== null && (
    <p className={cn('inline-flex shrink-0 items-center gap-1 font-semibold text-gauge-secondary', hud ? 'text-sm' : 'mt-1 text-sm')}>
      {tideIsRising ? <ArrowUp className="h-3.5 w-3.5" aria-hidden="true" /> : <ArrowDown className="h-3.5 w-3.5" aria-hidden="true" />}
      {tide.tide_direction}
    </p>
  )

  // nextTurn only ever holds an extreme tideExtremesByTime already found
  // usable (lib/tide-estimate.ts drops the -1-sentinel/no-time case before
  // this reads it), so its height is always real here — including a genuine
  // negative low, which must still show its figure rather than being read
  // as "no data".
  const turnLine = nextTurn !== null && (
    <>
      {nextTurn.isHigh ? 'High' : 'Low'}
      <span className={quiet}> {tideValueDisplay(nextTurn.heightFt, isImperial)} {lengthUnit}</span>
      {' · '}
      {clockTime(nextTurn.time)}
    </>
  )

  const estimate = estimatedDepthAtNextTurn !== null && (
    <span className="font-semibold text-gauge-secondary">
      {depthValueDisplay(estimatedDepthAtNextTurn, isImperial)} {lengthUnit}
    </span>
  )

  const tideBlock = tide !== null && nextTurn !== null ? (
    hud ? (
      <>
        <div className="flex items-center justify-between gap-3">
          <p className={cn('min-w-0 truncate', label)}>{tideLabel}</p>
          {direction}
        </div>
        <p className={cn('mt-0.5 truncate text-xs', body)}>
          {turnLine}
          {estimate && (
            <>
              {' · '}
              <span className={quiet}>est.</span> {estimate}
            </>
          )}
        </p>
      </>
    ) : (
      <>
        <p className={cn('truncate', label)}>{tideLabel}</p>
        {direction}
        <p className={cn('mt-1 truncate text-xs', body)}>{turnLine}</p>
        {estimate && (
          <p className={cn('truncate text-xs', body)}>
            <span className={quiet}>{tideIsRising ? 'Est. high' : 'Est. low'}</span> {estimate}
          </p>
        )}
      </>
    )
  ) : (
    <p className={cn('text-xs', quiet)}>No tide station</p>
  )

  // Anchor Watch's low-water clearance warning (ADR 0135): the depth at the
  // boat's current position, projected to the next low tide, against the
  // operator's configured margin. A display warning, not an alarm rule. On
  // the HUD the quiet lines drop out below sm so the panel doesn't cover
  // half a phone's map; the too-shallow alert never does.
  const quietLine = cn('truncate text-xs', quiet, hud ? 'hidden border-t border-white/10 px-3 py-1.5 sm:block' : 'mt-2')
  const clearance = (
    <>
      {lowWaterClearance.status === 'too_shallow' && (
        <div
          data-testid="low-water-clearance"
          className={cn(
            'truncate text-xs',
            hud
              ? 'border-t border-red-500/40 bg-red-500/20 px-3 py-1.5 text-red-300'
              : 'mt-2 rounded-md border border-red-500/40 bg-red-500/10 px-2 py-1 text-red-600 dark:text-red-400',
          )}
        >
          Too shallow at low water · {underKeelPhrase(lowWaterClearance.clearanceM)} at {clockTime(lowWaterClearance.lowTideTime)}
        </div>
      )}
      {lowWaterClearance.status === 'ok' && (
        <p data-testid="low-water-clearance" className={quietLine}>
          {underKeelPhrase(lowWaterClearance.clearanceM)} at low water · {clockTime(lowWaterClearance.lowTideTime)}
        </p>
      )}
      {lowWaterClearance.status === 'unknown' && (
        <p data-testid="low-water-clearance" className={quietLine}>
          {lowWaterClearanceReasonLabel(lowWaterClearance.reason)}
        </p>
      )}
    </>
  )

  if (hud) {
    return (
      <div data-testid="anchor-watch-hud-header" className="max-w-[calc(100vw-7rem)] sm:max-w-none">
        {depthBlock}
        <div className="border-t border-white/10 px-3 py-1.5">{tideBlock}</div>
        {clearance}
      </div>
    )
  }

  return (
    <div data-testid="anchor-watch-header" className="rounded-md border bg-background/60 px-3 py-3">
      <div className="flex flex-wrap items-start gap-4">
        {depthBlock}
        <div className="min-w-0 flex-1">{tideBlock}</div>
        {adjustAction}
      </div>
      {clearance}
    </div>
  )
}
