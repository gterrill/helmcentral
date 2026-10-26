import { Gauge as GaugeIcon, Settings2 } from 'lucide-react'
import { memo, useMemo } from 'react'

import { EngineClusterTile } from '@/components/engine-cluster-tile'
import { GaugeGroupTile } from '@/components/gauge-group-tile'
import { LampStripTile } from '@/components/lamp-strip-tile'
import { Tile, tilePillButtonClass } from '@/components/ui/tile'
import type { EngineStates } from '@/hooks/use-gauge-values'
import type { TankLevel } from '@/hooks/use-tanks-state'
import { formatAppLocation } from '@/lib/app-location'
import type { EngineClusterConfig, GaugeGroupWidgetConfig, LampStripWidgetConfig } from '@/lib/dashboard-widgets'
import {
  isEquipmentGroup,
  problemMessage,
  resolveCluster,
  resolveGaugeGroup,
  resolveLamps,
  type EquipmentTileContext,
  type TileProblem,
} from '@/lib/equipment-tiles'

/**
 * A tile that cannot show its readings says why, in the tile (ADR 0187): no
 * item picked yet, the item gone, the item not mapped to a source, or its source
 * not reporting. Never a fallback to a path the tile once held.
 */
function EquipmentTileNotice({ title, problem, editing, onConfigure }: {
  title: string
  problem: TileProblem
  editing: boolean
  onConfigure: () => void
}) {
  const item = 'item' in problem ? problem.item : null
  return (
    <Tile
      title={title}
      icon={<GaugeIcon className="h-3.5 w-3.5 text-gauge-secondary" />}
      titleExtra={
        editing ? (
          <button type="button" className={tilePillButtonClass} onClick={onConfigure} aria-label={`Configure ${title}`}>
            <Settings2 className="size-3.5" />
          </button>
        ) : undefined
      }
    >
      <div data-testid="equipment-tile-notice" className="flex h-full min-h-16 flex-col items-center justify-center gap-2 text-center">
        <p className="font-display text-sm uppercase tracking-[0.14em] text-muted-foreground">{problemMessage(problem)}</p>
        {item ? (
          <a
            className="text-xs text-primary underline"
            href={formatAppLocation({ panel: 'inventory', route: { view: 'equipment', id: item.id } }, { firstPageId: null })}
          >
            {item.name}
          </a>
        ) : (
          <button type="button" className="text-xs text-primary underline" onClick={onConfigure}>
            {problem.kind === 'pick' ? 'Choose the equipment this tile shows' : 'Choose different equipment'}
          </button>
        )}
      </div>
    </Tile>
  )
}

interface HostProps<C> {
  config: C
  ctx: EquipmentTileContext
  editing: boolean
  onConfigure: () => void
  values: Record<string, number | null>
  ages?: Record<string, number | null>
}

export const EngineClusterHost = memo(function EngineClusterHost({
  config, ctx, values, ages, engines, tanks, editing, onConfigure,
}: HostProps<EngineClusterConfig> & { engines?: EngineStates; tanks?: TankLevel[] }) {
  const resolved = useMemo(() => resolveCluster(config, ctx, tanks), [config, ctx, tanks])
  if (!resolved.ok) {
    return <EquipmentTileNotice title={config.title.trim() || 'Engine'} problem={resolved.problem} editing={editing} onConfigure={onConfigure} />
  }
  return <EngineClusterTile config={resolved.config} values={values} ages={ages} engines={engines} editing={editing} onConfigure={onConfigure} />
})

export const GaugeGroupHost = memo(function GaugeGroupHost({
  config, ctx, values, ages, editing, onConfigure,
}: HostProps<GaugeGroupWidgetConfig>) {
  const resolved = useMemo(
    () => (isEquipmentGroup(config) ? resolveGaugeGroup(config, ctx) : null),
    [config, ctx],
  )
  if (resolved && !resolved.ok) {
    return <EquipmentTileNotice title={config.title.trim() || 'Gauges'} problem={resolved.problem} editing={editing} onConfigure={onConfigure} />
  }
  return <GaugeGroupTile config={resolved ? resolved.config : config} values={values} ages={ages} editing={editing} onConfigure={onConfigure} />
})

export const LampStripHost = memo(function LampStripHost({
  config, ctx, ...rest
}: { config: LampStripWidgetConfig; ctx: EquipmentTileContext } & Omit<React.ComponentProps<typeof LampStripTile>, 'config'>) {
  const resolved = useMemo(() => resolveLamps(config, ctx), [config, ctx])
  return <LampStripTile config={resolved} {...rest} />
})
