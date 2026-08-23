import { Gauge as GaugeIcon, Settings2 } from 'lucide-react'
import { memo } from 'react'

import { Button } from '@/components/ui/button'
import { GaugeBody } from '@/components/gauge-tile'
import { Tile } from '@/components/ui/tile'
import { GAUGE_GROUP_MAX_COLUMNS, type GaugeGroupWidgetConfig } from '@/lib/dashboard-widgets'

/**
 * Columns when the operator has not chosen. One gauge alone gets the full
 * width; anything more pairs up, which is the shape an engine cluster wants on
 * the half-grid tile it usually occupies.
 */
function columnsFor(config: GaugeGroupWidgetConfig): number {
  if (config.columns) return Math.min(Math.max(config.columns, 1), GAUGE_GROUP_MAX_COLUMNS)
  return config.gauges.length > 1 ? 2 : 1
}

interface GaugeGroupTileProps {
  config: GaugeGroupWidgetConfig
  /** Every bound path's current SI value; the group looks up its own members. */
  values: Record<string, number | null>
  editing: boolean
  onConfigure: () => void
}

/**
 * A named cluster of gauges in one tile (ADR 0049) — "Port" holding RPM, oil
 * pressure and exhaust temperature, rather than three tiles scattered across
 * the grid.
 */
export const GaugeGroupTile = memo(function GaugeGroupTile({ config, values, editing, onConfigure }: GaugeGroupTileProps) {
  const title = config.title.trim() || 'Gauges'

  return (
    <Tile
      title={title}
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
        style={{ '--gauge-group-cols': columnsFor(config) } as React.CSSProperties}
      >
        {config.gauges.map((gauge, index) => (
          // Keyed by index: paths are not unique within a group (the same value
          // in two units is legitimate) and members have no id of their own.
          <div key={index} className="flex min-w-0 flex-col gap-1">
            <span className="truncate text-[10px] font-medium uppercase tracking-wider text-muted-foreground">
              {gauge.label.trim() || gauge.path.split('.').slice(-1)[0]}
            </span>
            <GaugeBody config={gauge} value={values[gauge.path] ?? null} density="compact" />
          </div>
        ))}
      </div>
    </Tile>
  )
})
