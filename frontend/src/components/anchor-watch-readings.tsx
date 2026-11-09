import type { ReactNode } from 'react'
import { useVesselStateSelector } from '@/hooks/use-vessel-state'
import { formatTimeOfDay, selectTimeZone } from '@/lib/vessel-time'
import { ArrowDown, ArrowUp } from 'lucide-react'
import type { TideToday } from '@/hooks/use-tide-today'
import {
  calibrationNotReadyText,
  lowWaterClearanceReasonLabel,
  readingsSpanText,
  underKeelPhrase,
  underKeelRangePhrase,
  type LowWaterClearanceResult,
} from '@/lib/low-water-clearance'
import { formatDataAge, isStale } from '@/lib/staleness'
import { estimateDepthAtNextTurn, nextTideExtreme } from '@/lib/tide-estimate'
import { isMeanSeaLevel, MEAN_SEA_LEVEL_LABEL } from '@/lib/tide-datum'
import { depthValueText, feetToMeters } from '@/lib/units'
import { cn } from '@/lib/utils'

/** A tide height in feet, converted to the host's chosen unit — matches depth-tide-tile.tsx's own conversion. */
function tideValueDisplay(heightFt: number, isImperial: boolean): string {
  return isImperial ? heightFt.toFixed(1) : feetToMeters(heightFt).toFixed(2)
}

/** A tide time as the host reads it on the boat's clock, e.g. "9.13 AM". */
function clockTime(iso: string | number, timeZone: string | undefined): string {
  return formatTimeOfDay(iso, timeZone)
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
  /** The server's verdict (ADR 0272); null until the first answer lands. After a failed poll it is the last one, and lowWaterFailed is true. */
  lowWaterClearance: LowWaterClearanceResult | null
  /** When the server worked that verdict out, RFC 3339; null until the first answer. */
  lowWaterWorkedOutAt: string | null
  /** True when the server could not give a verdict at all, as opposed to giving an unknown one. */
  lowWaterFailed: boolean
  isImperial: boolean
  /** Card only: the no-WebGL2 Adjust text button, beside the tide. */
  adjustAction?: ReactNode
  /** True when Drop could not place the anchor mark at the bow because there was no heading. */
  noHeadingAtDrop: boolean
}

