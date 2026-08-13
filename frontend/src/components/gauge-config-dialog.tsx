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
import { Input } from '@/components/ui/input'
import { useSignalKPaths } from '@/hooks/use-signalk-paths'
import type { DashboardLayoutItem, GaugeDisplay, GaugeWidgetConfig } from '@/lib/dashboard-widgets'
import { QUANTITIES, quantityById, quantityForSIUnit } from '@/lib/quantities'

const DISPLAYS: { id: GaugeDisplay; label: string }[] = [
  { id: 'numeric', label: 'Numeric' },
  { id: 'radial', label: 'Dial' },
  { id: 'bar', label: 'Bar' },
  { id: 'lamp', label: 'Indicator' },
]

function defaultConfig(): GaugeWidgetConfig {
  return { path: '', label: '', display: 'numeric', quantity: 'raw', unit: 'raw' }
}

interface GaugeConfigDialogProps {
  widget: DashboardLayoutItem | null
  onCancel: () => void
  onSave: (config: GaugeWidgetConfig) => void
}

export function GaugeConfigDialog({ widget, onCancel, onSave }: GaugeConfigDialogProps) {
  const { paths } = useSignalKPaths(widget !== null)
  const [config, setConfig] = useState<GaugeWidgetConfig>(defaultConfig)

  // Re-seed per instance, so editing one gauge never shows another's settings.
  useEffect(() => {
    setConfig(widget?.gauge ? { ...widget.gauge } : defaultConfig())
  }, [widget?.id, widget?.gauge])

  const quantity = quantityById(config.quantity)
  const known = useMemo(() => new Map(paths.map((p) => [p.path, p])), [paths])

  const set = <K extends keyof GaugeWidgetConfig>(key: K, value: GaugeWidgetConfig[K]) =>
    setConfig((current) => ({ ...current, [key]: value }))

  /**
   * Picking a path preselects the quantity from SignalK's own meta.units, so
   * the operator does not have to know that oil pressure arrives in pascals.
   */
  const choosePath = (path: string) => {
    const match = known.get(path)
    const inferred = quantityForSIUnit(match?.units)
    setConfig((current) => ({
      ...current,
      path,
      quantity: inferred.id,
      unit: inferred.units[0].id,
      label: current.label || path.split('.').slice(-1)[0],
    }))
  }

  const canSave = config.path.trim() !== ''

  return (
    <Dialog open={widget !== null} onOpenChange={(open) => { if (!open) onCancel() }}>
      <DialogContent className="max-w-lg">
        <DialogHeader>
          <DialogTitle>Gauge</DialogTitle>
          <DialogDescription>Show any value the SignalK server publishes.</DialogDescription>
        </DialogHeader>

        <div className="grid gap-3">
          <Field>
            <FieldLabel htmlFor="gauge-path">SignalK path</FieldLabel>
            <Input
              id="gauge-path"
              list="gauge-path-options"
              value={config.path}
              onChange={(e) => choosePath(e.target.value)}
              placeholder="propulsion.port.oilPressure"
            />
            {/*
              Free text, not a closed dropdown: the snapshot only holds paths seen
              since the stream connected, so with the engines off propulsion.* is
              absent — and that is exactly when someone sets an engine gauge up.
            */}
            <datalist id="gauge-path-options">
              {paths.map((p) => <option key={p.path} value={p.path} />)}
            </datalist>
            <FieldDescription>
              {paths.length > 0
                ? `${paths.length} paths currently published. Not listed? Type it anyway — a sensor that is off has no path until it reports.`
                : 'No paths seen yet. You can still type one.'}
            </FieldDescription>
          </Field>

          <Field>
            <FieldLabel htmlFor="gauge-label">Label</FieldLabel>
            <Input id="gauge-label" value={config.label} onChange={(e) => set('label', e.target.value)} />
          </Field>

          <div className="grid gap-3 sm:grid-cols-3">
            <Field>
              <FieldLabel htmlFor="gauge-display">Display</FieldLabel>
              <select id="gauge-display" className="h-9 w-full rounded-md border bg-transparent px-3 text-sm"
                value={config.display} onChange={(e) => set('display', e.target.value as GaugeDisplay)}>
                {DISPLAYS.map((d) => <option key={d.id} value={d.id}>{d.label}</option>)}
              </select>
            </Field>
            <Field>
              <FieldLabel htmlFor="gauge-quantity">Quantity</FieldLabel>
              <select id="gauge-quantity" className="h-9 w-full rounded-md border bg-transparent px-3 text-sm"
                value={config.quantity}
                onChange={(e) => {
                  const next = quantityById(e.target.value)
                  setConfig((current) => ({ ...current, quantity: next.id, unit: next.units[0].id }))
                }}>
                {QUANTITIES.map((q) => <option key={q.id} value={q.id}>{q.label}</option>)}
              </select>
            </Field>
            <Field>
              <FieldLabel htmlFor="gauge-unit">Unit</FieldLabel>
              <select id="gauge-unit" className="h-9 w-full rounded-md border bg-transparent px-3 text-sm"
                value={config.unit} onChange={(e) => set('unit', e.target.value)}>
                {quantity.units.map((u) => <option key={u.id} value={u.id}>{u.label || u.id}</option>)}
              </select>
            </Field>
          </div>

          {(config.display === 'radial' || config.display === 'bar') && (
            <div className="grid gap-3 sm:grid-cols-2">
              <Field>
                <FieldLabel htmlFor="gauge-min">Scale minimum</FieldLabel>
                <Input id="gauge-min" type="number" step="any" value={config.min ?? 0}
                  onChange={(e) => set('min', Number(e.target.value))} />
              </Field>
              <Field>
                <FieldLabel htmlFor="gauge-max">Scale maximum</FieldLabel>
                <Input id="gauge-max" type="number" step="any" value={config.max ?? 100}
                  onChange={(e) => set('max', Number(e.target.value))} />
              </Field>
              <FieldDescription className="sm:col-span-2">
                In the display unit above, not SignalK's SI unit.
              </FieldDescription>
            </div>
          )}
        </div>

        <DialogFooter>
          <Button variant="ghost" onClick={onCancel}>Cancel</Button>
          <Button disabled={!canSave} onClick={() => onSave(config)}>Save</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
