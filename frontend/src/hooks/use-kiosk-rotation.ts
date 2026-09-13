import { useEffect, useRef, useState } from 'react'
import { kioskFeed, nextKioskIndex, type KioskEligiblePage, type KioskFeedContext } from '@/lib/kiosk'

/**
 * Drives the wall display's rotation through /kiosk (ADR 0089).
 *
 * Rather than owning its own page-rendering state, this hook only decides
 * *which* page id is showing right now and hands that to `onShow` — App.tsx
 * passes `setActivePageId`, so `activePage`, `effectiveWidgets`,
 * `dashboardGrid` and `renderWidget` all keep working completely unchanged;
 * the kiosk is just another way `activePageId` gets set.
 */
export interface UseKioskRotationOptions {
  /** Gates everything below — false while the app isn't on /kiosk at all. */
  enabled: boolean
  pages: readonly KioskEligiblePage[]
  navigationState: string | null
  /** `?page=<id>` (lib/kiosk.ts's parseKioskOptions): pins one page for
   * authoring on the helm browser and for screenshots. No timer runs. */
  pinnedPageId: string | null
  refetch: () => Promise<void> | void
  onShow: (pageId: string) => void
}

export interface UseKioskRotationResult {
  currentPageId: string | null
  /** True once the feed itself is empty (nothing flagged, or nothing whose
   * condition currently holds) — kiosk-shell.tsx renders a waiting state on
   * this rather than a blank grid. */
  feedEmpty: boolean
}

// Matches the cadence an operator would notice: a page flagged mid-day should
// show up on the wall well inside a coffee break, not after the router logs
// a stale connection.
const EMPTY_FEED_POLL_MS = 15_000

export function useKioskRotation({
  enabled,
  pages,
  navigationState,
  pinnedPageId,
  refetch,
  onShow,
}: UseKioskRotationOptions): UseKioskRotationResult {
  const [currentPageId, setCurrentPageId] = useState<string | null>(null)
  const [feedEmpty, setFeedEmpty] = useState(false)

  // Read through refs inside the scheduling callbacks below so the effect's
  // own dependency array only needs [enabled, pinnedPageId]. `pages` changes
  // constantly for reasons that have nothing to do with the kiosk (any
  // widget edit on any page updates the same array reference from
  // useDashboardPages) — restarting the timer on every one of those would
  // mean a lap that never completes.
  const pagesRef = useRef(pages)
  pagesRef.current = pages
  const navigationStateRef = useRef(navigationState)
  navigationStateRef.current = navigationState
  const refetchRef = useRef(refetch)
  refetchRef.current = refetch
  const onShowRef = useRef(onShow)
  onShowRef.current = onShow
  // The source of truth advance() reads, not the currentPageId state value:
  // state updates are batched/async and advance() needs the id it last
  // scheduled against synchronously, on the very next tick.
  const currentIdRef = useRef<string | null>(null)
  // Lets the "pages changed while empty" effect below trigger an immediate
  // re-check without being a dependency of the main effect itself.
  const advanceNowRef = useRef<(() => void) | null>(null)

  useEffect(() => {
    if (!enabled) return

    if (pinnedPageId) {
      // Pinned for authoring/screenshots: shown once, never advances.
      currentIdRef.current = pinnedPageId
      setCurrentPageId(pinnedPageId)
      setFeedEmpty(false)
      onShowRef.current(pinnedPageId)
      return
    }

    let cancelled = false
    let timer: ReturnType<typeof setTimeout> | ReturnType<typeof setInterval> | null = null

    function clearScheduled() {
      if (timer !== null) {
        clearTimeout(timer)
        clearInterval(timer)
        timer = null
      }
    }

    function currentFeed(): KioskEligiblePage[] {
      const ctx: KioskFeedContext = { navigationState: navigationStateRef.current }
      return kioskFeed(pagesRef.current, ctx)
    }

    function showPage(target: KioskEligiblePage) {
      currentIdRef.current = target.id
      setCurrentPageId(target.id)
      setFeedEmpty(false)
      onShowRef.current(target.id)
      timer = setTimeout(() => { void advance() }, (target.kiosk_seconds ?? 0) * 1000)
    }

    async function pollTick() {
      if (cancelled) return
      await refetchRef.current()
      if (cancelled) return
      const feed = currentFeed()
      if (feed.length > 0) {
        clearScheduled()
        showPage(feed[0])
      }
    }

    function pollWhileEmpty() {
      currentIdRef.current = null
      setCurrentPageId(null)
      setFeedEmpty(true)
      timer = setInterval(() => { void pollTick() }, EMPTY_FEED_POLL_MS)
    }

    // Starts the very first page and moves to every one after it. A
    // transition back to index 0 that isn't the very first page shown -
    // a genuine wrap, or the showing page having vanished from the feed
    // entirely (nextKioskIndex's restart rule) - re-reads data first,
    // matching HelmCast's top-of-lap refresh: a page someone just flagged
    // or edited must not wait a full lap to show up correctly.
    async function advance() {
      if (cancelled) return
      clearScheduled()
      let feed = currentFeed()
      if (feed.length === 0) {
        pollWhileEmpty()
        return
      }
      const nextIndex = nextKioskIndex(feed, currentIdRef.current)
      const isWrap = nextIndex === 0 && currentIdRef.current !== null
      if (isWrap) {
        await refetchRef.current()
        if (cancelled) return
        feed = currentFeed()
        if (feed.length === 0) {
          pollWhileEmpty()
          return
        }
        showPage(feed[0])
        return
      }
      showPage(feed[nextIndex])
    }

    advanceNowRef.current = () => { void advance() }
    void advance()

    return () => {
      cancelled = true
      clearScheduled()
      advanceNowRef.current = null
    }
  }, [enabled, pinnedPageId])

  // Closes the startup race: on mount, `pages` is still `[]` while
  // useDashboardPages' own fetch is in flight, so the very first advance()
  // above almost always finds an empty feed and commits to the 15s poll
  // cadence in pollWhileEmpty(). Without this, a kiosk that loads with an
  // already-flagged page would still show "nothing flagged" for up to 15
  // seconds on every reload. This only ever fires while feedEmpty is
  // already true, so it never disrupts a lap already in progress - the
  // main effect's own deliberately narrow dependency array still governs
  // that case.
  useEffect(() => {
    if (!enabled || pinnedPageId || !feedEmpty) return
    advanceNowRef.current?.()
    // pages is compared by reference (useDashboardPages hands back a new
    // array on every fetch), so this only re-fires on an actual data
    // change, not on every render.
  }, [pages, feedEmpty, enabled, pinnedPageId])

  return { currentPageId, feedEmpty }
}
