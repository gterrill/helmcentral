import { ChevronDown, ChevronUp, Plus, Sparkles, Trash2 } from 'lucide-react'
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
import { Input } from '@/components/ui/input'
import { useEquipmentTileContext } from '@/hooks/use-equipment-tile-context'
import { useSignalKPaths } from '@/hooks/use-signalk-paths'
import {
  GAUGE_GROUP_TITLE_MAX_LENGTH,
  LAMP_GROUP_MAX_LENGTH,
  LAMP_LABEL_MAX_LENGTH,
  LAMP_STRIP_MAX_LAMPS,
  type DashboardLayoutItem,
  type LampStripWidgetConfig,
} from '@/lib/dashboard-widgets'
import { LAMP_ICON_NAMES } from '@/lib/lamp-icons'
import { LAMP_READINGS, suggestRibbonLamps } from '@/lib/ribbon-defaults'

/**
 * A `Pick` of `DashboardLayoutItem` rather than the full type, so this dialog
 * also serves the vessel-level ribbon (ADR 0082), which has no page, no
 * geometry and no real widget id — App.tsx passes it a synthetic
 * `{ id: 'ribbon', lamps: ribbon ?? undefined }` — while an ordinary
 * `DashboardLayoutItem` for a per-page `lamps:` widget still satisfies this
 * shape unchanged.
 */
export interface LampStripDialogWidget extends Pick<DashboardLayoutItem, 'lamps'> {
  id: string
}

// The ribbon's synthetic id, distinguishing it from a real lamps: widget id
// for the one thing that differs between them: a fresh draft's default title.
const RIBBON_DIALOG_ID = 'ribbon'

function defaultStripConfig(id: string): LampStripWidgetConfig {
  return {
    title: id === RIBBON_DIALOG_ID ? 'Indicators' : 'Status',
    lamps: [{ path: '', label: '' }],
    showCheck: true,
  }
}

interface LampStripConfigDialogProps {
  widget: LampStripDialogWidget | null
  onCancel: () => void
  onSave: (config: LampStripWidgetConfig) => void
  /** Present only for the ribbon: offers removing it entirely rather than just editing its contents. */
  onRemove?: () => void
}

