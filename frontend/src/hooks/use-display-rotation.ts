import { useCallback, useEffect, useRef, useState } from 'react'
import { displayFeed, nextDisplayIndex, prevDisplayIndex, DISPLAY_RECOVERY_POLL_MS, type DisplayEligiblePage, type DisplayFeedContext } from '@/lib/displays'

/**
 * Drives a wall display's rotation through /display/<slug> (ADR 0110,
 * superseding ADR 0089's /kiosk). Rather than owning its own page-rendering
 * state, this hook only decides *which* page id is showing right now and
 * hands that to `onShow` — App.tsx passes `setActivePageId`, so `activePage`,
 * `effectiveWidgets`, `dashboardGrid` and `renderWidget` all keep working
 * completely unchanged; the wall is just another way `activePageId` gets set.
 */
export interface UseDisplayRotationOptions {
  /** Gates everything below — false while the app isn't on the wall route at
   * all. */
  enabled: boolean
  /** The resolved display's id, or null before the displays fetch resolves
   * and the route's slug has been matched against it. This hook gates on it
   * too, in addition to `enabled` — see the effect below for why it must. */
  displayId: string | null
  pages: readonly DisplayEligiblePage[]
  navigationState: string | null
  /** `?page=<id>` (lib/displays.ts's parseDisplayOptions): pins one page for
   * authoring on the helm browser and for screenshots. No timer runs, and
   * next/previous/pause/resume are all no-ops. */
  pinnedPageId: string | null
  refetch: () => Promise<void> | void
  onShow: (pageId: string) => void
}

export interface UseDisplayRotationResult {
  currentPageId: string | null
  /** True once the feed itself is empty (nothing assigned to this display,
   * or nothing whose condition currently holds) — display-shell.tsx renders
   * a waiting state on this rather than a blank grid. */
  feedEmpty: boolean
  /** Steps to the next/previous page immediately and restarts its dwell —
   * unless currently paused, in which case the new page shows but no timer
   * is scheduled, so the rotation stays paused until resume() (ADR 0110
   * §5b — arrow keys still step a paused wall). No-ops while disabled, the
   * display hasn't resolved yet, a page is pinned, or the feed is empty. */
  next: () => void
  previous: () => void
  /** Stops the rotation: clears any pending timer and prevents the
   * top-of-lap refetch from firing while paused. No-op if already paused. */
  pause: () => void
  /** Resumes the rotation, restarting a FULL dwell for the page currently
   * showing — never the remainder of the one pause() interrupted. No-op if
   * not paused. */
  resume: () => void
  paused: boolean
  /** The current page's 0-based position in the feed, and the feed's
   * length, for the remote toast ("Engines (2 of 5)"). `{index: 0, total:
   * 0}` before anything is showing or while the feed is empty. */
  position: { index: number; total: number }
}

// Matches the cadence an operator would notice: a page assigned mid-day
// should show up on the wall well inside a coffee break, not after the
// router logs a stale connection.
const EMPTY_FEED_POLL_MS = DISPLAY_RECOVERY_POLL_MS

