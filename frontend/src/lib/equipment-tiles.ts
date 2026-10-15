import type { EquipmentItem } from '@/hooks/use-inventory'
import type { EngineProfile, EngineProfileGauge, EngineProfileZone } from '@/lib/engine-profiles'
import type {
  EngineClusterConfig,
  GaugeGroupWidgetConfig,
  GaugeWidgetConfig,
  GaugeZone,
  LampStripWidgetConfig,
  ResolvedClusterConfig,
} from '@/lib/dashboard-widgets'

/**
 * How a tile that shows a piece of equipment finds its readings (ADR 0185,
 * phase 2). The tile stores the item's id and a suffix per reading. The path is
 * the item's SignalK source plus the suffix, and the zones are the ones on the
 * gauge of the item's profile with the same suffix, so the tile, the alarms
 * raised from its zones and the profile can never disagree.
 *
 * Mirrors backend/dashboard_equipment.go; the backend does the same walk for
 * the alarms and for which paths to stream.
 */

/** The part of an equipment item a tile needs. */
export type TileItem = Pick<EquipmentItem, 'id' | 'name' | 'signalk_source' | 'source_kind' | 'profile_id'>

export interface EquipmentTileContext {
  items: readonly TileItem[]
  profiles: readonly EngineProfile[]
  /**
   * Sources the boat is reporting now (Discovery). Null while that is not known
   * (loading, or the request failed): the tile then shows its readings rather
   * than claiming the item is silent.
   */
  reporting: ReadonlySet<string> | null
  /** Engines whose state the server knows, by instance id (`port`). An engine that is off is known, not silent. */
  engines?: Readonly<Record<string, unknown>>
}

/** Why a tile cannot show its readings. `item` is set when the item exists. */
export type TileProblem =
  | { kind: 'pick' }
  | { kind: 'missing' }
  | { kind: 'not-mapped'; item: TileItem }
  | { kind: 'not-reporting'; item: TileItem }

export type Resolved<T> = { ok: true; config: T; item: TileItem } | { ok: false; problem: TileProblem }

const EMPTY_PROBLEM: TileProblem = { kind: 'pick' }

export function findItem(ctx: EquipmentTileContext, id: string | undefined): TileItem | undefined {
  return id ? ctx.items.find((item) => item.id === id) : undefined
}

/** `propulsion.port` -> `port`; the instance id of an engine source. */
function engineInstance(source: string): string {
  return source.split('.').slice(1).join('.')
}

/** The problem with an item, or null when its readings can be shown. */
function itemProblem(ctx: EquipmentTileContext, item: TileItem): TileProblem | null {
  if (item.signalk_source === '') return { kind: 'not-mapped', item }
  if (ctx.reporting !== null && !ctx.reporting.has(item.signalk_source)) {
    const known = item.source_kind === 'engine' && ctx.engines !== undefined && engineInstance(item.signalk_source) in ctx.engines
    if (!known) return { kind: 'not-reporting', item }
  }
  return null
}

export function profileOf(ctx: EquipmentTileContext, item: TileItem): EngineProfile | undefined {
  return item.profile_id ? ctx.profiles.find((p) => p.id === item.profile_id) : undefined
}

export function profileGaugeFor(profile: EngineProfile | undefined, suffix: string): EngineProfileGauge | undefined {
  return profile?.gauges?.find((g) => g.path_suffix === suffix)
}

/** A profile zone as the from/to band a tile draws, anchored to the scale. A zone with no threshold is a slot, never a band. */
function toBand(zone: EngineProfileZone, min: number, max: number): GaugeZone | null {
  if (zone.threshold === null || zone.threshold === undefined) return null
  return zone.direction === 'below'
    ? { from: min, to: zone.threshold, state: zone.state }
    : { from: zone.threshold, to: max, state: zone.state }
}

