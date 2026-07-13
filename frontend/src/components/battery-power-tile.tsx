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
  batteryRatePercentPerHour: number | null
  timeToGoHours: number | null
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
      <div className="mt-1 grid grid-cols-[minmax(0,1.2fr)_minmax(0,1fr)] gap-2">
        <div className="min-w-0 rounded-md border bg-background/60 px-3 py-3">
          <div className="flex items-baseline gap-2">
            <span className="font-display text-6xl leading-none tabular-nums text-gauge-primary md:text-7xl">{socLabel}</span>
            <span className="shrink-0 text-2xl leading-none text-foreground md:text-3xl">%</span>
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
          <p className={`font-display text-3xl leading-none md:text-3xl ${chargingValueClass}`}>
            {chargingCurrentLabel}
            <span className="ml-1 text-xl text-muted-foreground">A</span>
          </p>
          <p className={`mt-1 font-display text-3xl leading-none ${chargingValueClass}`}>
            {chargingPowerLabel}
            <span className="ml-1 text-xl text-muted-foreground">W</span>
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
          <p className={`mt-1 font-display text-3xl leading-none ${chargeRateClass}`}>
            {chargeRateLabel}
            <span className="ml-1 text-xl text-muted-foreground">%/hr</span>
          </p>
        </div>
      </div>
    </Tile>
  )
})
