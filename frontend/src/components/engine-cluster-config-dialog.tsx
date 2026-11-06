import { ChevronRight, Plus, Trash2 } from 'lucide-react'
import { useEffect, useMemo, useRef, useState, type ReactNode } from 'react'

import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Field, FieldDescription, FieldLabel } from '@/components/ui/field'
import { GaugeFields, slotFromProfileGauge } from '@/components/gauge-fields'
import { Input } from '@/components/ui/input'
import { BREAKPOINTS, useMinWidth } from '@/lib/breakpoints'
import { cn } from '@/lib/utils'
import { EquipmentPicker } from '@/components/equipment-picker'
import { useEquipmentTileContext } from '@/hooks/use-equipment-tile-context'
import { useSignalKPaths } from '@/hooks/use-signalk-paths'
import { useVesselRoles } from '@/hooks/use-vessel-roles'
import { useTileSave } from '@/hooks/use-tile-save'
import { useSlotZoneEditing } from '@/hooks/use-slot-zone-editing'
import { ZoneConflicts } from '@/components/zone-conflicts'
import {
  CLUSTER_ALTERNATOR_SLOTS,
  CLUSTER_MAX_CORNERS,
  CLUSTER_MAX_CORNER_ROWS,
  CLUSTER_MAX_FUEL_BARS,
  CLUSTER_FOOTER_SLOTS,
  GAUGE_GROUP_TITLE_MAX_LENGTH,
  isClusterAlternatorSlot,
  type ClusterAlternatorSlot,
  type ClusterFooter,
  type ClusterFooterSlot,
  type ClusterFuelRailRef,
  type ClusterSide,
  type DashboardLayoutItem,
  type EngineClusterConfig,
  type FuelBarRef,
  type GaugeWidgetConfig,
} from '@/lib/dashboard-widgets'
import { CLUSTER_ICON_NAMES, iconForSlot } from '@/lib/cluster-icons'
import { findItem, profileGaugeFor, profileOf, type EquipmentTileContext, type TileItem } from '@/lib/equipment-tiles'
import type { EngineProfile } from '@/lib/engine-profiles'
import { liveReadings } from '@/lib/live-readings'
import { ROLE_NONE } from '@/lib/vessel-settings'

/**
 * The slots a cluster fills from the engine's profile, and the suffix each looks
 * for. Anything the profile does not cover keeps the suffix and is set by hand.
 */
const SLOT_SUFFIXES = {
  ring: 'revolutions',
  // Hours in the hub, not fuel rate: hours barely move and suit a small line,
  // while fuel rate changes with every throttle input and earns a full readout
  // with a scale under it.
  centre: 'runTime',
  corners: [
    { label: 'Oil', suffixes: ['oilPressure'] },
    { label: 'Boost', suffixes: ['boostPressure'] },
    { label: 'Load', suffixes: ['engineLoad'] },
    // Burn rate only. Economy is a vessel figure, not an engine one: SignalK's
    // per-engine fuel.economy reads roughly twice the boat's on a twin, so
    // showing it here invites range planning that is out by the number of
    // engines running (ADR 0055).
    { label: 'Fuel', suffixes: ['fuel.rate'] },
  ],
  // The two arcs in the dial's gap: the engine's coolant on the left, the
  // gearbox oil temperature on the right (the engine has no oil temperature
  // sensor of its own).
  arcLeft: 'temperature',
  arcRight: 'transmission.oilTemperature',
  transPressure: 'transmission.oilPressure',
} as const

/**
 * The footer slots that read another item than the engine and name it
 * themselves: the kind of item each takes, the reading it starts on, and a blank
 * slot's own quantity (used until the item's profile has a gauge for the
 * reading). None carries a scale or a threshold: both come from the item's
 * profile. The alternator rows are not here: they share the footer's one
 * alternator.
 */
const FOREIGN_SLOTS: Record<'battery' | 'egt', {
  kind: 'battery' | 'engine'
  /** The picker's empty choice: the exhaust is read from an item of the engine kind, so "Pick engine" would mislead. */
  pick: string
  suffix: string
  slot: GaugeWidgetConfig
}> = {
  battery: { kind: 'battery', pick: 'Pick the starter battery…', suffix: 'voltage', slot: { path: '', label: 'Battery', display: 'numeric', quantity: 'potential', unit: 'V', decimals: 1 } },
  // The scale and any band (raw-water loss on a wet exhaust) are the exhaust
  // item's profile's to hold.
  egt: { kind: 'engine', pick: 'Pick the exhaust sensor…', suffix: 'exhaustTemperature', slot: { path: '', label: 'Exhaust', display: 'numeric', quantity: 'temperature', unit: 'C', decimals: 0 } },
}

/**
 * The five alternator rows: the reading each starts on (none for the charge
 * limit, which the operator chooses) and a blank row's own quantity, used until
 * the alternator's profile has a gauge for the reading.
 */
const ALTERNATOR_SLOTS: Record<ClusterAlternatorSlot, { suffix: string; slot: GaugeWidgetConfig }> = {
  altAmps: { suffix: 'current', slot: { path: '', label: 'Alternator', display: 'numeric', quantity: 'current', unit: 'A', decimals: 0 } },
  altWatts: { suffix: 'power', slot: { path: '', label: 'Alt output', display: 'numeric', quantity: 'power', unit: 'W', decimals: 0 } },
  altTemp: { suffix: 'temperature', slot: { path: '', label: 'Alt temp', display: 'numeric', quantity: 'temperature', unit: 'C', decimals: 0 } },
  // The regulator reports its stage as a number; the tile names it.
  altStage: { suffix: 'chargingModeNumber', slot: { path: '', label: 'Charge stage', display: 'numeric', quantity: 'raw', unit: 'raw', decimals: 0 } },
  altLimit: { suffix: '', slot: { path: '', label: 'Charge limit', display: 'numeric', quantity: 'current', unit: 'A', decimals: 0 } },
}

/** What a footer slot is called in full: its accessible name, its editor and the line beside Save. */
const FOOTER_TITLES: Record<ClusterFooterSlot, string> = {
  battery: 'Starter battery voltage',
  altAmps: 'Alternator current',
  altWatts: 'Alternator output power',
  altTemp: 'Alternator temperature',
  altStage: 'Charge stage',
  altLimit: 'Charge current limit',
  transPressure: 'Transmission oil pressure',
  egt: 'Exhaust temperature',
}

/** The same slots as they read on the card's schematic, where the room is a few characters. */
const FOOTER_SHORT: Record<ClusterFooterSlot, string> = {
  battery: 'Battery',
  altAmps: 'Current',
  altWatts: 'Output',
  altTemp: 'Temp',
  altStage: 'Stage',
  altLimit: 'Limit',
  transPressure: 'Trans',
  egt: 'Exhaust',
}

/** A line under a footer slot's editor, for the few that need one. */
const FOOTER_NOTES: Partial<Record<ClusterFooterSlot, string>> = {
  altAmps: 'The card also draws this as a one-hour trace.',
  altStage: 'The regulator reports its stage as a number; the card names it.',
}

type AlternatorRows = Partial<Record<ClusterAlternatorSlot, GaugeWidgetConfig>>

/**
 * Whether the equipment list has answered. The tile context says so, and an empty
 * list that has answered is a boat with no equipment. A context built by hand
 * without the signal counts as loaded once it has items.
 */
const listLoaded = (ctx: EquipmentTileContext) => ctx.loaded ?? ctx.items.length > 0

/** A slot without the equipment id it once named. */
function withoutEquipment(slot: GaugeWidgetConfig): GaugeWidgetConfig {
  const { equipmentId: _dropped, ...rest } = slot
  void _dropped
  return rest
}

/** Whether the item can show this reading: SignalK sends it now, or its profile has a gauge for it. */
function itemSends(item: TileItem, ctx: EquipmentTileContext, live: readonly string[], suffix: string): boolean {
  return live.includes(suffix) || (profileOf(ctx, item)?.gauges ?? []).some((g) => g.path_suffix === suffix)
}

const blankAlternatorRow = (key: ClusterAlternatorSlot): GaugeWidgetConfig => ({ ...ALTERNATOR_SLOTS[key].slot, suffix: '' })

/**
 * One alternator row set to a reading. Quantity, unit and scale reset to the
 * row's own, and any zones go, unless the alternator's profile has a gauge for
 * the reading, which then decides them. The row holds no equipment.
 */
function alternatorRow(key: ClusterAlternatorSlot, base: GaugeWidgetConfig | undefined, suffix: string, item: TileItem | undefined, ctx: EquipmentTileContext): GaugeWidgetConfig {
  const spec = ALTERNATOR_SLOTS[key].slot
  const next = { ...withoutEquipment(base ?? spec), suffix }
  const gauge = item && suffix.trim() !== '' ? profileGaugeFor(profileOf(ctx, item), suffix.trim()) : undefined
  if (gauge) return slotFromProfileGauge({ ...next, zones: undefined }, gauge)
  const { quantity, unit, decimals, display } = spec
  return { ...next, quantity, unit, decimals, display, min: undefined, max: undefined, zones: undefined }
}

/**
 * Each alternator row after `choose` says which reading it takes: given the
 * reading the row has, the one it expects, what SignalK sends and whether the
 * alternator can show a reading, it returns the new reading ('' for blank), or
 * undefined to leave the row as it is. `livePaths` is null while SignalK's paths
 * are not known, and then only the profile can vouch for a reading.
 */
