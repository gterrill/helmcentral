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
import { useSlotZoneEditing } from '@/hooks/use-slot-zone-editing'
import { ZoneConflicts } from '@/components/zone-conflicts'
import {
  CLUSTER_MAX_CORNERS,
  CLUSTER_MAX_CORNER_ROWS,
  CLUSTER_MAX_FUEL_BARS,
  CLUSTER_MAX_TELLTALES,
  GAUGE_GROUP_TITLE_MAX_LENGTH,
  type ClusterFuelRailRef,
  type DashboardLayoutItem,
  type EngineClusterConfig,
  type FuelBarRef,
  type GaugeWidgetConfig,
  type GaugeZone,
} from '@/lib/dashboard-widgets'
import { CLUSTER_ICON_NAMES, iconForSlot } from '@/lib/cluster-icons'
import { findItem, profileGaugeFor } from '@/lib/equipment-tiles'
import type { EngineProfile } from '@/lib/engine-profiles'

/**
 * The slots a cluster fills from a profile, and the suffix each looks for.
 * Anything the profile does not cover keeps the suffix and is set by hand.
 */
const SLOT_SUFFIXES = {
  ring: 'revolutions',
  // Hours in the notch, not fuel rate: hours barely move and suit a small
  // badge, while fuel rate changes with every throttle input and earns a full
  // readout with a scale under it.
  centre: 'runTime',
  corners: [
    { label: 'Oil', suffixes: ['oilPressure'] },
    { label: 'Boost', suffixes: ['boostPressure'] },
    // Load rather than the temperatures. Four boxes of one reading each keeps
    // them the same size, and a temperature is something you check by glancing
    // for a colour, which is what the telltale strip is for.
    { label: 'Load', suffixes: ['engineLoad'] },
    // Burn rate only. Economy is a vessel figure, not an engine one: SignalK's
    // per-engine fuel.economy reads roughly twice the boat's on a twin, so
    // showing it here invites range planning that is out by the number of
    // engines running (ADR 0055).
    { label: 'Fuel', suffixes: ['fuel.rate'] },
  ],
  // Exhaust is absent: on this vessel it lives under propulsion.0, outside the
  // engine's own source, so a tile that reads one engine cannot show it.
  telltales: ['temperature', 'transmission.oilTemperature'],
} as const

/** Every slot of a cluster in a fixed order: ring, centre, corner rows, telltales. */
function clusterSlotList(c: EngineClusterConfig): GaugeWidgetConfig[] {
  return [c.ring, c.centre, ...c.corners.flatMap((corner) => corner.rows), ...(c.telltales ?? [])]
}

/** The cluster with the zones that now live on the profile taken off the slots at `cleared`. */
function withoutSettledZones(c: EngineClusterConfig, cleared: Set<number>): EngineClusterConfig {
  let i = 0
  const settle = (slot: GaugeWidgetConfig): GaugeWidgetConfig => (cleared.has(i++) ? { ...slot, zones: undefined } : slot)
  const ring = settle(c.ring)
  const centre = settle(c.centre)
  const corners = c.corners.map((corner) => ({ ...corner, rows: corner.rows.map(settle) }))
  const telltales = c.telltales?.map(settle)
  return { ...c, ring, centre, corners, telltales }
}

function blankSlot(label: string): GaugeWidgetConfig {
  return { path: '', suffix: '', label, display: 'numeric', quantity: 'raw', unit: 'raw' }
}

/** A blank fuel bar: the tank is picked from the equipment list. */
function blankFuelBar(label: string): FuelBarRef {
  return { tankId: '', label }
}

/** Builds the slots of a cluster from a profile; the equipment is already chosen. */
function clusterFromProfile(profile: EngineProfile, current: EngineClusterConfig): EngineClusterConfig {
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

  return {
    ...current,
    ring,
    centre: pick(SLOT_SUFFIXES.centre, 'Hours'),
    corners: SLOT_SUFFIXES.corners.map((corner) => ({
      label: corner.label,
      rows: corner.suffixes.map((suffix) => pick(suffix, corner.label)),
    })),
    // Whole degrees in a strip: a tenth of a degree of coolant is noise at a
    // glance, and the strip is all glance.
    telltales: SLOT_SUFFIXES.telltales.map((suffix) => ({ ...pick(suffix, 'Temp'), decimals: 0 })),
  }
}

interface EngineClusterConfigDialogProps {
  widget: DashboardLayoutItem | null
  onCancel: () => void
  onSave: (config: EngineClusterConfig) => void
}

