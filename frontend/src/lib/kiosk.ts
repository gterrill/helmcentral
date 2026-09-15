import { GRID_MARGIN, GRID_ROW_HEIGHT } from '@/components/dashboard-bento-grid'
import type { DashboardLayoutItem } from '@/lib/dashboard-widgets'

// Pure and React-free (ADR 0089), same reasoning app-location.ts gives for
// its own purity: use-kiosk-rotation.ts and App.tsx both need this logic
// testable without mounting anything, and a page's eligibility for the
// rotation should never depend on how it happens to be rendered.

/** The wall display's own physical strip: 1920x360, mounted upside down. */
export const KIOSK_VIEWPORT_HEIGHT_PX = 360

/** kiosk-shell.tsx's root padding (`p-2` = 8px) on every edge. */
export const KIOSK_ROOT_PADDING_PX = 8

/**
 * What's actually left for the grid once the root's own top and bottom
 * padding are subtracted: 360 - 8 - 8 = 344px. At GRID_ROW_HEIGHT=32 and
 * GRID_MARGIN=16 that's exactly 7 rows (320px used, 24px to spare) — 8 rows
 * (368px) does not fit. kiosk-fold-guide.tsx draws its line here.
 */
export const KIOSK_FOLD_PX = KIOSK_VIEWPORT_HEIGHT_PX - 2 * KIOSK_ROOT_PADDING_PX

/** How many whole grid rows fit inside a budget of `px` pixels. */
export function kioskRowsThatFit(px: number): number {
  return Math.floor((px + GRID_MARGIN) / (GRID_ROW_HEIGHT + GRID_MARGIN))
}

export interface KioskOptions {
  /** Only 180 rotates (the strip is mounted upside down); anything else is 0. */
  rotate: 0 | 180
  /** `?page=<id>` pins one page, for authoring on the helm browser and for screenshots. */
  pageId: string | null
  /**
   * `?height=<px>` constrains the kiosk root to that many pixels at the top
   * of the viewport, for a kiosk browser whose framebuffer is taller than
   * the physical panel (a WPE-webkit build reporting 1920x1080 on a 1920x360
   * strip). An integer from 200 to 4320; anything else (missing, fractional,
   * non-numeric, out of range) is null, meaning the full viewport, matching
   * the app's behaviour before this option existed.
   */
  height: number | null
}

/** Parses the kiosk route's own query string. Never throws on a malformed value. */
export function parseKioskOptions(search: string): KioskOptions {
  const params = new URLSearchParams(search)
  const rawHeight = params.get('height')
  const parsedHeight = rawHeight === null ? NaN : Number(rawHeight)
  const height = Number.isInteger(parsedHeight) && parsedHeight >= 200 && parsedHeight <= 4320 ? parsedHeight : null
  return {
    rotate: params.get('rotate') === '180' ? 180 : 0,
    pageId: params.get('page'),
    height,
  }
}

/** The subset of DashboardPage that kioskFeed needs — kept minimal and
 * locally typed (as page-skin-select.tsx's SkinnablePage does) so this pure
 * module doesn't have to depend on the hook module that owns the full type. */
export interface KioskEligiblePage {
  id: string
  kiosk?: boolean
  kiosk_seconds?: number
  kiosk_when?: 'always' | 'anchored' | 'motoring' | 'sailing' | 'moored'
  widgets: DashboardLayoutItem[]
}

export interface KioskFeedContext {
  /** Current vessel navigation.state, typically from signalk-autostate. */
  navigationState: string | null
}

function normalizeKioskState(state: string | null): string | null {
  if (typeof state !== 'string') return null
  const normalized = state.trim().toLowerCase().replace(/_/g, ' ')
  if (normalized === 'under way using engine') return 'motoring'
  return normalized || null
}

/**
 * The pages that belong in the wall-display rotation right now, in server
 * order (feed order is page order — there is no separate kiosk ordering).
 *
 * A page needs kiosk on, a positive duration, and at least one widget: an
 * empty page flagged for kiosk would show nothing for its whole slot, which
 * is a mistake to skip rather than a blank screen to render. A page whose
 * condition no longer matches drops out entirely (not just "shows nothing"),
 * so the feed never offers a slot the wall has nothing to fill.
 */
export function kioskFeed(pages: readonly KioskEligiblePage[], ctx: KioskFeedContext): KioskEligiblePage[] {
  const navState = normalizeKioskState(ctx.navigationState)
  return pages.filter((page) => {
    if (!page.kiosk || !page.kiosk_seconds || page.kiosk_seconds <= 0) return false
    if (page.widgets.length === 0) return false
    if (page.kiosk_when && page.kiosk_when !== 'always' && page.kiosk_when !== navState) return false
    return true
  })
}

/**
 * The index to show next, wrapping around at the end of the feed. Restarts
 * at 0 when the current page id can no longer be found in the feed — it was
 * unflagged, its condition turned false, or it was deleted mid-lap — rather
 * than throwing or getting stuck, since the feed is recomputed at every
 * advance and can legitimately change shape between two ticks.
 *
 * Returns -1 for an empty feed; the caller (use-kiosk-rotation) is
 * responsible for polling instead of scheduling an advance in that case.
 */
export function nextKioskIndex(feed: readonly { id: string }[], currentId: string | null): number {
  if (feed.length === 0) return -1
  const currentIndex = feed.findIndex((page) => page.id === currentId)
  if (currentIndex === -1) return 0
  return (currentIndex + 1) % feed.length
}
