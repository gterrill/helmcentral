import { ChevronDown, ChevronUp, Plus, Star, Trash2 } from 'lucide-react'
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
import { EquipmentPicker } from '@/components/equipment-picker'
import { Field, FieldDescription, FieldLabel } from '@/components/ui/field'
import { GaugeFields, defaultGaugeConfig, slotFromProfileGauge } from '@/components/gauge-fields'
import { Input } from '@/components/ui/input'
import { useEquipmentTileContext } from '@/hooks/use-equipment-tile-context'
import { useSignalKPaths } from '@/hooks/use-signalk-paths'
import { useSlotZoneEditing } from '@/hooks/use-slot-zone-editing'
import { ZoneConflicts } from '@/components/zone-conflicts'
import { findItem } from '@/lib/equipment-tiles'
import { SOURCE_KINDS, SOURCE_KIND_NOUNS, type SourceKind } from '@/lib/vessel-settings'
import {
  GAUGE_GROUP_MAX_COLUMNS,
  GAUGE_GROUP_MAX_GAUGES,
  GAUGE_GROUP_TITLE_MAX_LENGTH,
  type DashboardLayoutItem,
  type GaugeGroupWidgetConfig,
  type GaugeWidgetConfig,
} from '@/lib/dashboard-widgets'

function defaultGroupConfig(): GaugeGroupWidgetConfig {
  return { title: '', gauges: [defaultGaugeConfig()] }
}

interface GaugeGroupConfigDialogProps {
  widget: DashboardLayoutItem | null
  onCancel: () => void
  onSave: (config: GaugeGroupWidgetConfig) => void
  onDuplicate?: (config: GaugeGroupWidgetConfig) => void
}

/**
 * Builds a gauge group (ADR 0049): a named cluster of gauges in one tile.
 *
 * A group shows either a piece of equipment (ADR 0185) or a hand-picked list of
 * paths. A group of equipment stores the item and a reading per gauge, takes
 * its zones from the item's profile, and is pointed at the other engine by
 * choosing the other item: duplicate "Port", pick the starboard engine.
 */