function mapAlternatorRows(
  rows: AlternatorRows, item: TileItem | undefined, ctx: EquipmentTileContext, livePaths: readonly string[] | null,
  choose: (have: string, expected: string, live: readonly string[], sends: (suffix: string) => boolean) => string | undefined,
): Record<ClusterAlternatorSlot, GaugeWidgetConfig> {
  const live = item && livePaths ? liveReadings(item.signalk_source, livePaths) : []
  const sends = (suffix: string) => item !== undefined && itemSends(item, ctx, live, suffix)
  return Object.fromEntries(CLUSTER_ALTERNATOR_SLOTS.map((key) => {
    const suffix = choose((rows[key]?.suffix ?? '').trim(), ALTERNATOR_SLOTS[key].suffix, live, sends)
    return [key, suffix === undefined ? (rows[key] ?? blankAlternatorRow(key)) : alternatorRow(key, rows[key], suffix, item, ctx)]
  })) as Record<ClusterAlternatorSlot, GaugeWidgetConfig>
}

/**
 * The five alternator rows after the alternator changes to `item` (none when
 * the picker is cleared). A row keeps its reading if the new alternator sends
 * it, live or on its profile. Otherwise it takes its expected reading if that
 * is sent, and otherwise goes blank.
 */
export function switchAlternatorRows(
  rows: AlternatorRows, item: TileItem | undefined, ctx: EquipmentTileContext, livePaths: readonly string[] | null,
): Record<ClusterAlternatorSlot, GaugeWidgetConfig> {
  return mapAlternatorRows(rows, item, ctx, livePaths, (have, expected, live, sends) => {
    if (!item) return ''
    if (have !== '' && sends(have)) return have
    return expected !== '' && live.includes(expected) ? expected : ''
  })
}

/**
 * The alternator rows with every blank one given its expected reading, if the
 * alternator is sending it. A row that already has a reading stays as the
 * operator chose it. With the paths unknown nothing is filled.
 */
export function fillBlankAlternatorRows(
  rows: AlternatorRows, item: TileItem | undefined, ctx: EquipmentTileContext, livePaths: readonly string[] | null,
): Record<ClusterAlternatorSlot, GaugeWidgetConfig> {
  return mapAlternatorRows(rows, item, ctx, livePaths, (have, expected, live) =>
    (have === '' && item && expected !== '' && live.includes(expected) ? expected : undefined))
}

/**
 * The alternator the Roles setting names for an Engine side: the starboard one
 * for a starboard engine, the port one otherwise. Empty when Roles names none,
 * says the boat has none, or can't be read.
 */
export function defaultAlternatorId(
  side: ClusterSide | undefined, roles: { port_alternator: string; starboard_alternator: string } | null,
): string {
  const id = (side === 'stbd' ? roles?.starboard_alternator : roles?.port_alternator) ?? ''
  return id === ROLE_NONE ? '' : id
}

/** Port or starboard from an engine's source, `propulsion.starboard` and so on; port when it says neither. */
function sideOf(source: string | undefined): ClusterSide {
  return /starboard|stbd/i.test(source ?? '') ? 'stbd' : 'port'
}

/** A fuel rail's edge follows the engine it belongs to: the outer edge. */
function fuelEdge(side: ClusterSide): ClusterFuelRailRef['side'] {
  return side === 'stbd' ? 'right' : 'left'
}

const isForeign = (key: ClusterFooterSlot): key is keyof typeof FOREIGN_SLOTS => key in FOREIGN_SLOTS

/**
 * The slots read from the cluster's own engine, in a fixed order: ring, centre,
 * corner rows, the two arcs, then the transmission slots. Their zones are the
 * engine profile's and are edited here. The footer slots that name their own
 * item are not in this list: their zones live on that item's profile.
 */
function clusterZoneSlots(
  c: EngineClusterConfig, cornerKeys: readonly string[] = [],
): Array<{ id: SlotId; key: string; slot: GaugeWidgetConfig }> {
  const named = (id: SlotId, slot: GaugeWidgetConfig, key: string = id) => ({ id, key, slot })
  return [
    named('ring', c.ring),
    named('centre', c.centre),
    // A readout's key is its own, so the zone choices on it survive the others coming and going.
    ...c.corners.flatMap((corner, i) => corner.rows.map((slot) => named(`corner:${i}`, slot, `corner:${cornerKeys[i] ?? i}`))),
    ...(c.arcs?.left ? [named('arcLeft', c.arcs.left)] : []),
    ...(c.arcs?.right ? [named('arcRight', c.arcs.right)] : []),
    ...(c.footer?.transPressure ? [named('footer:transPressure', c.footer.transPressure)] : []),
  ]
}

const clusterSlotList = (c: EngineClusterConfig): GaugeWidgetConfig[] => clusterZoneSlots(c).map((z) => z.slot)

/** The cluster with the zones that now live on the profile taken off the slots at `cleared`. */
function withoutSettledZones(c: EngineClusterConfig, cleared: Set<number>): EngineClusterConfig {
  let i = 0
  const settle = (slot: GaugeWidgetConfig): GaugeWidgetConfig => (cleared.has(i++) ? { ...slot, zones: undefined } : slot)
  const ring = settle(c.ring)
  const centre = settle(c.centre)
  const corners = c.corners.map((corner) => ({ ...corner, rows: corner.rows.map(settle) }))
  const arcs = c.arcs && {
    ...(c.arcs.left ? { left: settle(c.arcs.left) } : {}),
    ...(c.arcs.right ? { right: settle(c.arcs.right) } : {}),
  }
  const footer = c.footer && { ...c.footer }
  if (footer?.transPressure) footer.transPressure = settle(footer.transPressure)
  return { ...c, ring, centre, corners, arcs, footer }
}

function blankSlot(label: string): GaugeWidgetConfig {
  return { path: '', suffix: '', label, display: 'numeric', quantity: 'raw', unit: 'raw' }
}

/** A blank fuel bar: the tank is picked from the equipment list. */
function blankFuelBar(label: string): FuelBarRef {
  return { tankId: '', label }
}

/** An empty footer or arc slot: no reading yet, so the tile draws nothing for it. */
function blankArc(label: string, min: number, max: number): GaugeWidgetConfig {
  return { ...blankSlot(label), quantity: 'temperature', unit: 'C', decimals: 0, min, max }
}

/**
 * Builds the slots of a cluster from the engine's profile; the equipment is
 * already chosen. The alternator is the one on the alternator picker, which
 * this leaves alone: it only fills the alternator's blank rows, with readings
 * the alternator is sending. With no alternator chosen there is nothing to fill.
 */
export function clusterFromProfile(
  profile: EngineProfile, current: EngineClusterConfig, ctx: EquipmentTileContext,
  /** Every path SignalK publishes now, or null while that is not known. */
  livePaths: readonly string[] | null = null,
): EngineClusterConfig {
  // Zones a slot still holds are not the profile's to replace: carry them to
  // the new slot for the same reading, so filling never drops one.
  const held = new Map(clusterSlotList(current).filter((s) => (s.zones?.length ?? 0) > 0).map((s) => [s.suffix, s]))
  const pick = (suffix: string, fallbackLabel: string): GaugeWidgetConfig => {
    const gauge = profileGaugeFor(profile, suffix)
    const slot = gauge ? slotFromProfileGauge({ ...blankSlot(''), suffix }, gauge) : { ...blankSlot(fallbackLabel), suffix }
    const old = held.get(suffix)
    // The zones are in the old slot's unit and scale, so those come with them.
    return old
      ? { ...slot, zones: old.zones, quantity: old.quantity, unit: old.unit, min: old.min, max: old.max, ...(old.zoneIndex !== undefined ? { zoneIndex: old.zoneIndex } : {}) }
      : slot
  }

  const ring = { ...pick(SLOT_SUFFIXES.ring, 'RPM') }
  // RPM reads better as a divided scale, the way every tachometer draws it.
  ring.display = 'radial'
  ring.ringStyle = 'instrument'
  ring.labelDivisor = 100
  if (ring.max === undefined) ring.max = 3000

  const engine = findItem(ctx, current.equipmentId)
  const side = sideOf(engine?.signalk_source)

  const footer: NonNullable<EngineClusterConfig['footer']> = {}
  if (current.footer?.alternatorId !== undefined) footer.alternatorId = current.footer.alternatorId
  for (const key of CLUSTER_FOOTER_SLOTS) {
    if (!isForeign(key)) continue
    const spec = FOREIGN_SLOTS[key]
    // What the operator already chose for this slot stays.
    const existing = (current.footer?.[key]?.suffix ?? '').trim() !== '' ? current.footer?.[key] : undefined
    footer[key] = existing ?? { ...spec.slot, suffix: spec.suffix }
  }
  // What the alternator sends fills the rows still blank; a reading already
  // chosen stays.
  Object.assign(footer, fillBlankAlternatorRows(
    alternatorRowsOf(current), findItem(ctx, current.footer?.alternatorId || undefined), ctx, livePaths,
  ))
  footer.transPressure = { ...pick(SLOT_SUFFIXES.transPressure, 'Gear press'), decimals: 0 }

  return {
    ...current,
    side,
    ring,
    centre: pick(SLOT_SUFFIXES.centre, 'Hours'),
    corners: SLOT_SUFFIXES.corners.map((corner) => ({
      label: corner.label,
      rows: corner.suffixes.map((suffix) => pick(suffix, corner.label)),
    })),
    arcs: {
      // Whole degrees on an arc: a tenth of a degree of coolant is noise at a glance.
      left: { ...pick(SLOT_SUFFIXES.arcLeft, 'Coolant'), decimals: 0 },
      right: { ...pick(SLOT_SUFFIXES.arcRight, 'Gearbox'), decimals: 0 },
    },
    footer,
    ...(current.fuel ? { fuel: { ...current.fuel, side: fuelEdge(side) } } : {}),
  }
}

