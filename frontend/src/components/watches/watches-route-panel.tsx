import { memo, useMemo } from 'react'

import { WatchDetailsPage } from '@/components/watches/watch-details-page'
import { WatchesIndex } from '@/components/watches/watches-index'
import { useWatchList } from '@/hooks/use-watches'
import { useWatchesRoute } from '@/hooks/use-watches-route'
import { orderWatches, WATCHES_DEFAULT_SORT } from '@/lib/index-order'
import { sameListSort } from '@/lib/list-order'
import { recordPager } from '@/lib/record-pager'
import { WATCH_TEMPLATES } from '@/lib/watches'
import type { MateWatchRunContext } from '@/lib/mate-screen'

export interface WatchesRoutePanelProps {
  canWrite: boolean
  /** Settings is admin-only: without it the links to Alarm settings are plain words. */
  canAdmin: boolean
  onAskMate?: (question: string, options: { newConversation: true; watchRun: MateWatchRunContext }) => void
}

// ADR 0227: Watches wired to the app location. The index and one watch's page
// (or a new, unsaved one) are one panel; which renders is the route's view.
// AppShell re-renders on every live delta; memo keeps this page out of it.
export const WatchesRoutePanel = memo(function WatchesRoutePanel({ canWrite, canAdmin, onAskMate }: WatchesRoutePanelProps) {
  const { route, openIndex, openWatch, openNew, setSort } = useWatchesRoute()
  // A watch's page reads the list too, for its arrows: the index's watches in
  // the index's order, from the same query.
  const list = useWatchList(route.view !== 'new', route.view === 'index')
  const sort = route.view === 'new' ? undefined : route.sort
  const detailsId = route.view === 'details' ? route.id : null
  const pager = useMemo(
    () => (detailsId === null ? undefined : recordPager(orderWatches(list.watches, sort), detailsId, (w) => w.id, (w) => w.name, openWatch)),
    [detailsId, list.watches, sort, openWatch],
  )

  if (route.view === 'index') {
    return (
      <WatchesIndex
        watches={list.watches}
        sort={route.sort ?? null}
        onSortChange={(next) => { setSort(sameListSort(next, WATCHES_DEFAULT_SORT) ? null : next) }}
        max={list.max}
        cost24h={list.cost24h}
        storageBytes={list.storageBytes}
        problem={list.problem}
        loading={list.loading}
        error={list.error}
        canWrite={canWrite}
        onRetry={() => { void list.refresh() }}
        onOpen={(id) => { openWatch(id) }}
        onNew={(template) => { openNew(template?.id ?? null) }}
      />
    )
  }

  const mode = route.view === 'details'
    ? ({ kind: 'existing', id: route.id } as const)
    : ({ kind: 'new', template: WATCH_TEMPLATES.find((t) => t.id === route.template) ?? null } as const)

  return (
    <WatchDetailsPage
      // A different watch is a different draft: remount rather than carry one's edits to another.
      key={route.view === 'details' ? route.id : `new:${route.template ?? ''}`}
      mode={mode}
      pager={pager}
      linkedRunId={route.view === 'details' ? route.run ?? null : null}
      canWrite={canWrite}
      canAdmin={canAdmin}
      onOpenIndex={() => { openIndex() }}
      onCreated={(id) => { openWatch(id, { replace: true }) }}
      onDeleted={() => { openIndex({ replace: true }) }}
      onAskMate={onAskMate}
    />
  )
})
