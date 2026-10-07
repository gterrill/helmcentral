import { LampCeiling, Settings2 } from 'lucide-react'
import { memo, useMemo, type ReactNode } from 'react'

import { Tile, tilePillButtonClass } from '@/components/ui/tile'
import { ALARM_STATES, type ActiveAlarm } from '@/hooks/use-alarms'
import type { LampConfig, LampStripWidgetConfig } from '@/lib/dashboard-widgets'
import { LAMP_ICONS, lampIconName } from '@/lib/lamp-icons'
import { formatDataAge, isStale } from '@/lib/staleness'
import { cn } from '@/lib/utils'

/**
 * The state ladder (ADR 0163). An absent path is not a zero: a cell lit or
 * darkened because nothing has reported is the failure the gauges' structural
 * dash exists to prevent. `stale` (ADR 0083) is a second reason a cell goes
 * to the no-data look: a source that stopped reporting must never be left
 * showing its last "on". `alert`..`emergency` come from an active alarm on
 * the lamp's own path, never from a threshold on the lamp.
 */
type LampState = 'on' | 'off' | 'no data' | 'stale' | 'alert' | 'warn' | 'alarm' | 'emergency'

function lampState(lamp: LampConfig, value: number | null | undefined, stale: boolean): 'on' | 'off' | 'no data' | 'stale' {
  if (stale) return 'stale'
  if (value === null || value === undefined) return 'no data'
  const lit = value !== 0
  return (lamp.invert ? !lit : lit) ? 'on' : 'off'
}

function severityRank(state: string): number {
  return ALARM_STATES.indexOf(state as (typeof ALARM_STATES)[number])
}

/** Colours for the whole cell. "On" is the healthy green, not Signal Blue (ADR 0080). */
function cellClass(state: LampState): string {
  switch (state) {
    case 'on':
      return 'border-emerald-700 bg-emerald-600 text-white dark:border-emerald-500 dark:bg-emerald-700'
    case 'alert':
      return 'border-sky-700 bg-sky-600 text-white dark:border-sky-500 dark:bg-sky-700'
    case 'warn':
      return 'border-amber-600 bg-amber-500 text-black dark:border-amber-400 dark:bg-amber-500'
    case 'alarm':
      return 'border-red-700 bg-red-600 text-white dark:border-red-500 dark:bg-red-700'
    case 'emergency':
      return 'border-red-900 bg-red-800 text-white dark:border-red-500 dark:bg-red-900'
    case 'off':
      return 'border-border bg-card text-muted-foreground'
    default:
      return 'border-dashed border-border bg-transparent text-muted-foreground'
  }
}

const CELL_BASE = 'flex min-w-0 shrink-0 grow-0 items-center gap-1.5 rounded-md border px-1.5 py-1.5 text-left'

function Cell({ name, reading, icon, state, className, onClick, ariaLabel }: {
  name: string
  reading?: string
  icon: string
  state: LampState
  className?: string
  onClick?: () => void
  ariaLabel: string
}) {
  const Icon = LAMP_ICONS[icon] ?? LAMP_ICONS.generic
  const body = (
    <>
      <Icon className="size-4 shrink-0" aria-hidden="true" />
      <span className="flex min-w-0 flex-col leading-tight">
        <span className="truncate whitespace-nowrap text-xs font-semibold" title={name}>{name}</span>
        {reading !== undefined && <span className="truncate font-display text-xs tabular-nums">{reading}</span>}
      </span>
    </>
  )
  const classes = cn(CELL_BASE, cellClass(state), className)
  if (!onClick) {
    return <div className={classes} aria-label={ariaLabel} data-state={state} role="group">{body}</div>
  }
  return (
    <button type="button" onClick={onClick} className={cn(classes, 'hover:opacity-90')} aria-label={ariaLabel} data-state={state}>
      {body}
    </button>
  )
}

interface LampStripTileProps {
  config: LampStripWidgetConfig
  values: Record<string, number | null>
  /** Age in seconds behind each lamp's bound path (ADR 0083); absent is unknown. */
  ages?: Record<string, number | null>
  /** The worst currently-active alarm severity, driving the CHK rollup. */
  worstAlarmState: string
  /** Active alarms: a lamp takes the worst one on its path, CHK counts them all. */
  alarms?: ActiveAlarm[]
  editing: boolean
  onConfigure: () => void
  onOpenAlarms: () => void
}

interface Run {
  group: string | undefined
  cells: ReactNode[]
}

