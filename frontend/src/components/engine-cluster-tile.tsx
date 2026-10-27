import { Gauge as GaugeIcon, Settings2 } from 'lucide-react'
import { memo } from 'react'

import {
  AlternatorCard, BatteryCard, FOOTER_ICONS, FuelFan, GapArc, ReadingCard, ReadoutContent, SlotIcon,
  gapArcNames, toneStroke,
} from '@/components/engine-cluster-parts'
import { FitCanvas } from '@/components/fit-canvas'
import { majorStepFor } from '@/components/gauge-tile'
import { ClusterDial, CLUSTER_DIAL_SIZE } from '@/components/ui/cluster-dial'
import { Tile, tilePillButtonClass, tilePillClass } from '@/components/ui/tile'
import { cn } from '@/lib/utils'
import type { EngineStates } from '@/hooks/use-gauge-values'
import { clusterSlots, reading } from '@/lib/cluster-readings'
import type { ResolvedClusterConfig } from '@/lib/dashboard-widgets'
import { toneFor } from '@/lib/engine-cluster-bullet'
import { propulsionId } from '@/lib/equipment-tiles'
import {
  CLUSTER_H, CLUSTER_INNER_H, CLUSTER_INNER_TOP, CLUSTER_W, DIAL_CX, DIAL_CY, clusterLayout,
} from '@/lib/engine-cluster-layout'
import { severityTextClass, worstZoneState, type ZoneState } from '@/lib/severity'
import { formatDataAge } from '@/lib/staleness'

/**
 * An engine cluster: one engine as an 800 x 616 card. A ticked dial with the
 * rpm inside it, two arcs in the gap at its foot, four readout cards cut round
 * it, a fuel fan on the outer edge and a footer for battery, alternator,
 * transmission and exhaust.
 *
 * One tile is one engine. `side` mirrors the layout so a facing pair keep their
 * fuel on the outer edges and their readouts on the inner ones.
 *
 * Every slot is a GaugeWidgetConfig, and every band, tick and colour on the
 * card is drawn from that slot's zones: the graphs and the alarms are the same
 * thresholds.
 */

/**
 * The hub reads revs the way the reference does: whole revs rounded to the
 * nearest ten, as plain rpm. The scale around it is divided by a hundred and
 * says so, so the two never read as different numbers.
 */
function hubText(ring: ReturnType<typeof reading>, unit: string): string | null {
  if (ring.converted === null) return null
  return unit === 'rpm' ? String(Math.round(ring.converted / 10) * 10) : ring.text
}

/** `map` with every `propulsion.<id>.*` path removed: those readings are unknown while the engine is off. */
function withoutEnginePaths<T>(map: Record<string, T> | undefined, id: string): Record<string, T> | undefined {
  if (!map) return map
  const prefix = `propulsion.${id}.`
  return Object.fromEntries(Object.entries(map).filter(([path]) => !path.startsWith(prefix)))
}

/** "Off since 04:30" in the browser's local time, or just "Off" when the time is unusable. */
function offSinceLabel(lastUpdate: string | undefined): string {
  const at = lastUpdate ? new Date(lastUpdate) : null
  if (!at || Number.isNaN(at.getTime())) return 'Off'
  return `Off since ${at.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', hour12: false })}`
}

interface EngineClusterTileProps {
  config: ResolvedClusterConfig
  values: Record<string, number | null>
  /** Age in seconds behind each bound path (ADR 0083); absent is unknown. */
  ages?: Record<string, number | null>
  /**
   * Whether each engine reads as switched off (its computer powers down with
   * the key, so its readings just stop). Absent or unknown changes nothing.
   */
  engines?: EngineStates
  editing: boolean
  onConfigure: () => void
}

/**
 * The title-row chip: WARN or ALARM for a running engine, or null. A healthy engine
 * has no chip; the turning needle already says it is running. It is
 * drawn only for an engine known to be turning on live data: not for one that is
 * off (the title's OFF pill says so once), whose feed is lost, whose rpm is
 * stale, missing or zero, or whose tile is stale. Stale data carries no word
 * beside the STALE border.
 */