/** The bands of a profile gauge on a given scale. */
export function profileBands(gauge: EngineProfileGauge, min: number, max: number): GaugeZone[] {
  return (gauge.zones ?? []).map((z) => toBand(z, min, max)).filter((z): z is GaugeZone => z !== null)
}

/**
 * The scale a slot's zones are anchored to: the slot's own minimum and maximum
 * when it sets them, else the profile gauge's, else 0 to 100. The one place that
 * rule lives (backend: gaugeScale in dashboard_equipment.go).
 */
export function effectiveScale(
  slotMin: number | undefined,
  slotMax: number | undefined,
  gauge: { min?: number; max?: number } | undefined,
): { min: number; max: number } {
  return { min: slotMin ?? gauge?.min ?? 0, max: slotMax ?? gauge?.max ?? 100 }
}

/**
 * One slot with its path and the profile's zones filled in. When the profile has
 * a gauge for the reading, the quantity and unit are the profile's, because its
 * thresholds are in that unit, and the slot carries the scale its zones were
 * anchored to.
 */
export function resolveSlot(item: TileItem, profile: EngineProfile | undefined, slot: GaugeWidgetConfig): GaugeWidgetConfig {
  const suffix = (slot.suffix ?? '').trim()
  const { suffix: _suffix, zones: _zones, ...rest } = slot
  void _suffix
  void _zones
  if (suffix === '') return { ...rest, path: '' }
  const gauge = profileGaugeFor(profile, suffix)
  if (!gauge) return { ...rest, path: `${item.signalk_source}.${suffix}` }
  const { min, max } = effectiveScale(slot.min, slot.max, gauge)
  const zones = profileBands(gauge, min, max)
  return {
    ...rest,
    path: `${item.signalk_source}.${suffix}`,
    quantity: gauge.quantity,
    unit: gauge.unit,
    min,
    max,
    ...(zones.length > 0 ? { zones } : {}),
  }
}

function slotsOf(config: EngineClusterConfig): GaugeWidgetConfig[] {
  return [config.ring, config.centre, ...config.corners.flatMap((c) => c.rows), ...(config.telltales ?? [])]
}

/** Every slot of a cluster, for callers that need to walk them. */
export const clusterSlots = slotsOf

export function resolveCluster(config: EngineClusterConfig, ctx: EquipmentTileContext): Resolved<ResolvedClusterConfig> {
  if (!config.equipmentId) return { ok: false, problem: EMPTY_PROBLEM }
  const item = findItem(ctx, config.equipmentId)
  if (!item) return { ok: false, problem: { kind: 'missing' } }
  const problem = itemProblem(ctx, item)
  if (problem) return { ok: false, problem }

  const profile = profileOf(ctx, item)
  const slot = (s: GaugeWidgetConfig) => resolveSlot(item, profile, s)
  const fuel = config.fuel && {
    side: config.fuel.side,
    totalLabel: config.fuel.totalLabel,
    bars: config.fuel.bars.map((bar) => {
      const tank = findItem(ctx, bar.tankId)
      const source = tank && tank.source_kind === 'tank' ? tank.signalk_source : ''
      const label = bar.label ?? ''
      return {
        level: {
          path: source ? `${source}.currentLevel` : '', label, display: 'bar' as const, quantity: 'ratio', unit: 'percent', decimals: 0, min: 0, max: 100,
          ...(bar.zones && bar.zones.length > 0 ? { zones: bar.zones } : {}),
        },
        capacity: { path: source ? `${source}.capacity` : '', label: '', display: 'numeric' as const, quantity: 'volume', unit: 'L', decimals: 0 },
      }
    }),
  }
  return {
    ok: true,
    item,
    config: {
      ...config,
      title: config.title.trim() || item.name,
      ring: slot(config.ring),
      centre: slot(config.centre),
      corners: config.corners.map((corner) => ({ ...corner, rows: corner.rows.map(slot) })),
      telltales: config.telltales?.map(slot),
      fuel,
    },
  }
}

