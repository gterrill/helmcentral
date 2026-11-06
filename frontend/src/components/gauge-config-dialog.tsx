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
import { GaugeFields, defaultGaugeConfig } from '@/components/gauge-fields'
import { useTileSave } from '@/hooks/use-tile-save'
import { useSignalKPaths } from '@/hooks/use-signalk-paths'
import type { DashboardLayoutItem, GaugeWidgetConfig } from '@/lib/dashboard-widgets'

interface GaugeConfigDialogProps {
  widget: DashboardLayoutItem | null
  onCancel: () => void
  onSave: (config: GaugeWidgetConfig) => void | Promise<void>
}

export function GaugeConfigDialog({ widget, onCancel, onSave }: GaugeConfigDialogProps) {
  const { paths } = useSignalKPaths(widget !== null)
  const [config, setConfig] = useState<GaugeWidgetConfig>(defaultGaugeConfig)
  const { saving, error: saveError, run, reset } = useTileSave()

  // Re-seed per instance, so editing one gauge never shows another's settings.
  useEffect(() => {
    setConfig(widget?.gauge ? { ...widget.gauge } : defaultGaugeConfig())
    reset()
  }, [widget?.id, widget?.gauge, reset])

  const canSave = config.path.trim() !== ''

  return (
    <Dialog open={widget !== null} onOpenChange={(open) => { if (!open) onCancel() }}>
      <DialogContent className="max-w-lg">
        <DialogHeader>
          <DialogTitle>Gauge</DialogTitle>
          <DialogDescription>Show any value the SignalK server publishes.</DialogDescription>
        </DialogHeader>

        <GaugeFields value={config} onChange={setConfig} paths={paths} idPrefix="gauge" />

        <DialogFooter>
          {saveError && <p role="alert" className="mr-auto text-sm text-destructive">{saveError}</p>}
          <Button variant="ghost" onClick={onCancel}>Cancel</Button>
          <Button disabled={!canSave || saving} onClick={() => { void run(() => onSave(config)) }}>{saving ? 'Saving…' : 'Save'}</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
