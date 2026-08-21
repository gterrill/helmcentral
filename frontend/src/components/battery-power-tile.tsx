import { BatteryCharging, BatteryFull, BatteryLow, BatteryMedium, BatteryWarning } from 'lucide-react'
import { memo } from 'react'
import { Tile } from '@/components/ui/tile'

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

export const BatteryPowerTile = memo(function BatteryPowerTile({
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
}: BatteryPowerTileProps) {
  const socLabel = batterySocPercent !== null ? Math.round(batterySocPercent).toString() : '—'
  const socBarWidth = `${Math.max(0, Math.min(100, batterySocPercent ?? 0))}%`
  const chargingCurrentLabel = chargingCurrentA !== null
    ? `${chargingCurrentA >= 0 ? '+' : '-'}${Math.abs(chargingCurrentA).toFixed(1)}`
    : '—'
  const chargingPowerLabel = chargingPowerW !== null
    ? `${chargingPowerW >= 0 ? '+' : '-'}${Math.abs(Math.round(chargingPowerW))}`
    : '—'
  const isDischarging = (chargingCurrentA !== null && chargingCurrentA < 0) || (chargingPowerW !== null && chargingPowerW < 0)
  const chargingValueClass = isDischarging ? 'text-amber-600' : 'text-gauge-secondary'
  const solarOutputLabel = solarOutputW !== null ? Math.round(solarOutputW).toString() : '—'
  const acOutputLabel = acOutputW !== null ? Math.round(acOutputW).toString() : '—'
  const dc12vPowerLabel = dc12vPowerW !== null ? Math.round(dc12vPowerW).toString() : '—'
  const dc24vVoltageLabel = dc24vVoltageV !== null ? dc24vVoltageV.toFixed(2) : '—'
  const charger0CurrentLabel = typeof charger0CurrentA === 'number' ? charger0CurrentA.toFixed(1) : '—'
  const charger0AcIn1CurrentLabel = typeof charger0AcIn1CurrentA === 'number' ? charger0AcIn1CurrentA.toFixed(1) : '—'
  const charger0ChargingModeLabel = typeof charger0ChargingMode === 'string' && charger0ChargingMode.length > 0 ? charger0ChargingMode : '—'
  const charger0ErrorLabel = typeof charger0Error === 'string' && charger0Error.length > 0 ? charger0Error : '—'
  const charger0ErrorClass = hasChargerError(charger0Error) ? 'text-red-500' : 'text-muted-foreground'
  const chargeRateLabel = batteryRatePercentPerHour !== null
    ? `${batteryRatePercentPerHour >= 0 ? '+' : ''}${batteryRatePercentPerHour.toFixed(1)}`
    : '—'
  const chargeRateClass = batteryRatePercentPerHour !== null && batteryRatePercentPerHour < 0 ? 'text-amber-600' : 'text-gauge-secondary'
  const timeToGoLabel = formatTimeToGo(timeToGoHours)
  const timeToGoClass = timeToGoHours !== null && timeToGoHours < 0 ? 'text-amber-600' : 'text-gauge-secondary'

  let TimeToGoIcon = BatteryFull
  if (!isDischarging && batteryRatePercentPerHour !== null && batteryRatePercentPerHour > 0) {
    TimeToGoIcon = BatteryCharging
  } else {
    const soc = batterySocPercent ?? 0
    if (soc <= 20) TimeToGoIcon = BatteryWarning
    else if (soc <= 33) TimeToGoIcon = BatteryLow
    else if (soc <= 66) TimeToGoIcon = BatteryMedium
    else TimeToGoIcon = BatteryFull
  }

  return (
    <Tile title="Battery & Power">
      <div className="mt-1 grid grid-cols-2 gap-2">
        <div className="min-w-0 rounded-md border bg-background/60 px-3 py-3">
          <div className="flex items-baseline gap-2">
            {/* `md` used to mean "more room" and stepped this up. With the narrow
                dashboard it means the opposite — two columns *and* a fixed sidebar —
                so the step-up now waits for `lg`, where the RGL grid takes over. */}
            <span className="font-display text-6xl leading-none tabular-nums text-gauge-primary md:text-5xl lg:text-7xl">{socLabel}</span>
            <span className="shrink-0 text-2xl leading-none text-foreground md:text-xl lg:text-3xl">%</span>
          </div>
          <div className="mt-3 flex items-center gap-2">
            <div className="h-1.5 flex-1 rounded-full bg-muted/60">
              <div className="h-full rounded-full bg-gauge-primary" style={{ width: socBarWidth }} />
            </div>
            <span className="shrink-0 font-display text-sm tabular-nums leading-none text-gauge-secondary">
              {dc24vVoltageLabel}<span className="text-xs text-muted-foreground">V</span>
            </span>
          </div>
        </div>
        <div className="min-w-0 rounded-md border bg-background/60 px-3 py-3">
          <p className="text-[11px] uppercase tracking-[0.16em] text-muted-foreground">Battery</p>
          {/* Steps down between `md` and `lg`. That band is the tightest the tile ever
              gets: the narrow dashboard is two columns from `sm` up, but from `md` the
              sidebar is a fixed rail rather than an overlay sheet, so it takes ~256px
              off the content width — leaving this sub-card ~110px, where a `text-3xl`
              signed value with its unit suffix spills past the border. */}
          <p className={`font-display text-3xl leading-none md:text-2xl lg:text-3xl ${chargingValueClass}`}>
            {chargingCurrentLabel}
            <span className="ml-1 text-xl text-muted-foreground md:text-base lg:text-xl">A</span>
          </p>
          <p className={`mt-1 font-display text-3xl leading-none md:text-2xl lg:text-3xl ${chargingValueClass}`}>
            {chargingPowerLabel}
            <span className="ml-1 text-xl text-muted-foreground md:text-base lg:text-xl">W</span>
          </p>
        </div>
      </div>

      <div className="mt-2 grid grid-cols-2 gap-2 text-sm">
        <div className="rounded-md border bg-background/60 px-3 py-2">
          <p className="text-[11px] uppercase tracking-[0.16em] text-muted-foreground">AC Draw</p>
          <p className="font-display text-4xl leading-none text-gauge-primary">
            {acOutputLabel}
            <span className="ml-1 text-xl text-muted-foreground">W</span>
          </p>
        </div>
        <div className="rounded-md border bg-background/60 px-3 py-2">
          <p className="text-[11px] uppercase tracking-[0.16em] text-muted-foreground">DC Draw</p>
          <p className="font-display text-4xl leading-none text-gauge-primary">
            {dc12vPowerLabel}
            <span className="ml-1 text-xl text-muted-foreground">W</span>
          </p>
        </div>
      </div>

      <div className="mt-2 rounded-md border bg-background/60 px-3 py-2">
        <p className="text-[11px] uppercase tracking-[0.16em] text-muted-foreground">Solar</p>
        <p className="font-display text-4xl leading-none text-gauge-secondary">
          {solarOutputLabel}
          <span className="ml-1 text-xl text-muted-foreground">W</span>
        </p>
      </div>

      <div className="mt-2 rounded-md border bg-background/60 px-3 py-2">
        <p className="text-[11px] uppercase tracking-[0.16em] text-muted-foreground">Charger</p>
        <div className="mt-1 grid grid-cols-2 gap-2">
          <div className="min-w-0">
            <p className="text-[10px] uppercase tracking-[0.16em] text-muted-foreground">AC In</p>
            <p className="font-display text-2xl leading-none text-gauge-primary tabular-nums">
              {charger0AcIn1CurrentLabel}
              <span className="ml-1 text-base text-muted-foreground">A</span>
            </p>
            <p className={`mt-2 truncate text-[10px] uppercase tracking-[0.16em] ${charger0ErrorClass}`}>
              Error: <span className="text-foreground">{charger0ErrorLabel}</span>
            </p>
          </div>
          <div className="min-w-0">
            <p className="text-[10px] uppercase tracking-[0.16em] text-muted-foreground">Output</p>
            <p className="font-display text-2xl leading-none text-gauge-secondary tabular-nums">
              {charger0CurrentLabel}
              <span className="ml-1 text-base text-muted-foreground">A</span>
            </p>
            <p className="mt-2 truncate text-[10px] uppercase tracking-[0.16em] text-muted-foreground">
              Mode: <span className="text-foreground">{charger0ChargingModeLabel}</span>
            </p>
          </div>
        </div>
      </div>

      <div className="mt-2 grid grid-cols-2 gap-2">
        <div className="rounded-md border bg-background/60 px-3 py-2">
          <p className="flex items-center gap-1.5 text-[11px] uppercase tracking-[0.16em] text-muted-foreground">
            Time Remaining
            {timeToGoLabel !== '—' && <TimeToGoIcon className="h-3 w-3" />}
          </p>
          <p className={`mt-1 font-display text-3xl leading-none ${timeToGoClass}`}>
            {timeToGoLabel}
          </p>
        </div>
        <div className="rounded-md border bg-background/60 px-3 py-2">
          <p className="text-[11px] uppercase tracking-[0.16em] text-muted-foreground">Charge Rate</p>
          {/* Same md–lg squeeze as the Battery sub-card above. */}
          <p className={`mt-1 font-display text-3xl leading-none md:text-2xl lg:text-3xl ${chargeRateClass}`}>
            {chargeRateLabel}
            {/* Four glyphs rather than one, so this suffix steps down further than the
                A/W ones to keep the pair inside an ~86px sub-card. */}
            <span className="ml-1 text-xl text-muted-foreground md:text-xs lg:text-xl">%/hr</span>
          </p>
        </div>
      </div>
    </Tile>
  )
})