export function useDisplayRotation({
  enabled,
  displayId,
  pages,
  navigationState,
  pinnedPageId,
  refetch,
  onShow,
}: UseDisplayRotationOptions): UseDisplayRotationResult {
  const [currentPageId, setCurrentPageId] = useState<string | null>(null)
  const [feedEmpty, setFeedEmpty] = useState(false)
  const [paused, setPaused] = useState(false)

  // Read through refs inside the scheduling callbacks below so the effect's
  // own dependency array only needs [enabled, pinnedPageId, displayId].
  // `pages` changes constantly for reasons that have nothing to do with the
  // wall (any tile edit on any page updates the same array reference from
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
  // The source of truth advance()/next()/previous() read, not the
  // currentPageId state value: state updates are batched/async and these
  // need the id they last scheduled against synchronously, on the very next
  // tick or the very next imperative call.
  const currentIdRef = useRef<string | null>(null)
  const pausedRef = useRef(false)
  // Lets the "pages changed while empty" recovery effect below trigger an
  // immediate re-check without being a dependency of the main effect itself.
  const advanceNowRef = useRef<(() => void) | null>(null)
  // The current effect run's imperative controls, exposed to the stable
  // wrapper functions returned below. Same indirection advanceNowRef uses,
  // for the same reason: next/previous/pause/resume must be callable (from
  // use-display-remote.ts's keydown handler) without themselves being
  // recreated on every render or forcing the main effect to depend on them.
  const controlsRef = useRef<{
    next: () => void
    previous: () => void
    pause: () => void
    resume: () => void
  } | null>(null)

  useEffect(() => {
    controlsRef.current = null
    // A fresh run of this effect (a genuine restart: enabled/pinned/display
    // changed) always starts unpaused. Pause is a user action against a lap
    // already in progress, not a state that should survive one of those.
    pausedRef.current = false
    setPaused(false)

    if (!enabled || displayId === null) return

    if (pinnedPageId) {
      // Pinned for authoring/screenshots: shown once, never advances, and
      // leaves controlsRef null so next/previous/pause/resume are no-ops.
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

    function currentFeed(): DisplayEligiblePage[] {
      const ctx: DisplayFeedContext = { displayId: displayId as string, navigationState: navigationStateRef.current }
      return displayFeed(pagesRef.current, ctx)
    }

    function showPage(target: DisplayEligiblePage, opts: { schedule: boolean } = { schedule: true }) {
      currentIdRef.current = target.id
      setCurrentPageId(target.id)
      setFeedEmpty(false)
      onShowRef.current(target.id)
      if (opts.schedule) {
        timer = setTimeout(() => { void advance() }, (target.dwell_seconds ?? 0) * 1000)
      }
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
    // transition back to index 0 that isn't the very first page shown - a
    // genuine wrap, or the showing page having vanished from the feed
    // entirely (nextDisplayIndex's restart rule) - re-reads data first,
    // matching HelmCast's top-of-lap refresh: a page someone just assigned
    // or edited must not wait a full lap to show up correctly. Never runs
    // while paused: pause() clears the pending timer, so nothing schedules
    // this, but the guard also protects the "pages changed while empty"
    // recovery effect below from resurrecting it mid-pause.
    async function advance() {
      if (cancelled || pausedRef.current) return
      clearScheduled()
      let feed = currentFeed()
      if (feed.length === 0) {
        pollWhileEmpty()
        return
      }
      const nextIndex = nextDisplayIndex(feed, currentIdRef.current)
      const isWrap = nextIndex === 0 && currentIdRef.current !== null
      if (isWrap) {
        await refetchRef.current()
        if (cancelled || pausedRef.current) return
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

    // A manual step (next()/previous()) that lands on index 0 counts as a
    // lap wrap for refetch purposes too - the same rule advance() applies -
    // so a page assigned or edited elsewhere isn't stuck waiting for a full
    // lap just because the operator used the remote to get there.
    async function stepWrap() {
      await refetchRef.current()
      if (cancelled) return
      const feed = currentFeed()
      if (feed.length === 0) {
        pollWhileEmpty()
        return
      }
      showPage(feed[0], { schedule: !pausedRef.current })
    }

    function handleStep(direction: 'next' | 'previous') {
      clearScheduled()
      const feed = currentFeed()
      if (feed.length === 0) {
        pollWhileEmpty()
        return
      }
      const targetIndex = direction === 'next'
        ? nextDisplayIndex(feed, currentIdRef.current)
        : prevDisplayIndex(feed, currentIdRef.current)
      if (targetIndex === 0 && currentIdRef.current !== null) {
        void stepWrap()
        return
      }
      // Still paused after a manual step (ADR 0110 §5b: arrows step a
      // paused wall without resuming it) - so nothing is scheduled here in
      // that case, only when the rotation is actually running.
      showPage(feed[targetIndex], { schedule: !pausedRef.current })
    }

    function handlePause() {
      if (pausedRef.current) return
      pausedRef.current = true
      setPaused(true)
      clearScheduled()
    }

    function handleResume() {
      if (!pausedRef.current) return
      pausedRef.current = false
      setPaused(false)
      clearScheduled()
      const feed = currentFeed()
      if (feed.length === 0) {
        pollWhileEmpty()
        return
      }
      // The page currently on screen gets a full dwell from here, never the
      // remainder pause() interrupted - falls back to the first page if the
      // one showing vanished from the feed while paused.
      const target = feed.find((candidate) => candidate.id === currentIdRef.current) ?? feed[0]
      showPage(target, { schedule: true })
    }

    controlsRef.current = {
      next: () => handleStep('next'),
      previous: () => handleStep('previous'),
      pause: handlePause,
      resume: handleResume,
    }

    advanceNowRef.current = () => { void advance() }
    void advance()

    return () => {
      cancelled = true
      clearScheduled()
      advanceNowRef.current = null
      controlsRef.current = null
    }
  }, [enabled, pinnedPageId, displayId])

  // Closes the startup race: on mount, `pages` is still `[]` while
  // useDashboardPages' own fetch is in flight, so the very first advance()
  // above almost always finds an empty feed and commits to the 15s poll
  // cadence in pollWhileEmpty(). Without this, a wall that loads with an
  // already-assigned page would still show "nothing assigned" for up to 15
  // seconds on every reload. This only ever fires while feedEmpty is already
  // true (and not paused), so it never disrupts a lap already in progress -
  // the main effect's own deliberately narrow dependency array still governs
  // that case.
  useEffect(() => {
    if (!enabled || pinnedPageId || !feedEmpty || pausedRef.current) return
    advanceNowRef.current?.()
    // pages is compared by reference (useDashboardPages hands back a new
    // array on every fetch), so this only re-fires on an actual data
    // change, not on every render.
  }, [pages, feedEmpty, enabled, pinnedPageId])

  const next = useCallback(() => controlsRef.current?.next(), [])
  const previous = useCallback(() => controlsRef.current?.previous(), [])
  const pause = useCallback(() => controlsRef.current?.pause(), [])
  const resume = useCallback(() => controlsRef.current?.resume(), [])

  let position = { index: 0, total: 0 }
  if (displayId !== null && currentPageId !== null) {
    const feed = displayFeed(pages, { displayId, navigationState })
    const index = feed.findIndex((candidate) => candidate.id === currentPageId)
    position = { index: index === -1 ? 0 : index, total: feed.length }
  }

  return { currentPageId, feedEmpty, next, previous, pause, resume, paused, position }
}
