import { memo } from 'react'
import { Tile } from '@/components/ui/tile'
import { formatDataAge, isStale } from '@/lib/staleness'

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

  const socLabel = batterySocPercent !== null ? Math.round(batterySocPercent).toString() : '—'
  const socBarWidth = `${Math.max(0, Math.min(100, batterySocPercent ?? 0))}%`

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

  const timeToGoValue = formatTimeToGo(timeToGoHours !== null ? Math.abs(timeToGoHours) : null)
  // Direction now lives in the label rather than a battery glyph. The rate's
  // sign decides it; when the rate hasn't reported but a signed time-to-go
  // has, fall back to that, since use-electrical-state.ts already signs it
  // the same way (positive hours to full, negative hours to empty). With no
  // value to show, the label reads "—" too rather than guessing a direction.
  const timeToGoIsToFull = batteryRatePercentPerHour !== null
    ? batteryRatePercentPerHour > 0
    : timeToGoHours !== null && timeToGoHours > 0
  const timeToGoLabel = timeToGoValue === '—' ? '—' : (timeToGoIsToFull ? 'To full' : 'To empty')

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

  return (
    <Tile title="Battery & Power" stale={feedStale} staleLabel={formatDataAge(props.lastUpdateAgeS)}>
      <div className="mt-1 space-y-2">
        <div className="rounded-md border bg-background/60 px-3 py-3">
          <div className="flex items-start justify-between gap-2">
            <div className="flex min-w-0 items-baseline gap-1">
              <span className="font-display text-6xl leading-none tabular-nums text-gauge-primary md:text-5xl lg:text-7xl">
                {socLabel}
              </span>
              <span className="shrink-0 font-display text-2xl leading-none text-muted-foreground md:text-xl lg:text-2xl">
                %
              </span>
            </div>
            <div className="min-w-0 shrink-0 text-right">
              <p className="truncate text-[10px] uppercase tracking-[0.16em] text-muted-foreground">
                {timeToGoLabel}
              </p>
              <p className={`mt-1 truncate font-display text-2xl leading-none tabular-nums md:text-xl lg:text-3xl ${readoutClass}`}>
                {timeToGoValue}
              </p>
            </div>
          </div>
          <div className="relative mt-3 h-2 overflow-hidden rounded-full bg-muted/60">
            <div className="h-full rounded-full bg-gauge-primary" style={{ width: socBarWidth }} />
          </div>
          <div className="mt-2 flex items-center justify-between font-display text-[11px] tabular-nums text-muted-foreground">
            <span className="truncate">
              {dc24vVoltageLabel}
              <span className="font-display text-muted-foreground">V</span>
              {' · '}
              <span className={readoutClass}>{chargeRateLabel}</span>
              <span className="ml-1 font-display text-muted-foreground">%/h</span>
            </span>
            <span />
          </div>
        </div>

        <div className="grid grid-cols-2 gap-2">
          <div className="min-w-0 rounded-md border bg-background/60 px-3 py-2">
            <p className="text-[10px] uppercase tracking-[0.16em] text-muted-foreground">Net</p>
            <p className={`mt-1 truncate font-display text-4xl leading-none tabular-nums md:text-xl lg:text-4xl ${readoutClass}`}>
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
            <p className={`mt-1 truncate font-display text-4xl leading-none tabular-nums md:text-xl lg:text-4xl ${readoutClass}`}>
              {solarOutputLabel}
              <span className="ml-1 font-display text-xl text-muted-foreground md:text-sm lg:text-xl">W</span>
            </p>
          </div>
        </div>

        <div className="min-w-0 rounded-md border bg-background/60 px-3 py-2">
          <p className="text-[10px] uppercase tracking-[0.16em] text-muted-foreground">Loads</p>
          <p className="mt-1 truncate font-display text-4xl leading-none tabular-nums text-foreground md:text-xl lg:text-4xl">
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
