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
import { useSignalKPaths } from '@/hooks/use-signalk-paths'
import {
  GAUGE_GROUP_TITLE_MAX_LENGTH,
  LAMP_LABEL_MAX_LENGTH,
  LAMP_STRIP_MAX_LAMPS,
  type DashboardLayoutItem,
  type LampStripWidgetConfig,
} from '@/lib/dashboard-widgets'
import { suggestRibbonLamps } from '@/lib/ribbon-defaults'

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
  const [config, setConfig] = useState<LampStripWidgetConfig>(() => defaultStripConfig(widget?.id ?? ''))

  // Re-seed per instance, so editing one strip never shows another's settings.
  useEffect(() => {
    setConfig(widget?.lamps ? structuredClone(widget.lamps) : defaultStripConfig(widget?.id ?? ''))
  }, [widget?.id, widget?.lamps])

  // A fresh ribbon (ADR 0085): once the vessel's published paths have loaded,
  // replace the single blank lamp with a suggested set resolved against them.
  // Never runs for an existing ribbon config or a per-page widget, and never
  // overwrites lamps the operator has already started editing.
  useEffect(() => {
    if (widget?.id !== RIBBON_DIALOG_ID || widget?.lamps || paths.length === 0) return
    const suggested = suggestRibbonLamps(paths)
    if (suggested.length === 0) return
    setConfig((current) => ({ ...current, lamps: suggested }))
  }, [widget?.id, widget?.lamps, paths])

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

  // Every suggested lamp not already on the strip by path, for the "Suggest
  // lamps" button — offered only for the ribbon; empty (and so disabled)
  // until paths have loaded, since suggestRibbonLamps([]) is always [].
  const missingSuggestions = suggestRibbonLamps(paths).filter(
    (suggested) => !config.lamps.some((lamp) => lamp.path === suggested.path),
  )

  // A strip with neither lamps nor the rollup renders nothing at all, which the
  // backend rejects — so the dialog will not offer to save one.
  const canSave =
    config.title.trim() !== '' &&
    config.lamps.length <= LAMP_STRIP_MAX_LAMPS &&
    config.lamps.every((lamp) => lamp.path.trim() !== '') &&
    (config.lamps.length > 0 || config.showCheck === true)

  return (
    <Dialog open={widget !== null} onOpenChange={(open) => { if (!open) onCancel() }}>
      <DialogContent className="max-h-[85vh] max-w-2xl overflow-y-auto">
        <DialogHeader>
          <DialogTitle>Indicators</DialogTitle>
          <DialogDescription>
            A row of status lamps. Duplicate the tile onto your other pages so the same strip reads
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
            CHK takes the colour of the worst active alarm, including the ones your gauge zones raise.
          </FieldDescription>

          <datalist id="lamp-path-options">
            {paths.map((p) => <option key={p.path} value={p.path} />)}
          </datalist>

          {config.lamps.map((lamp, index) => (
            <div key={index} className="grid items-end gap-2 sm:grid-cols-[2fr_1fr_auto_auto]">
              <Field>
                <FieldLabel htmlFor={`lamp-${index}-path`}>Path</FieldLabel>
                <Input
                  id={`lamp-${index}-path`}
                  list="lamp-path-options"
                  value={lamp.path}
                  onChange={(e) => setLamp(index, { path: e.target.value, label: lamp.label || e.target.value.split('.').slice(-1)[0] })}
                  placeholder="electrical.generator.state"
                />
              </Field>
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
              <label className="flex h-9 items-center gap-1 text-[11px] text-muted-foreground">
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
