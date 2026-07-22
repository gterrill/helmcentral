import { Sun } from 'lucide-react'
import { memo } from 'react'
import { Tile } from '@/components/ui/tile'
import type { SolarController } from '@/hooks/use-solar-state'

interface SolarTileProps {
  currentW: number | null
  todayKWh: number | null
  yesterdayKWh: number | null
  peakTodayW: number | null
  controllers: SolarController[]
}

function formatKWh(value: number | null): string {
  if (value === null) {
    return '—'
  }
  return value.toFixed(2)
}

function formatW(value: number | null): string {
  if (value === null) {
    return '—'
  }
  return Math.round(value).toString()
}

function hasActiveError(value: string | null): boolean {
  if (value === null) {
    return false
  }
  const normalized = value.trim().toLowerCase()
  return normalized !== '' && normalized !== 'none' && normalized !== '0' && normalized !== 'ok'
}

export const SolarTile = memo(function SolarTile({
  currentW,
  todayKWh,
  yesterdayKWh,
  peakTodayW,
  controllers,
}: SolarTileProps) {
  return (
    <Tile title="Solar" icon={<Sun className="h-3.5 w-3.5 text-gauge-secondary" />}>
      <div className="grid grid-cols-2 gap-2">
        <div className="rounded-md border bg-background/60 px-3 py-2 min-w-0">
          <p className="text-[10px] uppercase tracking-[0.16em] text-muted-foreground">Now</p>
          <p className="font-display text-3xl leading-none text-gauge-secondary tabular-nums">
            {formatW(currentW)}
            <span className="ml-1 text-base text-muted-foreground">W</span>
          </p>
        </div>
        <div className="rounded-md border bg-background/60 px-3 py-2 min-w-0">
          <p className="text-[10px] uppercase tracking-[0.16em] text-muted-foreground">Peak Today</p>
          <p className="font-display text-3xl leading-none text-gauge-primary tabular-nums">
            {formatW(peakTodayW)}
            <span className="ml-1 text-base text-muted-foreground">W</span>
          </p>
        </div>
      </div>

      <div className="mt-2 grid grid-cols-2 gap-2">
        <div className="rounded-md border bg-background/60 px-3 py-2 min-w-0">
          <p className="text-[10px] uppercase tracking-[0.16em] text-muted-foreground">Today</p>
          <p className="font-display text-2xl leading-none text-gauge-primary tabular-nums">
            {formatKWh(todayKWh)}
            <span className="ml-1 text-[11px] text-muted-foreground">kWh</span>
          </p>
        </div>
        <div className="rounded-md border bg-background/60 px-3 py-2 min-w-0">
          <p className="text-[10px] uppercase tracking-[0.16em] text-muted-foreground">Yesterday</p>
          <p className="font-display text-2xl leading-none text-gauge-primary tabular-nums">
            {formatKWh(yesterdayKWh)}
            <span className="ml-1 text-[11px] text-muted-foreground">kWh</span>
          </p>
        </div>
      </div>

      <div className="mt-2 space-y-1.5">
        {controllers.length === 0 && (
          <div className="rounded-md border bg-background/60 px-3 py-2 text-[11px] text-muted-foreground">No controller data</div>
        )}

        {controllers.map((controller) => {
          const errorClass = hasActiveError(controller.error) ? 'text-red-500' : 'text-muted-foreground'
          return (
            <div key={controller.id} className="rounded-md border bg-background/60 px-3 py-2">
              <div className="grid grid-cols-[minmax(0,1fr)_auto] items-start gap-2">
                <div className="min-w-0">
                  <p className="truncate text-[10px] uppercase tracking-[0.16em] text-muted-foreground">{controller.label}</p>
                  <p className="mt-0.5 font-display text-xl leading-none text-gauge-secondary tabular-nums">
                    {formatW(controller.currentW)}
                    <span className="ml-1 text-[11px] text-muted-foreground">W</span>
                  </p>
                  <p className="mt-1 truncate text-[10px] uppercase tracking-[0.16em] text-muted-foreground">
                    Mode: <span className="text-foreground">{controller.mode ?? '—'}</span>
                  </p>
                </div>
                <div className="text-right">
                  <p className="font-display text-lg leading-none text-gauge-primary tabular-nums">
                    {formatKWh(controller.todayKWh)}
                    <span className="ml-1 text-[10px] text-muted-foreground">kWh</span>
                  </p>
                  <p className="mt-1 text-[10px] uppercase tracking-[0.16em] text-muted-foreground">
                    Yday: <span className="text-foreground">{formatKWh(controller.yesterdayKWh)}</span>
                  </p>
                  <p className="mt-1 text-[10px] uppercase tracking-[0.16em] text-muted-foreground">
                    Share: <span className="text-foreground">{controller.contributionPct !== null ? `${controller.contributionPct.toFixed(1)}%` : '—'}</span>
                  </p>
                </div>
              </div>
              <p className={`mt-1 truncate text-[10px] uppercase tracking-[0.16em] ${errorClass}`}>
                Error: <span className="text-foreground">{controller.error ?? '—'}</span>
              </p>
            </div>
          )
        })}
      </div>
    </Tile>
  )
})