/**
 * The footer slot if it reads something, else undefined: it has a reading, and
 * for another item's slot the item too, and for an alternator row the footer's
 * alternator. One rule for what the tab counts and what a save keeps.
 */
function boundFooterSlot(footer: ClusterFooter | undefined, key: ClusterFooterSlot): GaugeWidgetConfig | undefined {
  const slot = footer?.[key]
  if (!slot || (slot.suffix ?? '').trim() === '') return undefined
  if (isClusterAlternatorSlot(key)) return (footer?.alternatorId ?? '') !== '' ? slot : undefined
  if (isForeign(key) && (slot.equipmentId ?? '') === '') return undefined
  return slot
}

/** The alternator rows a config holds, as they are. */
function alternatorRowsOf(config: EngineClusterConfig): AlternatorRows {
  return Object.fromEntries(CLUSTER_ALTERNATOR_SLOTS.flatMap((key) => {
    const row = config.footer?.[key]
    return row ? [[key, row]] : []
  })) as AlternatorRows
}

/**
 * Every optional slot present, blank where unset, so each has a form to fill.
 * The alternator rows hold readings of the footer's alternator, so no row keeps
 * an equipment id of its own (an old card named one per row, which is ignored).
 * An old card's readings stay on its rows, with their zones. With no alternator
 * chosen they read nothing and a save drops them.
 */
function withBlankSlots(config: EngineClusterConfig): EngineClusterConfig {
  const footer: NonNullable<EngineClusterConfig['footer']> = { ...config.footer }
  for (const key of CLUSTER_FOOTER_SLOTS) {
    if (isClusterAlternatorSlot(key)) {
      const row = withoutEquipment(footer[key] ?? ALTERNATOR_SLOTS[key].slot)
      footer[key] = { ...row, suffix: row.suffix ?? '' }
      continue
    }
    if (footer[key]) continue
    footer[key] = isForeign(key)
      ? { ...FOREIGN_SLOTS[key].slot, suffix: '' }
      : { ...blankSlot('Gear press'), suffix: '' }
  }
  return {
    ...config,
    arcs: {
      left: config.arcs?.left ?? blankArc('Coolant', 0, 120),
      right: config.arcs?.right ?? blankArc('Gearbox', 0, 120),
    },
    footer,
  }
}

/**
 * Drops every optional slot with no reading, so an unbound slot is absent from
 * the saved config rather than a placeholder the tile and the backend would
 * each have to interpret. A foreign slot with a reading but no item is dropped
 * too: it could read nothing, and so is an alternator row with no alternator.
 * An alternator row is saved with its reading alone. The alternator itself is
 * kept even as an empty string, which is the operator's answer that there is
 * none, and apart from never having chosen.
 */
function pruneUnbound(config: EngineClusterConfig): EngineClusterConfig {
  const hasReading = (slot: GaugeWidgetConfig | undefined) => (slot && (slot.suffix ?? '').trim() !== '' ? slot : undefined)

  const left = hasReading(config.arcs?.left)
  const right = hasReading(config.arcs?.right)
  const footer: NonNullable<EngineClusterConfig['footer']> = {}
  for (const key of CLUSTER_FOOTER_SLOTS) {
    const slot = boundFooterSlot(config.footer, key)
    if (slot) footer[key] = isClusterAlternatorSlot(key) ? withoutEquipment(slot) : slot
  }
  if (config.footer?.alternatorId !== undefined) footer.alternatorId = config.footer.alternatorId

  const { arcs: _arcs, footer: _footer, ...rest } = config
  void _arcs
  void _footer
  return {
    ...rest,
    ...(left || right ? { arcs: { ...(left ? { left } : {}), ...(right ? { right } : {}) } } : {}),
    ...(Object.keys(footer).length > 0 ? { footer } : {}),
    // The fan always sits on the outer edge of the tile.
    ...(config.fuel ? { fuel: { ...config.fuel, side: fuelEdge(config.side ?? 'port') } } : {}),
  }
}

interface EngineClusterConfigDialogProps {
  widget: DashboardLayoutItem | null
  onCancel: () => void
  onSave: (config: EngineClusterConfig) => void | Promise<void>
}

