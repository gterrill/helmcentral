import { Plus, Trash2 } from 'lucide-react'
import { useEffect, useMemo, useState } from 'react'

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
import { GaugeFields } from '@/components/gauge-fields'
import { Input } from '@/components/ui/input'
import { useEngineProfiles } from '@/hooks/use-engine-profiles'
import { useSignalKPaths } from '@/hooks/use-signalk-paths'
import {
  CLUSTER_MAX_CORNERS,
  CLUSTER_MAX_CORNER_ROWS,
  CLUSTER_MAX_FUEL_BARS,
  CLUSTER_MAX_TELLTALES,
  GAUGE_GROUP_TITLE_MAX_LENGTH,
  type ClusterFuelRail,
  type DashboardLayoutItem,
  type EngineClusterConfig,
  type FuelBarConfig,
  type GaugeWidgetConfig,
} from '@/lib/dashboard-widgets'
import { CLUSTER_ICON_NAMES, iconForSlot } from '@/lib/cluster-icons'
import { QUANTITIES, quantityById, unitOption } from '@/lib/quantities'
import { commonInstancePrefix, instancePrefixCandidates, profileToGauges, type EngineProfile } from '@/lib/engine-profiles'

/**
 * The slots a cluster fills from a profile, and the suffix each looks for.
 * Anything the profile does not cover stays blank and is typed by hand — which
 * is how exhaust temperature gets in, since it lives under propulsion.0 rather
 * than the engine node and no suffix can reach it.
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
  // Exhaust is deliberately absent: it lives under propulsion.0 rather than the
  // engine node, so no suffix reaches it and it stays hand-typed.
  telltales: ['temperature', 'transmission.oilTemperature'],
} as const

function blankSlot(label: string): GaugeWidgetConfig {
  return { path: '', label, display: 'numeric', quantity: 'raw', unit: 'raw' }
}

/**
 * A blank fuel bar (ADR 0061).
 *
 * The quantities are set here rather than left for the operator to pick,
 * because no tank path on this vessel publishes meta.units: the path picker
 * would preselect Unitless, convertFromSI would be the identity, and the rail
 * would read 1.2 where it should read 890. The backend rejects that config, so
 * seeding it correctly is what keeps the rule invisible.
 */
function blankFuelBar(label: string): FuelBarConfig {
  return {
    level: { path: '', label, display: 'bar', quantity: 'ratio', unit: 'percent', decimals: 0, min: 0, max: 100 },
    capacity: { path: '', label: '', display: 'numeric', quantity: 'volume', unit: 'L', decimals: 0 },
  }
}

/**
 * The capacity path that goes with a level path.
 *
 * SignalK names them as siblings, so picking `tanks.fuel.5.currentLevel` all
 * but names `tanks.fuel.5.capacity`. Filling it is what makes a rail a
 * half-minute of configuration instead of eight paths typed by hand.
 */
function capacityPathFor(levelPath: string): string {
  return levelPath.endsWith('.currentLevel') ? levelPath.replace(/\.currentLevel$/, '.capacity') : ''
}

/**
 * Hold a fuel slot to the quantity the rail needs.
 *
 * GaugeFields preselects the quantity from SignalK's meta.units whenever a path
 * is picked, which is right everywhere else and wrong here: no tank path on
 * this vessel publishes units, so it infers Unitless and silently undoes the
 * seed. The backend then refuses the save, and the operator gets an error about
 * a field the form never showed them. The rail's quantities are fixed by what
 * it computes, so pin them rather than asking.
 */
function pinQuantity(slot: GaugeWidgetConfig, quantity: string): GaugeWidgetConfig {
  if (slot.quantity === quantity) return slot
  return { ...slot, quantity, unit: unitOption(quantity, slot.unit).id }
}

