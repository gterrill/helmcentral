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
  CLUSTER_MAX_TELLTALES,
  type DashboardLayoutItem,
  type EngineClusterConfig,
  type GaugeWidgetConfig,
} from '@/lib/dashboard-widgets'
import { CLUSTER_ICON_NAMES, iconForSlot } from '@/lib/cluster-icons'
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
    skin: 'instrument',
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
  const { profiles } = useEngineProfiles(widget !== null)
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

  const canSave =
    config !== null &&
    config.title.trim() !== '' &&
    config.ring.path.trim() !== '' &&
    config.corners.every((corner) => corner.rows.every((row) => row.path.trim() !== ''))
    && (config.telltales ?? []).every((telltale) => telltale.path.trim() !== '')

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
              <div className="grid gap-3 sm:grid-cols-[1fr_auto_auto]">
                <Field>
                  <FieldLabel htmlFor="cluster-title">Title</FieldLabel>
                  <Input id="cluster-title" value={config.title}
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
                <Field>
                  <FieldLabel htmlFor="cluster-skin">Skin</FieldLabel>
                  <select id="cluster-skin" className="h-9 w-full rounded-md border bg-transparent px-3 text-sm"
                    value={config.skin ?? 'default'}
                    onChange={(e) => setSlot((c) => ({ ...c, skin: e.target.value as 'default' | 'instrument' }))}>
                    <option value="instrument">Instrument (always dark)</option>
                    <option value="default">Follow the app theme</option>
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