/** Whether a gauge group shows equipment (as opposed to a plain list of paths). */
export function isEquipmentGroup(config: GaugeGroupWidgetConfig): boolean {
  return (config.equipmentKind ?? '') !== ''
}

export function resolveGaugeGroup(config: GaugeGroupWidgetConfig, ctx: EquipmentTileContext): Resolved<GaugeGroupWidgetConfig> {
  if (!config.equipmentId) return { ok: false, problem: EMPTY_PROBLEM }
  const item = findItem(ctx, config.equipmentId)
  if (!item) return { ok: false, problem: { kind: 'missing' } }
  const problem = itemProblem(ctx, item)
  if (problem) return { ok: false, problem }
  const profile = profileOf(ctx, item)
  return {
    ok: true,
    item,
    config: { ...config, title: config.title.trim() || item.name, gauges: config.gauges.map((g) => resolveSlot(item, profile, g)) },
  }
}

/**
 * A strip with every equipment lamp's path filled in. A lamp whose item is
 * gone, unmapped or unpicked gets no path and reads as no data, so the strip
 * still draws the lamps that can be read.
 */
export function resolveLamps(config: LampStripWidgetConfig, ctx: EquipmentTileContext): LampStripWidgetConfig {
  return {
    ...config,
    lamps: config.lamps.map((lamp) => {
      if (!lamp.equipmentId) return lamp
      const item = findItem(ctx, lamp.equipmentId)
      const suffix = (lamp.suffix ?? '').trim()
      return { ...lamp, path: item && item.signalk_source !== '' && suffix !== '' ? `${item.signalk_source}.${suffix}` : '' }
    }),
  }
}

/** The text a tile shows for a problem. */
export function problemMessage(problem: TileProblem): string {
  switch (problem.kind) {
    case 'pick': return 'Pick equipment'
    case 'missing': return 'Equipment removed'
    case 'not-mapped': return 'Not mapped'
    case 'not-reporting': return 'Not reporting'
  }
}

/**
 * The names of the items that follow a profile, for the notice that says what
 * editing a profile's zones touches: "Applies to every tile and alarm using
 * Yanmar 4JH (Port engine, Starboard engine)".
 */
export function itemsUsingProfile(ctx: EquipmentTileContext, profileId: string): string[] {
  return ctx.items.filter((i) => i.profile_id === profileId).map((i) => i.name)
}

/**
 * Bands as a profile holds them: a direction and a threshold. A band touching
 * neither end of the scale has no such form, which the zone editor cannot
 * produce, so it is left out rather than guessed at.
 */
export function bandsToProfileZones(bands: readonly GaugeZone[], min: number, max: number): EngineProfileZone[] {
  const epsilon = (max - min) * 0.001
  const out: EngineProfileZone[] = []
  for (const band of bands) {
    if (band.from <= min + epsilon && band.to < max - epsilon) {
      out.push({ direction: 'below', threshold: band.to, state: band.state })
    } else if (band.to >= max - epsilon && band.from > min + epsilon) {
      out.push({ direction: 'above', threshold: band.from, state: band.state })
    }
  }
  return out
}

/**
 * The profile with the zones of one gauge replaced. Zones the manufacturer
 * defines without a published threshold (slots) are kept; they are not bands
 * the editor shows.
 */
export function withGaugeZones(
  profile: EngineProfile,
  suffix: string,
  bands: readonly GaugeZone[],
  min: number,
  max: number,
): EngineProfile {
  return {
    ...profile,
    gauges: (profile.gauges ?? []).map((gauge) => {
      if (gauge.path_suffix !== suffix) return gauge
      const slots = (gauge.zones ?? []).filter((z) => z.threshold === null || z.threshold === undefined)
      const zones = [...slots, ...bandsToProfileZones(bands, min, max)]
      return { ...gauge, min: gauge.min ?? min, max: gauge.max ?? max, zones }
    }),
  }
}
