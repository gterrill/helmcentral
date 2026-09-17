import { memo } from 'react'
import { Tile } from '@/components/ui/tile'
import { formatDataAge, isStale } from '@/lib/staleness'
import { hoursToBand, socSeverity, type SocBands } from '@/lib/soc-bands'
import { severityFill, severityTextClass } from '@/lib/severity'
import { projectSocAtDawn, type OvernightProjection } from '@/lib/soc-projection'

export interface BatteryPowerTileProps {
  batterySocPercent: number | null
  chargingCurrentA: number | null
  chargingPowerW: number | null
  solarOutputW: number | null
  acOutputW: number | null
  dc12vPowerW: number | null
  dc24vVoltageV: number | null
  charger0CurrentA: number | null
  charger0AcIn1CurrentA: number | null
  charger0ChargingMode: string | null
  charger0Error: string | null
  batteryRatePercentPerHour: number | null
  timeToGoHours: number | null
  /** Seconds since the electrical feed last reported, or null if unknown. */
  lastUpdateAgeS: number | null
  /**
   * Warn/alarm SoC thresholds derived from the operator's alarm rules
   * (use-soc-bands.ts). Undefined or both-null means no rule exists yet, and
   * the tile falls back to today's plain, uncoloured behaviour rather than
   * inventing a threshold.
   */
  socBands?: SocBands
  /**
   * The overnight dawn projection from use-overnight-projection.ts.
   * Undefined or null means the backend has no sunrise for the vessel's
   * position (a 503) or the response didn't parse, and the tile omits the
   * whole Dawn segment rather than showing a guess. When present but its
   * basis is 'none' (not enough history and the linear fallback is off),
   * the segment still renders with a dash and the backend's reason.
   */
  overnight?: OvernightProjection | null
}

/**
 * Title for the history-basis Dawn tooltip: the nights used out of the
 * lookback window, plus, when the backend actually dropped any, why some of
 * the window's nights weren't clean enough to use.
 */
function historyBasisTitle(overnightProjection: OvernightProjection): string {
  const { nightsUsed, lookbackNights, nightsExcludedShore, nightsExcludedGenerator } = overnightProjection
  const skippedClause = nightsExcludedShore + nightsExcludedGenerator > 0
    ? `; skipped ${nightsExcludedShore} on shore power and ${nightsExcludedGenerator} with the generator running`
    : ''
  return `Median of ${nightsUsed} clean nights from the last ${lookbackNights}${skippedClause}, then the current rate until sunset`
}

function hasChargerError(errorValue: string | null): boolean {
  if (errorValue === null) {
    return false
  }
  const normalized = errorValue.trim().toLowerCase()
  return normalized !== '' && normalized !== 'none' && normalized !== '0' && normalized !== 'ok'
}

function formatTimeToGo(hours: number | null) {
  if (hours === null || !Number.isFinite(hours)) {
    return '—'
  }

  const absHours = Math.abs(hours)
  const totalMinutes = Math.max(0, Math.round(absHours * 60))
  if (totalMinutes === 0) {
    return '—'
  }

  if (absHours >= 24 * 7) {
    return `${Math.round(absHours / (24 * 7))}w`
  }

  if (absHours >= 24) {
    const days = Math.floor(absHours / 24)
    let remainingHours = Math.round(absHours - days * 24)
    if (remainingHours === 24) {
      remainingHours = 0
    }
    if (remainingHours === 0) {
      return `${days}d`
    }
    return `${days}d ${remainingHours}h`
  }

  if (absHours >= 10) {
    return `${Math.round(absHours)}h`
  }

  const hh = Math.floor(totalMinutes / 60)
  const mm = totalMinutes % 60

  return `${hh}h ${mm.toString().padStart(2, '0')}m`
}

/**
 * Every reading blanked. When the feed is stale the values below are
 * historical artefacts, and routing them through this instead of the live
 * props reuses the existing "no value" formatting rather than repeating a
 * stale branch at each of the readouts.
 */
const NO_READINGS = {
  batterySocPercent: null,
  chargingCurrentA: null,
  chargingPowerW: null,
  solarOutputW: null,
  acOutputW: null,
  dc12vPowerW: null,
  dc24vVoltageV: null,
  charger0CurrentA: null,
  charger0AcIn1CurrentA: null,
  charger0ChargingMode: null,
  charger0Error: null,
  batteryRatePercentPerHour: null,
  timeToGoHours: null,
} as const