export function GaugeGroupConfigDialog({ widget, onCancel, onSave, onDuplicate }: GaugeGroupConfigDialogProps) {
  const { paths } = useSignalKPaths(widget !== null)
  const ctx = useEquipmentTileContext(widget !== null)
  const [config, setConfig] = useState<GaugeGroupWidgetConfig>(defaultGroupConfig)
  const [saving, setSaving] = useState(false)
  const [saveError, setSaveError] = useState<string | null>(null)

  // Re-seed per instance, so editing one group never shows another's settings.
  useEffect(() => {
    setConfig(widget?.gaugeGroup ? structuredClone(widget.gaugeGroup) : defaultGroupConfig())
    setSaveError(null)
  }, [widget?.id, widget?.gaugeGroup])

  const kind = (config.equipmentKind ?? '') as SourceKind | ''
  const item = findItem(ctx, config.equipmentId)
  const zoneEditing = useSlotZoneEditing(ctx, item, config.gauges, paths)
  const { profile } = zoneEditing

  const setKind = (next: SourceKind | '') =>
    setConfig((current) => ({
      title: current.title,
      columns: current.columns,
      equipmentKind: next === '' ? undefined : next,
      equipmentId: undefined,
      // The gauges of the other kind of group would not fit this one.
      gauges: [next === '' ? defaultGaugeConfig() : { ...defaultGaugeConfig(), suffix: '' }],
    }))

  const fillFromProfile = () => {
    if (!profile) return
    setConfig((current) => {
      // Zones a gauge still holds are not the profile's to replace: carry them over.
      const held = new Map(current.gauges.filter((g) => (g.zones?.length ?? 0) > 0).map((g) => [g.suffix, g]))
      const gauges = (profile.gauges ?? []).map((g) => {
        const slot = slotFromProfileGauge({ ...defaultGaugeConfig(), suffix: g.path_suffix }, g)
        const old = held.get(g.path_suffix)
        // The zones are in the old gauge's unit and scale, so those come with them.
        return old ? { ...slot, zones: old.zones, quantity: old.quantity, unit: old.unit, min: old.min, max: old.max } : slot
      })
      const hero = (profile.gauges ?? []).findIndex((g) => g.hero === true)
      return { ...current, gauges, hero: hero === -1 ? undefined : hero }
    })
  }

  const setGauge = (index: number, next: GaugeWidgetConfig) =>
    setConfig((current) => ({
      ...current,
      gauges: current.gauges.map((g, i) => (i === index ? next : g)),
    }))

  const addGauge = () =>
    setConfig((current) => ({ ...current, gauges: [...current.gauges, defaultGaugeConfig()] }))

  const removeGauge = (index: number) =>
    setConfig((current) => {
      const gauges = current.gauges.filter((_, i) => i !== index)
      let hero = current.hero
      if (hero !== undefined) {
        if (hero === index) hero = undefined
        else if (hero > index) hero = hero - 1
      }
      return { ...current, gauges, hero }
    })

  const moveGauge = (index: number, delta: number) =>
    setConfig((current) => {
      const target = index + delta
      if (target < 0 || target >= current.gauges.length) return current
      const gauges = [...current.gauges]
      ;[gauges[index], gauges[target]] = [gauges[target], gauges[index]]
      let hero = current.hero
      if (hero === index) hero = target
      else if (hero === target) hero = index
      return { ...current, gauges, hero }
    })

  const canSave =
    (kind !== '' || config.title.trim() !== '') &&
    config.gauges.length > 0 &&
    config.gauges.length <= GAUGE_GROUP_MAX_GAUGES &&
    (config.hero === undefined || (config.hero >= 0 && config.hero < config.gauges.length)) &&
    (kind === ''
      ? config.gauges.every((g) => g.path.trim() !== '')
      : (config.equipmentId ?? '') !== '' && config.gauges.every((g) => (g.suffix ?? '').trim() !== '') && zoneEditing.unresolved === 0)

  /** The group with the zones that now live on the profile taken off its gauges. */
  const settled = (cleared: Set<number>): GaugeGroupWidgetConfig => ({
    ...config,
    gauges: config.gauges.map((g, i) => (cleared.has(i) ? { ...g, zones: undefined } : g)),
  })

  const save = async () => {
    // Nothing to write to a profile: save the tile at once, in the same tick.
    if (!zoneEditing.dirty) {
      onSave(config)
      return
    }
    setSaving(true)
    setSaveError(null)
    try {
      onSave(settled(await zoneEditing.commit()))
    } catch (err) {
      setSaveError(err instanceof Error ? err.message : 'Could not save the profile zones')
    } finally {
      setSaving(false)
    }
  }

  return (
    <Dialog open={widget !== null} onOpenChange={(open) => { if (!open) onCancel() }}>
      <DialogContent className="max-h-[85vh] max-w-2xl overflow-y-auto">
        <DialogHeader>
          <DialogTitle>Gauge Group</DialogTitle>
          <DialogDescription>
            A named cluster of gauges in one tile: the readings of one piece of equipment, or any paths you pick.
          </DialogDescription>
        </DialogHeader>

        <div className="grid gap-4">
          <div className="grid gap-3 sm:grid-cols-[1fr_auto_auto]">
            <Field>
              <FieldLabel htmlFor="gauge-group-title">Title</FieldLabel>
              <Input
                id="gauge-group-title"
                maxLength={GAUGE_GROUP_TITLE_MAX_LENGTH}
                value={config.title}
                onChange={(e) => setConfig((current) => ({ ...current, title: e.target.value }))}
                placeholder={kind === '' ? 'Port' : (item?.name ?? 'Equipment name')}
              />
              {kind !== '' && <FieldDescription>Blank shows the equipment's name.</FieldDescription>}
            </Field>
            <Field>
              <FieldLabel htmlFor="gauge-group-columns">Columns</FieldLabel>
              <select
                id="gauge-group-columns"
                className="h-9 w-full rounded-md border bg-transparent px-3 text-sm"
                value={config.columns ?? ''}
                onChange={(e) =>
                  setConfig((current) => ({
                    ...current,
                    columns: e.target.value === '' ? undefined : Number(e.target.value),
                  }))
                }
              >
                <option value="">Auto</option>
                {Array.from({ length: GAUGE_GROUP_MAX_COLUMNS }, (_, i) => i + 1).map((n) => (
                  <option key={n} value={n}>{n}</option>
                ))}
              </select>
            </Field>
          </div>

          <div className="grid gap-3 sm:grid-cols-[auto_1fr]">
            <Field>
              <FieldLabel htmlFor="gauge-group-kind">Shows</FieldLabel>
              <select
                id="gauge-group-kind"
                className="h-9 w-full rounded-md border bg-transparent px-3 text-sm"
                value={kind}
                onChange={(e) => setKind(e.target.value as SourceKind | '')}
              >
                <option value="">Paths I pick</option>
                {SOURCE_KINDS.filter((k) => k !== 'tank').map((k) => (
                  <option key={k} value={k}>A {SOURCE_KIND_NOUNS[k]}</option>
                ))}
              </select>
            </Field>
            {kind !== '' && (
              <EquipmentPicker
                idPrefix="gauge-group"
                label="Equipment"
                kind={kind}
                value={config.equipmentId ?? ''}
                // Choosing equipment ends the source the tile remembered.
                onChange={(id) => setConfig((current) => ({ ...current, equipmentId: id, source: id === '' ? current.source : undefined }))}
                ctx={ctx}
              />
            )}
          </div>

          {kind !== '' && (
            <ZoneConflicts
              plan={zoneEditing.plan} choices={zoneEditing.choices} onChoose={zoneEditing.setChoice}
              heldWithoutItem={zoneEditing.heldWithoutItem} idPrefix="gauge-group"
            />
          )}

          {kind !== '' && (
            <div className="flex flex-wrap items-center gap-2">
              <Button variant="secondary" className="w-fit" disabled={!profile} onClick={fillFromProfile}>
                Fill gauges from profile
              </Button>
              <FieldDescription>
                {item && !profile
                  ? 'This equipment has no profile, so there is nothing to fill from and no zones.'
                  : 'Replaces the gauges below with the profile\'s. Zones come from the profile either way.'}
              </FieldDescription>
            </div>
          )}

          {config.gauges.map((gauge, index) => (
            // Keyed by index: members have no id of their own, and duplicate
            // paths within a group are allowed.
            <div key={index} className="rounded-md border border-border p-3">
              <div className="mb-2 flex items-center gap-1">
                <span className="flex-1 truncate text-[10px] font-medium uppercase tracking-wider text-muted-foreground">
                  Gauge {index + 1}
                </span>
                <Button
                  size="sm"
                  variant="ghost"
                  aria-label={config.hero === index ? `Unset gauge ${index + 1} hero` : `Set gauge ${index + 1} as hero`}
                  aria-pressed={config.hero === index}
                  onClick={() =>
                    setConfig((current) => ({
                      ...current,
                      hero: current.hero === index ? undefined : index,
                    }))
                  }
                >
                  <Star className={`size-3.5 ${config.hero === index ? 'fill-current text-primary' : 'text-muted-foreground'}`} />
                </Button>
                <Button size="sm" variant="ghost" aria-label={`Move gauge ${index + 1} up`}
                  disabled={index === 0} onClick={() => moveGauge(index, -1)}>
                  <ChevronUp className="size-3.5" />
                </Button>
                <Button size="sm" variant="ghost" aria-label={`Move gauge ${index + 1} down`}
                  disabled={index === config.gauges.length - 1} onClick={() => moveGauge(index, 1)}>
                  <ChevronDown className="size-3.5" />
                </Button>
                <Button size="sm" variant="ghost" aria-label={`Remove gauge ${index + 1}`}
                  onClick={() => removeGauge(index)}>
                  <Trash2 className="size-3.5" />
                </Button>
              </div>
              <GaugeFields
                value={gauge}
                onChange={(next) => setGauge(index, next)}
                paths={paths}
                idPrefix={`gauge-group-${index}`}
                equipment={kind === '' ? undefined : zoneEditing.modeFor(gauge)}
              />
            </div>
          ))}

          <Button
            variant="ghost"
            className="w-fit"
            disabled={config.gauges.length >= GAUGE_GROUP_MAX_GAUGES}
            onClick={addGauge}
          >
            <Plus className="size-3.5" />
            Add gauge
          </Button>
        </div>

        <DialogFooter className="mt-2 flex-col items-stretch gap-3 sm:flex-row sm:items-center sm:justify-between">
          <div className="flex min-w-0 flex-1 justify-start">
            <Button
              variant="outline"
              className="w-fit border-dashed border-primary/40 bg-primary/5 text-primary hover:bg-primary/10"
              onClick={() => {
                if (!zoneEditing.dirty) {
                  onDuplicate?.(config)
                  onCancel()
                  return
                }
                void (async () => {
                  try {
                    onDuplicate?.(settled(await zoneEditing.commit()))
                  } catch (err) {
                    setSaveError(err instanceof Error ? err.message : 'Could not save the profile zones')
                    return
                  }
                  onCancel()
                })()
              }}
              title="Duplicate this tile"
              aria-label="Duplicate this tile"
            >
              Duplicate
            </Button>
          </div>

          <div className="flex shrink-0 items-center justify-end gap-2">
            {saveError && <p role="alert" className="mr-2 text-sm text-destructive">{saveError}</p>}
            <Button variant="ghost" onClick={onCancel}>Cancel</Button>
            <Button disabled={!canSave || saving} onClick={() => { void save() }}>Save</Button>
          </div>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
