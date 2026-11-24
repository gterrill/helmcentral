import { ChevronDown, Sun } from 'lucide-react'
import { Fragment, memo } from 'react'
import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover'
import { Tile } from '@/components/ui/tile'
import { useSolarState, type SolarController } from '@/hooks/use-solar-state'
import { formatDataAge, isStale } from '@/lib/staleness'

interface SolarTileProps {
  currentW: number | null
  todayKWh: number | null
  yesterdayKWh: number | null
  peakTodayW: number | null
  lastUpdateAgeS: number | null
  /** Null until the first solar event: no rows, and no claim that there are no controllers. */
  controllers: SolarController[] | null
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

type ArrayStatus = 'live' | 'stale' | 'fault'

interface ArrayState {
  controller: SolarController
  status: ArrayStatus
  /** Set for a stale or faulted array; names it under the bar. */
  problem: string | null
  /** The array's share of the bar. */
  grow: number
}

/**
 * One decision per array, read by the bar, the line under it and the table.
 * Silence outranks a frozen error. Mode "off" is not a fault: a Victron MPPT
 * reports it every night. A silent array's yield is frozen, so it is drawn at
 * the average share instead of vanishing; a faulted array's yield so far is real.
 */
function arrayStates(controllers: SolarController[], feedStale: boolean): { states: ArrayState[]; anyYield: boolean } {
  const classified = controllers.map((controller) => {
    if (feedStale || isStale(controller.lastUpdateAgeS)) {
      const problem = feedStale ? null : `${controller.label} · no update for ${formatDataAge(controller.lastUpdateAgeS)}`
      return { controller, status: 'stale' as const, problem, kwh: 0 }
    }
    const kwh = Math.max(controller.todayKWh ?? 0, 0)
    if (hasActiveError(controller.error)) {
      return { controller, status: 'fault' as const, problem: `${controller.label} · error ${controller.error?.trim()}`, kwh }
    }
    return { controller, status: 'live' as const, problem: null, kwh }
  })
  const producing = classified.filter(({ kwh }) => kwh > 0)
  const total = producing.reduce((sum, { kwh }) => sum + kwh, 0)
  const average = producing.length > 0 ? total / producing.length : 1
  const states = classified.map(({ kwh, ...state }) => ({
    ...state,
    grow: producing.length === 0 || state.status === 'stale' ? average : kwh,
  }))
  return { states, anyYield: producing.length > 0 }
}

const HATCH =
  'repeating-linear-gradient(135deg, var(--color-muted-foreground) 0 2px, transparent 2px 5px)'

function segmentClass(status: ArrayStatus, anyYield: boolean, index: number): string {
  if (status === 'stale') return 'bg-muted opacity-60'
  if (status === 'fault') return 'bg-red-500'
  if (!anyYield) return 'bg-muted-foreground/30'
  return index % 2 === 0 ? 'bg-gauge-primary' : 'bg-gauge-primary/60'
}

function SolarArrays({ controllers, feedStale }: { controllers: SolarController[]; feedStale: boolean }) {
  const { states, anyYield } = arrayStates(controllers, feedStale)
  // A controller error outranks silence for the one line under the bar.
  const problems = states
    .filter((state) => state.problem !== null)
    .sort((a, b) => Number(b.status === 'fault') - Number(a.status === 'fault'))
  const first = problems[0]

  return (
    <Popover>
      <PopoverTrigger
        className="group block w-full rounded-md text-left focus-visible:outline-hidden focus-visible:ring-2 focus-visible:ring-ring"
      >
        <span className="flex items-center gap-1.5">
          <span className="flex h-3 min-w-0 flex-1 gap-px overflow-hidden rounded-sm">
            {states.map(({ controller, status, grow }, index) => (
              <span
                key={controller.id}
                data-testid={`solar-segment-${controller.id}`}
                className={`block min-w-3 basis-0 ${segmentClass(status, anyYield, index)}`}
                style={{ flexGrow: grow, backgroundImage: status === 'stale' ? HATCH : undefined }}
              />
            ))}
          </span>
          <ChevronDown className="h-3 w-3 shrink-0 text-muted-foreground" aria-hidden="true" />
        </span>
        <span className="sr-only">Show each array. </span>
        <span data-testid="solar-array-status" className="mt-1 block truncate text-xs text-muted-foreground">
          {first === undefined ? (
            `${controllers.length} ${controllers.length === 1 ? 'array' : 'arrays'} · ${feedStale ? 'no update' : 'no faults'}`
          ) : (
            <>
              <span className={first.status === 'fault' ? 'text-red-500' : 'text-amber-600 dark:text-amber-400'}>{first.problem}</span>
              {problems.length > 1 && ` +${problems.length - 1} more`}
            </>
          )}
        </span>
      </PopoverTrigger>
      <PopoverContent align="end" className="w-auto max-w-[min(92vw,26rem)] p-2 text-xs">
        <table className="w-full tabular-nums">
          <thead>
            <tr className="text-[10px] uppercase tracking-[0.16em] text-muted-foreground">
              <th className="px-1.5 py-1 text-left font-normal">Array</th>
              <th className="px-1.5 py-1 text-right font-normal">Now (W)</th>
              <th className="px-1.5 py-1 text-right font-normal">Today (kWh)</th>
              <th className="px-1.5 py-1 text-right font-normal">Yday (kWh)</th>
              <th className="px-1.5 py-1 text-left font-normal">Mode</th>
            </tr>
          </thead>
          <tbody>
            {states.map(({ controller, status }) => {
              const stale = status === 'stale'
              const age = formatDataAge(controller.lastUpdateAgeS)
              return (
                <Fragment key={controller.id}>
                  <tr>
                    <td className="px-1.5 py-1 text-left">
                      {controller.label}
                      {stale && !feedStale && (
                        <span
                          data-testid={`controller-stale-${controller.id}`}
                          title={`No update for ${age}`}
                          className="ml-1.5 rounded-xs border border-amber-500/40 bg-amber-500/10 px-1 py-0.5 text-[9px] leading-none text-amber-600 dark:text-amber-400"
                        >
                          {age}
                        </span>
                      )}
                    </td>
                    <td className="px-1.5 py-1 text-right">{stale ? NO_VALUE : formatW(controller.currentW)}</td>
                    <td className="px-1.5 py-1 text-right">{stale ? NO_VALUE : formatKWh(controller.todayKWh)}</td>
                    <td className="px-1.5 py-1 text-right">{stale ? NO_VALUE : formatKWh(controller.yesterdayKWh)}</td>
                    <td className="px-1.5 py-1 text-left">{stale ? NO_VALUE : (controller.mode ?? NO_VALUE)}</td>
                  </tr>
                  {status === 'fault' && (
                    <tr>
                      <td colSpan={5} className="px-1.5 pb-1 text-left text-red-500">
                        Error {controller.error?.trim()}
                      </td>
                    </tr>
                  )}
                </Fragment>
              )
            })}
          </tbody>
        </table>
      </PopoverContent>
    </Popover>
  )
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
            <span className="ml-1 text-xs text-muted-foreground">kWh</span>
          </p>
        </div>
        <div className="rounded-md border bg-background/60 px-3 py-2 min-w-0">
          <p className="text-[10px] uppercase tracking-[0.16em] text-muted-foreground">Yesterday</p>
          <p className="font-display text-2xl leading-none text-gauge-primary tabular-nums">
            <span>{kwh(yesterdayKWh)}</span>
            <span className="ml-1 text-xs text-muted-foreground">kWh</span>
          </p>
        </div>
      </div>

      <div className="mt-2 space-y-1.5">
        {controllers?.length === 0 && (
          <div className="rounded-md border bg-background/60 px-3 py-2 text-xs text-muted-foreground">No controller data</div>
        )}

        {controllers !== null && controllers.length > 0 && (
          <SolarArrays controllers={controllers} feedStale={feedStale} />
        )}
      </div>
    </Tile>
  )
})

/** The Solar tile: the whole solar stream. */
export const LiveSolarTile = memo(function LiveSolarTile() {
  return <SolarTile {...useSolarState()} />
})
