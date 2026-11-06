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
import { ChoiceList } from '@/components/patterns'
import { Field, FieldDescription, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { useTileSave } from '@/hooks/use-tile-save'
import { Switch } from '@/components/ui/switch'
import {
  GAUGE_GROUP_TITLE_MAX_LENGTH,
  POI_MAP_RANGE_NM_MAX,
  POI_MAP_RANGE_NM_MIN,
  POI_MAP_SUMMARY_CYCLE_SECONDS_DEFAULT,
  POI_MAP_SUMMARY_CYCLE_SECONDS_MAX,
  POI_MAP_SUMMARY_CYCLE_SECONDS_MIN,
  isValidPoiMapConfig,
  type DashboardLayoutItem,
  type DashboardWidgetId,
  type PoiMapWidgetConfig,
} from '@/lib/dashboard-widgets'
import { POI_CATEGORIES } from '@/lib/poi'

interface PoiMapConfigDialogProps {
  widget: DashboardLayoutItem | null
  open: boolean
  onOpenChange: (open: boolean) => void
  onSave: (id: DashboardWidgetId, config: PoiMapWidgetConfig) => void | Promise<void>
}

function defaultPoiMapConfig(): PoiMapWidgetConfig {
  return {
    title: '',
    rangeNm: 5,
    categories: POI_CATEGORIES.map((c) => c.id),
    layout: 'split',
    showAis: true,
    showTrail: false,
  }
}

export function PoiMapConfigDialog({ widget, open, onOpenChange, onSave }: PoiMapConfigDialogProps) {
  const [config, setConfig] = useState<PoiMapWidgetConfig>(defaultPoiMapConfig)
  const { saving, error: saveError, run } = useTileSave(widget?.id)

  // Re-seed whenever a different widget is opened, so editing one Nearby
  // widget never shows another's settings.
  useEffect(() => {
    setConfig(widget?.poiMap ? { ...widget.poiMap, categories: [...widget.poiMap.categories] } : defaultPoiMapConfig())
  }, [widget?.id, widget?.poiMap])

  if (!widget) return null

  const canSave = isValidPoiMapConfig(config)

  // The page closes the dialog once the tile is written. Closing here would
  // also close it over a failed write.
  const handleSave = () => {
    if (!canSave) return
    void run(() => onSave(widget.id, { ...config, title: config.title.trim() }))
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-h-[85vh] overflow-y-auto sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>Nearby map</DialogTitle>
          <DialogDescription>
            A map of points of interest near the vessel, ranked by distance.
          </DialogDescription>
        </DialogHeader>

        <div className="space-y-4">
          <Field>
            <FieldLabel htmlFor="poi-map-title">Title</FieldLabel>
            <Input
              id="poi-map-title"
              value={config.title}
              maxLength={GAUGE_GROUP_TITLE_MAX_LENGTH}
              placeholder="Nearby"
              onChange={(e) => setConfig((prev) => ({ ...prev, title: e.target.value }))}
            />
          </Field>

          <Field>
            <FieldLabel htmlFor="poi-map-range">Range (nm)</FieldLabel>
            <Input
              id="poi-map-range"
              type="number"
              min={POI_MAP_RANGE_NM_MIN}
              max={POI_MAP_RANGE_NM_MAX}
              step={0.5}
              value={config.rangeNm}
              onChange={(e) => setConfig((prev) => ({ ...prev, rangeNm: Number(e.target.value) }))}
            />
            <FieldDescription>
              {POI_MAP_RANGE_NM_MIN} to {POI_MAP_RANGE_NM_MAX} nautical miles.
            </FieldDescription>
          </Field>

          <ChoiceList
            label="Layout"
            value={config.layout}
            onChange={(layout) => setConfig((prev) => ({ ...prev, layout }))}
            listClassName="grid grid-cols-2"
            choices={[
              { value: 'map', label: 'Map only' },
              { value: 'split', label: 'Map and list' },
            ]}
          />

          {config.layout === 'split' && (
            <Field>
              <FieldLabel htmlFor="poi-map-summary-cycle">Summary cycle (seconds)</FieldLabel>
              <Input
                id="poi-map-summary-cycle"
                type="number"
                min={POI_MAP_SUMMARY_CYCLE_SECONDS_MIN}
                max={POI_MAP_SUMMARY_CYCLE_SECONDS_MAX}
                step={1}
                placeholder={String(POI_MAP_SUMMARY_CYCLE_SECONDS_DEFAULT)}
                value={config.summaryCycleSeconds ?? ''}
                onChange={(e) => setConfig((prev) => ({
                  ...prev,
                  summaryCycleSeconds: e.target.value === '' ? undefined : Number(e.target.value),
                }))}
              />
              <FieldDescription>
                How long the ranked list highlights one point, with a ring on its marker,
                before cycling to the next. Its summary shows too if it has one.{' '}
                {POI_MAP_SUMMARY_CYCLE_SECONDS_MIN} to{' '}
                {POI_MAP_SUMMARY_CYCLE_SECONDS_MAX} seconds; {POI_MAP_SUMMARY_CYCLE_SECONDS_DEFAULT} when left blank.
              </FieldDescription>
            </Field>
          )}

          <ChoiceList
            multiple
            label="Categories"
            value={config.categories}
            onChange={(categories) => setConfig((prev) => ({ ...prev, categories }))}
            choices={POI_CATEGORIES.map((c) => ({ value: c.id, label: c.label }))}
            listClassName="grid grid-cols-2 sm:grid-cols-3"
          />

          <Field orientation="horizontal">
            <Switch
              id="poi-map-ais"
              checked={config.showAis ?? false}
              onCheckedChange={(checked) => setConfig((prev) => ({ ...prev, showAis: checked }))}
            />
            <FieldLabel htmlFor="poi-map-ais">Show AIS traffic</FieldLabel>
          </Field>

          <Field orientation="horizontal">
            <Switch
              id="poi-map-trail"
              checked={config.showTrail ?? false}
              onCheckedChange={(checked) => setConfig((prev) => ({ ...prev, showTrail: checked }))}
            />
            <FieldLabel htmlFor="poi-map-trail">Show own trail</FieldLabel>
          </Field>
        </div>

        <DialogFooter>
          {saveError && <p role="alert" className="mr-auto text-sm text-destructive">{saveError}</p>}
          <Button variant="outline" onClick={() => onOpenChange(false)}>
            Cancel
          </Button>
          <Button onClick={handleSave} disabled={!canSave || saving}>
            {saving ? 'Saving…' : 'Save'}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
