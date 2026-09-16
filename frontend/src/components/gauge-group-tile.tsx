import { Gauge as GaugeIcon, Settings2 } from 'lucide-react'
import { memo } from 'react'

import { Button } from '@/components/ui/button'
import { GaugeBody } from '@/components/gauge-tile'
import { Tile } from '@/components/ui/tile'
import { reading } from '@/lib/cluster-readings'
import { GAUGE_GROUP_MAX_COLUMNS, type GaugeGroupWidgetConfig } from '@/lib/dashboard-widgets'
import { worstZoneState } from '@/lib/severity'
import { formatDataAge } from '@/lib/staleness'

/**
 * Columns when the operator has not chosen. One gauge alone gets the full
 * width; anything more pairs up, which is the shape an engine cluster wants on
 * the half-grid tile it usually occupies.
 */
function columnsFor(config: GaugeGroupWidgetConfig): number {
  if (config.columns) return Math.min(Math.max(config.columns, 1), GAUGE_GROUP_MAX_COLUMNS)
  return config.gauges.length > 1 ? 2 : 1
}

function heroIndexFor(config: GaugeGroupWidgetConfig): number | null {
  if (config.hero === undefined) return null
  if (!Number.isInteger(config.hero)) return null
  return config.hero >= 0 && config.hero < config.gauges.length ? config.hero : null
}

interface GaugeGroupTileProps {
  config: GaugeGroupWidgetConfig
  /** Every bound path's current SI value; the group looks up its own members. */
  values: Record<string, number | null>
  /** Age in seconds behind each bound path (ADR 0083); absent is unknown. */
  ages?: Record<string, number | null>
  editing: boolean
  onConfigure: () => void
}

/**
 * A named cluster of gauges in one tile (ADR 0049) — "Port" holding RPM, oil
 * pressure and exhaust temperature, rather than three tiles scattered across
 * the grid.
 */
export const GaugeGroupTile = memo(function GaugeGroupTile({ config, values, ages, editing, onConfigure }: GaugeGroupTileProps) {
  const title = config.title.trim() || 'Gauges'
  const columns = columnsFor(config)
  const heroIndex = heroIndexFor(config)
  // reading() does the same conversion GaugeBody does per member, and blanks
  // a stale member to the same absence a never-reported one gets (ADR 0083),
  // so the tile edge, worstZoneState and each member's readout all agree on
  // which zone a member is in without staleness needing a separate check.
  const readings = config.gauges.map((gauge) => reading(gauge, values, ages))
  const state = worstZoneState(readings.map((r) => r.zone))

  // The tile itself only goes stale once every member whose age is actually
  // known has frozen. A group with no known ages at all is neither fresh nor
  // stale -- the same "no evidence" rule staleness follows everywhere else --
  // and a single stale member among several fresh ones marks itself rather
  // than dragging the whole cluster down.
  const knownAges = readings.filter((r) => r.age !== null)
  const tileStale = knownAges.length > 0 && knownAges.every((r) => r.stale)
  const staleLabel = tileStale ? formatDataAge(Math.max(...knownAges.map((r) => r.age as number))) : undefined

  return (
    <Tile
      title={title}
      state={state}
      stale={tileStale}
      staleLabel={staleLabel}
      icon={<GaugeIcon className="h-3.5 w-3.5 text-gauge-secondary" />}
      titleExtra={
        editing ? (
          <Button size="sm" variant="ghost" onClick={onConfigure} aria-label={`Configure ${title}`}>
            <Settings2 className="size-3.5" />
          </Button>
        ) : undefined
      }
    >
      <div
        className="grid gap-2 [grid-template-columns:repeat(var(--gauge-group-cols),minmax(0,1fr))]"
        style={{ '--gauge-group-cols': columns } as React.CSSProperties}
      >
        {config.gauges.map((gauge, index) => {
          const r = readings[index]
          const isHero = heroIndex === index
          const label = gauge.label.trim() || gauge.path.split('.').slice(-1)[0]
          const isTemperature = /(^|\.)temperature$/i.test(gauge.path) || /^temp(?:erature)?$/i.test(label)
          const labelTrackingClass = isHero || isTemperature ? 'tracking-[0.16em]' : 'tracking-[0.14em]'
          const readoutSizeClass = isHero ? 'text-4xl' : isTemperature ? 'text-xl' : 'text-lg'
          const unitSizeClass = isHero ? 'text-xs' : 'text-[10px]'
          const readoutGapClass = isHero ? 'gap-1' : 'gap-0.5'
          const panelClass = isHero
            ? 'rounded-md border bg-background/60 px-3 py-3'
            : isTemperature
              ? 'rounded-md border bg-background/60 px-3 py-2'
              : 'rounded-md border bg-background/60 px-2 py-2'
          const fallbackTextClass = isHero
            ? 'text-gauge-secondary'
            : isTemperature
              ? 'text-gauge-secondary'
              : 'text-foreground'
          return (
            // Keyed by index: paths are not unique within a group (the same
            // value in two units is legitimate) and members have no id of
            // their own.
            <div
              key={index}
              className="flex min-w-0 flex-col gap-1"
              style={isHero && columns > 1 ? { gridColumn: 'span 2 / span 2' } : undefined}
              data-testid={isHero ? 'gauge-group-hero' : undefined}
            >
              <div className={`${panelClass} ${r.stale ? 'grayscale' : ''}`.trim()}>
                <span className={`flex min-w-0 items-center gap-1 text-[10px] uppercase ${labelTrackingClass} text-muted-foreground`}>
                  <span className="truncate">{label}</span>
                  {r.stale && (
                    <span className="shrink-0 rounded-sm border border-amber-500/40 bg-amber-500/10 px-1.5 py-0.5 text-[10px] leading-none text-amber-600 dark:text-amber-400">
                      Stale {formatDataAge(r.age)}
                    </span>
                  )}
                </span>
                <GaugeBody
                  config={gauge}
                  value={r.stale ? null : (values[gauge.path] ?? null)}
                  density={isHero ? 'hero' : 'compact'}
                  fallbackTextClass={fallbackTextClass}
                  readoutSizeClass={readoutSizeClass}
                  unitSizeClass={unitSizeClass}
                  readoutGapClass={readoutGapClass}
                />
              </div>
            </div>
          )
        })}
      </div>
    </Tile>
  )
})