/** Builds a whole cluster from a profile and an instance prefix. */
function clusterFromProfile(profile: EngineProfile, instance: string, title: string): EngineClusterConfig {
  const gauges = profileToGauges(profile, instance)
  const bySuffix = new Map(profile.gauges.map((g, i) => [g.path_suffix, gauges[i]]))
  const pick = (suffix: string, fallbackLabel: string) => bySuffix.get(suffix) ?? blankSlot(fallbackLabel)

  const ring = { ...pick(SLOT_SUFFIXES.ring, 'RPM') }
  // RPM reads better as a divided scale, the way every tachometer draws it.
  ring.display = 'radial'
  ring.ringStyle = 'instrument'
  ring.labelDivisor = 100
  if (ring.max === undefined) ring.max = 3000

  return {
    title,
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
  const { paths } = useSignalKPaths(widget !== null)
  const { profiles, error: profilesError } = useEngineProfiles(widget !== null)
  const [config, setConfig] = useState<EngineClusterConfig | null>(null)
  const [profileID, setProfileID] = useState('')
  const [instance, setInstance] = useState('')

  useEffect(() => {
    setConfig(widget?.cluster ? structuredClone(widget.cluster) : null)
    setProfileID('')
  }, [widget?.id, widget?.cluster])

  const profile = useMemo(() => profiles.find((p) => p.id === profileID) ?? profiles[0], [profiles, profileID])

  const candidates = useMemo(
    () => (profile ? instancePrefixCandidates(profile, paths) : []),
    [profile, paths],
  )

  // Seed from the cluster's own slots when it has any, so re-opening an
  // existing tile never proposes the other engine.
  const seeded = useMemo(() => {
    const slots = config
      ? [config.ring, config.centre, ...config.corners.flatMap((c) => c.rows), ...(config.telltales ?? [])]
      : []
    const own = profile
      ? commonInstancePrefix(slots.filter((s) => s.path.trim() !== ''), profile.gauges.map((g) => g.path_suffix))
      : null
    return own ?? candidates[0] ?? ''
  }, [config, candidates, profile])

  useEffect(() => { setInstance(seeded) }, [seeded])

  const applyProfile = () => {
    if (!profile || instance.trim() === '') return
    const title = config?.title?.trim() || instanceTitle(instance)
    setConfig(clusterFromProfile(profile, instance.trim(), title))
  }

  const setSlot = (update: (current: EngineClusterConfig) => EngineClusterConfig) =>
    setConfig((current) => (current ? update(current) : current))

  const setFuel = (update: (rail: ClusterFuelRail) => ClusterFuelRail) =>
    setSlot((c) => (c.fuel ? { ...c, fuel: update(c.fuel) } : c))

  const canSave =
    config !== null &&
    config.title.trim() !== '' &&
    config.ring.path.trim() !== '' &&
    config.corners.every((corner) => corner.rows.every((row) => row.path.trim() !== ''))
    && (config.telltales ?? []).every((telltale) => telltale.path.trim() !== '')
    && (config.fuel === undefined ||
        (config.fuel.bars.length > 0 &&
         config.fuel.bars.every((bar) => bar.level.path.trim() !== '' && bar.capacity.path.trim() !== '')))

  return (
    <Dialog open={widget !== null} onOpenChange={(open) => { if (!open) onCancel() }}>
      <DialogContent className="max-h-[85vh] max-w-2xl overflow-y-auto">
        <DialogHeader>
          <DialogTitle>Engine Cluster</DialogTitle>
          <DialogDescription>
            A ticked ring with the primary reading inside it and four corner cards. Pick an engine to
            fill it from a profile, then adjust any slot.
          </DialogDescription>
        </DialogHeader>

        <div className="grid gap-3">
          {/* An empty Profile list means "none installed". A failed fetch has
              to say so instead of borrowing that same empty look, or a broken
              backend reads as a bare cupboard. */}
          {profilesError && (
            <p className="rounded-md border border-destructive/40 bg-destructive/10 p-2 text-sm text-destructive" role="alert">
              Could not load engine profiles: {profilesError}
            </p>
          )}
          <div className="grid items-end gap-2 sm:grid-cols-[1fr_1fr_auto]">
            <Field>
              <FieldLabel htmlFor="cluster-profile">Profile</FieldLabel>
              <select id="cluster-profile" className="h-9 w-full rounded-md border bg-transparent px-3 text-sm"
                value={profile?.id ?? ''} onChange={(e) => setProfileID(e.target.value)}>
                {profiles.map((p) => <option key={p.id} value={p.id}>{p.name}</option>)}
              </select>
            </Field>
            <Field>
              <FieldLabel htmlFor="cluster-instance">Engine instance</FieldLabel>
              <Input id="cluster-instance" list="cluster-instance-options" value={instance}
                onChange={(e) => setInstance(e.target.value)} placeholder="propulsion.port" />
              <datalist id="cluster-instance-options">
                {candidates.map((c) => <option key={c} value={c} />)}
              </datalist>
            </Field>
            <Button variant="secondary" disabled={!profile || instance.trim() === ''} onClick={applyProfile}>
              Fill slots
            </Button>
          </div>
          <FieldDescription>
            Filling replaces every slot. Anything the profile does not cover — exhaust temperature, on
            a vessel that publishes it outside the engine node — stays blank for you to type.
          </FieldDescription>

          {config && (
            <>
              <div className="grid gap-3 sm:grid-cols-2">
                <Field>
                  <FieldLabel htmlFor="cluster-title">Title</FieldLabel>
                  <Input id="cluster-title" maxLength={GAUGE_GROUP_TITLE_MAX_LENGTH} value={config.title}
                    onChange={(e) => setSlot((c) => ({ ...c, title: e.target.value }))} placeholder="Port" />
                </Field>
                <Field>
                  <FieldLabel htmlFor="cluster-centre-icon">Centre icon</FieldLabel>
                  <select id="cluster-centre-icon" className="h-9 w-full rounded-md border bg-transparent px-3 text-sm"
                    value={config.centreIcon ?? ''}
                    onChange={(e) => setSlot((c) => ({ ...c, centreIcon: e.target.value === '' ? undefined : e.target.value }))}>
                    <option value="">Auto ({iconForSlot(config.centre)})</option>
                    {CLUSTER_ICON_NAMES.map((name) => <option key={name} value={name}>{name}</option>)}
                  </select>
                </Field>
              </div>

              <SlotSection title="Ring" idPrefix="cluster-ring" paths={paths} value={config.ring}
                onChange={(next) => setSlot((c) => ({ ...c, ring: next }))} />
              <SlotSection title="Centre" idPrefix="cluster-centre" paths={paths} value={config.centre}
                onChange={(next) => setSlot((c) => ({ ...c, centre: next }))} />

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
                        Auto{corner.rows[0] ? ` (${iconForSlot(corner.rows[0])})` : ''}
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
                      <GaugeFields value={row} paths={paths} idPrefix={`cluster-${cornerIndex}-${rowIndex}`}
                        onChange={(next) => setSlot((c) => ({
                          ...c,
                          corners: c.corners.map((x, i) => (i === cornerIndex
                            ? { ...x, rows: x.rows.map((r, j) => (j === rowIndex ? next : r)) }
                            : x)),
                        }))} />
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
                    <GaugeFields value={telltale} paths={paths} idPrefix={`cluster-telltale-${index}`}
                      onChange={(next) => setSlot((c) => ({
                        ...c,
                        telltales: (c.telltales ?? []).map((t, i) => (i === index ? next : t)),
                      }))} />
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
                  A bar per tank down one edge of the tile, with the side's total underneath.
                  Each tank needs two paths: the level, and the capacity that turns it into a
                  volume. Picking a <code>.currentLevel</code> path fills the capacity for you.
                </FieldDescription>

                {config.fuel === undefined ? (
                  <Button variant="ghost" className="w-fit"
                    onClick={() => setSlot((c) => ({
                      ...c,
                      // Left for a cluster titled Port, right otherwise, so a
                      // facing pair puts its rails outboard without being told.
                      fuel: {
                        side: /^p(ort)?\b/i.test(c.title.trim()) ? 'left' : 'right',
                        bars: [blankFuelBar('Fwd'), blankFuelBar('Aft')],
                      },
                    }))}>
                    <Plus className="size-3.5" /> Add fuel rail
                  </Button>
                ) : (
                  <>
                    <Field className="mb-2">
                      {/* "Edge", not "side": which side of the boat the tanks
                          are on is carried by their paths and the tile title. */}
                      <FieldLabel htmlFor="cluster-fuel-side">Tile edge</FieldLabel>
                      <select id="cluster-fuel-side"
                        className="h-9 rounded-md border bg-transparent px-3 text-sm"
                        value={config.fuel.side}
                        onChange={(e) => setFuel((f) => ({ ...f, side: e.target.value as ClusterFuelRail['side'] }))}>
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
                      <div key={index} className="mb-2 border-b border-border/60 pb-2 last:border-b-0">
                        <GaugeFields value={bar.level} paths={paths} idPrefix={`cluster-fuel-${index}`}
                          onChange={(next) => setFuel((f) => ({
                            ...f,
                            bars: f.bars.map((b, i) => (i === index
                              ? {
                                  ...b,
                                  level: pinQuantity(next, 'ratio'),
                                  // Only when the operator has not typed one:
                                  // overwriting a hand-picked path would be the
                                  // convenience undoing their work.
                                  capacity: b.capacity.path.trim() === ''
                                    ? { ...b.capacity, path: capacityPathFor(next.path.trim()) }
                                    : b.capacity,
                                }
                              : b)),
                          }))} />
                        <CapacityFields value={bar.capacity} paths={paths} index={index}
                          onChange={(next) => setFuel((f) => ({
                            ...f,
                            bars: f.bars.map((b, i) => (i === index ? { ...b, capacity: pinQuantity(next, 'volume') } : b)),
                          }))} />
                        <Button size="sm" variant="ghost" aria-label={`Remove fuel tank ${index + 1}`}
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
          <Button variant="ghost" onClick={onCancel}>Cancel</Button>
          <Button disabled={!canSave} onClick={() => config && onSave(config)}>Save</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

/**
 * The capacity slot's form: path, quantity, unit and nothing else.
 *
 * Not the full GaugeFields, because a tank's capacity is a constant: min, max,
 * zones and a history window are all meaningless on it, and a form offering
 * them invites a config the renderer will ignore. Not a `compact` flag on
 * GaugeFields either, since four dialogs share that component and a mode flag
 * there is the kind of thing that grows.
 */
function CapacityFields({ value, paths, index, onChange }: {
  value: GaugeWidgetConfig
  paths: { path: string }[]
  index: number
  onChange: (next: GaugeWidgetConfig) => void
}) {
  const quantity = quantityById(value.quantity)
  return (
    <div className="mt-2 grid grid-cols-1 gap-2 sm:grid-cols-3">
      <Field className="sm:col-span-1">
        <FieldLabel htmlFor={`cluster-fuel-cap-${index}`}>Capacity path</FieldLabel>
        <Input id={`cluster-fuel-cap-${index}`} value={value.path}
          list={`cluster-fuel-cap-list-${index}`}
          placeholder="tanks.fuel.5.capacity"
          onChange={(e) => onChange({ ...value, path: e.target.value })} />
        <datalist id={`cluster-fuel-cap-list-${index}`}>
          {paths.map((p) => <option key={p.path} value={p.path} />)}
        </datalist>
      </Field>
      <Field>
        <FieldLabel htmlFor={`cluster-fuel-capq-${index}`}>Quantity</FieldLabel>
        <select id={`cluster-fuel-capq-${index}`}
          className="h-9 rounded-md border bg-transparent px-3 text-sm"
          value={value.quantity}
          onChange={(e) => {
            const next = quantityById(e.target.value)
            onChange({ ...value, quantity: next.id, unit: next.units[0].id })
          }}>
          {QUANTITIES.map((q) => <option key={q.id} value={q.id}>{q.label}</option>)}
        </select>
      </Field>
      <Field>
        <FieldLabel htmlFor={`cluster-fuel-capu-${index}`}>Unit</FieldLabel>
        <select id={`cluster-fuel-capu-${index}`}
          className="h-9 rounded-md border bg-transparent px-3 text-sm"
          value={unitOption(value.quantity, value.unit).id}
          onChange={(e) => onChange({ ...value, unit: e.target.value })}>
          {quantity.units.map((u) => <option key={u.id} value={u.id}>{u.label}</option>)}
        </select>
      </Field>
    </div>
  )
}

function SlotSection({ title, idPrefix, value, onChange, paths }: {
  title: string
  idPrefix: string
  value: GaugeWidgetConfig
  onChange: (next: GaugeWidgetConfig) => void
  paths: ReturnType<typeof useSignalKPaths>['paths']
}) {
  return (
    <div className="rounded-md border border-border p-3">
      <p className="mb-2 text-[10px] font-medium uppercase tracking-wider text-muted-foreground">{title}</p>
      <GaugeFields value={value} onChange={onChange} paths={paths} idPrefix={idPrefix} />
    </div>
  )
}

/** "propulsion.port" -> "Port" */
function instanceTitle(instance: string): string {
  const last = instance.split('.').filter(Boolean).slice(-1)[0] ?? ''
  return last.charAt(0).toUpperCase() + last.slice(1)
}
