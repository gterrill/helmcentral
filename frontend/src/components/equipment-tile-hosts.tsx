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
import {
  engineInstance,
  findItem,
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
}

/**
 * Every bound path in a tile's config: each slot, lamp and bar carries its own
 * `path`, so walking the config cannot miss one a tile later grows.
 */
function boundPaths(config: unknown): string[] {
  const out = new Set<string>()
  const walk = (node: unknown) => {
    if (Array.isArray(node)) {
      for (const child of node) walk(child)
    } else if (node !== null && typeof node === 'object') {
      for (const [key, child] of Object.entries(node)) {
        if (key === 'path' && typeof child === 'string') {
          if (child.trim() !== '') out.add(child)
        } else {
          walk(child)
        }
      }
    }
  }
  walk(config)
  return [...out].sort()
}

/** The live values and ages for the paths a resolved config reads, from the gauge-values store (ADR 0215). */
function useBoundReadings(config: unknown) {
  const paths = useMemo(() => boundPaths(config), [config])
  return useGaugeSubset(paths)
}

/**
 * The state of the engine an item is, when it is one, as the one-entry map
 * the resolver and the cluster read. Undefined when the item is not an engine
 * or the server cannot classify it.
 */
function useItemEngine(ctx: EquipmentTileContext, equipmentId: string | undefined): EngineStates | undefined {
  const item = findItem(ctx, equipmentId)
  const id = item && item.source_kind === 'engine' && item.signalk_source !== '' ? engineInstance(item.signalk_source) : null
  const status = useEngineStatus(id)
  return useMemo(() => (id !== null && status ? { [id]: status } : undefined), [id, status])
}

export const EngineClusterHost = memo(function EngineClusterHost({
  config, ctx, tanks, editing, onConfigure,
}: HostProps<EngineClusterConfig> & { tanks?: TankLevel[] }) {
  const engines = useItemEngine(ctx, config.equipmentId)
  const resolved = useMemo(() => resolveCluster(config, ctx, tanks, engines), [config, ctx, tanks, engines])
  const { values, ages } = useBoundReadings(resolved.ok ? resolved.config : null)
  if (!resolved.ok) {
    return <EquipmentTileNotice title={config.title.trim() || 'Engine'} problem={resolved.problem} editing={editing} onConfigure={onConfigure} />
  }
  return <EngineClusterTile config={resolved.config} values={values} ages={ages} engines={engines} editing={editing} onConfigure={onConfigure} />
})

export const GaugeGroupHost = memo(function GaugeGroupHost({
  config, ctx, editing, onConfigure,
}: HostProps<GaugeGroupWidgetConfig>) {
  const equipment = isEquipmentGroup(config)
  const engines = useItemEngine(ctx, equipment ? config.equipmentId : undefined)
  const resolved = useMemo(
    () => (equipment ? resolveGaugeGroup(config, ctx, engines) : null),
    [equipment, config, ctx, engines],
  )
  const shown = resolved ? (resolved.ok ? resolved.config : null) : config
  const { values, ages } = useBoundReadings(shown)
  if (resolved && !resolved.ok) {
    return <EquipmentTileNotice title={config.title.trim() || 'Gauges'} problem={resolved.problem} editing={editing} onConfigure={onConfigure} />
  }
  return <GaugeGroupTile config={shown ?? config} values={values} ages={ages} editing={editing} onConfigure={onConfigure} />
})

export const LampStripHost = memo(function LampStripHost({
  config, ctx, ...rest
}: { config: LampStripWidgetConfig; ctx: EquipmentTileContext } & Omit<React.ComponentProps<typeof LampStripTile>, 'config' | 'values' | 'ages'>) {
  const resolved = useMemo(() => resolveLamps(config, ctx), [config, ctx])
  const { values, ages } = useBoundReadings(resolved)
  return <LampStripTile config={resolved} values={values} ages={ages} {...rest} />
})