function chipFor(running: boolean, state: ZoneState | null): { text: string; className: string } | null {
  if (!running) return null
  if (state === 'alarm' || state === 'emergency') return { text: 'ALARM', className: 'bg-red-600 text-white' }
  if (state === 'warn' || state === 'alert') return { text: 'WARN', className: 'bg-amber-500 text-black' }
  return null
}

export const EngineClusterTile = memo(function EngineClusterTile({
  config, values: liveValues, ages: liveAges, engines, editing, onConfigure,
}: EngineClusterTileProps) {
  const side = config.side ?? 'port'

  // A switched-off engine's last readings are idle numbers, not facts: its
  // own paths read as absent (the dash) and carry no age, so nothing on the
  // tile is stale. Other bound paths, such as the fuel tank, are live.
  const engineId = propulsionId(config.ring.path)
  const engineStatus = engineId ? engines?.[engineId] : undefined
  const off = engineStatus?.state === 'off'
  const values = off && engineId ? withoutEnginePaths(liveValues, engineId)! : liveValues
  const ages = off && engineId ? withoutEnginePaths(liveAges, engineId) : liveAges

  const title = config.title.trim() || 'Engine'
  const layout = clusterLayout(side)

  const ring = reading(config.ring, values, ages)
  const centre = reading(config.centre, values, ages)
  const ringMin = config.ring.min ?? 0
  const ringMax = config.ring.max ?? 3000
  const ringStep = majorStepFor(ringMin, ringMax)

  // The tile edge carries the worst of everything the card reads (ADR 0081),
  // each slot's zone already blanked when its reading is stale (ADR 0083). An
  // engine that is off has no readings of its own (they were removed above), so
  // it adds no state: neutral border, OFF pill. A slot reading another item (the
  // starter battery, a tank) is live and still counts.
  const readings = clusterSlots(config).map((slot) => reading(slot, values, ages))
  const state = worstZoneState(readings.map((r) => r.zone))
  const knownAges = readings.filter((r) => r.age !== null)
  const tileStale = knownAges.length > 0 && knownAges.every((r) => r.stale)
  const staleLabel = tileStale ? formatDataAge(Math.max(...knownAges.map((r) => r.age as number))) : undefined

  const footer = config.footer ?? {}
  const running = !off && !tileStale && engineStatus?.state !== 'lost' && ring.converted !== null && ring.converted > 0
  const chip = chipFor(running, state)
  const stbd = side === 'stbd'

  const cornerTones = config.corners.slice(0, 4).map((corner) => {
    const row = corner.rows[0]
    return row ? toneFor(reading(row, values, ages).zone) : 'neutral'
  })

  return (
    <Tile
      title={title}
      fill
      state={state}
      stale={tileStale}
      staleLabel={staleLabel}
      off={off}
      offLabel={off ? offSinceLabel(engineStatus?.lastUpdate) : undefined}
      icon={<GaugeIcon className="h-3.5 w-3.5 text-gauge-secondary" />}
      titleExtra={
        chip || editing ? (
          <>
            {chip && (
              <span data-testid="cluster-chip"
                className={cn(tilePillClass, 'font-semibold', chip.className)}>
                {chip.text}
              </span>
            )}
            {editing && (
              <button type="button" className={tilePillButtonClass} onClick={onConfigure} aria-label={`Configure ${title}`}>
                <Settings2 className="size-3.5" />
              </button>
            )}
          </>
        ) : undefined
      }
    >
      <FitCanvas designWidth={CLUSTER_W} designHeight={CLUSTER_H} canvasProps={{ 'data-cluster-canvas': '', 'data-side': side }}>
        <div className="absolute left-0" style={{ top: CLUSTER_INNER_TOP, width: CLUSTER_W, height: CLUSTER_INNER_H }}>
          {/* The card outlines and the fuel fan share one SVG layer under the
              dial: outlines first, so nothing they paint lands on a reading. */}
          <svg width={CLUSTER_W} height={CLUSTER_INNER_H} viewBox={`0 0 ${CLUSTER_W} ${CLUSTER_INNER_H}`}
            className="absolute left-0 top-0" aria-hidden="true">
            {config.fuel && (
              <path data-fuel-card="" d={layout.fuelCard} strokeWidth={1.5}
                style={{ fill: 'hsl(var(--cluster-surface))', stroke: 'hsl(var(--cluster-stroke))' }} />
            )}
            {layout.readouts.map((box, i) => (
              <path key={i} data-card-outline={i} d={box.path}
                strokeWidth={cornerTones[i] === 'neutral' ? 1.5 : 2.5}
                style={{ fill: 'hsl(var(--cluster-surface))', stroke: toneStroke(cornerTones[i] ?? 'neutral') }} />
            ))}
            {config.fuel && <FuelFan config={config.fuel} title={title} side={side} values={values} ages={ages} />}
          </svg>

          <div data-cluster-dial="" className="absolute"
            style={{ left: DIAL_CX - CLUSTER_DIAL_SIZE / 2, top: DIAL_CY - CLUSTER_DIAL_SIZE / 2 }}>
            <ClusterDial
              value={ring.converted}
              min={ringMin}
              max={ringMax}
              majorStep={ringStep}
              labelDivisor={config.ring.labelDivisor}
              zones={config.ring.zones}
            >
              <div data-testid="cluster-centre" className="flex flex-col items-center gap-1.5">
                <span data-testid="cluster-centre-value"
                  className={`font-display text-6xl font-semibold leading-[0.9] tracking-tight tabular-nums ${
                    severityTextClass(ring.zone, 'text-foreground')}`}>
                  {hubText(ring, config.ring.unit) ?? '--'}
                </span>
                <div className="h-0.5 w-[118px] bg-foreground/25" />
                <span className="text-2xl font-semibold uppercase tracking-[0.16em] text-foreground">
                  {config.ring.label.trim() || ring.unit}
                </span>
                {config.ring.labelDivisor ? (
                  <span data-testid="cluster-scale-note" className="text-xs uppercase tracking-[0.16em] text-muted-foreground">
                    scale x{config.ring.labelDivisor}
                  </span>
                ) : null}
                <span data-testid="cluster-hours" className="mt-1 flex items-center gap-2">
                  <SlotIcon name={config.centreIcon} slot={config.centre} className="size-4 shrink-0 text-primary" />
                  <span className="font-display text-lg font-semibold tabular-nums text-foreground">{centre.text ?? '--'}</span>
                  <span className="text-xs font-semibold tracking-[0.16em] text-muted-foreground">HRS</span>
                </span>
              </div>
            </ClusterDial>
          </div>

          <svg width={CLUSTER_W} height={CLUSTER_INNER_H} viewBox={`0 0 ${CLUSTER_W} ${CLUSTER_INNER_H}`}
            className="pointer-events-none absolute left-0 top-0" aria-hidden="true">
            {gapArcNames().map((name) => (
              <GapArc key={name} name={name} slot={config.arcs?.[name]} values={values} ages={ages} />
            ))}
          </svg>

          {config.corners.slice(0, 4).map((corner, i) => {
            const row = corner.rows[0]
            const box = layout.readouts[i]
            if (!row) return null
            return (
              <div key={i} className="absolute" style={{ left: box.left, top: box.top, width: box.width }}>
                <ReadoutContent
                  index={i} slot={row} icon={corner.icon} label={corner.label.trim() || row.label.trim()}
                  values={values} ages={ages} side={side} width={box.width} bulletWidth={layout.bulletWidth}
                />
              </div>
            )
          })}

          <div data-testid="cluster-footer" className="absolute flex items-center gap-3"
            style={{ left: 40, top: 536, width: 720, height: 100, flexDirection: stbd ? 'row-reverse' : 'row' }}>
            <BatteryCard slot={footer.battery} values={values} ages={ages} />
            <AlternatorCard footer={footer} values={values} ages={ages}
              historyEnabled={!off && footer.altAmps !== undefined} />
            <ReadingCard
              testId="cluster-trans" label="Trans" icon={FOOTER_ICONS.trans}
              slot={footer.transPressure}
              flex={`0 0 ${layout.bulletWidth + 40}px`} values={values} ages={ages}
              bulletWidth={layout.bulletWidth} side={side}
            />
            <ReadingCard
              testId="cluster-egt" label="EGT" icon={FOOTER_ICONS.egt}
              slot={footer.egt}
              flex={`0 0 ${layout.bulletWidth + 24}px`} values={values} ages={ages}
              bulletWidth={layout.bulletWidth} side={side}
            />
          </div>
        </div>

      </FitCanvas>
    </Tile>
  )
})
