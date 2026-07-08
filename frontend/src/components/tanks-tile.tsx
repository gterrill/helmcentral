import { faucet } from '@lucide/lab'
import { Droplet, Droplets, Fuel, Icon } from 'lucide-react'

import { Tile } from '@/components/ui/tile'
import type { TankLevel } from '@/hooks/use-tanks-state'

type TanksTileProps = {
  tanks: TankLevel[]
  loading: boolean
}

function clampPercent(value: number) {
  return Math.max(0, Math.min(100, value))
}

function isAmberColor(kind: TankLevel['kind'], percent: number) {
  if (kind === 'water') {
    return percent > 10 && percent <= 20
  }

  if (kind === 'waste') {
    return percent > 80 && percent <= 90
  }

  return percent >= 10 && percent < 30
}

function isRedColor(kind: TankLevel['kind'], percent: number) {
  if (kind === 'water') {
    return percent <= 10
  }

  if (kind === 'waste') {
    return percent > 90
  }

  return percent < 10
}

function toneColor(kind: TankLevel['kind'], percent: number) {
  if (isRedColor(kind, percent)) {
    return '#dc2626'
  }

  if (isAmberColor(kind, percent)) {
    return '#d97706'
  }

  return '#047857'
}

function tankKindIcon(kind: TankLevel['kind']) {
  if (kind === 'fuel') {
    return <Fuel className="h-4 w-4 text-amber-600" aria-hidden="true" />
  }

  if (kind === 'waste') {
    return <Droplet className="h-4 w-4 text-amber-900" aria-hidden="true" />
  }

  return <Icon iconNode={faucet} className="h-4 w-4 text-emerald-700" aria-hidden="true" />
}

export function TanksTile({ tanks, loading }: TanksTileProps) {
  const visibleTanks = tanks.slice(0, 8)

  return (
    <Tile title="Tanks" icon={<Droplets className="h-3.5 w-3.5 text-gauge-secondary" />}>
      <div className="mt-3 space-y-2">
        {visibleTanks.map((tank) => {
          const percent = clampPercent(tank.level_percent)
          const color = toneColor(tank.kind, percent)

          return (
            <div key={tank.id} className="flex items-center gap-3">
              <span className="grid h-6 w-6 shrink-0 place-items-center">{tankKindIcon(tank.kind)}</span>
              <p className="w-28 shrink-0 font-display text-sm uppercase leading-tight tracking-[0.08em] sm:w-32" style={{ color }}>
                {tank.label}
              </p>
              <div className="h-5 min-w-0 flex-1 overflow-hidden rounded-md border bg-muted/30">
                <div className="h-full transition-[width] duration-500" style={{ width: `${percent}%`, backgroundColor: color }} />
              </div>
              <p className="w-20 shrink-0 text-right font-display text-2xl leading-none" style={{ color }}>
                {Math.round(percent)}%
              </p>
            </div>
          )
        })}

        {!loading && visibleTanks.length === 0 ? (
          <div className="rounded-md border border-dashed bg-muted/25 px-3 py-4 text-center text-sm text-muted-foreground">No tank data available</div>
        ) : null}

        {loading ? (
          <div className="rounded-md border border-dashed bg-muted/25 px-3 py-4 text-center text-sm text-muted-foreground">Loading tank levels...</div>
        ) : null}
      </div>
    </Tile>
  )
}