export function EngineClusterConfigDialog({ widget, onCancel, onSave }: EngineClusterConfigDialogProps) {
  const ctx = useEquipmentTileContext(widget !== null)
  const { paths } = useSignalKPaths(widget !== null)
  const [config, setConfig] = useState<EngineClusterConfig | null>(null)
  const [saving, setSaving] = useState(false)
  const [saveError, setSaveError] = useState<string | null>(null)

  useEffect(() => {
    setConfig(widget?.cluster ? structuredClone(widget.cluster) : null)
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
    [c.ring, c.centre, ...c.corners.flatMap((corner) => corner.rows), ...(c.telltales ?? [])]
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
      onSave(config)
      return
    }
    setSaving(true)
    setSaveError(null)
    try {
      onSave(withoutSettledZones(config, await zoneEditing.commit()))
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
            A ticked ring with the primary reading inside it and four corner cards. Pick the engine, fill
            the slots from its profile, then adjust any slot.
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
                onClick={() => setSlot((c) => clusterFromProfile(profile!, c))}
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
            {profile ? ' Filling replaces every slot.' : ''}
          </FieldDescription>

          {config && (
            <>
              <div className="grid gap-3 sm:grid-cols-2">
                <Field>
                  <FieldLabel htmlFor="cluster-title">Title</FieldLabel>
                  <Input id="cluster-title" maxLength={GAUGE_GROUP_TITLE_MAX_LENGTH} value={config.title}
                    onChange={(e) => setSlot((c) => ({ ...c, title: e.target.value }))} placeholder={item?.name ?? 'Engine name'} />
                  <FieldDescription>Blank shows the engine's name.</FieldDescription>
                </Field>
                <Field>
                  <FieldLabel htmlFor="cluster-centre-icon">Centre icon</FieldLabel>
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

                  {corner.rows.map((row, rowIndex) => (
                    <div key={rowIndex} className="mb-2 border-b border-border/60 pb-2 last:border-b-0">
                      {slotForm(row, `cluster-${cornerIndex}-${rowIndex}`, (next) => setSlot((c) => ({
                        ...c,
                        corners: c.corners.map((x, i) => (i === cornerIndex
                          ? { ...x, rows: x.rows.map((r, j) => (j === rowIndex ? next : r)) }
                          : x)),
                      })))}
                      <Button size="sm" variant="ghost" aria-label={`Remove corner ${cornerIndex + 1} row ${rowIndex + 1}`}
                        onClick={() => setSlot((c) => ({
                          ...c,
                          corners: c.corners.map((x, i) => (i === cornerIndex
                            ? { ...x, rows: x.rows.filter((_, j) => j !== rowIndex) }
                            : x)),
                        }))}>
                        <Trash2 className="size-3.5" /> Remove row
                      </Button>
                    </div>
                  ))}

                  <Button variant="ghost" className="w-fit"
                    disabled={corner.rows.length >= CLUSTER_MAX_CORNER_ROWS}
                    onClick={() => setSlot((c) => ({
                      ...c,
                      corners: c.corners.map((x, i) => (i === cornerIndex
                        ? { ...x, rows: [...x.rows, blankSlot('')] }
                        : x)),
                    }))}>
                    <Plus className="size-3.5" /> Add row
                  </Button>
                </div>
              ))}

              <Button variant="ghost" className="w-fit" disabled={config.corners.length >= CLUSTER_MAX_CORNERS}
                onClick={() => setSlot((c) => ({ ...c, corners: [...c.corners, { label: 'New', rows: [blankSlot('')] }] }))}>
                <Plus className="size-3.5" /> Add corner
              </Button>

              <div className="rounded-md border border-border p-3">
                <p className="mb-1 text-[10px] font-medium uppercase tracking-wider text-muted-foreground">Telltales</p>
                <FieldDescription className="mb-2">
                  Shown as a strip under the dial: icon, label, value, coloured by zone. For the
                  readings you check by glancing for a colour rather than reading a number.
                </FieldDescription>

                {(config.telltales ?? []).map((telltale, index) => (
                  <div key={index} className="mb-2 border-b border-border/60 pb-2 last:border-b-0">
                    {slotForm(telltale, `cluster-telltale-${index}`, (next) => setSlot((c) => ({
                      ...c,
                      telltales: (c.telltales ?? []).map((t, i) => (i === index ? next : t)),
                    })))}
                    <Button size="sm" variant="ghost" aria-label={`Remove telltale ${index + 1}`}
                      onClick={() => setSlot((c) => ({
                        ...c,
                        telltales: (c.telltales ?? []).filter((_, i) => i !== index),
                      }))}>
                      <Trash2 className="size-3.5" /> Remove telltale
                    </Button>
                  </div>
                ))}

                <Button variant="ghost" className="w-fit"
                  disabled={(config.telltales ?? []).length >= CLUSTER_MAX_TELLTALES}
                  onClick={() => setSlot((c) => ({ ...c, telltales: [...(c.telltales ?? []), blankSlot('')] }))}>
                  <Plus className="size-3.5" /> Add telltale
                </Button>
              </div>

              <div className="rounded-md border border-border p-3">
                <p className="mb-1 text-[10px] font-medium uppercase tracking-wider text-muted-foreground">Fuel rail</p>
                <FieldDescription className="mb-2">
                  A bar per tank down one edge of the tile, with the side's total underneath. Pick the
                  tank items; their level and capacity are read live.
                </FieldDescription>

                {config.fuel === undefined ? (
                  <Button variant="ghost" className="w-fit"
                    onClick={() => setSlot((c) => ({
                      ...c,
                      // Left for a cluster titled Port, right otherwise, so a
                      // facing pair puts its rails outboard without being told.
                      fuel: {
                        side: /^p(ort)?\b/i.test((c.title.trim() || item?.name || '')) ? 'left' : 'right',
                        bars: [blankFuelBar('Fwd'), blankFuelBar('Aft')],
                      },
                    }))}>
                    <Plus className="size-3.5" /> Add fuel rail
                  </Button>
                ) : (
                  <>
                    <Field className="mb-2">
                      {/* "Edge", not "side": which side of the boat the tanks
                          are on is the tanks' own business. */}
                      <FieldLabel htmlFor="cluster-fuel-side">Tile edge</FieldLabel>
                      <select id="cluster-fuel-side"
                        className="h-9 rounded-md border bg-transparent px-3 text-sm"
                        value={config.fuel.side}
                        onChange={(e) => setFuel((f) => ({ ...f, side: e.target.value as ClusterFuelRailRef['side'] }))}>
                        <option value="left">Left</option>
                        <option value="right">Right</option>
                      </select>
                    </Field>

                    <Field className="mb-2">
                      <FieldLabel htmlFor="cluster-fuel-total">Total caption</FieldLabel>
                      <Input id="cluster-fuel-total" value={config.fuel.totalLabel ?? ''}
                        placeholder="Total"
                        onChange={(e) => setFuel((f) => ({ ...f, totalLabel: e.target.value }))} />
                    </Field>

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
