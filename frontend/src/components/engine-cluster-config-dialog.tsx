import { ChevronRight, Plus, Trash2 } from 'lucide-react'
import { useEffect, useState } from 'react'

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
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { cn } from '@/lib/utils'
import { EquipmentPicker } from '@/components/equipment-picker'
import { useEquipmentTileContext } from '@/hooks/use-equipment-tile-context'
import { useSignalKPaths } from '@/hooks/use-signalk-paths'
import { useVesselRoles } from '@/hooks/use-vessel-roles'
import { useSlotZoneEditing } from '@/hooks/use-slot-zone-editing'
import { ZoneConflicts } from '@/components/zone-conflicts'
import {
  CLUSTER_MAX_CORNERS,
  CLUSTER_MAX_CORNER_ROWS,
  CLUSTER_MAX_FUEL_BARS,
  CLUSTER_FOOTER_SLOTS,
  GAUGE_GROUP_TITLE_MAX_LENGTH,
  type ClusterFooterSlot,
  type ClusterFuelRailRef,
  type ClusterSide,
  type DashboardLayoutItem,
  type EngineClusterConfig,
  type FuelBarRef,
  type GaugeWidgetConfig,
} from '@/lib/dashboard-widgets'
import { CLUSTER_ICON_NAMES, iconForSlot } from '@/lib/cluster-icons'
import { findItem, profileGaugeFor, profileOf, type EquipmentTileContext } from '@/lib/equipment-tiles'
import type { EngineProfile } from '@/lib/engine-profiles'
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
 * The footer slots that read another item than the engine: the kind of item
 * each takes, the reading it starts on, and a blank slot's own quantity (used
 * until the item's profile has a gauge for the reading). None carries a scale or
 * a threshold: both come from the item's profile.
 */
const FOREIGN_SLOTS: Record<'battery' | 'altAmps' | 'altWatts' | 'altTemp' | 'altStage' | 'altLimit' | 'egt', {
  kind: 'battery' | 'alternator' | 'engine'
  suffix: string
  slot: GaugeWidgetConfig
}> = {
  battery: { kind: 'battery', suffix: 'voltage', slot: { path: '', label: 'Battery', display: 'numeric', quantity: 'potential', unit: 'V', decimals: 1 } },
  altAmps: { kind: 'alternator', suffix: 'current', slot: { path: '', label: 'Alternator', display: 'numeric', quantity: 'current', unit: 'A', decimals: 0 } },
  altWatts: { kind: 'alternator', suffix: 'power', slot: { path: '', label: 'Alt output', display: 'numeric', quantity: 'power', unit: 'W', decimals: 0 } },
  altTemp: { kind: 'alternator', suffix: 'temperature', slot: { path: '', label: 'Alt temp', display: 'numeric', quantity: 'temperature', unit: 'C', decimals: 0 } },
  // The regulator reports its stage as a number; the tile names it.
  altStage: { kind: 'alternator', suffix: 'chargingModeNumber', slot: { path: '', label: 'Charge stage', display: 'numeric', quantity: 'raw', unit: 'raw', decimals: 0 } },
  altLimit: { kind: 'alternator', suffix: '', slot: { path: '', label: 'Charge limit', display: 'numeric', quantity: 'current', unit: 'A', decimals: 0 } },
  // The scale and any band (raw-water loss on a wet exhaust) are the exhaust
  // item's profile's to hold.
  egt: { kind: 'engine', suffix: 'exhaustTemperature', slot: { path: '', label: 'Exhaust', display: 'numeric', quantity: 'temperature', unit: 'C', decimals: 0 } },
}