export const BatteryPowerTile = memo(function BatteryPowerTile(props: BatteryPowerTileProps) {
  const feedStale = isStale(props.lastUpdateAgeS)
  const {
    batterySocPercent,
    chargingCurrentA,
    chargingPowerW,
    solarOutputW,
    acOutputW,
    dc12vPowerW,
    dc24vVoltageV,
    charger0CurrentA,
    charger0AcIn1CurrentA,
    charger0ChargingMode,
    charger0Error,
    batteryRatePercentPerHour,
    timeToGoHours,
  } = feedStale ? NO_READINGS : props

  // No rule on the pinned SoC path means no bands, so the tile falls back to
  // today's plain, uncoloured reading rather than inventing a threshold.
  const socBands = props.socBands ?? { warnBelow: null, alarmBelow: null }
  // Bands are configuration, not a reading, so they still draw on a stale
  // feed; the severity check below uses the (possibly blanked) SoC value, so
  // the numeral and bar fill still go plain the moment the feed is stale.
  const socSeverityState = socSeverity(batterySocPercent, socBands)
  const socClass = severityTextClass(socSeverityState, 'text-gauge-primary')

  const socLabel = batterySocPercent !== null ? Math.round(batterySocPercent).toString() : '—'
  const socBarWidth = `${Math.max(0, Math.min(100, batterySocPercent ?? 0))}%`
  const socFillStyle: { width: string; backgroundColor?: string } = { width: socBarWidth }
  if (socSeverityState !== null) {
    socFillStyle.backgroundColor = severityFill(socSeverityState)
  }

  const chargingCurrentLabel = chargingCurrentA !== null
    ? `${chargingCurrentA >= 0 ? '+' : '-'}${Math.abs(chargingCurrentA).toFixed(1)}`
    : '—'
  const chargingPowerLabel = chargingPowerW !== null
    ? `${chargingPowerW >= 0 ? '+' : '-'}${Math.abs(Math.round(chargingPowerW))}`
    : '—'
  // Discharging at anchor is the ordinary state, not a fault: text-amber-600
  // is the alert palette and belongs to actual alert thresholds (a charger
  // error, an over-temp alternator), not to a battery quietly running the
  // fridge. Net, the rate and time-to-go all read as the same teal readout
  // token regardless of direction; the sign on the number and the "To
  // full"/"To empty" label already carry which way the current is flowing.
  const readoutClass = 'text-gauge-secondary'

  const solarOutputLabel = solarOutputW !== null ? Math.round(solarOutputW).toString() : '—'
  const acOutputLabel = acOutputW !== null ? Math.round(acOutputW).toString() : '—'
  const dc12vPowerLabel = dc12vPowerW !== null ? Math.round(dc12vPowerW).toString() : '—'
  // A missing load component makes the total unknown, not a partial sum:
  // reporting the AC draw alone as "the total" when the DC channel hasn't
  // reported would understate what the bank is actually feeding.
  const loadsTotalLabel = acOutputW !== null && dc12vPowerW !== null
    ? Math.round(acOutputW + dc12vPowerW).toString()
    : '—'

  const dc24vVoltageLabel = dc24vVoltageV !== null ? dc24vVoltageV.toFixed(2) : '—'
  const chargeRateLabel = batteryRatePercentPerHour !== null
    ? `${batteryRatePercentPerHour >= 0 ? '+' : ''}${batteryRatePercentPerHour.toFixed(1)}`
    : '—'

  // Direction now lives in the label rather than a battery glyph. The rate's
  // sign decides it; when the rate hasn't reported but a signed time-to-go
  // has, fall back to that, since use-electrical-state.ts already signs it
  // the same way (positive hours to full, negative hours to empty). With no
  // value to show, the label reads "—" too rather than guessing a direction.
  const timeToGoIsToFull = batteryRatePercentPerHour !== null
    ? batteryRatePercentPerHour > 0
    : timeToGoHours !== null && timeToGoHours > 0
  // While discharging, a configured band below the current SoC is a more
  // useful figure than time to fully flat: hoursToBand already returns null
  // while charging, with no rate, or with no lower band to reach, so this
  // falls through to the plain "To empty" figure exactly as before in those
  // cases.
  const bandTarget = hoursToBand(batterySocPercent, batteryRatePercentPerHour, socBands)
  const timeToGoValue = bandTarget
    ? formatTimeToGo(bandTarget.hours)
    : formatTimeToGo(timeToGoHours !== null ? Math.abs(timeToGoHours) : null)
  const timeToGoLabel = timeToGoValue === '—'
    ? '—'
    : bandTarget
      ? `To ${Math.round(bandTarget.targetPercent)}%`
      : (timeToGoIsToFull ? 'To full' : 'To empty')

  // The charger becomes one status line rather than four separate fields:
  // shown when any of them has reported, so an error surfaces even if the
  // current channel hasn't, and gone entirely at anchor with no shore power.
  const shoreHasError = hasChargerError(charger0Error)
  const shoreModeText = shoreHasError
    ? charger0Error
    : (typeof charger0ChargingMode === 'string' && charger0ChargingMode.length > 0 ? charger0ChargingMode : null)
  const shoreAcInText = charger0AcIn1CurrentA !== null ? `${charger0AcIn1CurrentA.toFixed(1)} A` : null
  const shoreVisible = charger0CurrentA !== null
    || charger0AcIn1CurrentA !== null
    || charger0ChargingMode !== null
    || charger0Error !== null

  // The dawn estimate reads the same (possibly blanked) SoC and rate as the
  // rest of the tile above, so a stale feed yields a dash here too rather
  // than projecting forward from a frozen reading as if it were live.
  // overnight is null on a 503 (no vessel position) or an unparsable
  // response, and in that case the whole Dawn segment is omitted below
  // instead of showing a guess.
  const overnightProjection = props.overnight ?? null
  const dawn = projectSocAtDawn({
    socPercent: batterySocPercent,
    liveRatePercentPerHour: batteryRatePercentPerHour,
    projection: overnightProjection,
    now: new Date(),
  })
  // Teal by default, ladder colour when the estimate itself lands in a
  // band: an amber dawn figure is the cue to run the generator before bed.
  const dawnEstimateClass = dawn !== null
    ? severityTextClass(socSeverity(dawn.socPercent, socBands), 'text-gauge-secondary')
    : 'text-muted-foreground'
  const dawnEstimateText = dawn !== null ? `${Math.round(dawn.socPercent)}%` : '—'
  const dawnBasisLabel = dawn === null
    ? ''
    : dawn.basis === 'history'
      ? `${overnightProjection?.nightsUsed ?? 0} nights`
      : 'at current rate'

  // charger0AcIn1CurrentA has already been through the feedStale swap above,
  // so it reads null on a stale feed and this is false without a separate
  // staleness check here. A charger actually drawing current in means the
  // bank isn't going to spend tonight discharging, so projecting a dawn
  // figure from the discharge rate would be the same mistake the overnight
  // history basis just learned to exclude from its own nights, applied to
  // the night ahead instead of a night behind.
  const onShorePower = charger0AcIn1CurrentA !== null && charger0AcIn1CurrentA > 0.5

  return (
    <Tile
      title="Battery & Power"
      // The tile edge carries the same SoC band the numeral and bar already
      // colour (ADR 0081): warn and alarm map straight across; in band or
      // with no rule configured, the tile carries no state.
      state={socSeverityState ?? 'normal'}
      stale={feedStale}
      staleLabel={formatDataAge(props.lastUpdateAgeS)}
    >
      <div className="mt-1 space-y-2">
        <div className="rounded-md border bg-background/60 px-3 py-3">
          <div className="flex items-start justify-between gap-2">
            <div className="flex min-w-0 items-baseline gap-1">
              <span className={`font-display text-6xl leading-none tabular-nums md:text-5xl lg:text-7xl ${socClass}`}>
                {socLabel}
              </span>
              <span className="shrink-0 font-display text-2xl leading-none text-muted-foreground md:text-xl md:leading-7 lg:text-2xl lg:leading-8">
                %
              </span>
            </div>
            <div className="min-w-0 shrink-0 text-right">
              <p className="truncate text-[10px] uppercase tracking-[0.16em] text-muted-foreground">
                {timeToGoLabel}
              </p>
              <p className={`mt-1 truncate font-display text-2xl leading-none tabular-nums md:text-xl md:leading-7 lg:text-3xl lg:leading-9 ${readoutClass}`}>
                {timeToGoValue}
              </p>
            </div>
          </div>
          <div className="relative mt-3 h-2 overflow-hidden rounded-full bg-muted/60">
            {socBands.alarmBelow !== null && (
              <div
                data-testid="soc-band-alarm"
                className="absolute inset-y-0 left-0 bg-red-600/15"
                style={{ width: `${socBands.alarmBelow}%` }}
              />
            )}
            {socBands.warnBelow !== null && (
              <div
                data-testid="soc-band-warn"
                className="absolute inset-y-0 bg-amber-500/15"
                style={{
                  left: `${socBands.alarmBelow ?? 0}%`,
                  width: `${Math.max(0, socBands.warnBelow - (socBands.alarmBelow ?? 0))}%`,
                }}
              />
            )}
            <div
              className={`h-full rounded-full ${socSeverityState === null ? 'bg-gauge-primary' : ''}`}
              style={socFillStyle}
            />
          </div>
          {/* Two rows, not one: at the 768 band a single row has to truncate and the
              first thing the ellipsis eats is the dawn percentage, which is the
              figure the footer exists to show. */}
          <div className="mt-2 space-y-1 font-display text-[11px] tabular-nums text-muted-foreground">
            <span className="block truncate">
              {dc24vVoltageLabel}
              <span className="font-display text-muted-foreground">V</span>
              {' · '}
              <span className={readoutClass}>{chargeRateLabel}</span>
              <span className="ml-1 font-display text-muted-foreground">%/h</span>
            </span>
            {overnightProjection !== null && (
              <span
                className="block truncate"
                title={
                  onShorePower
                    ? 'The charger is on shore power; the bank is not expected to discharge overnight'
                    : dawn === null
                      ? (overnightProjection.reason ?? 'No estimate')
                      : dawn.basis === 'history'
                        ? historyBasisTitle(overnightProjection)
                        : `Current rate held until sunrise${overnightProjection.reason ? `; ${overnightProjection.reason}` : ''}`
                }
              >
                {'Dawn '}
                {overnightProjection.sunrise.toLocaleTimeString('en-US', { hour: 'numeric', minute: '2-digit' })}
                {' · '}
                <span data-testid="dawn-estimate" className={onShorePower ? 'text-muted-foreground' : dawnEstimateClass}>
                  {onShorePower ? 'on shore power' : dawnEstimateText}
                </span>
                {!onShorePower && dawn !== null && (
                  <span className="hidden lg:inline"> · {dawnBasisLabel}</span>
                )}
              </span>
            )}
            {/* Below lg the basis gets a row of its own instead of being dropped:
                a tablet has no hover, so a tooltip-only basis would leave the
                iPad, the primary helm target, unable to tell "4 nights" from
                "at current rate", which is the one distinction the label exists
                to make. On shore power there is no basis to show, so this row
                is skipped along with the inline token above. */}
            {overnightProjection !== null && dawn !== null && !onShorePower && (
              <span className="block truncate lg:hidden">{dawnBasisLabel}</span>
            )}
          </div>
        </div>

        <div className="grid grid-cols-2 gap-2">
          <div className="min-w-0 rounded-md border bg-background/60 px-3 py-2">
            <p className="text-[10px] uppercase tracking-[0.16em] text-muted-foreground">Net</p>
            <p className={`mt-1 truncate font-display text-4xl leading-none tabular-nums md:text-xl md:leading-7 lg:text-4xl lg:leading-10 ${readoutClass}`}>
              {chargingPowerLabel}
              <span className="ml-1 font-display text-xl text-muted-foreground md:text-sm lg:text-xl">W</span>
            </p>
            <p className="mt-1 truncate font-display text-[11px] tabular-nums text-muted-foreground">
              <span className={readoutClass}>{chargingCurrentLabel}</span>
              <span className="ml-1 font-display text-muted-foreground">A</span>
            </p>
          </div>
          <div className="min-w-0 rounded-md border bg-background/60 px-3 py-2">
            <p className="text-[10px] uppercase tracking-[0.16em] text-muted-foreground">Solar</p>
            <p className={`mt-1 truncate font-display text-4xl leading-none tabular-nums md:text-xl md:leading-7 lg:text-4xl lg:leading-10 ${readoutClass}`}>
              {solarOutputLabel}
              <span className="ml-1 font-display text-xl text-muted-foreground md:text-sm lg:text-xl">W</span>
            </p>
          </div>
        </div>

        <div className="min-w-0 rounded-md border bg-background/60 px-3 py-2">
          <p className="text-[10px] uppercase tracking-[0.16em] text-muted-foreground">Loads</p>
          <p className="mt-1 truncate font-display text-4xl leading-none tabular-nums text-foreground md:text-xl md:leading-7 lg:text-4xl lg:leading-10">
            {loadsTotalLabel}
            <span className="ml-1 font-display text-xl text-muted-foreground md:text-sm lg:text-xl">W</span>
          </p>
          <p className="mt-1 truncate font-display text-[11px] tabular-nums text-muted-foreground">
            {`AC ${acOutputLabel} · DC ${dc12vPowerLabel}`}
          </p>
        </div>

        {shoreVisible && (
          <div className="flex items-center justify-between gap-2 rounded-md border bg-background/60 px-3 py-2">
            <span className="shrink-0 text-[10px] uppercase tracking-[0.16em] text-muted-foreground">Shore</span>
            <span className="min-w-0 truncate font-display text-sm tabular-nums text-foreground">
              {shoreModeText === null && shoreAcInText === null ? (
                '—'
              ) : shoreHasError ? (
                <>
                  <span className="text-red-600">{shoreModeText}</span>
                  {shoreAcInText !== null ? ` · ${shoreAcInText}` : null}
                </>
              ) : (
                [shoreModeText, shoreAcInText].filter((part): part is string => part !== null).join(' · ')
              )}
            </span>
          </div>
        )}
      </div>
    </Tile>
  )
})
