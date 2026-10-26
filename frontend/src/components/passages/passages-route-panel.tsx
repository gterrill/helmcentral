import type { DistanceUnits } from '@/config/app-config'
import { PassageDetailsPage } from '@/components/passages/passage-details-page'
import { PassagesPanel } from '@/components/passages/passages-panel'
import { usePassageList } from '@/hooks/use-passages'
import { usePassagesRoute } from '@/hooks/use-passages-route'

export interface PassagesRoutePanelProps {
  units: DistanceUnits
  canWrite: boolean
  isDarkTheme: boolean
}

// ADR 0171: Passages wired to the app location. The listing and the details
// pages (one passage, one Cruise, a combined selection) are one panel; which
// renders is the route's view.
export function PassagesRoutePanel({ units, canWrite, isDarkTheme }: PassagesRoutePanelProps) {
  const { route, showFilter, openPassage, openCruise, openCombined, close } = usePassagesRoute()
  const list = usePassageList()

  if (route.view === 'index') {
    return (
      <PassagesPanel
        filter={route.filter}
        passages={list.passages}
        cruises={list.cruises}
        loading={list.loading}
        error={list.error}
        canWrite={canWrite}
        onRetry={() => { void list.refresh() }}
        onFilter={showFilter}
        onOpenPassage={(id) => { openPassage(id) }}
        onOpenCruise={(id) => { openCruise(id) }}
        onCombine={(ids) => { openCombined(ids) }}
      />
    )
  }

  const mode =
    route.view === 'passage' ? ({ kind: 'passage', id: route.id } as const)
    : route.view === 'cruise' ? ({ kind: 'cruise', id: route.id } as const)
    : ({ kind: 'combined', ids: route.ids } as const)

  return (
    <PassageDetailsPage
      mode={mode}
      units={units}
      canWrite={canWrite}
      isDarkTheme={isDarkTheme}
      list={list.passages}
      filter={route.view === 'cruise' ? { kind: 'cruise', id: route.id } : route.filter}
      cruises={list.cruises}
      onBack={() => { close() }}
      onOpenPassage={(id) => { openPassage(id) }}
      onOpenCruise={(id) => { openCruise(id) }}
    />
  )
}
