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
  onSave: (id: DashboardWidgetId, config: PoiMapWidgetConfig) => void
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

  // Re-seed whenever a different widget is opened, so editing one Nearby
  // widget never shows another's settings.
  useEffect(() => {
    setConfig(widget?.poiMap ? { ...widget.poiMap, categories: [...widget.poiMap.categories] } : defaultPoiMapConfig())
  }, [widget?.id, widget?.poiMap])

  if (!widget) return null

  const canSave = isValidPoiMapConfig(config)

  const toggleCategory = (id: string, checked: boolean) => {
    setConfig((prev) => ({
      ...prev,
      categories: checked
        ? [...prev.categories, id]
        : prev.categories.filter((c) => c !== id),
    }))
  }

  const handleSave = () => {
    if (!canSave) return
    onSave(widget.id, { ...config, title: config.title.trim() })
    onOpenChange(false)
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>Nearby map</DialogTitle>
          <DialogDescription>
            A moving map of points of interest near the vessel, ranked by distance.
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

          <Field>
            <FieldLabel>Layout</FieldLabel>
            <div className="flex gap-4 text-sm">
              <label className="flex items-center gap-1.5">
                <input
                  type="radio"
                  name="poi-map-layout"
                  value="map"
                  checked={config.layout === 'map'}
                  onChange={() => setConfig((prev) => ({ ...prev, layout: 'map' }))}
                />
                Map only
              </label>
              <label className="flex items-center gap-1.5">
                <input
                  type="radio"
                  name="poi-map-layout"
                  value="split"
                  checked={config.layout === 'split'}
                  onChange={() => setConfig((prev) => ({ ...prev, layout: 'split' }))}
                />
                Map and list
              </label>
            </div>
          </Field>

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
                How long the ranked list shows one point's summary before cycling to the next
                one that has a summary. {POI_MAP_SUMMARY_CYCLE_SECONDS_MIN} to{' '}
                {POI_MAP_SUMMARY_CYCLE_SECONDS_MAX} seconds; {POI_MAP_SUMMARY_CYCLE_SECONDS_DEFAULT} when left blank.
              </FieldDescription>
            </Field>
          )}

          <Field>
            <FieldLabel>Categories</FieldLabel>
            <div className="grid grid-cols-2 gap-x-4 gap-y-1.5 sm:grid-cols-3">
              {POI_CATEGORIES.map((category) => (
                <label key={category.id} className="flex items-center gap-1.5 text-sm">
                  <input
                    type="checkbox"
                    checked={config.categories.includes(category.id)}
                    onChange={(e) => toggleCategory(category.id, e.target.checked)}
                  />
                  {category.label}
                </label>
              ))}
            </div>
          </Field>

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
          <Button variant="outline" onClick={() => onOpenChange(false)}>
            Cancel
          </Button>
          <Button onClick={handleSave} disabled={!canSave}>
            Save
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
