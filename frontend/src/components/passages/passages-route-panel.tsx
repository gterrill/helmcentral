import { memo } from 'react'

import type { DistanceUnits } from '@/config/app-config'
import { InboxUploadPage } from '@/components/passages/inbox-upload-page'
import { InboxView } from '@/components/passages/inbox-view'
import { PassageDetailsPage } from '@/components/passages/passage-details-page'
import { PassagesPanel } from '@/components/passages/passages-panel'
import { useInboxCount } from '@/hooks/use-inbox'
import { usePassageList } from '@/hooks/use-passages'
import { usePassagesRoute, type PassagesRouteApi } from '@/hooks/use-passages-route'
import type { PassagesRoute } from '@/lib/app-location'

export interface PassagesRoutePanelProps {
  units: DistanceUnits
  canWrite: boolean
  isDarkTheme: boolean
}

// ADR 0171: Passages wired to the app location. The listing and the details
// pages (one passage, one Cruise, a combined selection) are one panel; which
// renders is the route's view. The Inbox of shared photos (ADR 0279) and the
// page that shares them are two more views of it. Each view is its own
// component, so it reads only what it shows: the upload page reads no passages,
// and the Inbox count is read only where the badge is.
// AppShell re-renders on every live delta; memo keeps this page out of it.
export const PassagesRoutePanel = memo(function PassagesRoutePanel({ units, canWrite, isDarkTheme }: PassagesRoutePanelProps) {
  const nav = usePassagesRoute()
  const { route, close, openInbox } = nav

  switch (route.view) {
    case 'index':
      return <PassagesIndex route={route} nav={nav} canWrite={canWrite} />
    case 'inbox':
      return <InboxRouteView nav={nav} canWrite={canWrite} />
    case 'upload':
      return <InboxUploadPage canWrite={canWrite} onBack={() => { close() }} onOpenInbox={() => { openInbox() }} />
    default:
      return <PassageDetails route={route} nav={nav} units={units} canWrite={canWrite} isDarkTheme={isDarkTheme} />
  }
})

function PassagesIndex({ route, nav, canWrite }: { route: Extract<PassagesRoute, { view: 'index' }>; nav: PassagesRouteApi; canWrite: boolean }) {
  const list = usePassageList()
  const inboxCount = useInboxCount()
  return (
    <PassagesPanel
      filter={route.filter}
      passages={list.passages}
      cruises={list.cruises}
      loading={list.loading}
      error={list.error}
      canWrite={canWrite}
      onRetry={() => { void list.refresh() }}
      onFilter={nav.showFilter}
      onOpenPassage={(id) => { nav.openPassage(id) }}
      onOpenCruise={(id) => { nav.openCruise(id) }}
      onCombine={(ids) => { nav.openCombined(ids) }}
      inbox={{ count: inboxCount, onOpen: () => { nav.openInbox() } }}
    />
  )
}

function InboxRouteView({ nav, canWrite }: { nav: PassagesRouteApi; canWrite: boolean }) {
  const list = usePassageList()
  return <InboxView canWrite={canWrite} passages={list.passages} onBack={() => { nav.close() }} onOpenUpload={() => { nav.openUpload() }} />
}

function PassageDetails({ route, nav, units, canWrite, isDarkTheme }: {
  route: Exclude<PassagesRoute, { view: 'index' | 'inbox' | 'upload' }>
  nav: PassagesRouteApi
  units: DistanceUnits
  canWrite: boolean
  isDarkTheme: boolean
}) {
  const list = usePassageList()
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
      onBack={() => { nav.close() }}
      onOpenPassage={(id) => { nav.openPassage(id) }}
      onOpenCruise={(id) => { nav.openCruise(id) }}
    />
  )
}
