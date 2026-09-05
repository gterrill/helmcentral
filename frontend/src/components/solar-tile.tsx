import { Sun } from 'lucide-react'
import { memo } from 'react'
import { Tile } from '@/components/ui/tile'
import type { SolarController } from '@/hooks/use-solar-state'
import { formatDataAge, isStale } from '@/lib/staleness'

interface SolarTileProps {
  currentW: number | null
  todayKWh: number | null
  yesterdayKWh: number | null
  peakTodayW: number | null
  lastUpdateAgeS: number | null
  controllers: SolarController[]
}

const NO_VALUE = '—'

function formatKWh(value: number | null): string {
  if (value === null) {
    return NO_VALUE
  }
  return value.toFixed(2)
}

function formatW(value: number | null): string {
  if (value === null) {
    return NO_VALUE
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
  lastUpdateAgeS,
  controllers,
}: SolarTileProps) {
  // A dead feed keeps publishing its last reading. Once the source goes quiet
  // every number it fed us is a historical artefact, so none of them are shown
  // as measurements: a frozen "0 W" is the one value an operator is most
  // likely to read as fact.
  const feedStale = isStale(lastUpdateAgeS)
  const w = (value: number | null) => (feedStale ? NO_VALUE : formatW(value))
  const kwh = (value: number | null) => (feedStale ? NO_VALUE : formatKWh(value))

  return (
    <Tile
      title="Solar"
      icon={<Sun className="h-3.5 w-3.5 text-gauge-secondary" />}
      stale={feedStale}
      staleLabel={formatDataAge(lastUpdateAgeS)}
    >
      <div className="grid grid-cols-2 gap-2">
        <div className="rounded-md border bg-background/60 px-3 py-2 min-w-0">
          <p className="text-[10px] uppercase tracking-[0.16em] text-muted-foreground">Now</p>
          <p className="font-display text-3xl leading-none text-gauge-secondary tabular-nums">
            <span>{w(currentW)}</span>
            <span className="ml-1 text-base text-muted-foreground">W</span>
          </p>
        </div>
        <div className="rounded-md border bg-background/60 px-3 py-2 min-w-0">
          <p className="text-[10px] uppercase tracking-[0.16em] text-muted-foreground">Peak Today</p>
          <p className="font-display text-3xl leading-none text-gauge-primary tabular-nums">
            <span>{w(peakTodayW)}</span>
            <span className="ml-1 text-base text-muted-foreground">W</span>
          </p>
        </div>
      </div>

      <div className="mt-2 grid grid-cols-2 gap-2">
        <div className="rounded-md border bg-background/60 px-3 py-2 min-w-0">
          <p className="text-[10px] uppercase tracking-[0.16em] text-muted-foreground">Today</p>
          <p className="font-display text-2xl leading-none text-gauge-primary tabular-nums">
            <span>{kwh(todayKWh)}</span>
            <span className="ml-1 text-[11px] text-muted-foreground">kWh</span>
          </p>
        </div>
        <div className="rounded-md border bg-background/60 px-3 py-2 min-w-0">
          <p className="text-[10px] uppercase tracking-[0.16em] text-muted-foreground">Yesterday</p>
          <p className="font-display text-2xl leading-none text-gauge-primary tabular-nums">
            <span>{kwh(yesterdayKWh)}</span>
            <span className="ml-1 text-[11px] text-muted-foreground">kWh</span>
          </p>
        </div>
      </div>

      <div className="mt-2 space-y-1.5">
        {controllers.length === 0 && (
          <div className="rounded-md border bg-background/60 px-3 py-2 text-[11px] text-muted-foreground">No controller data</div>
        )}

        {controllers.map((controller) => {
          // One dead MPPT among healthy ones is worth calling out on its own
          // row: the array total stays live, but that controller's share of it
          // is unknown rather than zero.
          const controllerStale = feedStale || isStale(controller.lastUpdateAgeS)
          const cw = (value: number | null) => (controllerStale ? NO_VALUE : formatW(value))
          const ckwh = (value: number | null) => (controllerStale ? NO_VALUE : formatKWh(value))
          const errorClass = !controllerStale && hasActiveError(controller.error) ? 'text-red-500' : 'text-muted-foreground'

          return (
            <div
              key={controller.id}
              className={`rounded-md border bg-background/60 px-3 py-2 ${controllerStale && !feedStale ? 'opacity-50' : ''}`}
            >
              <div className="grid grid-cols-[minmax(0,1fr)_auto] items-start gap-2">
                <div className="min-w-0">
                  <p className="truncate text-[10px] uppercase tracking-[0.16em] text-muted-foreground">
                    {controller.label}
                    {controllerStale && !feedStale && (
                      <span
                        data-testid={`controller-stale-${controller.id}`}
                        title={`No update for ${formatDataAge(controller.lastUpdateAgeS)}`}
                        className="ml-1.5 rounded-sm border border-amber-500/40 bg-amber-500/10 px-1 py-0.5 text-[9px] leading-none text-amber-600 dark:text-amber-400"
                      >
                        {formatDataAge(controller.lastUpdateAgeS)}
                      </span>
                    )}
                  </p>
                  <p className="mt-0.5 font-display text-xl leading-none text-gauge-secondary tabular-nums">
                    <span>{cw(controller.currentW)}</span>
                    <span className="ml-1 text-[11px] text-muted-foreground">W</span>
                  </p>
                  <p className="mt-1 truncate text-[10px] uppercase tracking-[0.16em] text-muted-foreground">
                    Mode: <span className="text-foreground">{controllerStale ? NO_VALUE : controller.mode ?? NO_VALUE}</span>
                  </p>
                </div>
                <div className="text-right">
                  <p className="font-display text-lg leading-none text-gauge-primary tabular-nums">
                    <span>{ckwh(controller.todayKWh)}</span>
                    <span className="ml-1 text-[10px] text-muted-foreground">kWh</span>
                  </p>
                  <p className="mt-1 text-[10px] uppercase tracking-[0.16em] text-muted-foreground">
                    Yday: <span className="text-foreground">{ckwh(controller.yesterdayKWh)}</span> kWh
                  </p>
                  <p className="mt-1 text-[10px] uppercase tracking-[0.16em] text-muted-foreground">
                    Share:{' '}
                    <span className="text-foreground">
                      {controllerStale || controller.contributionPct === null
                        ? NO_VALUE
                        : `${controller.contributionPct.toFixed(1)}%`}
                    </span>
                  </p>
                </div>
              </div>
              <p className={`mt-1 truncate text-[10px] uppercase tracking-[0.16em] ${errorClass}`}>
                Error: <span className="text-foreground">{controllerStale ? NO_VALUE : controller.error ?? NO_VALUE}</span>
              </p>
            </div>
          )
        })}
      </div>
    </Tile>
  )
})
