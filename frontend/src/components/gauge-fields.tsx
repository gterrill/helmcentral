import { Plus, Trash2 } from 'lucide-react'
import { useMemo } from 'react'

import { Button } from '@/components/ui/button'
import { Field, FieldDescription, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import type { SignalKPath } from '@/hooks/use-signalk-paths'
import { GAUGE_TREND_WINDOWS, type GaugeDisplay, type GaugeWidgetConfig, type GaugeZone } from '@/lib/dashboard-widgets'
import { inferredQuantityForSIUnit, QUANTITIES, quantityById, unitOption } from '@/lib/quantities'

/**
 * Severities are the alarm vocabulary (ADR 0038), because a zone *is* an alarm
 * rule now (ADR 0050) rather than a colour that happens to look like one.
 */
const ZONE_STATES: { id: GaugeZone['state']; label: string }[] = [
  // `normal` colours the band without raising anything — zoneDerivedAlarmRules
  // skips it. It is how an advisory operating range is expressed, and engine
  // profiles (ADR 0053) ship nothing else.
  { id: 'normal', label: 'Healthy (no alarm)' },
  { id: 'alert', label: 'Alert' },
  { id: 'warn', label: 'Warning' },
  { id: 'alarm', label: 'Alarm' },
  { id: 'emergency', label: 'Emergency' },
]

/** Only a display with a scale has anything to band. */
function hasScale(display: GaugeDisplay): boolean {
  return display === 'radial' || display === 'bar' || display === 'trend'
}

const DISPLAYS: { id: GaugeDisplay; label: string }[] = [
  { id: 'numeric', label: 'Numeric' },
  { id: 'radial', label: 'Dial' },
  { id: 'bar', label: 'Bar' },
  { id: 'lamp', label: 'Indicator' },
  { id: 'trend', label: 'Trend' },
]

const SELECT_CLASS = 'h-9 w-full rounded-md border bg-transparent px-3 text-sm'

export function defaultGaugeConfig(): GaugeWidgetConfig {
  return { path: '', label: '', display: 'numeric', quantity: 'raw', unit: 'raw' }
}

interface GaugeFieldsProps {
  value: GaugeWidgetConfig
  onChange: (next: GaugeWidgetConfig) => void
  paths: SignalKPath[]
  /** Namespaces the input and datalist ids, so N copies coexist in one dialog. */
  idPrefix: string
}

/**
 * The per-gauge form, shared between the single-gauge dialog and each member
 * row of a gauge group (ADR 0049) so the two never drift apart.
 */
export function GaugeFields({ value, onChange, paths, idPrefix }: GaugeFieldsProps) {
  const quantity = quantityById(value.quantity)
  const known = useMemo(() => new Map(paths.map((p) => [p.path, p])), [paths])
  const listId = `${idPrefix}-path-options`

  const set = <K extends keyof GaugeWidgetConfig>(key: K, next: GaugeWidgetConfig[K]) =>
    onChange({ ...value, [key]: next })

  /**
   * Picking a path preselects the quantity from SignalK's own meta.units, so
   * the operator does not have to know that oil pressure arrives in pascals.
   *
   * Most paths on a real boat carry no meta.units at all, so "nothing to
   * infer" has to leave the gauge's existing quantity and unit alone rather
   * than reset them to raw. Without this, repicking a path on a gauge that
   * was already set up (say temperature/C, with zones authored in °C) wipes
   * that config to raw/raw while the zone thresholds stay in °C — the
   * backend then compares a °C threshold against a Kelvin reading and fires
   * a bogus alarm. This is exactly what happens when the group-duplicate
   * button repicks every member's path.
   */
  const choosePath = (path: string) => {
    const match = known.get(path)
    const inferred = inferredQuantityForSIUnit(match?.units)
    onChange({
      ...value,
      path,
      ...(inferred ? { quantity: inferred.id, unit: inferred.units[0].id } : {}),
      label: value.label || path.split('.').slice(-1)[0],
    })
  }

  const min = value.min ?? 0
  const max = value.max ?? 100
  const unitLabel = unitOption(value.quantity, value.unit).label

  /**
   * Zones are edited as a direction plus a threshold, never as a free from/to
   * pair. The backend turns each band into one alarm threshold, and a band
   * floating in the middle of the scale has no equivalent — so the editor is
   * built so it cannot produce one, rather than rejecting it afterwards.
   */
  const setZone = (index: number, next: GaugeZone) =>
    onChange({ ...value, zones: (value.zones ?? []).map((z, i) => (i === index ? next : z)) })

  const anchoredZone = (direction: 'below' | 'above', threshold: number, state: GaugeZone['state']): GaugeZone =>
    direction === 'below' ? { from: min, to: threshold, state } : { from: threshold, to: max, state }

  const zoneDirection = (zone: GaugeZone): 'below' | 'above' => (zone.from <= min ? 'below' : 'above')
  const zoneThreshold = (zone: GaugeZone): number => (zoneDirection(zone) === 'below' ? zone.to : zone.from)

  const addZone = () =>
    onChange({
      ...value,
      zones: [...(value.zones ?? []), anchoredZone('below', min + (max - min) * 0.15, 'alarm')],
    })

  const removeZone = (index: number) =>
    onChange({ ...value, zones: (value.zones ?? []).filter((_, i) => i !== index) })

  return (
    <div className="grid gap-3">
      <Field>
        <FieldLabel htmlFor={`${idPrefix}-path`}>SignalK path</FieldLabel>
        <Input
          id={`${idPrefix}-path`}
          list={listId}
          value={value.path}
          onChange={(e) => choosePath(e.target.value)}
          placeholder="propulsion.port.oilPressure"
        />
        {/*
          Free text, not a closed dropdown: the snapshot only holds paths seen
          since the stream connected, so with the engines off propulsion.* is
          absent — and that is exactly when someone sets an engine gauge up.
        */}
        <datalist id={listId}>
          {paths.map((p) => <option key={p.path} value={p.path} />)}
        </datalist>
        <FieldDescription>
          {paths.length > 0
            ? `${paths.length} paths currently published. Not listed? Type it anyway — a sensor that is off has no path until it reports.`
            : 'No paths seen yet. You can still type one.'}
        </FieldDescription>
      </Field>

      <Field>
        <FieldLabel htmlFor={`${idPrefix}-label`}>Label</FieldLabel>
        <Input id={`${idPrefix}-label`} value={value.label} onChange={(e) => set('label', e.target.value)} />
      </Field>

      <div className="grid gap-3 sm:grid-cols-3">
        <Field>
          <FieldLabel htmlFor={`${idPrefix}-display`}>Display</FieldLabel>
          <select id={`${idPrefix}-display`} className={SELECT_CLASS}
            value={value.display} onChange={(e) => set('display', e.target.value as GaugeDisplay)}>
            {DISPLAYS.map((d) => <option key={d.id} value={d.id}>{d.label}</option>)}
          </select>
        </Field>
        <Field>
          <FieldLabel htmlFor={`${idPrefix}-quantity`}>Quantity</FieldLabel>
          <select id={`${idPrefix}-quantity`} className={SELECT_CLASS}
            value={value.quantity}
            onChange={(e) => {
              const next = quantityById(e.target.value)
              onChange({ ...value, quantity: next.id, unit: next.units[0].id })
            }}>
            {QUANTITIES.map((q) => <option key={q.id} value={q.id}>{q.label}</option>)}
          </select>
        </Field>
        <Field>
          <FieldLabel htmlFor={`${idPrefix}-unit`}>Unit</FieldLabel>
          <select id={`${idPrefix}-unit`} className={SELECT_CLASS}
            value={value.unit} onChange={(e) => set('unit', e.target.value)}>
            {quantity.units.map((u) => <option key={u.id} value={u.id}>{u.label || u.id}</option>)}
          </select>
        </Field>
      </div>

      {value.display === 'trend' && (
        <Field>
          <FieldLabel htmlFor={`${idPrefix}-window`}>History window</FieldLabel>
          <select id={`${idPrefix}-window`} className={SELECT_CLASS}
            value={value.window ?? '3h'} onChange={(e) => set('window', e.target.value)}>
            {GAUGE_TREND_WINDOWS.map((w) => <option key={w} value={w}>{w}</option>)}
          </select>
          <FieldDescription>History comes from InfluxDB. Without it configured, a trend gauge says so rather than drawing a flat line.</FieldDescription>
        </Field>
      )}

      {hasScale(value.display) && (
        <div className="grid gap-3 sm:grid-cols-2">
          <Field>
            <FieldLabel htmlFor={`${idPrefix}-min`}>Scale minimum</FieldLabel>
            <Input id={`${idPrefix}-min`} type="number" step="any" value={value.min ?? 0}
              onChange={(e) => set('min', Number(e.target.value))} />
          </Field>
          <Field>
            <FieldLabel htmlFor={`${idPrefix}-max`}>Scale maximum</FieldLabel>
            <Input id={`${idPrefix}-max`} type="number" step="any" value={value.max ?? 100}
              onChange={(e) => set('max', Number(e.target.value))} />
          </Field>
          <FieldDescription className="sm:col-span-2">
            In the display unit above, not SignalK's SI unit.
          </FieldDescription>
        </div>
      )}

      {hasScale(value.display) && (
        <div className="grid gap-2 rounded-md border border-border p-3">
          <span className="text-[10px] font-medium uppercase tracking-wider text-muted-foreground">
            Zones
          </span>
          <FieldDescription>
            A zone colours the gauge, and every severity except Healthy also raises an alarm at its threshold. Thresholds are in {unitLabel || 'the display unit'}, not SignalK's SI unit.
          </FieldDescription>

          {(value.zones ?? []).map((zone, index) => {
            const direction = zoneDirection(zone)
            return (
              <div key={index} className="grid items-end gap-2 sm:grid-cols-[auto_1fr_1fr_auto]">
                <select
                  aria-label={`Zone ${index + 1} direction`}
                  className={SELECT_CLASS}
                  value={direction}
                  onChange={(e) =>
                    setZone(index, anchoredZone(e.target.value as 'below' | 'above', zoneThreshold(zone), zone.state))
                  }
                >
                  <option value="below">Below</option>
                  <option value="above">Above</option>
                </select>
                <div className="flex items-center gap-1">
                  <Input
                    aria-label={`Zone ${index + 1} threshold`}
                    type="number"
                    step="any"
                    value={zoneThreshold(zone)}
                    onChange={(e) => setZone(index, anchoredZone(direction, Number(e.target.value), zone.state))}
                  />
                  {unitLabel && <span className="text-[11px] text-muted-foreground">{unitLabel}</span>}
                </div>
                <select
                  aria-label={`Zone ${index + 1} severity`}
                  className={SELECT_CLASS}
                  value={zone.state}
                  onChange={(e) =>
                    setZone(index, anchoredZone(direction, zoneThreshold(zone), e.target.value as GaugeZone['state']))
                  }
                >
                  {ZONE_STATES.map((s) => <option key={s.id} value={s.id}>{s.label}</option>)}
                </select>
                <Button size="sm" variant="ghost" aria-label={`Remove zone ${index + 1}`} onClick={() => removeZone(index)}>
                  <Trash2 className="size-3.5" />
                </Button>
              </div>
            )
          })}

          <Button variant="ghost" className="w-fit" onClick={addZone}>
            <Plus className="size-3.5" />
            Add zone
          </Button>
        </div>
      )}
    </div>
  )
}
