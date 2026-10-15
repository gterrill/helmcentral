import { useState } from 'react'
import { toast } from 'sonner'

import { EngineProfileDialog } from '@/components/engine-profile-dialog'
import { Button } from '@/components/ui/button'
import { useDashboardPages, type DashboardPage } from '@/hooks/use-dashboard-pages'
import { apiRequest } from '@/lib/api-query'
import { newGaugeGroupWidgetId, type GaugeWidgetConfig } from '@/lib/dashboard-widgets'

interface ApplyGaugeZonesProps {
  /** The engine item's saved profile. */
  profileId: string
  /** The engine's SignalK source, such as propulsion.port. */
  source: string
}

/**
 * "Apply gauge zones" on an engine item: adds a gauge tile for this engine to
 * the first dashboard page, with the profile's ranges marked. It acts at once,
 * as its own action, and needs nothing from the item's draft except the saved
 * profile and source. Each use adds another tile, so move or remove it from the
 * dashboard afterwards.
 */
export function ApplyGaugeZones({ profileId, source }: ApplyGaugeZonesProps) {
  const { pages, updatePage } = useDashboardPages()
  const [open, setOpen] = useState(false)

  const handleApply = async (title: string, gauges: GaugeWidgetConfig[], _suffixes: string[], hero?: number) => {
    setOpen(false)
    // Lands on the first dashboard page - this page has no notion of "the
    // active page" the way the dashboard itself does, so this is a deliberate
    // simplification: move the tile afterward from the dashboard if it belongs
    // somewhere else.
    const targetId = pages[0]?.id
    if (!targetId) return

    // The local `pages` copy can be stale the moment a second apply follows a
    // first one in the same visit (each apply is its own round trip, and React
    // may not have re-rendered with the previous save's result yet) - re-reading
    // the page fresh right before building the patch is what makes applying
    // port's zones and then starboard's keep both tiles, rather than the second
    // overwrite the first's.
    let current: DashboardPage
    try {
      current = await apiRequest<DashboardPage>(`/api/dashboard-pages/${targetId}`)
    } catch (err) {
      toast.error('Could not apply gauge zones', { description: err instanceof Error ? err.message : 'Request failed' })
      return
    }

    const maxY = current.widgets.reduce((max, w) => Math.max(max, w.y + w.h), 0)
    const saved = await updatePage(targetId, {
      widgets: [...current.widgets, {
        id: newGaugeGroupWidgetId(current.widgets),
        x: 0, y: maxY, w: 4, h: 7,
        gaugeGroup: { title, gauges, ...(hero === undefined ? {} : { hero }) },
      }],
    })
    // updatePage already toasts on failure (use-dashboard-pages.ts); only the
    // success case needs its own feedback here.
    if (saved) toast.success(`Added "${title}" tile`)
  }

  return (
    <>
      <Button type="button" size="sm" variant="outline" onClick={() => setOpen(true)}>
        Apply gauge zones
      </Button>
      <EngineProfileDialog
        open={open}
        onCancel={() => setOpen(false)}
        onApply={handleApply}
        applyLabel="Add tile"
        initialProfileId={profileId}
        initialInstance={source}
      />
    </>
  )
}
