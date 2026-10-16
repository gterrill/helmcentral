import { Plus, Trash2 } from 'lucide-react'
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
  type GaugeZone,
} from '@/lib/dashboard-widgets'
import { CLUSTER_ICON_NAMES, iconForSlot } from '@/lib/cluster-icons'
import { findItem, profileGaugeFor, profileOf, type EquipmentTileContext } from '@/lib/equipment-tiles'
import type { EngineProfile } from '@/lib/engine-profiles'

/**
 * The slots a cluster fills from the engine's profile, and the suffix each looks
 * for. Anything the profile does not cover keeps the suffix and is set by hand.
 */
const SLOT_SUFFIXES = {
  ring: 'revolutions',
  // Hours in the tab, not fuel rate: hours barely move and suit a small badge,
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
function clusterFromProfile(
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
    return old ? { ...slot, zones: old.zones, quantity: old.quantity, unit: old.unit, min: old.min, max: old.max } : slot
  }

  const ring = { ...pick(SLOT_SUFFIXES.ring, 'RPM') }
  // RPM reads better as a divided scale, the way every tachometer draws it.
  ring.display = 'radial'
  ring.ringStyle = 'instrument'
  ring.labelDivisor = 100
  if (ring.max === undefined) ring.max = 3000

  const engine = findItem(ctx, current.equipmentId)
  const side = sideOf(engine?.signalk_source)
  const alternatorId = (side === 'stbd' ? roles?.starboard_alternator : roles?.port_alternator) ?? ''

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

  useEffect(() => {
    setConfig(widget?.cluster ? withBlankSlots(structuredClone(widget.cluster)) : null)
    setSaveError(null)
  }, [widget?.id, widget?.cluster])

  const item = findItem(ctx, config?.equipmentId)
  const zoneEditing = useSlotZoneEditing(ctx, item, config ? clusterSlotList(config) : [], paths)
  const { profile } = zoneEditing

  const setSlot = (update: (current: EngineClusterConfig) => EngineClusterConfig) =>
    setConfig((current) => (current ? update(current) : current))

  const setFuel = (update: (rail: ClusterFuelRailRef) => ClusterFuelRailRef) =>
    setSlot((c) => (c.fuel ? { ...c, fuel: update(c.fuel) } : c))

  const slotsFilled = (c: EngineClusterConfig) =>
    [c.ring, c.centre, ...c.corners.flatMap((corner) => corner.rows)]
      .every((slot) => (slot.suffix ?? '').trim() !== '')

  const canSave =
    config !== null &&
    (config.equipmentId ?? '') !== '' &&
    slotsFilled(config) &&
    zoneEditing.unresolved === 0 &&
    (config.fuel === undefined || (config.fuel.bars.length > 0 && config.fuel.bars.every((bar) => (bar.tankId ?? '') !== '')))

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

  return (
    <Dialog open={widget !== null} onOpenChange={(open) => { if (!open) onCancel() }}>
      <DialogContent className="max-h-[85vh] max-w-2xl overflow-y-auto">
        <DialogHeader>
          <DialogTitle>Engine Cluster</DialogTitle>
          <DialogDescription>
            One engine as a tachometer with four readout cards, two temperature arcs, a fuel fan and a
            footer. Pick the engine, fill the slots from its profile, then adjust any slot.
          </DialogDescription>
        </DialogHeader>

        <div className="grid gap-3">
          {config && (
            <div className="grid items-end gap-2 sm:grid-cols-[1fr_auto]">
              <EquipmentPicker
                idPrefix="cluster"
                label="Engine"
                kind="engine"
                value={config.equipmentId ?? ''}
                // Choosing equipment ends the source the tile remembered.
                onChange={(id) => setSlot((c) => ({ ...c, equipmentId: id, source: id === '' ? c.source : undefined }))}
                ctx={ctx}
              />
              <Button
                variant="secondary"
                disabled={!profile}
                onClick={() => setSlot((c) => clusterFromProfile(profile!, c, ctx, roles))}
              >
                Fill slots from profile
              </Button>
            </div>
          )}
          {config && (
            <ZoneConflicts
              plan={zoneEditing.plan} choices={zoneEditing.choices} onChoose={zoneEditing.setChoice}
              heldWithoutItem={zoneEditing.heldWithoutItem} idPrefix="cluster"
            />
          )}
          <FieldDescription>
            The tile reads this engine's readings live and takes its zones from the engine's profile.
            {item && !profile ? ' This engine has no profile, so there is nothing to fill from and no zones.' : ''}
            {profile ? ' Filling replaces the engine slots and seeds the footer; a footer slot you have already set is kept.' : ''}
          </FieldDescription>

          {config && (
            <>
              <div className="grid gap-3 sm:grid-cols-3">
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
                <Field>
                  <FieldLabel htmlFor="cluster-centre-icon">Hours icon</FieldLabel>
                  <select id="cluster-centre-icon" className="h-9 w-full rounded-md border bg-transparent px-3 text-sm"
                    value={config.centreIcon ?? ''}
                    onChange={(e) => setSlot((c) => ({ ...c, centreIcon: e.target.value === '' ? undefined : e.target.value }))}>
                    <option value="">Auto ({iconForSlot({ path: config.centre.suffix ?? '', quantity: config.centre.quantity })})</option>
                    {CLUSTER_ICON_NAMES.map((name) => <option key={name} value={name}>{name}</option>)}
                  </select>
                </Field>
              </div>

              <SlotSection title="Ring">
                {slotForm(config.ring, 'cluster-ring', (next) => setSlot((c) => ({ ...c, ring: next })))}
              </SlotSection>
              <SlotSection title="Centre">
                {slotForm(config.centre, 'cluster-centre', (next) => setSlot((c) => ({ ...c, centre: next })))}
              </SlotSection>

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

              <SlotSection title="Dial arc, left (coolant)">
                {slotForm(config.arcs?.left ?? blankArc('Coolant', 0, 120), 'cluster-arc-left',
                  (next) => setSlot((c) => ({ ...c, arcs: { ...c.arcs, left: next } })))}
              </SlotSection>
              <SlotSection title="Dial arc, right (gearbox oil temperature)">
                {slotForm(config.arcs?.right ?? blankArc('Gearbox', 0, 120), 'cluster-arc-right',
                  (next) => setSlot((c) => ({ ...c, arcs: { ...c.arcs, right: next } })))}
              </SlotSection>

              {CLUSTER_FOOTER_SLOTS.map((key) => {
                const slot = config.footer?.[key]
                const set = (next: GaugeWidgetConfig) => setSlot((c) => ({ ...c, footer: { ...c.footer, [key]: next } }))
                return (
                  <SlotSection key={key} title={`Footer: ${FOOTER_TITLES[key]}`}>
                    {isForeign(key) ? (
                      <ForeignSlotForm
                        idPrefix={`cluster-footer-${key}`} slotKey={key} slot={slot ?? { ...FOREIGN_SLOTS[key].slot, suffix: '' }}
                        ctx={ctx} onChange={set}
                      />
                    ) : (
                      slotForm(slot ?? blankSlot(''), `cluster-footer-${key}`, set)
                    )}
                  </SlotSection>
                )
              })}

              <div className="rounded-md border border-border p-3">
                <p className="mb-1 text-[10px] font-medium uppercase tracking-wider text-muted-foreground">Fuel rail</p>
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
                        <FuelZones
                          index={index}
                          zones={bar.zones ?? []}
                          onChange={(zones) => setFuel((f) => ({
                            ...f,
                            bars: f.bars.map((b, i) => (i === index ? { ...b, zones: zones.length > 0 ? zones : undefined } : b)),
                          }))}
                        />
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
            </>
          )}
        </div>

        <DialogFooter>
          {saveError && <p role="alert" className="mr-auto text-sm text-destructive">{saveError}</p>}
          <Button variant="ghost" onClick={onCancel}>Cancel</Button>
          <Button disabled={!canSave || saving} onClick={() => { void save() }}>Save</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
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

const FUEL_ZONE_STATES: { id: GaugeZone['state']; label: string }[] = [
  { id: 'alert', label: 'Alert' },
  { id: 'warn', label: 'Warning' },
  { id: 'alarm', label: 'Alarm' },
  { id: 'emergency', label: 'Emergency' },
]

/**
 * Low-fuel bands for one tank, as a level to fall below and a severity. A tank
 * has no equipment profile to hold zones, so these stay with the fuel bar, and
 * each one raises an alarm when the tank's level falls past it.
 */
function FuelZones({ index, zones, onChange }: { index: number; zones: GaugeZone[]; onChange: (zones: GaugeZone[]) => void }) {
  const set = (i: number, next: GaugeZone) => onChange(zones.map((z, j) => (j === i ? next : z)))
  return (
    <div className="grid gap-2">
      <FieldDescription>
        Low fuel alarm. Unlike engine readings, these belong to this bar, because a tank has no profile.
      </FieldDescription>
      {zones.map((zone, i) => (
        <div key={i} className="grid items-end gap-2 sm:grid-cols-[1fr_1fr_auto]">
          <div className="flex items-center gap-1">
            <span className="text-xs text-muted-foreground">Below</span>
            <Input
              aria-label={`Tank ${index + 1} low fuel level ${i + 1}`}
              type="number" step="any" min={1} max={99} value={zone.to}
              onChange={(e) => set(i, { ...zone, from: 0, to: Number(e.target.value) })}
            />
            <span className="text-xs text-muted-foreground">%</span>
          </div>
          <select
            aria-label={`Tank ${index + 1} low fuel severity ${i + 1}`}
            className="h-9 w-full rounded-md border bg-transparent px-3 text-sm"
            value={zone.state}
            onChange={(e) => set(i, { ...zone, state: e.target.value as GaugeZone['state'] })}
          >
            {FUEL_ZONE_STATES.map((st) => <option key={st.id} value={st.id}>{st.label}</option>)}
          </select>
          <Button size="sm" variant="ghost" aria-label={`Remove tank ${index + 1} low fuel level ${i + 1}`}
            onClick={() => onChange(zones.filter((_, j) => j !== i))}>
            <Trash2 className="size-3.5" />
          </Button>
        </div>
      ))}
      <Button size="sm" variant="ghost" className="w-fit" onClick={() => onChange([...zones, { from: 0, to: 15, state: 'warn' }])}>
        <Plus className="size-3.5" /> Add low fuel alarm
      </Button>
    </div>
  )
}