export function LampStripConfigDialog({ widget, onCancel, onSave, onRemove }: LampStripConfigDialogProps) {
  const { paths } = useSignalKPaths(widget !== null)
  const ctx = useEquipmentTileContext(widget !== null)
  const mappedItems = ctx.items.filter((i) => i.signalk_source !== '')
  // The readings a lamp for an item can watch: the signal a lamp of that kind
  // usually watches, plus whatever the boat reports under the item's source,
  // plus the one already chosen so the field never goes blank.
  const lampReadings = (itemId: string | undefined, current = ''): string[] => {
    const picked = mappedItems.find((i) => i.id === itemId)
    if (!picked) return []
    const prefix = `${picked.signalk_source}.`
    const reported = paths.filter((p) => p.path.startsWith(prefix)).map((p) => p.path.slice(prefix.length))
    const known = picked.source_kind ? (LAMP_READINGS[picked.source_kind] ?? []) : []
    return [...new Set([...known, ...reported, ...(current ? [current] : [])])].sort()
  }
  const [config, setConfig] = useState<LampStripWidgetConfig>(() => defaultStripConfig(widget?.id ?? ''))

  // Re-seed per instance, so editing one strip never shows another's settings.
  useEffect(() => {
    setConfig(widget?.lamps ? structuredClone(widget.lamps) : defaultStripConfig(widget?.id ?? ''))
  }, [widget?.id, widget?.lamps])

  // A fresh ribbon (ADR 0085): once the equipment items have loaded, replace
  // the single blank lamp with a suggested set built from the mapped ones.
  // Never runs for an existing ribbon config or a per-page widget, and never
  // overwrites lamps the operator has already started editing.
  useEffect(() => {
    if (widget?.id !== RIBBON_DIALOG_ID || widget?.lamps || mappedItems.length === 0) return
    const suggested = suggestRibbonLamps(ctx.items)
    if (suggested.length === 0) return
    setConfig((current) => (current.lamps.every((l) => l.path === '' && !l.equipmentId) ? { ...current, lamps: suggested } : current))
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [widget?.id, widget?.lamps, ctx.items])

  const setLamp = (index: number, patch: Partial<LampStripWidgetConfig['lamps'][number]>) =>
    setConfig((current) => ({
      ...current,
      lamps: current.lamps.map((lamp, i) => (i === index ? { ...lamp, ...patch } : lamp)),
    }))

  const move = (index: number, delta: number) =>
    setConfig((current) => {
      const target = index + delta
      if (target < 0 || target >= current.lamps.length) return current
      const lamps = [...current.lamps]
      ;[lamps[index], lamps[target]] = [lamps[target], lamps[index]]
      return { ...current, lamps }
    })

  // Every suggested lamp not already on the strip for the same item and
  // reading, for the "Suggest lamps" button — offered only for the ribbon;
  // empty (and so disabled) until the items have loaded.
  const missingSuggestions = suggestRibbonLamps(ctx.items).filter(
    (suggested) => !config.lamps.some((lamp) => lamp.equipmentId === suggested.equipmentId && lamp.suffix === suggested.suffix),
  )

  // A strip with neither lamps nor the rollup renders nothing at all, which the
  // backend rejects — so the dialog will not offer to save one.
  const canSave =
    config.title.trim() !== '' &&
    config.lamps.length <= LAMP_STRIP_MAX_LAMPS &&
    config.lamps.every((lamp) => (lamp.equipmentId ? (lamp.suffix ?? '').trim() !== '' : lamp.path.trim() !== '')) &&
    (config.lamps.length > 0 || config.showCheck === true)

  return (
    <Dialog open={widget !== null} onOpenChange={(open) => { if (!open) onCancel() }}>
      <DialogContent className="max-h-[85vh] max-w-2xl overflow-y-auto">
        <DialogHeader>
          <DialogTitle>Indicators</DialogTitle>
          <DialogDescription>
            A row of labelled status cells. Duplicate the tile onto your other pages so the same strip reads
            the same everywhere.
          </DialogDescription>
        </DialogHeader>

        <div className="grid gap-3">
          <Field>
            <FieldLabel htmlFor="lamp-strip-title">Title</FieldLabel>
            <Input
              id="lamp-strip-title"
              maxLength={GAUGE_GROUP_TITLE_MAX_LENGTH}
              value={config.title}
              onChange={(e) => setConfig((current) => ({ ...current, title: e.target.value }))}
            />
          </Field>

          <label className="flex items-center gap-2 text-sm">
            <input
              type="checkbox"
              checked={config.showCheck ?? false}
              onChange={(e) => setConfig((current) => ({ ...current, showCheck: e.target.checked }))}
            />
            Show the CHK indicator
          </label>
          <FieldDescription>
            CHK takes the colour of the worst active alarm, including the ones your gauge zones raise. A cell turns amber or red while an active alarm is on its path.
          </FieldDescription>

          <datalist id="lamp-path-options">
            {paths.map((p) => <option key={p.path} value={p.path} />)}
          </datalist>

          {config.lamps.map((lamp, index) => (
            <div key={index} className="grid items-end gap-2 sm:grid-cols-[2fr_1fr_1fr_1fr_auto_auto]">
              <div className="grid gap-1">
                <div className="grid grid-cols-[auto_1fr] items-end gap-2">
                  <Field>
                    <FieldLabel htmlFor={`lamp-${index}-kind`}>Reads</FieldLabel>
                    <select
                      id={`lamp-${index}-kind`}
                      className="h-9 rounded-md border border-input bg-background px-2 text-sm"
                      value={lamp.equipmentId === undefined ? 'path' : 'equipment'}
                      onChange={(e) =>
                        setLamp(index, e.target.value === 'path'
                          ? { equipmentId: undefined, suffix: undefined }
                          : { equipmentId: '', suffix: '', path: '' })}
                    >
                      <option value="path">A path</option>
                      <option value="equipment">Equipment</option>
                    </select>
                  </Field>
                  {lamp.equipmentId === undefined ? (
                    <Field>
                      <FieldLabel htmlFor={`lamp-${index}-path`}>Path</FieldLabel>
                      <Input
                        id={`lamp-${index}-path`}
                        list="lamp-path-options"
                        value={lamp.path}
                        onChange={(e) => setLamp(index, { path: e.target.value, label: lamp.label || e.target.value.split('.').slice(-1)[0] })}
                        placeholder="electrical.switches.bilge"
                      />
                    </Field>
                  ) : (
                    <Field>
                      <FieldLabel htmlFor={`lamp-${index}-item`}>Equipment</FieldLabel>
                      <select
                        id={`lamp-${index}-item`}
                        className="h-9 w-full rounded-md border border-input bg-background px-2 text-sm"
                        value={lamp.equipmentId}
                        onChange={(e) => {
                          const picked = mappedItems.find((i) => i.id === e.target.value)
                          const reading = picked?.source_kind ? LAMP_READINGS[picked.source_kind]?.[0] : undefined
                          setLamp(index, {
                            equipmentId: e.target.value,
                            ...(lamp.suffix ? {} : { suffix: reading ?? '' }),
                            label: lamp.label || (picked?.name ?? '').slice(0, LAMP_LABEL_MAX_LENGTH),
                          })
                        }}
                      >
                        <option value="">Pick equipment…</option>
                        {mappedItems.map((i) => <option key={i.id} value={i.id}>{i.name}</option>)}
                      </select>
                    </Field>
                  )}
                </div>
                {lamp.equipmentId !== undefined && (
                  <Field>
                    <FieldLabel htmlFor={`lamp-${index}-suffix`}>Reading</FieldLabel>
                    <select
                      id={`lamp-${index}-suffix`}
                      className="h-9 w-full rounded-md border border-input bg-background px-2 text-sm"
                      value={lamp.suffix ?? ''}
                      onChange={(e) => setLamp(index, { suffix: e.target.value })}
                    >
                      <option value="">Pick a reading…</option>
                      {lampReadings(lamp.equipmentId, lamp.suffix ?? '').map((suffix) => <option key={suffix} value={suffix}>{suffix}</option>)}
                    </select>
                  </Field>
                )}
              </div>
              <Field>
                <FieldLabel htmlFor={`lamp-${index}-label`}>Label</FieldLabel>
                <Input
                  id={`lamp-${index}-label`}
                  maxLength={LAMP_LABEL_MAX_LENGTH}
                  value={lamp.label}
                  onChange={(e) => setLamp(index, { label: e.target.value })}
                  placeholder="GEN"
                />
              </Field>
              <Field>
                <FieldLabel htmlFor={`lamp-${index}-group`}>Group</FieldLabel>
                <Input
                  id={`lamp-${index}-group`}
                  maxLength={LAMP_GROUP_MAX_LENGTH}
                  value={lamp.group ?? ''}
                  onChange={(e) => setLamp(index, { group: e.target.value || undefined })}
                  placeholder="Power"
                />
              </Field>
              <Field>
                <FieldLabel htmlFor={`lamp-${index}-icon`}>Icon</FieldLabel>
                <select
                  id={`lamp-${index}-icon`}
                  className="h-9 w-full rounded-md border border-input bg-background px-2 text-sm"
                  value={lamp.icon ?? ''}
                  onChange={(e) => setLamp(index, { icon: e.target.value || undefined })}
                >
                  <option value="">Automatic</option>
                  {LAMP_ICON_NAMES.map((name) => <option key={name} value={name}>{name}</option>)}
                </select>
              </Field>
              <label className="flex h-9 items-center gap-1 text-xs text-muted-foreground">
                <input
                  type="checkbox"
                  aria-label={`Lamp ${index + 1} lights when off`}
                  checked={lamp.invert ?? false}
                  onChange={(e) => setLamp(index, { invert: e.target.checked })}
                />
                Lit when off
              </label>
              <div className="flex gap-1">
                <Button size="sm" variant="ghost" aria-label={`Move lamp ${index + 1} up`} disabled={index === 0} onClick={() => move(index, -1)}>
                  <ChevronUp className="size-3.5" />
                </Button>
                <Button size="sm" variant="ghost" aria-label={`Move lamp ${index + 1} down`} disabled={index === config.lamps.length - 1} onClick={() => move(index, 1)}>
                  <ChevronDown className="size-3.5" />
                </Button>
                <Button size="sm" variant="ghost" aria-label={`Remove lamp ${index + 1}`}
                  onClick={() => setConfig((current) => ({ ...current, lamps: current.lamps.filter((_, i) => i !== index) }))}>
                  <Trash2 className="size-3.5" />
                </Button>
              </div>
            </div>
          ))}

          <div className="flex flex-wrap gap-2">
            <Button
              variant="ghost"
              className="w-fit"
              disabled={config.lamps.length >= LAMP_STRIP_MAX_LAMPS}
              onClick={() => setConfig((current) => ({ ...current, lamps: [...current.lamps, { path: '', label: '' }] }))}
            >
              <Plus className="size-3.5" />
              Add lamp
            </Button>
            {widget?.id === RIBBON_DIALOG_ID && (
              <Button
                variant="outline"
                className="w-fit"
                disabled={missingSuggestions.length === 0}
                onClick={() => setConfig((current) => ({ ...current, lamps: [...current.lamps, ...missingSuggestions] }))}
              >
                <Sparkles className="size-3.5" />
                Suggest lamps
              </Button>
            )}
          </div>
        </div>

        <DialogFooter>
          {onRemove && (
            <Button variant="outline" onClick={onRemove}>Remove ribbon</Button>
          )}
          <Button variant="ghost" onClick={onCancel}>Cancel</Button>
          <Button disabled={!canSave} onClick={() => onSave(config)}>Save</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
