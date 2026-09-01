import { ChevronDown, ChevronUp, Plus, Trash2 } from 'lucide-react'
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
import { EngineProfileDialog } from '@/components/engine-profile-dialog'
import { Field, FieldLabel } from '@/components/ui/field'
import { GaugeFields, defaultGaugeConfig } from '@/components/gauge-fields'
import { Input } from '@/components/ui/input'
import { useSignalKPaths } from '@/hooks/use-signalk-paths'
import { mergeGaugeSettingsBySuffix } from '@/lib/engine-profiles'
import {
  GAUGE_GROUP_MAX_COLUMNS,
  GAUGE_GROUP_MAX_GAUGES,
  GAUGE_GROUP_TITLE_MAX_LENGTH,
  rewriteGaugePaths,
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
}

/**
 * Builds and retargets a gauge group (ADR 0049).
 *
 * The find/replace row is the reason the feature exists: duplicate "Port",
 * replace `port` with `starboard`, and five gauges move to the other engine in
 * one pass instead of five rounds of retyping.
 */
export function GaugeGroupConfigDialog({ widget, onCancel, onSave }: GaugeGroupConfigDialogProps) {
  const { paths } = useSignalKPaths(widget !== null)
  const [config, setConfig] = useState<GaugeGroupWidgetConfig>(defaultGroupConfig)
  const [replaceFrom, setReplaceFrom] = useState('')
  const [replaceTo, setReplaceTo] = useState('')
  const [profileOpen, setProfileOpen] = useState(false)

  // Re-seed per instance, so editing one group never shows another's settings.
  useEffect(() => {
    setConfig(widget?.gaugeGroup ? structuredClone(widget.gaugeGroup) : defaultGroupConfig())
    setReplaceFrom('')
    setReplaceTo('')
  }, [widget?.id, widget?.gaugeGroup])

  const setGauge = (index: number, next: GaugeWidgetConfig) =>
    setConfig((current) => ({
      ...current,
      gauges: current.gauges.map((g, i) => (i === index ? next : g)),
    }))

  const addGauge = () =>
    setConfig((current) => ({ ...current, gauges: [...current.gauges, defaultGaugeConfig()] }))

  const removeGauge = (index: number) =>
    setConfig((current) => ({ ...current, gauges: current.gauges.filter((_, i) => i !== index) }))

  const moveGauge = (index: number, delta: number) =>
    setConfig((current) => {
      const target = index + delta
      if (target < 0 || target >= current.gauges.length) return current
      const gauges = [...current.gauges]
      ;[gauges[index], gauges[target]] = [gauges[target], gauges[index]]
      return { ...current, gauges }
    })

  // Computed, never applied on its own: an operator sees a typo'd search term
  // produce nothing before pressing Apply, rather than after.
  const preview = useMemo(() => {
    if (replaceFrom === '') return []
    return config.gauges
      .map((gauge, index) => ({ index, from: gauge.path, to: gauge.path.split(replaceFrom).join(replaceTo) }))
      .filter((row) => row.from !== row.to)
  }, [config.gauges, replaceFrom, replaceTo])

  const applyReplace = () =>
    setConfig((current) => ({ ...current, gauges: rewriteGaugePaths(current.gauges, replaceFrom, replaceTo) }))

  const canSave =
    config.title.trim() !== '' &&
    config.gauges.length > 0 &&
    config.gauges.length <= GAUGE_GROUP_MAX_GAUGES &&
    config.gauges.every((g) => g.path.trim() !== '')

  return (
    <Dialog open={widget !== null} onOpenChange={(open) => { if (!open) onCancel() }}>
      <DialogContent className="max-h-[85vh] max-w-2xl overflow-y-auto">
        <DialogHeader>
          <DialogTitle>Gauge Group</DialogTitle>
          <DialogDescription>
            A named cluster of gauges in one tile. Duplicate the tile to build the other engine, then
            replace the paths in one pass.
          </DialogDescription>
        </DialogHeader>

        <div className="grid gap-4">
          <div className="grid gap-3 sm:grid-cols-[1fr_auto]">
            <Field>
              <FieldLabel htmlFor="gauge-group-title">Title</FieldLabel>
              <Input
                id="gauge-group-title"
                maxLength={GAUGE_GROUP_TITLE_MAX_LENGTH}
                value={config.title}
                onChange={(e) => setConfig((current) => ({ ...current, title: e.target.value }))}
                placeholder="Port"
              />
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

          {/* Fills scales and bands on the members already here and appends the
              ones the profile has that this tile lacks, matching by path
              suffix. Paths and labels are the operator's own (ADR 0053). */}
          <Button variant="secondary" className="w-fit" onClick={() => setProfileOpen(true)}>
            Apply an engine profile…
          </Button>

          <div className="rounded-md border border-border bg-background/60 p-3">
            <div className="grid items-end gap-2 sm:grid-cols-[1fr_1fr_auto]">
              <Field>
                <FieldLabel htmlFor="gauge-group-replace-from">Replace</FieldLabel>
                <Input
                  id="gauge-group-replace-from"
                  value={replaceFrom}
                  onChange={(e) => setReplaceFrom(e.target.value)}
                  placeholder="port"
                />
              </Field>
              <Field>
                <FieldLabel htmlFor="gauge-group-replace-to">With</FieldLabel>
                <Input
                  id="gauge-group-replace-to"
                  value={replaceTo}
                  onChange={(e) => setReplaceTo(e.target.value)}
                  placeholder="starboard"
                />
              </Field>
              <Button variant="secondary" disabled={preview.length === 0} onClick={applyReplace}>
                Apply
              </Button>
            </div>

            {replaceFrom !== '' && (
              <div data-testid="gauge-group-replace-preview" className="mt-2 grid gap-0.5">
                <span className="text-[10px] font-medium uppercase tracking-wider text-muted-foreground">
                  {preview.length === 0
                    ? 'No paths match'
                    : `${preview.length} of ${config.gauges.length} paths will change`}
                </span>
                {preview.map((row) => (
                  <span key={row.index} className="truncate text-[11px] text-muted-foreground">
                    {row.from} → <span className="text-primary">{row.to}</span>
                  </span>
                ))}
              </div>
            )}
          </div>

          {config.gauges.map((gauge, index) => (
            // Keyed by index: members have no id of their own, and duplicate
            // paths within a group are allowed.
            <div key={index} className="rounded-md border border-border p-3">
              <div className="mb-2 flex items-center gap-1">
                <span className="flex-1 truncate text-[10px] font-medium uppercase tracking-wider text-muted-foreground">
                  Gauge {index + 1}
                </span>
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

        <EngineProfileDialog
          open={profileOpen}
          applyLabel="Apply to these gauges"
          existingGauges={config.gauges}
          onCancel={() => setProfileOpen(false)}
          onApply={(_title, profileGauges, suffixes) => {
            setConfig((current) => ({
              ...current,
              gauges: mergeGaugeSettingsBySuffix(current.gauges, profileGauges, suffixes).gauges,
            }))
            setProfileOpen(false)
          }}
        />

        <DialogFooter>
          <Button variant="ghost" onClick={onCancel}>Cancel</Button>
          <Button disabled={!canSave} onClick={() => onSave(config)}>Save</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
