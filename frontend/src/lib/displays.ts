import { GRID_MARGIN, GRID_ROW_HEIGHT, WALL_ROW_MARGIN } from '@/components/dashboard-bento-grid'
import type { DashboardLayoutItem } from '@/lib/dashboard-widgets'

// Pure and React-free (ADR 0089/0110), same reasoning app-location.ts gives
// for its own purity: the wall rotation hook and App.tsx both need this
// logic testable without mounting anything, and a page's eligibility for
// the rotation should never depend on how it happens to be rendered.

/** A wall display record (ADR 0110): the screen's own geometry, separate
 * from the pages assigned to it. Width/height/scale/rotate are all
 * server-validated, so this module treats them as already-valid input
 * rather than re-checking them - it only ever reads a record the backend
 * has accepted. */
export interface Display {
  id: string
  name: string
  slug: string
  /** The logical canvas a page is authored against, in CSS px. Both 0 means
   * "whatever this browser reports": the shell claims the full viewport and
   * no fold guide is drawn. */
  width: number
  height: number
  /** On-screen magnification of the logical canvas. 0 means 1.0 - use
   * displayScale rather than reading this field directly. */
  scale: number
  rotate: 0 | 180
  /** Slow four-position shift of the whole board, to spare an OLED panel. */
  pixel_shift: boolean
  wake_lock: boolean
  created_at: string
  updated_at: string
}

/** display-shell.tsx's root padding (`p-1` = 4px) on every edge, in logical px. */
export const DISPLAY_ROOT_PADDING_PX = 4

/** How far usePixelShift moves the board on each axis. displayFoldPx reserves
 * twice this (there and back) so the shift never clips the bottom of the board. */
export const PIXEL_SHIFT_AMPLITUDE_PX = 8

/** The dwell a page gets when it first goes onto a wall display and has
 * none stored. Lives here rather than in the control that sets it because
 * two paths put a page on a display - PageDisplaySelect and App's
 * duplicate-to-display - and the server rejects either one that arrives
 * without a dwell. */
export const DEFAULT_DWELL_SECONDS = 30

/** How often an unattended wall retries its way out of a dead state: an
 * empty feed in use-display-rotation, and a failed displays fetch in
 * App's wall branch. Nobody is standing at a wall screen to reload it, so
 * every state it can reach has to have a way back out on its own. */
export const DISPLAY_RECOVERY_POLL_MS = 15_000

/** `d.scale || 1` as a named function: 0 or absent both mean 1.0. */
export function displayScale(d: Display): number {
  return d.scale || 1
}

/**
 * What's left for the grid once display-shell.tsx's own root padding, and -
 * when the pixel shift is on - the room it needs to shift into, are both
 * subtracted from the display's configured height. All of this is in
 * **logical** px; scale is orthogonal and never enters this arithmetic.
 *
 * Null for a zero canvas (height === 0): "whatever this browser reports" has
 * no fold to guide against, so display-fold-guide.tsx draws nothing rather
 * than a line computed from a height nobody configured.
 */
export function displayFoldPx(d: Display): number | null {
  if (d.height === 0) return null
  const base = d.height - 2 * DISPLAY_ROOT_PADDING_PX
  return d.pixel_shift ? base - 2 * PIXEL_SHIFT_AMPLITUDE_PX : base
}

/** Fold budgets at or above this use the generous GRID_MARGIN; a narrower one
 * (a strip, not a television) can't spare 16px gutters and uses the tight
 * WALL_ROW_MARGIN instead. See displayRowMargin. */
const GENEROUS_ROW_MARGIN_FOLD_PX = 600

/**
 * The vertical row margin to author a display's grid with. `WALL_ROW_MARGIN`
 * exists because a 360px strip cannot spare 16px gutters; a 1080p canvas
 * can. Deriving it from the display here, rather than each caller picking a
 * margin by hand, is what keeps the fold guide and the grid it's drawn over
 * from ever disagreeing. A zero (unmeasured) canvas gets the generous
 * margin: it is not known to be a tight strip, so it is not treated as one.
 */
export function displayRowMargin(d: Display): number {
  const foldPx = displayFoldPx(d)
  if (foldPx === null) return GRID_MARGIN
  return foldPx < GENEROUS_ROW_MARGIN_FOLD_PX ? WALL_ROW_MARGIN : GRID_MARGIN
}

/**
 * The tightest wall this project serves: a 1920x360 strip, no pixel shift -
 * 360 - 2*DISPLAY_ROOT_PADDING_PX. Kept as a standalone constant (rather
 * than deleted once displayFoldPx existed) because
 * dashboard-bento-grid-constraints.test.ts uses it as a real guard: every
 * wall-display tile's floor (ADR 0092) was sized to fit inside it on its
 * own, on the tightest screen this project serves, or it could never appear
 * on a wall at all.
 */
