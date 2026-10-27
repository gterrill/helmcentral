import { Gauge as GaugeIcon, Settings2 } from 'lucide-react'
import { memo, useMemo } from 'react'

import { EngineClusterTile } from '@/components/engine-cluster-tile'
import { GaugeGroupTile } from '@/components/gauge-group-tile'
import { LampStripTile } from '@/components/lamp-strip-tile'
import { Tile, tilePillButtonClass } from '@/components/ui/tile'
import { useEngineStatus, useGaugeSubset, type EngineStates } from '@/hooks/use-gauge-values'
import type { TankLevel } from '@/hooks/use-tanks-state'
import { formatAppLocation } from '@/lib/app-location'
import type { EngineClusterConfig, GaugeGroupWidgetConfig, LampStripWidgetConfig } from '@/lib/dashboard-widgets'
import { clusterPaths } from '@/lib/cluster-readings'
import {
  engineIdOf,
  findItem,
  isEquipmentGroup,
  propulsionId,
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
}

const NO_PATHS: readonly string[] = []

/**
 * The state of one engine, by instance id, as the one-entry map the resolver
 * and the cluster read. Undefined for no engine, or one the server cannot
 * classify.
 */
function useEngine(id: string | null): EngineStates | undefined {
  const status = useEngineStatus(id)
  return useMemo(() => (id !== null && status ? { [id]: status } : undefined), [id, status])
}

export const EngineClusterHost = memo(function EngineClusterHost({
  config, ctx, tanks, editing, onConfigure,
}: HostProps<EngineClusterConfig> & { tanks?: TankLevel[] }) {
  // An engine item that is off is known, not silent, when it comes to saying
  // Not reporting. OFF on the card follows the propulsion id the readings come
  // from, whatever kind the item is.
  const itemEngine = useEngine(engineIdOf(findItem(ctx, config.equipmentId)))
  const resolved = useMemo(() => resolveCluster(config, ctx, tanks, itemEngine), [config, ctx, tanks, itemEngine])
  const shown = resolved.ok ? resolved.config : null
  const cardEngine = useEngine(shown ? propulsionId(shown.ring.path) : null)
  const paths = useMemo(() => (shown ? clusterPaths(shown) : NO_PATHS), [shown])
  const { values, ages } = useGaugeSubset(paths)
  if (!resolved.ok) {
    return <EquipmentTileNotice title={config.title.trim() || 'Engine'} problem={resolved.problem} editing={editing} onConfigure={onConfigure} />
  }
  return <EngineClusterTile config={resolved.config} values={values} ages={ages} engines={cardEngine} editing={editing} onConfigure={onConfigure} />
})

export const GaugeGroupHost = memo(function GaugeGroupHost({
  config, ctx, editing, onConfigure,
}: HostProps<GaugeGroupWidgetConfig>) {
  const equipment = isEquipmentGroup(config)
  const engines = useEngine(equipment ? engineIdOf(findItem(ctx, config.equipmentId)) : null)
  const resolved = useMemo(
    () => (isEquipmentGroup(config) ? resolveGaugeGroup(config, ctx, engines) : null),
    [config, ctx, engines],
  )
  const shown = resolved ? (resolved.ok ? resolved.config : null) : config
  const paths = useMemo(() => (shown ? shown.gauges.map((g) => g.path).filter((p) => p.trim() !== '') : NO_PATHS), [shown])
  const { values, ages } = useGaugeSubset(paths)
  if (resolved && !resolved.ok) {
    return <EquipmentTileNotice title={config.title.trim() || 'Gauges'} problem={resolved.problem} editing={editing} onConfigure={onConfigure} />
  }
  return <GaugeGroupTile config={shown ?? config} values={values} ages={ages} editing={editing} onConfigure={onConfigure} />
})

export const LampStripHost = memo(function LampStripHost({
  config, ctx, ...rest
}: { config: LampStripWidgetConfig; ctx: EquipmentTileContext } & Omit<React.ComponentProps<typeof LampStripTile>, 'config' | 'values' | 'ages'>) {
  const resolved = useMemo(() => resolveLamps(config, ctx), [config, ctx])
  const paths = useMemo(() => resolved.lamps.map((l) => l.path).filter((p) => p.trim() !== ''), [resolved])
  const { values, ages } = useGaugeSubset(paths)
  return <LampStripTile config={resolved} values={values} ages={ages} {...rest} />
})