/**
 * The indicator ribbon (ADR 0052, cells per ADR 0163): one glance tells you
 * whether anything wants attention, and the CHK cell says whether to go
 * looking.
 */
export const LampStripTile = memo(function LampStripTile({
  config, values, ages, worstAlarmState, alarms = [], editing, onConfigure, onOpenAlarms,
}: LampStripTileProps) {
  const title = config.title.trim() || 'Indicators'

  const live = useMemo(() => alarms.filter((a) => severityRank(a.state) > 0), [alarms])
  const worstByPath = useMemo(() => {
    const map = new Map<string, ActiveAlarm['state']>()
    for (const a of live) {
      const current = map.get(a.path)
      if (current === undefined || severityRank(a.state) > severityRank(current)) map.set(a.path, a.state)
    }
    return map
  }, [live])
  const worstAlarm = useMemo(
    () => live.reduce<ActiveAlarm | null>((w, a) => (w === null || severityRank(a.state) > severityRank(w.state) ? a : w), null),
    [live],
  )

  const runs: Run[] = []
  if (config.showCheck) {
    const lit = worstAlarmState !== 'normal'
    runs.push({
      group: 'Alerts',
      cells: [
        <Cell
          key="chk"
          name={lit && worstAlarm ? `CHK · ${live.length} · ${worstAlarm.label}` : 'CHK'}
          icon="generic"
          state={(lit ? worstAlarmState : 'off') as LampState}
          className={lit ? 'max-w-full' : 'w-32'}
          onClick={onOpenAlarms}
          ariaLabel={`CHK: ${worstAlarmState}`}
        />,
      ],
    })
  }
  const lampRunsStart = runs.length

  config.lamps.forEach((lamp, index) => {
    const age = ages?.[lamp.path] ?? null
    const stale = isStale(age)
    const base = lampState(lamp, values[lamp.path], stale)
    // Alarms raised elsewhere on the network carry a "notifications." prefix.
    const onPath = worstByPath.get(lamp.path)
    const onNotification = worstByPath.get(`notifications.${lamp.path}`)
    const alarmState = (onPath !== undefined && onNotification !== undefined
      ? (severityRank(onNotification) > severityRank(onPath) ? onNotification : onPath)
      : onPath ?? onNotification) as Exclude<ActiveAlarm['state'], 'normal'> | undefined
    const state: LampState = alarmState ?? base
    const name = lamp.label.trim() || lamp.path.split('.').slice(-1)[0]
    // The reading says what the signal is doing; the colour says whether that
    // is good, which `invert` flips.
    const raw = values[lamp.path]
    const reading = base === 'no data' || base === 'stale' || raw === null || raw === undefined ? '--' : raw !== 0 ? 'On' : 'Off'
    const ariaLabel = !alarmState && stale ? `${name}: stale ${formatDataAge(age)}` : `${name}: ${state}`
    const group = lamp.group?.trim() || undefined
    const cell = (
      <Cell
        key={index}
        name={name}
        reading={reading}
        icon={lampIconName(lamp)}
        state={state}
        className="w-32"
        onClick={alarmState ? onOpenAlarms : undefined}
        ariaLabel={ariaLabel}
      />
    )
    const last = runs[runs.length - 1]
    if (runs.length > lampRunsStart && last.group === group) last.cells.push(cell)
    else runs.push({ group, cells: [cell] })
  })

  return (
    <Tile
      title={title}
      fill
      icon={<LampCeiling className="h-3.5 w-3.5 text-gauge-secondary" />}
      titleExtra={
        editing ? (
          <button type="button" className={tilePillButtonClass} onClick={onConfigure} aria-label={`Configure ${title}`}>
            <Settings2 className="size-3.5" />
          </button>
        ) : undefined
      }
    >
      {/* One cell shape at every width: wraps to more rows rather than
          widening the tile. When the saved tile height is too short for every
          row, the cells scroll vertically so no lamp is ever cut off. */}
      <div data-testid="lamp-strip-row" className="flex min-h-0 min-w-0 flex-1 flex-wrap content-start items-start gap-x-4 gap-y-2 overflow-y-auto">
        {runs.map((run, i) => (
          <div key={i} data-testid="lamp-group" className="flex min-w-0 max-w-full flex-col gap-1">
            {run.group && (
              <span className="text-[10px] font-medium uppercase tracking-wider text-muted-foreground">{run.group}</span>
            )}
            <div className="flex min-w-0 flex-wrap gap-1.5">{run.cells}</div>
          </div>
        ))}
      </div>
    </Tile>
  )
})