export const NARROW_STRIP_FOLD_PX = 360 - 2 * DISPLAY_ROOT_PADDING_PX

/** How many whole grid rows fit inside a budget of `px` pixels at the given
 * row margin - the fold guide and a display's actual grid must use the same
 * margin (displayRowMargin) or they can disagree about what fits. */
export function displayRowsThatFit(px: number, rowMargin: number): number {
  return Math.floor((px + rowMargin) / (GRID_ROW_HEIGHT + rowMargin))
}

export interface DisplayOptions {
  /** `?page=<id>` pins one page, for authoring on the helm browser and for
   * screenshots. The only query param the wall route still reads - rotation
   * and viewport height moved onto the display record (ADR 0110) and have
   * no back-compat shim here. */
  pageId: string | null
}

/** Parses the wall display route's own query string. Never throws on a malformed value. */
export function parseDisplayOptions(search: string): DisplayOptions {
  const params = new URLSearchParams(search)
  return { pageId: params.get('page') }
}

/** The subset of DashboardPage that displayFeed needs — kept minimal and
 * locally typed (as page-skin-select.tsx's SkinnablePage does) so this pure
 * module doesn't have to depend on the hook module that owns the full type. */
export interface DisplayEligiblePage {
  id: string
  display_id?: string
  dwell_seconds?: number
  show_when?: 'always' | 'anchored' | 'motoring' | 'sailing' | 'moored'
  widgets: DashboardLayoutItem[]
}

export interface DisplayFeedContext {
  /** The display currently being rendered — displayFeed scopes to exactly
   * this id, so the caller must have already resolved a slug to a display
   * (app-location.ts's diagnostic path is what happens when it can't). */
  displayId: string
  /** Current vessel navigation.state, typically from signalk-autostate. */
  navigationState: string | null
}

function normalizeDisplayState(state: string | null): string | null {
  if (typeof state !== 'string') return null
  const normalized = state.trim().toLowerCase().replace(/_/g, ' ')
  if (normalized === 'under way using engine') return 'motoring'
  return normalized || null
}

/**
 * The pages that belong in this display's rotation right now, in server
 * order (feed order is page order — there is no separate wall ordering).
 *
 * A page needs to be assigned to this exact display, a positive dwell, and
 * at least one widget: an empty page assigned to the wall would show
 * nothing for its whole slot, which is a mistake to skip rather than a
 * blank screen to render. A page whose condition no longer matches drops
 * out entirely (not just "shows nothing"), so the feed never offers a slot
 * the wall has nothing to fill.
 */
export function displayFeed(pages: readonly DisplayEligiblePage[], ctx: DisplayFeedContext): DisplayEligiblePage[] {
  const navState = normalizeDisplayState(ctx.navigationState)
  return pages.filter((page) => {
    if (page.display_id !== ctx.displayId) return false
    if (!page.dwell_seconds || page.dwell_seconds <= 0) return false
    if (page.widgets.length === 0) return false
    if (page.show_when && page.show_when !== 'always' && page.show_when !== navState) return false
    return true
  })
}

/**
 * The index to show next, wrapping around at the end of the feed. Restarts
 * at 0 when the current page id can no longer be found in the feed — it was
 * reassigned, its condition turned false, or it was deleted mid-lap —
 * rather than throwing or getting stuck, since the feed is recomputed at
 * every advance and can legitimately change shape between two ticks.
 *
 * Returns -1 for an empty feed; the caller (the wall rotation hook) is
 * responsible for polling instead of scheduling an advance in that case.
 */
export function nextDisplayIndex(feed: readonly { id: string }[], currentId: string | null): number {
  if (feed.length === 0) return -1
  const currentIndex = feed.findIndex((page) => page.id === currentId)
  if (currentIndex === -1) return 0
  return (currentIndex + 1) % feed.length
}

/**
 * The index to show on a "previous" step, wrapping around at the start of
 * the feed. Same recovery as nextDisplayIndex when the current id has
 * vanished — restart at 0 rather than guessing at a position that no
 * longer means anything.
 *
 * Returns -1 for an empty feed, for the same reason nextDisplayIndex does.
 */
export function prevDisplayIndex(feed: readonly { id: string }[], currentId: string | null): number {
  if (feed.length === 0) return -1
  const currentIndex = feed.findIndex((page) => page.id === currentId)
  if (currentIndex === -1) return 0
  return (currentIndex - 1 + feed.length) % feed.length
}
