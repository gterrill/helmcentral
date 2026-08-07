export const DASHBOARD_WIDGET_IDS = [
  'vessel',
  'wind',
  'depth-tide',
  'position',
  'today-now',
  'anchor-watch',
  'rode-scope',
  'tanks',
  'route',
  'nearby-vessels',
  'battery-power',
  'solar',
  'alternator',
  'generator',
  'czone-switches',
  'hot-water',
] as const

/** A widget baked into the app, with a fixed id and at most one instance per page. */
export type BuiltinWidgetId = typeof DASHBOARD_WIDGET_IDS[number]

/**
 * Embed widgets (ADR 0031) are the only widget that can appear more than once on
 * a page, so each instance carries a token in its id: `embed:<token>`.
 */
export const EMBED_WIDGET_ID_PREFIX = 'embed:'
export type EmbedWidgetId = `${typeof EMBED_WIDGET_ID_PREFIX}${string}`

export type DashboardWidgetId = BuiltinWidgetId | EmbedWidgetId

export const DASHBOARD_WIDGET_LABELS: Record<BuiltinWidgetId, string> = {
  'vessel': 'Vessel',
  'wind': 'Apparent Wind',
  'depth-tide': 'Depth & Tide',
  'position': 'Position',
  'today-now': 'Today & Now',
  'anchor-watch': 'Anchor Watch',
  'rode-scope': 'Rode & Scope',
  'tanks': 'Tanks',
  'route': 'Route',
  'nearby-vessels': 'Nearby Vessels',
  'battery-power': 'Battery & Power',
  'solar': 'Solar',
  'alternator': 'Alternator',
  'generator': 'Generator',
  'czone-switches': 'Switches',
  'hot-water': 'Hot Water',
}

export interface EmbedWidgetConfig {
  title: string
  url: string
}

export interface DashboardLayoutItem {
  id: DashboardWidgetId
  x: number
  y: number
  w: number
  h: number
  /** Present only on `embed:` widgets; the backend rejects it on any other id. */
  embed?: EmbedWidgetConfig
}

/** Length caps mirroring embedURLMaxLen / embedTitleMaxLen in backend/dashboard_pages.go. */
export const EMBED_URL_MAX_LENGTH = 2048
export const EMBED_TITLE_MAX_LENGTH = 64

export function isEmbedWidgetId(id: string): id is EmbedWidgetId {
  return id.startsWith(EMBED_WIDGET_ID_PREFIX)
}

/**
 * Mirrors validateEmbedWidget in backend/dashboard_pages.go. Duplicated rather
 * than shared because the config dialog needs synchronous feedback while the
 * server must not trust the client — keep the two rule sets in step.
 */
export function isValidEmbedUrl(url: string): boolean {
  const trimmed = url.trim()
  if (trimmed === '' || trimmed.length > EMBED_URL_MAX_LENGTH) return false

  // The WHATWG parser silently rewrites an empty authority ("http:///a/b") into
  // a host ("http://a/b"); Go's net/url leaves the host blank and rejects it.
  // Reject here too, so the dialog can't accept a URL the server will 400.
  if (/^https?:\/\/\//i.test(trimmed)) return false

  let parsed: URL
  try {
    parsed = new URL(trimmed)
  } catch {
    return false
  }
  return (parsed.protocol === 'http:' || parsed.protocol === 'https:') && parsed.host !== ''
}

/**
 * Mints an `embed:<token>` id unique within the given page.
 *
 * Deliberately not `crypto.randomUUID()`: that API is secure-context-only and is
 * undefined when Helmcentral is served over plain HTTP from a LAN address, which
 * is the normal case on a boat. The token is a layout key needing uniqueness, not
 * a secret, so a timestamp plus randomness is the honest tool — and it avoids a
 * runtime fallback that would mask the missing API.
 */
export function newEmbedWidgetId(existing: readonly DashboardLayoutItem[]): EmbedWidgetId {
  for (;;) {
    // padEnd keeps the token at the backend's 8-character minimum even when
    // Math.random() happens to stringify short (0.5 -> "0.i").
    const token = `${Date.now().toString(36)}${Math.random().toString(36).slice(2, 10).padEnd(8, '0')}`
    const id: EmbedWidgetId = `${EMBED_WIDGET_ID_PREFIX}${token}`
    if (!existing.some((w) => w.id === id)) return id
  }
}

/**
 * Re-applies the grid's geometry to the persisted widget list.
 *
 * RGL's LayoutItem carries only `{i,x,y,w,h}`, so rebuilding widgets from it
 * alone drops the `embed` config that `embed:` widgets require — the backend
 * then rejects the whole PATCH (validateEmbedWidget in dashboard_pages.go) and
 * no layout change on the page persists at all. Iterating `widgets` rather than
 * the geometry keeps every non-geometry field, including ones added later.
 */
export function mergeLayoutGeometry(
  widgets: readonly DashboardLayoutItem[],
  geometry: readonly { i: string; x: number; y: number; w: number; h: number }[],
): DashboardLayoutItem[] {
  const byId = new Map(geometry.map((g) => [g.i, g]))
  return widgets.map((w) => {
    const g = byId.get(w.id)
    return g ? { ...w, x: g.x, y: g.y, w: g.w, h: g.h } : w
  })
}

/** Human-readable name for a placed widget, for labels and screen-reader text. */
export function widgetDisplayName(widget: DashboardLayoutItem): string {
  if (isEmbedWidgetId(widget.id)) {
    return widget.embed?.title.trim() || 'Embed'
  }
  return DASHBOARD_WIDGET_LABELS[widget.id]
}