export function EngineClusterConfigDialog({ widget, onCancel, onSave }: EngineClusterConfigDialogProps) {
  const ctx = useEquipmentTileContext(widget !== null)
  const roles = useVesselRoles(widget !== null)
  const { paths, loading: pathsLoading, error: pathsError } = useSignalKPaths(widget !== null)
  const pathNames = useMemo(() => paths.map((p) => p.path), [paths])
  const pathsState: PathsState = pathsError ? 'error' : pathsLoading ? 'loading' : 'ready'
  const [config, setConfig] = useState<EngineClusterConfig | null>(null)
  const { saving, error: saveError, run: runSave } = useTileSave(widget?.id)
  // The slot of the card being edited. Null until the card has loaded and the
  // first slot needing attention (or the ring) has been chosen.
  // 'none' is a phone row the operator folded.
  const [selected, setSelected] = useState<SlotId | 'none' | null>(null)
  const wide = useMinWidth(BREAKPOINTS.sm)

  // A saved alternator, and one the operator picked in this dialog, are theirs
  // and stay put. One the Roles setting supplied follows the Engine side.
  const savedAlternator = widget?.cluster?.footer?.alternatorId !== undefined
  const operatorChose = useRef(false)
  // The reading each alternator row held when the dialog opened tells a typed
  // reading from one carried over.
  const openedReadings = useMemo(
    () => (widget?.cluster ? withBlankSlots(widget.cluster) : null),
    [widget?.cluster],
  )
  // Set when an alternator was picked before SignalK's paths (or its item) were
  // known. The rows keep their readings until they are, and then are switched
  // over to the new alternator.
  const switchPending = useRef(false)

  // A readout has no id of its own in the saved tile, so the dialog names each
  // for as long as it is open. Zone choices and editor state follow the name,
  // not the position, so adding or removing a readout moves neither.
  const lastKey = useRef(0)
  const newKey = () => `r${lastKey.current++}`
  const [cornerKeys, setCornerKeys] = useState<string[]>([])

  useEffect(() => {
    setConfig(widget?.cluster ? withBlankSlots(structuredClone(widget.cluster)) : null)
    setCornerKeys((widget?.cluster?.corners ?? []).map(newKey))
    operatorChose.current = false
    switchPending.current = false
  }, [widget?.id, widget?.cluster])

  useEffect(() => { setSelected(null) }, [widget?.id])

  /**
   * The config with the alternator set to `id` (undefined for never chosen). The
   * rows are switched over to it too, unless that has to wait for what SignalK
   * sends.
   */
  const withAlternator = (c: EngineClusterConfig, id: string | undefined, switchRows: boolean): EngineClusterConfig => {
    const rows = switchRows ? switchAlternatorRows(alternatorRowsOf(c), findItem(ctx, id), ctx, pathNames) : {}
    const footer = { ...c.footer, ...rows }
    if (id === undefined) delete footer.alternatorId
    else footer.alternatorId = id
    return { ...c, footer }
  }

  const setAlternator = (id: string | undefined) => {
    // A new alternator can only be judged once SignalK's paths and its item are
    // known. Clearing it needs neither.
    const waiting = !!id && (pathsState !== 'ready' || findItem(ctx, id) === undefined)
    switchPending.current = waiting
    setConfig((c) => (c ? withAlternator(c, id, !waiting) : c))
  }

  // The Roles default: while the operator hasn't chosen one, the alternator on
  // the picker is the one Roles names for the Engine side. This runs when a card
  // opens, when an engine is picked and when the side changes. With no role it
  // leaves the picker as it is, and never guesses.
  useEffect(() => {
    if (!config || (config.equipmentId ?? '') === '' || roles === null) return
    if (savedAlternator || operatorChose.current) return
    // An Engine side that was set wins. Otherwise the engine's own source says.
    const side = config.side ?? sideOf(findItem(ctx, config.equipmentId)?.signalk_source)
    const wanted = defaultAlternatorId(side, roles)
    if (wanted === (config.footer?.alternatorId ?? '')) return
    setAlternator(wanted === '' ? undefined : wanted)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [config?.equipmentId, config?.side, config?.footer?.alternatorId, roles, ctx])

  // Once SignalK's paths and the alternator's item are known, switch the rows an
  // earlier pick had to leave as they were.
  useEffect(() => {
    if (!switchPending.current || pathsState !== 'ready' || !config) return
    const alternator = findItem(ctx, config.footer?.alternatorId || undefined)
    if (!alternator) return
    switchPending.current = false
    setConfig((c) => (c ? { ...c, footer: { ...c.footer, ...switchAlternatorRows(alternatorRowsOf(c), alternator, ctx, pathNames) } } : c))
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [pathsState, ctx, config?.footer?.alternatorId])

  const item = findItem(ctx, config?.equipmentId)
  // The slots the zone plan counts, each with the part of the card it is. The
  // plan only changes when the card does, not when the operator picks a slot.
  const zoneSlots = useMemo(() => (config ? clusterZoneSlots(config, cornerKeys) : []), [config, cornerKeys])
  const zoneGauges = useMemo(() => zoneSlots.map((z) => z.slot), [zoneSlots])
  const zoneKeys = useMemo(() => zoneSlots.map((z) => z.key), [zoneSlots])
  const zoneEditing = useSlotZoneEditing(ctx, item, zoneGauges, paths, zoneKeys)
  const { profile } = zoneEditing

  const setSlot = (update: (current: EngineClusterConfig) => EngineClusterConfig) =>
    setConfig((current) => (current ? update(current) : current))

  const setFuel = (update: (rail: ClusterFuelRailRef) => ClusterFuelRailRef) =>
    setSlot((c) => (c.fuel ? { ...c, fuel: update(c.fuel) } : c))

  // Save stays busy until the tile write settles (onSave may return a promise).
  // With no zones to write to a profile, onSave is called in the same tick. A
  // failed write is toasted by the caller and the draft stays open for a retry;
  // a rejection is shown in the dialog, not swallowed.
  const save = () => runSave(async () => {
    if (!config) return
    if (!zoneEditing.dirty) {
      await onSave(pruneUnbound(config))
      return
    }
    await onSave(pruneUnbound(withoutSettledZones(config, await zoneEditing.commit())))
  })

  // The card never reads a slot's display, so the form leaves it out and folds
  // the quantity and unit away. `scaled` is whether the card draws the slot on a
  // scale (every slot but the hours).
  const slotForm = (slot: GaugeWidgetConfig, idPrefix: string, onChange: (next: GaugeWidgetConfig) => void, scaled: boolean, zoned = false) => (
    <GaugeFields
      value={slot} paths={[]} idPrefix={idPrefix} onChange={onChange} equipment={zoneEditing.modeFor(slot)}
      variant="cluster" scaled={scaled} zoned={zoned}
    />
  )

  const blankReading = (s: GaugeWidgetConfig) => (s.suffix ?? '').trim() === ''
  const ringBlank = config ? blankReading(config.ring) : false
  const centreBlank = config ? blankReading(config.centre) : false
  const fuelAttention = config?.fuel !== undefined && (config.fuel.bars.length === 0 || config.fuel.bars.some((bar) => (bar.tankId ?? '') === ''))

  // Footer slots of another item that have the item but no reading: pruning would drop them silently.
  const slotsNeedingReading = config
    ? CLUSTER_FOOTER_SLOTS.filter((key) => {
      const slot = config.footer?.[key]
      return isForeign(key) && (slot?.equipmentId ?? '') !== '' && (slot?.suffix ?? '').trim() === ''
    })
    : []

  // What the chosen alternator sends, for the rows, the picker's notes and the save check.
  const alternatorId = config?.footer?.alternatorId ?? ''
  const alternator = findItem(ctx, alternatorId || undefined)
  const choices = useReadingChoices(alternator, ctx, pathNames, pathsState)
  // Chosen, but not found. With an empty list that is only the list not loaded yet,
  // so only a loaded list says the alternator is gone.
  const alternatorUnresolved = alternatorId !== '' && alternator === undefined
  const alternatorMissing = alternatorUnresolved && listLoaded(ctx)

  // A reading typed in this dialog on an alternator row that the alternator isn't
  // sending: saving it would draw a blank. One already there when the dialog
  // opened, or the row's own expected reading, doesn't block.
  const alternatorUnsentRows = config && choices.ready
    ? CLUSTER_ALTERNATOR_SLOTS.filter((key) => {
      const suffix = (config.footer?.[key]?.suffix ?? '').trim()
      return suffix !== '' && suffix !== (openedReadings?.footer?.[key]?.suffix ?? '').trim() && suffix !== ALTERNATOR_SLOTS[key].suffix
        && !choices.sends(suffix)
    })
    : []
  const alternatorUnsent = alternatorUnsentRows.map((key) => (config?.footer?.[key]?.suffix ?? '').trim())

  // Everything that blocks Save, in the order the line beside it reports them.
  // A blocker that is about a part of the card names it, and that part is
  // outlined amber: amber always means Save is waiting on it.
  const unresolvedConflicts = zoneEditing.plan.conflicts.filter((c) => zoneEditing.choices[c.index] === undefined)
  const blockers: Array<{ slotId?: SlotId; reason: string }> = []
  if (config) {
    if ((config.equipmentId ?? '') === '') blockers.push({ reason: 'Pick the engine.' })
    const dialReason = `Choose a reading for the ${ringBlank && centreBlank ? 'ring and centre' : ringBlank ? 'ring' : 'centre'}.`
    if (ringBlank) blockers.push({ slotId: 'ring', reason: dialReason })
    if (centreBlank) blockers.push({ slotId: 'centre', reason: dialReason })
    config.corners.forEach((corner, i) => {
      if (corner.rows.some(blankReading)) blockers.push({ slotId: `corner:${i}`, reason: 'Choose a reading for every readout card.' })
    })
    const conflictReason = `Choose which zones to keep on ${unresolvedConflicts.map((c) => c.label).join(', ')}.`
    for (const c of unresolvedConflicts) blockers.push({ slotId: zoneSlots[c.index].id, reason: conflictReason })
    if (alternatorMissing) {
      blockers.push({ slotId: 'alternator', reason: 'The alternator on this card is no longer in Equipment. Pick another, or clear it.' })
    }
    const needReading = `${slotsNeedingReading.map((key) => FOOTER_TITLES[key]).join('; ')}${slotsNeedingReading.length === 1 ? ' needs a reading' : ' need a reading'}. Pick one it sends, or clear its equipment.`
    for (const key of slotsNeedingReading) blockers.push({ slotId: `footer:${key}`, reason: needReading })
    const unsentReason = `${alternator?.name} isn't sending ${alternatorUnsent.join(', ')}. Pick a reading it sends, or clear it.`
    for (const key of alternatorUnsentRows) blockers.push({ slotId: `footer:${key}`, reason: unsentReason })
    if (fuelAttention) blockers.push({ slotId: 'fuel', reason: 'Choose a tank for every fuel bar, or remove the rail.' })
  }
  const saveReason = blockers[0]?.reason ?? null
  const blockedSlots = new Set(blockers.flatMap((b) => (b.slotId ? [b.slotId] : [])))
  const canSave = config !== null && saveReason === null

  // Every slot of the card, in reading order for a port engine. The same list
  // drives the schematic, the phone list and the editors, so they cannot drift.
  const cornerIds = (config?.corners ?? []).map((_, i): SlotId => `corner:${i}`)
  /** What React tells editors apart by: a readout's own key, so its editor isn't handed to the next one when it is removed. */
  const keyOf = (id: SlotId) => (id.startsWith('corner:') ? `corner:${cornerKeys[Number(id.slice(7))] ?? id}` : id)
  const ids: SlotId[] = config
    ? [
      'ring', 'centre', 'arcLeft', 'arcRight',
      ...cornerIds,
      'footer:battery', 'alternator', ...CLUSTER_ALTERNATOR_SLOTS.map((key): SlotId => `footer:${key}`),
      'footer:transPressure', 'footer:egt', 'fuel',
    ]
    : []

  // How each slot looks. It is set when it reads something, empty when it
  // doesn't, and attention when a blocker names it (see above).
  const viewOf = (id: SlotId, c: EngineClusterConfig): SlotView => {
    const named = (title: string, short: string) => ({ id, title, short })
    // A slot that reads the engine. Without a reading the ring, the centre and
    // a readout card block Save, which is what the amber words say; an arc is
    // simply left off the card.
    const own = (title: string, short: string, slot: GaugeWidgetConfig | undefined, mustRead: boolean): SlotView => {
      const reading = (slot?.suffix ?? '').trim()
      if (reading !== '') return { ...named(title, short), reading, state: 'set', summary: <span title={reading}>{reading}</span> }
      return {
        ...named(title, short), reading, state: 'empty',
        summary: mustRead ? <span className={AMBER_TEXT}>Needs a reading</span> : <span>Not shown</span>,
      }
    }
    if (id === 'ring') return own('Ring', 'Ring', c.ring, true)
    if (id === 'centre') return own('Centre', 'Centre', c.centre, true)
    if (id === 'arcLeft') return own('Coolant arc', 'Coolant', c.arcs?.left, false)
    if (id === 'arcRight') return own('Gearbox arc', 'Gearbox', c.arcs?.right, false)
    if (id.startsWith('corner:')) {
      const i = Number(id.slice(7))
      const name = c.corners[i].label.trim() || `Readout ${i + 1}`
      return own(name, name, c.corners[i].rows[0], true)
    }
    if (id === 'alternator') {
      const name = alternator?.name ?? ''
      const base = { ...named('Alternator', 'Alternator') }
      if (alternatorUnresolved) {
        return { ...base, reading: '', state: 'set', summary: <span className={AMBER_TEXT}>{missingLabel(ctx)}</span> }
      }
      return alternator
        ? { ...base, reading: name, state: 'set', summary: <span>{name}</span> }
        : { ...base, reading: '', state: 'empty', summary: <span>Not set</span> }
    }
    if (id === 'fuel') {
      const base = named('Fuel rail', 'Fuel')
      const bars = c.fuel?.bars.length ?? 0
      if (c.fuel === undefined) return { ...base, reading: '', state: 'empty', summary: <span>Not shown</span>, tileSummary: 'Off' }
      if (fuelAttention) return { ...base, reading: '', state: 'empty', summary: <span className={AMBER_TEXT}>Needs a tank</span>, tileSummary: <span className={AMBER_TEXT}>!</span> }
      const words = `${bars} ${bars === 1 ? 'tank' : 'tanks'}`
      return { ...base, reading: words, state: 'set', summary: <span>{words}</span>, tileSummary: bars }
    }
    const key = id.slice(7) as ClusterFooterSlot
    const slot = c.footer?.[key]
    const bound = boundFooterSlot(c.footer, key) !== undefined
    const reading = (slot?.suffix ?? '').trim()
    const notSending = isClusterAlternatorSlot(key) && choices.ready
      && reading === '' && ALTERNATOR_SLOTS[key].suffix !== '' && !choices.live.includes(ALTERNATOR_SLOTS[key].suffix)
    return {
      ...named(FOOTER_TITLES[key], FOOTER_SHORT[key]), reading, state: bound ? 'set' : 'empty',
      summary: <RowSummary rowKey={key} slot={slot} bound={bound} reading={reading} notSending={notSending} alternatorMissing={alternatorUnresolved} ctx={ctx} />,
    }
  }
  const views = (config
    ? Object.fromEntries(ids.map((id) => {
      const view = viewOf(id, config)
      return [id, blockedSlots.has(id) ? { ...view, state: 'attention' as const } : view]
    }))
    : {}) as Record<SlotId, SlotView>

  // The first slot needing attention, or the ring. What needs attention is not
  // known until the equipment list and SignalK's paths have loaded (a missing
  // alternator, a zone plan), so the pick waits for them, and is made only if
  // the operator hasn't opened a slot meanwhile. After that their pick stands,
  // even when its reading is fixed.
  const firstAttention: SlotId = blockers.find((b) => b.slotId)?.slotId ?? 'ring'
  const settled = config !== null && listLoaded(ctx) && pathsState !== 'loading'
  useEffect(() => {
    if (settled && selected === null) setSelected(firstAttention)
  }, [settled, selected, firstAttention])
  // Null when a phone row has been folded, or the pick is still waiting.
  const open: SlotId | null = selected !== null && selected !== 'none' && ids.includes(selected) ? selected : null
  // The wide layout always has an editor showing; until a pick is made it is the ring's.
  const current: SlotId = open ?? 'ring'
  /** A phone row folds when it is tapped while open. */
  const toggleRow = (id: SlotId) => setSelected(id === open ? 'none' : id)

  const addCorner = () => {
    if (!config) return
    setSlot((c) => ({ ...c, corners: [...c.corners, { label: 'New', rows: [blankSlot('')] }] }))
    setCornerKeys((keys) => [...keys, newKey()])
    setSelected(`corner:${config.corners.length}`)
  }

  /** The form for one slot. Hidden, not unmounted, when it is not the one open: each keeps the state it fills in by itself. */
  const editorBody = (id: SlotId): ReactNode => {
    if (!config) return null
    if (id === 'ring') {
      return slotForm(config.ring, 'cluster-ring', (next) => setSlot((c) => ({ ...c, ring: next })), true)
    }
    if (id === 'centre') {
      return (
        <div className="grid gap-3">
          <Field>
            <FieldLabel htmlFor="cluster-centre-icon">Hours icon</FieldLabel>
            <select id="cluster-centre-icon" className="h-9 w-full rounded-md border bg-transparent px-3 text-sm"
              value={config.centreIcon ?? ''}
              onChange={(e) => setSlot((c) => ({ ...c, centreIcon: e.target.value === '' ? undefined : e.target.value }))}>
              <option value="">Auto ({iconForSlot({ path: config.centre.suffix ?? '', quantity: config.centre.quantity })})</option>
              {CLUSTER_ICON_NAMES.map((name) => <option key={name} value={name}>{name}</option>)}
            </select>
          </Field>
          {/* Hours have no scale, but zones the centre still holds keep alarming until cleared. */}
          {slotForm(config.centre, 'cluster-centre', (next) => setSlot((c) => ({ ...c, centre: next })), false, (config.centre.zones?.length ?? 0) > 0)}
        </div>
      )
    }
    if (id === 'arcLeft') {
      return slotForm(config.arcs!.left!, 'cluster-arc-left',
        (next) => setSlot((c) => ({ ...c, arcs: { ...c.arcs, left: next } })), true)
    }
    if (id === 'arcRight') {
      return slotForm(config.arcs!.right!, 'cluster-arc-right',
        (next) => setSlot((c) => ({ ...c, arcs: { ...c.arcs, right: next } })), true)
    }
    if (id.startsWith('corner:')) {
      const cornerIndex = Number(id.slice(7))
      const corner = config.corners[cornerIndex]
      return (
        <div className="grid gap-3">
          <div className="flex items-center gap-2">
            <Input aria-label={`Readout ${cornerIndex + 1} label`} className="h-8 max-w-[12rem]"
              value={corner.label}
              onChange={(e) => setSlot((c) => ({
                ...c,
                corners: c.corners.map((x, i) => (i === cornerIndex ? { ...x, label: e.target.value } : x)),
              }))} />
            <select
              aria-label={`Readout ${cornerIndex + 1} icon`}
              className="h-8 rounded-md border bg-transparent px-2 text-sm"
              value={corner.icon ?? ''}
              onChange={(e) => setSlot((c) => ({
                ...c,
                corners: c.corners.map((x, i) => (i === cornerIndex
                  ? { ...x, icon: e.target.value === '' ? undefined : e.target.value }
                  : x)),
              }))}
            >
              <option value="">
                Auto{corner.rows[0] ? ` (${iconForSlot({ path: corner.rows[0].suffix ?? '', quantity: corner.rows[0].quantity })})` : ''}
              </option>
              {CLUSTER_ICON_NAMES.map((name) => <option key={name} value={name}>{name}</option>)}
            </select>
            <div className="flex-1" />
            <Button size="sm" variant="ghost" aria-label={`Remove readout ${cornerIndex + 1}`}
              onClick={() => {
                setSlot((c) => ({ ...c, corners: c.corners.filter((_, i) => i !== cornerIndex) }))
                setCornerKeys((keys) => keys.filter((_, i) => i !== cornerIndex))
                setSelected('ring')
              }}>
              <Trash2 className="size-3.5" />
            </Button>
          </div>
          {corner.rows.slice(0, CLUSTER_MAX_CORNER_ROWS).map((row, rowIndex) => (
            <div key={rowIndex}>
              {slotForm(row, `cluster-${cornerIndex}-${rowIndex}`, (next) => setSlot((c) => ({
                ...c,
                corners: c.corners.map((x, i) => (i === cornerIndex
                  ? { ...x, rows: x.rows.map((r, j) => (j === rowIndex ? next : r)) }
                  : x)),
              })), true)}
            </div>
          ))}
        </div>
      )
    }
    if (id === 'alternator') {
      return (
        <div className="grid gap-2">
          <EquipmentPicker
            idPrefix="cluster-alternator"
            label="Alternator"
            kind="alternator"
            value={alternatorId}
            onChange={(picked) => { operatorChose.current = true; setAlternator(picked) }}
            ctx={ctx}
          />
          {alternatorMissing && (
            <p className={cn('text-xs', AMBER_TEXT)}>That alternator is no longer in Equipment. Pick another.</p>
          )}
          <AlternatorNote item={alternator} pathsState={pathsState} live={choices.live} ready={choices.ready} />
          <FieldDescription>The five alternator readouts on the card all read this one alternator.</FieldDescription>
        </div>
      )
    }
    if (id === 'fuel') {
      return (
        <div className="grid gap-2">
          <FieldDescription>
            An arc per tank (forward and aft) on the outer edge of the tile, with the total underneath. Pick the
            tank items; their level and capacity are read live.
          </FieldDescription>

          {config.fuel === undefined ? (
            <Button variant="ghost" className="w-fit"
              onClick={() => setSlot((c) => ({
                ...c,
                // The outer edge of the tile, which is the engine's side.
                fuel: {
                  side: fuelEdge(c.side ?? 'port'),
                  bars: [blankFuelBar('Fwd'), blankFuelBar('Aft')],
                },
              }))}>
              <Plus className="size-3.5" /> Add fuel rail
            </Button>
          ) : (
            <>
              {config.fuel.bars.map((bar, index) => (
                <div key={index} className="mb-2 grid gap-2 border-b border-border/60 pb-2 last:border-b-0">
                  <EquipmentPicker
                    idPrefix={`cluster-fuel-${index}`}
                    label={`Tank ${index + 1}`}
                    kind="tank"
                    value={bar.tankId ?? ''}
                    ctx={ctx}
                    onChange={(tankId) => setFuel((f) => ({
                      ...f,
                      bars: f.bars.map((b, i) => (i === index
                        ? { ...b, tankId, source: tankId === '' ? b.source : undefined, label: b.label || (findItem(ctx, tankId)?.name ?? '') }
                        : b)),
                    }))}
                  />
                  <Field>
                    <FieldLabel htmlFor={`cluster-fuel-label-${index}`}>Label</FieldLabel>
                    <Input id={`cluster-fuel-label-${index}`} maxLength={12} value={bar.label ?? ''}
                      onChange={(e) => setFuel((f) => ({
                        ...f,
                        bars: f.bars.map((b, i) => (i === index ? { ...b, label: e.target.value } : b)),
                      }))} />
                  </Field>
                  <FieldDescription>
                    This bar's colours and alarms follow the tank's levels. Change them on the tank's equipment page.
                  </FieldDescription>
                  <Button size="sm" variant="ghost" className="w-fit" aria-label={`Remove fuel tank ${index + 1}`}
                    onClick={() => setFuel((f) => ({ ...f, bars: f.bars.filter((_, i) => i !== index) }))}>
                    <Trash2 className="size-3.5" /> Remove tank
                  </Button>
                </div>
              ))}

              <div className="flex items-center gap-2">
                <Button variant="ghost" className="w-fit"
                  disabled={config.fuel.bars.length >= CLUSTER_MAX_FUEL_BARS}
                  onClick={() => setFuel((f) => ({ ...f, bars: [...f.bars, blankFuelBar('')] }))}>
                  <Plus className="size-3.5" /> Add tank
                </Button>
                <Button variant="ghost" className="w-fit"
                  onClick={() => setSlot((c) => ({ ...c, fuel: undefined }))}>
                  <Trash2 className="size-3.5" /> Remove fuel rail
                </Button>
              </div>
            </>
          )}
        </div>
      )
    }
    const key = id.slice(7) as ClusterFooterSlot
    // withBlankSlots gave every footer slot a form, and every change keeps them.
    const slot = config.footer?.[key]
    const set = (next: GaugeWidgetConfig) => setSlot((c) => ({ ...c, footer: { ...c.footer, [key]: next } }))
    return (
      <div className="grid gap-2">
        {isClusterAlternatorSlot(key) ? (
          <AlternatorRowForm
            idPrefix={`cluster-footer-${key}`} slotKey={key} slot={slot!}
            alternator={alternator} choices={choices} ctx={ctx} onChange={set}
          />
        ) : isForeign(key) ? (
          <ForeignSlotForm
            idPrefix={`cluster-footer-${key}`} slotKey={key} slot={slot!}
            ctx={ctx} paths={pathNames} pathsState={pathsState} onChange={set}
          />
        ) : (
          slotForm(slot!, `cluster-footer-${key}`, set, true)
        )}
        {FOOTER_NOTES[key] && <FieldDescription>{FOOTER_NOTES[key]}</FieldDescription>}
      </div>
    )
  }

  // The zone notes belong to the slot they are about, so they show in its editor
  // only. The plan counts slots in the order of `zoneSlots`, so a note finds its
  // slot by position. Every entry of the plan has one, so a conflict that Save
  // waits on is always in the editor of the slot that is outlined for it.
  const zonesFor = (id: SlotId): ReactNode => {
    const at = zoneSlots.flatMap((z, i) => (z.id === id ? [i] : []))
    if (at.length === 0) return null
    const { plan, choices, heldWithoutItem } = zoneEditing
    return (
      <ZoneConflicts
        className="mb-3"
        plan={{
          moves: plan.moves.filter((m) => at.includes(m.index)),
          conflicts: plan.conflicts.filter((c) => at.includes(c.index)),
          stays: plan.stays.filter((s) => at.includes(s.index)),
        }}
        choices={choices} onChoose={zoneEditing.setChoice}
        heldWithoutItem={heldWithoutItem.filter((h) => at.includes(h.index))}
      />
    )
  }
  const editorFor = (id: SlotId): ReactNode => (
    <>
      {zonesFor(id)}
      {editorBody(id)}
    </>
  )

  return (
    <Dialog open={widget !== null} onOpenChange={(open) => { if (!open) onCancel() }}>
      <DialogContent className="flex max-h-[90vh] max-w-4xl flex-col gap-3 overflow-y-auto sm:h-[min(90vh,52rem)] sm:overflow-hidden">
        <DialogHeader className="flex-none">
          <DialogTitle>Engine Cluster</DialogTitle>
          <DialogDescription className="sr-only">
            {wide
              ? 'Pick the engine and fill the card from its profile, then click any part of the card to adjust it.'
              : 'Pick the engine and fill the card from its profile, then tap a row to open its settings.'}
          </DialogDescription>
        </DialogHeader>

        {config && (
          <div className="grid flex-none gap-3">
            <div className="grid items-start gap-3 sm:grid-cols-[minmax(0,1.4fr)_minmax(0,1fr)_minmax(0,9rem)]">
              <EquipmentPicker
                idPrefix="cluster"
                label="Engine"
                kind="engine"
                value={config.equipmentId ?? ''}
                // Choosing equipment ends the source the tile remembered.
                onChange={(id) => setSlot((c) => ({ ...c, equipmentId: id, source: id === '' ? c.source : undefined }))}
                ctx={ctx}
              />
              <Field>
                <FieldLabel htmlFor="cluster-title">Title</FieldLabel>
                {/* Blank shows the engine's name, which the placeholder already is. */}
                <Input id="cluster-title" maxLength={GAUGE_GROUP_TITLE_MAX_LENGTH} value={config.title}
                  onChange={(e) => setSlot((c) => ({ ...c, title: e.target.value }))} placeholder={item?.name ?? 'Engine name'} />
              </Field>
              <Field>
                <FieldLabel htmlFor="cluster-side">Engine side</FieldLabel>
                <select id="cluster-side" className="h-9 w-full rounded-md border bg-transparent px-3 text-sm"
                  value={config.side ?? 'port'}
                  onChange={(e) => setSlot((c) => {
                    const side = e.target.value as ClusterSide
                    return { ...c, side, ...(c.fuel ? { fuel: { ...c.fuel, side: fuelEdge(side) } } : {}) }
                  })}>
                  <option value="port">Port</option>
                  <option value="stbd">Starboard</option>
                </select>
              </Field>
            </div>

            <div className="flex flex-wrap items-center gap-x-3 gap-y-2">
              <Button
                variant="secondary"
                disabled={!profile}
                onClick={() => {
                  setSlot((c) => clusterFromProfile(profile!, c, ctx, pathsState === 'ready' ? pathNames : null))
                  // The profile's readouts replace the old ones, so none of their keys carry over.
                  setCornerKeys(SLOT_SUFFIXES.corners.map(newKey))
                }}
              >
                Fill from profile
              </Button>
              <FieldDescription className="min-w-0 flex-1 basis-56">
                {item && !profile ? "This engine has no profile, so there's nothing to fill from." : ''}
                {profile ? 'Replaces the dial, arcs, readouts and gearbox pressure. Alternator, battery, exhaust and fuel stay.' : ''}
              </FieldDescription>
            </div>
          </div>
        )}

        {config ? (
          <>
            {/* One list of slots, laid out as the card on a wide screen and as rows on a phone. */}
            <div className="flex flex-col gap-3 sm:grid sm:min-h-0 sm:flex-1 sm:grid-cols-[minmax(0,1.15fr)_minmax(0,1fr)] sm:gap-4">
              {wide ? (
                <>
                  <div className="min-w-0 sm:overflow-y-auto sm:pr-1">
                    <CardSchematic
                      side={config.side ?? 'port'} views={views} cornerIds={cornerIds} current={current} onSelect={setSelected}
                      canAddCorner={config.corners.length < CLUSTER_MAX_CORNERS} onAddCorner={addCorner}
                    />
                  </div>
                  <div className="min-w-0 rounded-md border border-border p-3 sm:overflow-y-auto">
                    {ids.map((id) => (
                      <div key={keyOf(id)} hidden={id !== current}>
                        <p className="mb-3 text-sm font-medium">
                          {views[id].title}
                          {views[id].reading !== '' && <span className="text-muted-foreground"> · {views[id].reading}</span>}
                        </p>
                        {editorFor(id)}
                      </div>
                    ))}
                  </div>
                </>
              ) : (
                <div className="min-w-0">
                  <div className="divide-y divide-border rounded-md border border-border">
                    {ids.map((id) => (
                      <div key={keyOf(id)}>
                        <SlotButton view={views[id]} selected={id === open} onSelect={toggleRow} variant="row" controls={editorPanelId(id)} />
                        <div id={editorPanelId(id)} hidden={id !== open} className="px-3 pb-3">{editorFor(id)}</div>
                      </div>
                    ))}
                    {config.corners.length < CLUSTER_MAX_CORNERS && (
                      <div className="p-2"><AddReadoutButton onClick={addCorner} /></div>
                    )}
                  </div>
                </div>
              )}
            </div>
          </>
        ) : (
          <div className="min-h-0 flex-1" />
        )}

        <DialogFooter className="flex-none">
          {saveError
            ? <p role="alert" className="mr-auto text-sm text-destructive">{saveError}</p>
            : (!canSave && !saving && saveReason && <p role="status" className="mr-auto text-xs text-muted-foreground">{saveReason}</p>)}
          <Button variant="ghost" onClick={onCancel}>Cancel</Button>
          <Button disabled={!canSave || saving} onClick={() => { void save() }}>{saving ? 'Saving…' : 'Save'}</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

/** A part of the card the operator can open: the ring, a footer row, the fuel rail. */
type SlotId =
  | 'ring' | 'centre' | 'arcLeft' | 'arcRight' | 'alternator' | 'fuel'
  | `corner:${number}` | `footer:${ClusterFooterSlot}`

/**
 * How a slot looks on the card's schematic: set, needing the operator before
 * Save will work, or showing nothing (which is allowed).
 */
type SlotState = 'set' | 'attention' | 'empty'

interface SlotView {
  id: SlotId
  /** The full name, which the screen reader says and the editor is headed with. */
  title: string
  /** The name that fits on the schematic. */
  short: string
  /** The reading it takes, or the item or count it shows; empty when it shows nothing. */
  reading: string
  state: SlotState
  /** The words under the name: what it shows, or why it shows nothing. */
  summary: ReactNode
  /** A shorter form of the summary for a narrow button on the schematic; the screen reader still hears the summary. */
  tileSummary?: ReactNode
}

const editorPanelId = (id: SlotId) => `cluster-editor-${id.replace(':', '-')}`

const AMBER_TEXT = 'text-amber-600 dark:text-amber-400'

/** What a slot reads while the item it names can't be found: dashes until the list loads, then the fact. */
const missingLabel = (ctx: EquipmentTileContext) => (listLoaded(ctx) ? 'Equipment missing' : '--')

/** The focus and hover a button on the card shares, whatever its outline says. */
const SLOT_INTERACTION = 'transition-colors hover:bg-muted/60 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring motion-reduce:transition-none'

/**
 * One part of the card as a button. A set slot has a plain outline, a slot that
 * Save is waiting on an amber one and a slot that shows nothing a dashed one,
 * and the open slot a ring. Amber text on its own ("Not sending", "Equipment
 * missing") is a warning, not a block: only the amber outline means Save waits. The screen reader gets "Ring, revolutions" or "Exhaust
 * temperature, Not shown", with the full name where the picture has room for
 * only the short one.
 */
function SlotButton({ view, selected, onSelect, variant, className, controls }: {
  view: SlotView
  selected: boolean
  onSelect: (id: SlotId) => void
  variant: 'tile' | 'row'
  className?: string
  /** A row's editor panel, which the row opens and folds. */
  controls?: string
}) {
  const row = variant === 'row'
  return (
    <button
      type="button"
      // A tile picks which editor shows. A row is a disclosure for its own.
      {...(row ? { 'aria-expanded': selected, 'aria-controls': controls } : { 'aria-pressed': selected })}
      data-slot-id={view.id}
      data-state={view.state}
      onClick={() => onSelect(view.id)}
      className={cn(
        'flex min-w-0 text-left text-xs', SLOT_INTERACTION,
        row ? 'w-full items-center gap-2 px-3 py-3 text-sm' : 'flex-col gap-0.5 rounded-md border bg-card px-2 py-1.5',
        !row && (view.state === 'empty' ? 'border-dashed border-border text-muted-foreground' : view.state === 'attention' ? 'border-amber-500' : 'border-border'),
        !row && selected && 'ring-2 ring-primary',
        row && selected && 'bg-muted',
        className,
      )}
    >
      <span className={cn('flex min-w-0 items-center gap-1.5', row && 'flex-1')}>
        {view.state === 'attention' && <span aria-hidden className="size-1.5 flex-none rounded-full bg-amber-500" />}
        <span aria-hidden className={cn('truncate', !row && 'font-medium')}>{row ? view.title : view.short}</span>
        <span className="sr-only">{view.title}, </span>
      </span>
      {!row && view.tileSummary !== undefined ? (
        <>
          <span aria-hidden className={cn('min-w-0', view.state === 'empty' && 'text-muted-foreground')}>{view.tileSummary}</span>
          <span className="sr-only">{view.summary}</span>
        </>
      ) : (
        // On the card a few words wrap or clamp to two lines rather than cut off; on a phone row they truncate.
        <span className={cn('min-w-0 break-words', row ? 'max-w-[50%] truncate text-xs' : 'line-clamp-2', view.state === 'empty' && 'text-muted-foreground')}>
          {view.summary}
        </span>
      )}
      {row && <ChevronRight aria-hidden className={cn('size-4 flex-none text-muted-foreground transition-transform motion-reduce:transition-none', selected && 'rotate-90')} />}
    </button>
  )
}

/** Puts another readout card on the card. One button, for the schematic and the phone list. */
function AddReadoutButton({ onClick }: { onClick: () => void }) {
  return (
    <Button variant="outline" size="sm" className="w-full border-dashed text-xs text-muted-foreground" onClick={onClick}>
      <Plus aria-hidden className="size-3.5" /> Add readout
    </Button>
  )
}

/**
 * The card drawn small enough to click: the fuel rail on the outer edge, the
 * dial with its hours and two arcs, the readout cards down the inner edge and
 * the footer under it. A starboard card is the port one mirrored, and the
 * buttons are in the order they appear, so the tab key walks the picture.
 */
function CardSchematic({ side, views, cornerIds, current, onSelect, canAddCorner, onAddCorner }: {
  side: ClusterSide
  views: Record<SlotId, SlotView>
  /** The readout cards, in order, from the dialog's one list of slots. */
  cornerIds: SlotId[]
  current: SlotId
  onSelect: (id: SlotId) => void
  canAddCorner: boolean
  onAddCorner: () => void
}) {
  const stbd = side === 'stbd'
  const btn = (id: SlotId, className?: string) => (
    <SlotButton key={id} view={views[id]} selected={id === current} onSelect={onSelect} variant="tile" className={className} />
  )

  // A narrow strip on the outer edge: "Fuel" and the tank count.
  const fuel = <div key="fuel" className="flex min-w-0">{btn('fuel', 'w-full items-center justify-center px-0.5 text-center')}</div>
  const dial = (
    <div key="dial" className="grid min-w-0 content-start gap-2">
      <div className="relative mx-auto aspect-square w-full max-w-40">
        {btn('ring', 'absolute inset-0 items-center justify-start rounded-full pt-[12%] text-center')}
        {btn('centre', 'absolute left-1/2 top-[58%] w-[60%] -translate-x-1/2 -translate-y-1/2 items-center px-1 py-1 text-center')}
      </div>
      {/* The arcs do not mirror with the card: coolant stays on the left. */}
      <div className="grid grid-cols-2 gap-2">{btn('arcLeft')}{btn('arcRight')}</div>
    </div>
  )
  const corners = (
    <div key="corners" className="grid min-w-0 content-start gap-2">
      {cornerIds.map((id) => btn(id))}
      {canAddCorner && <AddReadoutButton onClick={onAddCorner} />}
    </div>
  )
  const body = stbd ? [corners, dial, fuel] : [fuel, dial, corners]

  const alternator = (
    <div key="alternator" className="grid min-w-0 content-start gap-1.5 rounded-md border border-border p-1.5">
      {btn('alternator')}
      <div className="grid grid-cols-3 gap-1.5">
        {CLUSTER_ALTERNATOR_SLOTS.map((key) => btn(`footer:${key}`, 'px-1 py-1'))}
      </div>
    </div>
  )
  const footer: Array<{ node: ReactNode; width: string }> = [
    { node: <div key="battery" className="flex min-w-0">{btn('footer:battery', 'w-full')}</div>, width: '1fr' },
    { node: alternator, width: '3.2fr' },
    // Stacked, so each gets the column's width and its title is not cut off.
    {
      node: <div key="trans-egt" className="grid min-w-0 gap-2">{btn('footer:transPressure', 'w-full')}{btn('footer:egt', 'w-full')}</div>,
      width: '1.3fr',
    },
  ]
  if (stbd) footer.reverse()

  return (
    <div className="grid gap-3">
      <div className="grid gap-2" style={{ gridTemplateColumns: stbd ? 'minmax(0,7rem) minmax(0,1fr) 2.5rem' : '2.5rem minmax(0,1fr) minmax(0,7rem)' }}>
        {body}
      </div>
      {/* Each footer box is as tall as its own content, not as the alternator's box. */}
      <div className="grid items-start gap-2" style={{ gridTemplateColumns: footer.map((f) => `minmax(0,${f.width})`).join(' ') }}>
        {footer.map((f) => f.node)}
      </div>
    </div>
  )
}

type PathsState = 'loading' | 'error' | 'ready'

/** What an item can show: SignalK's live readings under its source, its profile's, and the two together as the choices to offer. */
interface ReadingChoices {
  profile: EngineProfile | undefined
  profileSuffixes: readonly string[]
  live: readonly string[]
  options: readonly string[]
  /** The item exists and SignalK's paths are known (it may still have no source). */
  ready: boolean
  /** Ready, and the item has a source to look under. */
  known: boolean
  /** Whether the item can show this reading: sent now, or on its profile. */
  sends: (suffix: string) => boolean
}

function useReadingChoices(
  item: TileItem | undefined, ctx: EquipmentTileContext, paths: readonly string[], pathsState: PathsState,
): ReadingChoices {
  const profile = item ? profileOf(ctx, item) : undefined
  const profileSuffixes = useMemo(() => (profile?.gauges ?? []).map((g) => g.path_suffix), [profile])
  const source = item?.signalk_source ?? ''
  const live = useMemo(() => (pathsState === 'ready' ? liveReadings(source, paths) : []), [pathsState, source, paths])
  const options = useMemo(() => [...new Set([...profileSuffixes, ...live])].filter((v) => v !== ''), [profileSuffixes, live])
  const ready = item !== undefined && pathsState === 'ready'
  return {
    profile, profileSuffixes, live, options, ready, known: ready && source !== '',
    sends: (suffix) => item !== undefined && itemSends(item, ctx, live, suffix),
  }
}

/** The right-hand side of a footer row's summary: what it shows, or why it shows nothing. */
function RowSummary({ rowKey, slot, bound, reading, notSending, alternatorMissing, ctx }: {
  rowKey: ClusterFooterSlot
  slot: GaugeWidgetConfig | undefined
  bound: boolean
  reading: string
  notSending: boolean
  /** The footer's alternator is chosen but isn't found in Equipment. */
  alternatorMissing: boolean
  ctx: EquipmentTileContext
}) {
  const amber = cn('break-words text-xs', AMBER_TEXT)
  if (!bound || !slot) {
    if (isForeign(rowKey) && slot && (slot.equipmentId ?? '') !== '') return <span className={amber}>Needs a reading</span>
    if (notSending) return <span className={amber}>Not sending</span>
    return <span className="text-xs text-muted-foreground">Not shown</span>
  }
  const wrap = 'flex min-w-0 max-w-full items-baseline gap-1.5 text-xs'
  if (isClusterAlternatorSlot(rowKey)) {
    // The alternator may have been deleted: the row reads nothing, as the battery's would.
    return (
      <span className={wrap}>
        {alternatorMissing
          ? <span className={cn('truncate', AMBER_TEXT)}>{missingLabel(ctx)}</span>
          : <span className="truncate">{reading}</span>}
      </span>
    )
  }
  if (!isForeign(rowKey)) return <span className={wrap}><span className="truncate">{slot.label || reading}</span></span>
  const owner = findItem(ctx, slot.equipmentId)
  // The item's name is what the operator recognises, so it gets up to two lines.
  // The reading is in the hover text and in the name a screen reader hears.
  return (
    <span className="block min-w-0 max-w-full break-words" title={reading}>
      {owner
        ? <span>{owner.name}</span>
        : <span className={AMBER_TEXT}>{missingLabel(ctx)}</span>}
      <span className="sr-only"> {reading}</span>
    </span>
  )
}

/**
 * A footer slot that reads another item than the engine: pick the item (the
 * starter battery, the alternator, the exhaust's node) and the reading under its
 * source. Its zones come from that item's profile and are edited there, not
 * here. Leaving the reading empty leaves the slot out.
 */
function ForeignSlotForm({ idPrefix, slotKey, slot, ctx, paths, pathsState, onChange }: {
  idPrefix: string
  slotKey: keyof typeof FOREIGN_SLOTS
  slot: GaugeWidgetConfig
  ctx: EquipmentTileContext
  /** Every path SignalK is publishing now. Only meaningful when pathsState is ready. */
  paths: readonly string[]
  pathsState: PathsState
  onChange: (next: GaugeWidgetConfig) => void
}) {
  const spec = FOREIGN_SLOTS[slotKey]
  const item = findItem(ctx, slot.equipmentId)
  const { profile, live, options, known, sends } = useReadingChoices(item, ctx, paths, pathsState)
  const source = item?.signalk_source ?? ''
  const suffix = (slot.suffix ?? '').trim()

  const set = (patch: Partial<GaugeWidgetConfig>) => {
    const next = { ...slot, ...patch }
    const nextItem = findItem(ctx, next.equipmentId)
    // A reading on the item's profile brings the profile's quantity, unit and scale.
    const gauge = profileGaugeFor(nextItem ? profileOf(ctx, nextItem) : undefined, (next.suffix ?? '').trim())
    if (gauge) return onChange(slotFromProfileGauge({ ...next, zones: undefined }, gauge))
    if ('equipmentId' in patch && patch.equipmentId !== slot.equipmentId) {
      // The old item's quantity, scale and zones belong to its profile, not to this one.
      const { quantity, unit, decimals, display } = spec.slot
      return onChange({ ...next, quantity, unit, decimals, display, min: undefined, max: undefined, zones: undefined })
    }
    onChange(next)
  }

  // Fill the expected reading once per picked item, as soon as SignalK is known
  // to send it. Clearing the field afterwards is the operator's, and stays.
  const filledFor = useRef('')
  useEffect(() => {
    if (!item || !known || filledFor.current === item.id) return
    filledFor.current = item.id
    if (suffix === '' && spec.suffix !== '' && live.includes(spec.suffix)) set({ suffix: spec.suffix })
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [known, item?.id])

  const pickItem = (id: string) => {
    const picked = id === '' ? undefined : findItem(ctx, id)
    filledFor.current = ''
    // A reading carries over only when the new item is known to send it.
    // Where that can't be known, it is left empty rather than assumed.
    const pickedProfile = picked ? profileOf(ctx, picked) : undefined
    const pickedLive = picked && pathsState === 'ready' ? liveReadings(picked.signalk_source, paths) : []
    const keep = suffix !== '' && picked !== undefined
      && (pickedLive.includes(suffix) || (pickedProfile?.gauges ?? []).some((g) => g.path_suffix === suffix))
    set({ equipmentId: picked ? id : undefined, ...(keep ? {} : { suffix: '' }) })
  }

  let note: string
  if (item && source === '') note = `${item.name} has no SignalK source, so it has no readings to pick from.`
  else if (item && pathsState === 'error') note = "Helmcentral can't read what SignalK is sending right now. Type the reading, or try again."
  else if (item && known && suffix === '' && !live.includes(spec.suffix)) {
    note = spec.suffix !== ''
      ? `${item.name} isn't sending ${spec.suffix}. Pick a reading it sends.`
      : `Pick a reading ${item.name} sends.`
  } else if (item && known && suffix !== '' && !sends(suffix)) {
    note = `${item.name} isn't sending ${suffix}. Pick a reading it sends.`
  } else if (item && !profile) note = 'This equipment has no profile, so the reading has no zones.'
  else note = 'Zones come from the equipment\'s profile; edit them on the equipment.'

  return (
    <div className="grid gap-2">
      <EquipmentPicker
        idPrefix={idPrefix} label="Equipment" kind={spec.kind} value={slot.equipmentId ?? ''} ctx={ctx}
        placeholder={spec.pick} onChange={pickItem}
      />
      <Field>
        <FieldLabel htmlFor={`${idPrefix}-suffix`}>Reading</FieldLabel>
        <Input id={`${idPrefix}-suffix`} list={`${idPrefix}-suffixes`} value={slot.suffix ?? ''}
          placeholder={item ? 'reading this equipment sends' : (spec.suffix || 'reading under the equipment')}
          onChange={(e) => set({ suffix: e.target.value })} />
        <datalist id={`${idPrefix}-suffixes`}>
          {options.map((v) => <option key={v} value={v} />)}
        </datalist>
        <FieldDescription>{note} Leave equipment empty to leave this out.</FieldDescription>
      </Field>
    </div>
  )
}

/** What to tell the operator about the chosen alternator: it has no source, SignalK can't be read, or it sends nothing. */
function AlternatorNote({ item, pathsState, live, ready }: { item: TileItem | undefined; pathsState: PathsState; live: readonly string[]; ready: boolean }) {
  if (!item) return null
  if (item.signalk_source === '') {
    return <FieldDescription>{item.name} has no SignalK source, so it has no readings to pick from.</FieldDescription>
  }
  if (pathsState === 'error') {
    return <FieldDescription>Helmcentral can't read what SignalK is sending right now. Type the readings, or try again.</FieldDescription>
  }
  if (ready && live.length === 0) {
    return (
      <p className={cn('text-xs', AMBER_TEXT)}>
        {item.name} isn't sending anything. Check its source in Equipment.
      </p>
    )
  }
  return null
}

/**
 * One alternator row: only the reading, since the alternator is the footer's
 * one picker. Its zones come from the alternator's profile and are edited there.
 * Leaving the reading empty leaves the row off the card.
 */
function AlternatorRowForm({ idPrefix, slotKey, slot, alternator, choices, ctx, onChange }: {
  idPrefix: string
  slotKey: ClusterAlternatorSlot
  slot: GaugeWidgetConfig
  alternator: TileItem | undefined
  choices: ReadingChoices
  ctx: EquipmentTileContext
  onChange: (next: GaugeWidgetConfig) => void
}) {
  const { live, options, known, sends } = choices
  const expected = ALTERNATOR_SLOTS[slotKey].suffix
  const suffix = (slot.suffix ?? '').trim()

  // A reading on the alternator's profile brings the profile's quantity, unit and
  // scale. Any other reading is only renamed: the row's own scale and zones stay.
  const set = (next: string) => {
    const gauge = alternator ? profileGaugeFor(profileOf(ctx, alternator), next.trim()) : undefined
    onChange(gauge ? alternatorRow(slotKey, slot, next, alternator, ctx) : { ...slot, suffix: next })
  }

  let note: string
  if (!alternator) note = 'Pick the alternator first.'
  else if (known && suffix !== '' && !sends(suffix)) note = `${alternator.name} isn't sending ${suffix}. Pick a reading it sends.`
  else if (known && suffix === '' && expected !== '' && !live.includes(expected)) note = `${alternator.name} isn't sending ${expected}. Pick a reading it sends.`
  else if (alternator && !profileOf(ctx, alternator)) note = 'This alternator has no profile, so the reading has no zones.'
  else note = "Zones come from the alternator's profile; edit them on the equipment."

  return (
    <Field>
      <FieldLabel htmlFor={`${idPrefix}-suffix`}>Reading</FieldLabel>
      <Input id={`${idPrefix}-suffix`} list={`${idPrefix}-suffixes`} value={slot.suffix ?? ''} disabled={!alternator}
        placeholder={alternator ? 'reading this alternator sends' : 'pick the alternator first'}
        onChange={(e) => set(e.target.value)} />
      <datalist id={`${idPrefix}-suffixes`}>
        {options.map((v) => <option key={v} value={v} />)}
      </datalist>
      <FieldDescription>{note} Leave empty to leave this out.</FieldDescription>
    </Field>
  )
}