/** Depth, tide and low-water clearance, as one presenter for both homes. */
export function AnchorWatchReadings({
  variant,
  depthMeters,
  depthLastUpdateAgeS,
  tide,
  lowWaterClearance,
  lowWaterWorkedOutAt,
  lowWaterFailed,
  isImperial,
  adjustAction,
  noHeadingAtDrop,
}: AnchorWatchReadingsProps) {
  const timeZone = useVesselStateSelector(selectTimeZone)
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
  const warn = hud ? 'text-amber-400' : 'text-amber-600 dark:text-amber-400'

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
              warn,
            )}
          >
            Stale{depthStaleLabel ? ` ${depthStaleLabel}` : ''}
          </span>
        )}
      </p>
      <p className={cn('font-display leading-none tabular-nums text-gauge-primary', hud ? 'text-3xl' : 'text-4xl')}>
        {depthMeters !== null ? depthValueText(depthMeters, isImperial) : '—'}
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
      <span className={quiet}> {tideValueDisplay(nextTurn.heightFt, isImperial)} {lengthUnit}{isMeanSeaLevel(tide?.height_datum) ? ` ${MEAN_SEA_LEVEL_LABEL}` : ''}</span>
      {' · '}
      {clockTime(nextTurn.time, timeZone)}
    </>
  )

  const estimate = estimatedDepthAtNextTurn !== null && (
    <span className="font-semibold text-gauge-secondary">
      {depthValueText(estimatedDepthAtNextTurn, isImperial)} {lengthUnit}
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
  // After a failed poll the last verdict stays, marked as not updated since the
  // time the server worked it out, and drawn faded. It is a labelled stale
  // display. Before any good answer there is nothing to show but the failure.
  const stale = lowWaterFailed && lowWaterClearance !== null
  const staleSuffix = stale && lowWaterWorkedOutAt !== null ? ` · not updated since ${clockTime(lowWaterWorkedOutAt, timeZone)}` : ''
  const staleStyle = stale ? 'italic opacity-70' : ''
  const quietLine = cn('truncate text-xs', quiet, staleStyle, hud ? 'hidden border-t border-white/10 px-3 py-1.5 sm:block' : 'mt-2')
  // A calibrated clearance shows its range. A too-shallow warning on the boat's
  // own cell also shows the bottom of the range, which is what it is judged on.
  const length = { format: (m: number) => depthValueText(m, isImperial), unit: lengthUnit }
  const clearanceText = (v: Exclude<LowWaterClearanceResult, { status: 'unknown' }>, tooShallow: boolean) => {
    if (v.depthAtLowHalfWidthM === undefined) return underKeelPhrase(v.clearanceM)
    // A too-shallow line shows the figure the verdict rests on: the boat's cell,
    // the station estimate, or the shallowest sounded spot.
    if (tooShallow && v.verdictClearanceM !== undefined) {
      return v.verdictHalfWidthM === undefined
        ? underKeelPhrase(v.verdictClearanceM)
        : underKeelRangePhrase(v.verdictClearanceM, v.verdictHalfWidthM, true, length)
    }
    return underKeelRangePhrase(v.clearanceM, v.depthAtLowHalfWidthM, false, length)
  }
  const clearance = (
    <>
      {lowWaterFailed && lowWaterClearance === null && (
        <p data-testid="low-water-clearance" className={quietLine}>
          Low-water clearance unavailable
        </p>
      )}
      {lowWaterClearance?.status === 'too_shallow' && (
        <div
          data-testid="low-water-clearance"
          className={cn(
            'truncate text-xs',
            staleStyle,
            hud
              ? 'border-t border-red-500/40 bg-red-500/20 px-3 py-1.5 text-red-300'
              : 'mt-2 rounded-md border border-red-500/40 bg-red-500/10 px-2 py-1 text-red-600 dark:text-red-400',
          )}
        >
          Too shallow at low water · {clearanceText(lowWaterClearance, true)} at {clockTime(lowWaterClearance.lowTideTime, timeZone)}
          {lowWaterClearance.cautiousSource === 'shallowest_sounded' && ' · set by the shallowest sounded spot'}
          {lowWaterClearance.cautiousSource === 'station' && ' · set by the tide station estimate'}
          {staleSuffix}
        </div>
      )}
      {lowWaterClearance?.status === 'ok' && (
        <p data-testid="low-water-clearance" className={quietLine}>
          {clearanceText(lowWaterClearance, false)} at low water · {clockTime(lowWaterClearance.lowTideTime, timeZone)}
          {staleSuffix}
        </p>
      )}
      {lowWaterClearance?.status === 'unknown' && lowWaterClearance.reason !== 'not_anchored' && (
        <p data-testid="low-water-clearance" className={quietLine}>
          {lowWaterClearanceReasonLabel(lowWaterClearance.reason)}
          {staleSuffix}
        </p>
      )}
    </>
  )

  // Where the low-water depth comes from (ADR 0274): the station's prediction,
  // labelled as such, until the sounder's own readings at this anchorage have
  // narrowed to better than the margin; then the calibrated estimate with its
  // 95% interval. The shallowest and deepest sounded spots, the next high and the
  // tide-match note come with it. Wrapped lines, because the figures run long.
  const verdict = lowWaterClearance !== null && lowWaterClearance.status !== 'unknown' ? lowWaterClearance : null
  const calibration = verdict?.calibration
  const depthText = (m: number) => depthValueText(m, isImperial)
  const withInterval = (f: { depthM: number; halfWidthM: number }) => `${depthText(f.depthM)} ${lengthUnit} ±${depthText(f.halfWidthM)}`
  const noteLine = cn('text-xs', quiet, staleStyle, hud ? 'hidden w-0 min-w-full border-t border-white/10 px-3 py-1.5 sm:block' : 'mt-1')
  let methodText: string | null = null
  if (verdict !== null) {
    if (calibration?.ready && verdict.depthAtLowHalfWidthM !== undefined) {
      methodText = `Locally calibrated depth estimate (${readingsSpanText(calibration.hoursOfReadings)} of readings) · ${withInterval({ depthM: verdict.depthAtLowM, halfWidthM: verdict.depthAtLowHalfWidthM })}`
    } else {
      methodText = `Depth based on tide station prediction · ${depthText(verdict.depthAtLowM)} ${lengthUnit}`
      if (calibration && !calibration.ready && calibration.notReady) {
        methodText += ` · ${calibrationNotReadyText(calibration.notReady, calibration.hoursOfReadings)}`
      }
    }
  }
  const calibrationLines = methodText !== null && (
    <>
      <p data-testid="low-water-method" className={noteLine}>
        {methodText}
        {staleSuffix}
      </p>
      {calibration?.shallowestDiffers && calibration.shallowestSounded && (
        <p data-testid="low-water-shallowest" className={noteLine}>
          Shallowest sounded spot {withInterval(calibration.shallowestSounded)} at low water
        </p>
      )}
      {calibration?.highAtBoat && calibration.highTideTime && (
        <p data-testid="high-water-depth" className={noteLine}>
          High water {withInterval(calibration.highAtBoat)} · {clockTime(calibration.highTideTime, timeZone)}
          {calibration.deepestDiffers && calibration.deepestSounded && <> · deepest sounded spot {withInterval(calibration.deepestSounded)}</>}
        </p>
      )}
      {calibration?.stationMatchNote && (
        <p data-testid="tide-match-note" className={noteLine}>
          {calibration.stationMatchNote}
        </p>
      )}
    </>
  )

  // Always shown, phone included: the mark is somewhere other than where the
  // operator assumes. Moving it needs the map, which only the hud variant has,
  // so only that variant points to Adjust. w-0 min-w-full lets the note wrap
  // to the width the other rows set instead of widening the shrink-to-fit panel.
  const bowWarning = noHeadingAtDrop && (
    <p
      role="note"
      className={cn('text-xs', hud ? 'w-0 min-w-full border-t border-white/10 px-3 py-1.5' : 'mt-2', warn)}
    >
      Anchor mark is at the GPS, not the bow. There was no heading at Drop.{hud && ' Move it with Adjust.'}
    </p>
  )

  if (hud) {
    return (
      <div data-testid="anchor-watch-hud-header" className="max-w-[calc(100vw-7rem)] sm:max-w-none">
        {depthBlock}
        <div className="border-t border-white/10 px-3 py-1.5">{tideBlock}</div>
        {clearance}
        {calibrationLines}
        {bowWarning}
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
      {calibrationLines}
      {bowWarning}
    </div>
  )
}