const FOOTER_TITLES: Record<ClusterFooterSlot, string> = {
  battery: 'Starter battery voltage',
  altAmps: 'Alternator current (also drawn as a one-hour trace)',
  altWatts: 'Alternator output power',
  altTemp: 'Alternator temperature',
  altStage: 'Charge stage (regulator state number)',
  altLimit: 'Charge current limit from the battery management system',
  transPressure: 'Transmission oil pressure',
  egt: 'Exhaust temperature',
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
function clusterSlotList(c: EngineClusterConfig): GaugeWidgetConfig[] {
  return [
    c.ring, c.centre, ...c.corners.flatMap((corner) => corner.rows),
    ...(c.arcs?.left ? [c.arcs.left] : []), ...(c.arcs?.right ? [c.arcs.right] : []),
    ...(c.footer?.transPressure ? [c.footer.transPressure] : []),
  ]
}

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
 * already chosen. The alternator is the one the Roles setting names for this
 * engine's side, when it names one: there is no default and no guess otherwise.
 */
export function clusterFromProfile(
  profile: EngineProfile, current: EngineClusterConfig, ctx: EquipmentTileContext, roles: { port_alternator: string; starboard_alternator: string } | null,
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
  const roleAlternator = (side === 'stbd' ? roles?.starboard_alternator : roles?.port_alternator) ?? ''
  // "This boat has none" is an answer, not an item.
  const alternatorId = roleAlternator === ROLE_NONE ? '' : roleAlternator

  const footer: NonNullable<EngineClusterConfig['footer']> = {}
  for (const key of CLUSTER_FOOTER_SLOTS) {
    if (!isForeign(key)) continue
    const spec = FOREIGN_SLOTS[key]
    // What the operator already chose for this slot stays; only blanks are seeded.
    const existing = (current.footer?.[key]?.suffix ?? '').trim() !== '' ? current.footer?.[key] : undefined
    footer[key] = existing ?? { ...spec.slot, suffix: spec.suffix, ...(spec.kind === 'alternator' && alternatorId ? { equipmentId: alternatorId } : {}) }
  }
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

/** Every optional slot present, blank where unset, so each has a form to fill. */
function withBlankSlots(config: EngineClusterConfig): EngineClusterConfig {
  const footer: NonNullable<EngineClusterConfig['footer']> = { ...config.footer }
  for (const key of CLUSTER_FOOTER_SLOTS) {
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
 * too: it could read nothing.
 */
function pruneUnbound(config: EngineClusterConfig): EngineClusterConfig {
  const bound = (slot: GaugeWidgetConfig | undefined, needsItem = false) =>
    slot && (slot.suffix ?? '').trim() !== '' && (!needsItem || (slot.equipmentId ?? '') !== '') ? slot : undefined

  const left = bound(config.arcs?.left)
  const right = bound(config.arcs?.right)
  const footer: NonNullable<EngineClusterConfig['footer']> = {}
  for (const key of CLUSTER_FOOTER_SLOTS) {
    const slot = bound(config.footer?.[key], isForeign(key))
    if (slot) footer[key] = slot
  }

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
  onSave: (config: EngineClusterConfig) => void
}

export function EngineClusterConfigDialog({ widget, onCancel, onSave }: EngineClusterConfigDialogProps) {
  const ctx = useEquipmentTileContext(widget !== null)
  const roles = useVesselRoles(widget !== null)
  const { paths } = useSignalKPaths(widget !== null)
  const [config, setConfig] = useState<EngineClusterConfig | null>(null)
  const [saving, setSaving] = useState(false)
  const [saveError, setSaveError] = useState<string | null>(null)
  const [tab, setTab] = useState<ClusterTab>('dial')

  useEffect(() => {
    setConfig(widget?.cluster ? withBlankSlots(structuredClone(widget.cluster)) : null)
    setSaveError(null)
  }, [widget?.id, widget?.cluster])

  useEffect(() => { setTab('dial') }, [widget?.id])

  const item = findItem(ctx, config?.equipmentId)
  const zoneEditing = useSlotZoneEditing(ctx, item, config ? clusterSlotList(config) : [], paths)
  const { profile } = zoneEditing

  const setSlot = (update: (current: EngineClusterConfig) => EngineClusterConfig) =>
    setConfig((current) => (current ? update(current) : current))

  const setFuel = (update: (rail: ClusterFuelRailRef) => ClusterFuelRailRef) =>
    setSlot((c) => (c.fuel ? { ...c, fuel: update(c.fuel) } : c))

  const save = async () => {
    if (!config) return
    // Nothing to write to a profile: save the tile at once, in the same tick.
    if (!zoneEditing.dirty) {
      onSave(pruneUnbound(config))
      return
    }
    setSaving(true)
    setSaveError(null)
    try {
      onSave(pruneUnbound(withoutSettledZones(config, await zoneEditing.commit())))
    } catch (err) {
      setSaveError(err instanceof Error ? err.message : 'Could not save the profile zones')
    } finally {
      setSaving(false)
    }
  }

  const slotForm = (slot: GaugeWidgetConfig, idPrefix: string, onChange: (next: GaugeWidgetConfig) => void) => (
    <GaugeFields value={slot} paths={[]} idPrefix={idPrefix} onChange={onChange} equipment={zoneEditing.modeFor(slot)} />
  )

  const dialAttention = config ? [config.ring, config.centre].some((s) => (s.suffix ?? '').trim() === '') : false
  const cornersAttention = config ? config.corners.flatMap((corner) => corner.rows).some((s) => (s.suffix ?? '').trim() === '') : false
  const footerBound = config ? CLUSTER_FOOTER_SLOTS.filter((key) => footerSlotBound(config, key)).length : 0
  const fuelAttention = config?.fuel !== undefined && (config.fuel.bars.length === 0 || config.fuel.bars.some((bar) => (bar.tankId ?? '') === ''))

  const saveReason = ((): string | null => {
    if (!config) return null
    if ((config.equipmentId ?? '') === '') return 'Pick the engine.'
    if (dialAttention) return 'Choose a reading for the ring and centre on the Dial tab.'
    if (cornersAttention) return 'Choose a reading for every readout card.'
    if (zoneEditing.unresolved > 0) return 'Settle the zone conflicts above.'
    if (fuelAttention) return 'Choose a tank for every fuel bar, or remove the rail.'
    return null
  })()
  // One list of what blocks Save, so the line beside it always names the reason.
  const canSave = config !== null && saveReason === null

  return (
    <Dialog open={widget !== null} onOpenChange={(open) => { if (!open) onCancel() }}>
      <DialogContent className="flex max-h-[85vh] max-w-2xl flex-col gap-3 overflow-y-auto sm:h-[min(85vh,52rem)] sm:overflow-hidden">
        <DialogHeader className="flex-none">
          <div className="flex items-start justify-between gap-3 pr-6">
            <div className="grid min-w-0 gap-1.5">
              <DialogTitle>Engine Cluster</DialogTitle>
              <DialogDescription>
                Pick the engine and fill the card from its profile, then adjust any part on its tab.
              </DialogDescription>
            </div>
            <div className="hidden sm:block">
              <CardLocator active={tab} side={config?.side ?? 'port'} />
            </div>
          </div>
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
                <Input id="cluster-title" maxLength={GAUGE_GROUP_TITLE_MAX_LENGTH} value={config.title}
                  onChange={(e) => setSlot((c) => ({ ...c, title: e.target.value }))} placeholder={item?.name ?? 'Engine name'} />
                <FieldDescription>Blank shows the engine's name.</FieldDescription>
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
                <FieldDescription>Starboard mirrors the card.</FieldDescription>
              </Field>
            </div>

            <div className="flex flex-wrap items-center gap-x-3 gap-y-2">
              <Button
                variant="secondary"
                disabled={!profile}
                onClick={() => setSlot((c) => clusterFromProfile(profile!, c, ctx, roles))}
              >
                Fill slots from profile
              </Button>
              <FieldDescription className="min-w-0 flex-1 basis-56">
                {item && !profile ? "This engine has no profile, so there's nothing to fill from and no zones." : ''}
                {profile ? 'Replaces the engine slots and fills any empty footer row. Readings are live; zones come from the profile.' : ''}
              </FieldDescription>
            </div>

            <ZoneConflicts
              plan={zoneEditing.plan} choices={zoneEditing.choices} onChoose={zoneEditing.setChoice}
              heldWithoutItem={zoneEditing.heldWithoutItem} idPrefix="cluster"
            />
          </div>
        )}

        {config ? (
          <Tabs value={tab} onValueChange={(value) => setTab(value as ClusterTab)} className="flex flex-col gap-0 sm:min-h-0 sm:flex-1">
            <TabsList variant="line" className="flex-none">
              <TabsTrigger value="dial" className="text-sm">
                Dial
                {dialAttention && <AttentionMark />}
              </TabsTrigger>
              <TabsTrigger value="readouts" className="text-sm">
                Readouts
                {cornersAttention ? <AttentionMark /> : <TabCount shown={config.corners.length} spoken={`${config.corners.length} cards`} />}
              </TabsTrigger>
              <TabsTrigger value="footer" className="text-sm">
                Footer
                <TabCount shown={`${footerBound}/${CLUSTER_FOOTER_SLOTS.length}`} spoken={`${footerBound} of ${CLUSTER_FOOTER_SLOTS.length} shown`} />
              </TabsTrigger>
              <TabsTrigger value="fuel" className="text-sm">
                Fuel
                {fuelAttention
                  ? <AttentionMark />
                  : <TabCount shown={config.fuel === undefined ? 'Off' : config.fuel.bars.length} spoken={config.fuel === undefined ? 'off' : `${config.fuel.bars.length} tanks`} />}
              </TabsTrigger>
            </TabsList>

            <div className="sm:min-h-0 sm:flex-1 sm:overflow-y-auto">
              <TabsContent keepMounted value="dial" className="grid min-w-0 gap-3">
                <SlotSection title="Ring">
                  {slotForm(config.ring, 'cluster-ring', (next) => setSlot((c) => ({ ...c, ring: next })))}
                </SlotSection>
                <SlotSection title="Centre">
                  <Field className="mb-3">
                    <FieldLabel htmlFor="cluster-centre-icon">Hours icon</FieldLabel>
                    <select id="cluster-centre-icon" className="h-9 w-full rounded-md border bg-transparent px-3 text-sm"
                      value={config.centreIcon ?? ''}
                      onChange={(e) => setSlot((c) => ({ ...c, centreIcon: e.target.value === '' ? undefined : e.target.value }))}>
                      <option value="">Auto ({iconForSlot({ path: config.centre.suffix ?? '', quantity: config.centre.quantity })})</option>
                      {CLUSTER_ICON_NAMES.map((name) => <option key={name} value={name}>{name}</option>)}
                    </select>
                  </Field>
                  {slotForm(config.centre, 'cluster-centre', (next) => setSlot((c) => ({ ...c, centre: next })))}
                </SlotSection>
                <SlotSection title="Left arc (coolant)">
                  {slotForm(config.arcs?.left ?? blankArc('Coolant', 0, 120), 'cluster-arc-left',
                    (next) => setSlot((c) => ({ ...c, arcs: { ...c.arcs, left: next } })))}
                </SlotSection>
                <SlotSection title="Right arc (gearbox oil temperature)">
                  {slotForm(config.arcs?.right ?? blankArc('Gearbox', 0, 120), 'cluster-arc-right',
                    (next) => setSlot((c) => ({ ...c, arcs: { ...c.arcs, right: next } })))}
                </SlotSection>
              </TabsContent>

              <TabsContent keepMounted value="readouts" className="grid min-w-0 gap-3">
                {config.corners.map((corner, cornerIndex) => (
                  <div key={cornerIndex} className="rounded-md border border-border p-3">
                    <div className="mb-2 flex items-center gap-2">
                      <Input aria-label={`Corner ${cornerIndex + 1} label`} className="h-8 max-w-[12rem]"
                        value={corner.label}
                        onChange={(e) => setSlot((c) => ({
                          ...c,
                          corners: c.corners.map((x, i) => (i === cornerIndex ? { ...x, label: e.target.value } : x)),
                        }))} />
                      <select
                        aria-label={`Corner ${cornerIndex + 1} icon`}
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
                      <Button size="sm" variant="ghost" aria-label={`Remove corner ${cornerIndex + 1}`}
                        onClick={() => setSlot((c) => ({ ...c, corners: c.corners.filter((_, i) => i !== cornerIndex) }))}>
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
                        })))}
                      </div>
                    ))}
                  </div>
                ))}

                <Button variant="ghost" className="w-fit" disabled={config.corners.length >= CLUSTER_MAX_CORNERS}
                  onClick={() => setSlot((c) => ({ ...c, corners: [...c.corners, { label: 'New', rows: [blankSlot('')] }] }))}>
                  <Plus className="size-3.5" /> Add corner
                </Button>
              </TabsContent>

              <TabsContent keepMounted value="footer" className="min-w-0">
                <div className="divide-y divide-border rounded-md border border-border">
                  {CLUSTER_FOOTER_SLOTS.map((key) => {
                    const slot = config.footer?.[key]
                    const set = (next: GaugeWidgetConfig) => setSlot((c) => ({ ...c, footer: { ...c.footer, [key]: next } }))
                    const bound = footerSlotBound(config, key)
                    const reading = (slot?.suffix ?? '').trim()
                    return (
                      <details key={key} className="group">
                        <summary className="flex cursor-pointer list-none items-center gap-2 p-3 text-sm focus-visible:outline-hidden focus-visible:ring-2 focus-visible:ring-ring [&::-webkit-details-marker]:hidden">
                          <span className="min-w-0 flex-1 truncate">{FOOTER_TITLES[key]}</span>
                          {bound && slot ? (
                            <span className="flex min-w-0 max-w-[50%] items-baseline gap-1.5 text-xs">
                              {isForeign(key) && !findItem(ctx, slot.equipmentId)
                                ? <span className="truncate text-amber-600 dark:text-amber-400">{ctx.items.length === 0 ? '--' : 'Equipment missing'}</span>
                                : <span className="truncate">{isForeign(key) ? findItem(ctx, slot.equipmentId)?.name : (slot.label || reading)}</span>}
                              {isForeign(key) && <span className="truncate text-muted-foreground">{reading}</span>}
                            </span>
                          ) : (
                            <span className="text-xs text-muted-foreground">Not shown</span>
                          )}
                          <ChevronRight aria-hidden className="size-4 flex-none text-muted-foreground transition-transform group-open:rotate-90 motion-reduce:transition-none" />
                        </summary>
                        <div className="px-3 pb-3">
                          {isForeign(key) ? (
                            <ForeignSlotForm
                              idPrefix={`cluster-footer-${key}`} slotKey={key} slot={slot ?? { ...FOREIGN_SLOTS[key].slot, suffix: '' }}
                              ctx={ctx} onChange={set}
                            />
                          ) : (
                            slotForm(slot ?? blankSlot(''), `cluster-footer-${key}`, set)
                          )}
                        </div>
                      </details>
                    )
                  })}
                </div>
              </TabsContent>

              <TabsContent keepMounted value="fuel" className="min-w-0">
                <div className="rounded-md border border-border p-3">
                  <FieldDescription className="mb-2">
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
                            onChange={(id) => setFuel((f) => ({
                              ...f,
                              bars: f.bars.map((b, i) => (i === index
                                ? { ...b, tankId: id, source: id === '' ? b.source : undefined, label: b.label || (findItem(ctx, id)?.name ?? '') }
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
              </TabsContent>
            </div>
          </Tabs>
        ) : (
          <div className="min-h-0 flex-1" />
        )}

        <DialogFooter className="flex-none">
          {saveError
            ? <p role="alert" className="mr-auto text-sm text-destructive">{saveError}</p>
            : (!canSave && !saving && saveReason && <p className="mr-auto text-xs text-muted-foreground">{saveReason}</p>)}
          <Button variant="ghost" onClick={onCancel}>Cancel</Button>
          <Button disabled={!canSave || saving} onClick={() => { void save() }}>Save</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

type ClusterTab = 'dial' | 'readouts' | 'footer' | 'fuel'

/** Whether a footer slot reads something: a reading, and for another item's slot, the item too. Mirrors pruneUnbound. */
function footerSlotBound(config: EngineClusterConfig, key: ClusterFooterSlot): boolean {
  const slot = config.footer?.[key]
  return !!slot && (slot.suffix ?? '').trim() !== '' && (!isForeign(key) || (slot.equipmentId ?? '') !== '')
}

/** A tab's count: the figure on screen, and what it counts for a screen reader. */
function TabCount({ shown, spoken }: { shown: string | number; spoken: string }) {
  return (
    <>
      <span aria-hidden className="text-xs text-muted-foreground tabular-nums">{shown}</span>
      <span className="sr-only">{spoken}</span>
    </>
  )
}

function AttentionMark() {
  return (
    <>
      <span aria-hidden className="size-1.5 rounded-full bg-amber-500" />
      <span className="sr-only">needs attention</span>
    </>
  )
}

/**
 * A small schematic of the card with the part the open tab edits lit. Drawn for
 * a port engine and mirrored for starboard, like the card itself.
 */
function CardLocator({ active, side }: { active: ClusterTab; side: ClusterSide }) {
  const region = (on: boolean) => cn(
    'transition-colors duration-150 motion-reduce:transition-none',
    on ? 'fill-primary/20 stroke-primary' : 'fill-muted stroke-border',
  )
  const dial = active === 'dial'
  const corner = active === 'readouts'
  return (
    <svg aria-hidden viewBox="0 0 100 77" className="w-24 shrink-0">
      <g transform={side === 'stbd' ? 'matrix(-1 0 0 1 100 0)' : undefined}>
        <path d="M 12 8 A 36 36 0 0 0 12 52" strokeWidth={4} strokeLinecap="round"
          className={cn(
            'transition-colors duration-150 motion-reduce:transition-none',
            active === 'fuel' ? 'stroke-primary' : 'stroke-border',
          )}
          fill="none" />
        <circle cx={55} cy={30} r={20} strokeWidth={1.5} className={region(dial)} />
        <path d="M 46 52 A 14 14 0 0 1 38 49" strokeWidth={1.5} fill="none" className={cn(region(dial), 'fill-transparent')} />
        <path d="M 64 52 A 14 14 0 0 0 72 49" strokeWidth={1.5} fill="none" className={cn(region(dial), 'fill-transparent')} />
        {[[20, 4], [76, 4], [20, 40], [76, 40]].map(([x, y]) => (
          <rect key={`${x}-${y}`} x={x} y={y} width={14} height={12} rx={2} strokeWidth={1.5} className={region(corner)} />
        ))}
        <rect x={2} y={62} width={96} height={12} rx={2} strokeWidth={1.5} className={region(active === 'footer')} />
      </g>
    </svg>
  )
}

/**
 * A footer slot that reads another item than the engine: pick the item (the
 * starter battery, the alternator, the exhaust's node) and the reading under its
 * source. Its zones come from that item's profile and are edited there, not
 * here. Leaving the reading empty leaves the slot out.
 */
function ForeignSlotForm({ idPrefix, slotKey, slot, ctx, onChange }: {
  idPrefix: string
  slotKey: keyof typeof FOREIGN_SLOTS
  slot: GaugeWidgetConfig
  ctx: EquipmentTileContext
  onChange: (next: GaugeWidgetConfig) => void
}) {
  const spec = FOREIGN_SLOTS[slotKey]
  const item = findItem(ctx, slot.equipmentId)
  const profile = item ? profileOf(ctx, item) : undefined
  const suffixes = (profile?.gauges ?? []).map((g) => g.path_suffix)
  const set = (patch: Partial<GaugeWidgetConfig>) => {
    const next = { ...slot, ...patch }
    // A reading on the item's profile brings the profile's quantity, unit and scale.
    const gauge = profileGaugeFor(item ? profileOf(ctx, findItem(ctx, next.equipmentId) ?? item) : undefined, (next.suffix ?? '').trim())
    onChange(gauge ? slotFromProfileGauge({ ...next, zones: undefined }, gauge) : next)
  }
  return (
    <div className="grid gap-2">
      <EquipmentPicker
        idPrefix={idPrefix} label="Equipment" kind={spec.kind} value={slot.equipmentId ?? ''} ctx={ctx}
        onChange={(id) => set({ equipmentId: id === '' ? undefined : id })}
      />
      <Field>
        <FieldLabel htmlFor={`${idPrefix}-suffix`}>Reading</FieldLabel>
        <Input id={`${idPrefix}-suffix`} list={`${idPrefix}-suffixes`} value={slot.suffix ?? ''}
          placeholder={spec.suffix || 'reading under the equipment'}
          onChange={(e) => set({ suffix: e.target.value })} />
        <datalist id={`${idPrefix}-suffixes`}>
          {[spec.suffix, ...suffixes].filter((v, i, a) => v !== '' && a.indexOf(v) === i).map((v) => <option key={v} value={v} />)}
        </datalist>
        <FieldDescription>
          {item && !profile ? 'This equipment has no profile, so the reading has no zones.' : 'Zones come from the equipment\'s profile; edit them on the equipment.'}
          {' '}Leave empty to leave this out.
        </FieldDescription>
      </Field>
    </div>
  )
}

function SlotSection({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <div className="rounded-md border border-border p-3">
      <p className="mb-2 text-[10px] font-medium uppercase tracking-wider text-muted-foreground">{title}</p>
      {children}
    </div>
  )
}

