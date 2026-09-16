import { POI_CATEGORY_IDS } from '@/lib/poi'

export const DASHBOARD_WIDGET_IDS = [
  'vessel',
  'wind',
  'depth-tide',
  'position',
  'today-now',
  'anchor-watch',
  'tanks',
  'route',
  'nearby-vessels',
  'radar-targets',
  'battery-power',
  'solar',
  'alternator',
  'generator',
  'czone-switches',
  'hot-water',
  'autopilot',
  'clock',
  'current-conditions',
  'forecast-days',
  'sea-state',
] as const

/** A widget baked into the app, with a fixed id and at most one instance per page. */
export type BuiltinWidgetId = typeof DASHBOARD_WIDGET_IDS[number]

/**
 * Embed widgets (ADR 0031) are the only widget that can appear more than once on
 * a page, so each instance carries a token in its id: `embed:<token>`.
 */
export const EMBED_WIDGET_ID_PREFIX = 'embed:'
export const GAUGE_WIDGET_ID_PREFIX = 'gauge:'
/**
 * Gauge groups (ADR 0049) are the third multi-instance widget: a named cluster
 * of gauges in one tile. Note this prefix is not a prefix of `gauge:` and
 * `gauge:` is not a prefix of it, so the two id checks never overlap.
 */
export const GAUGE_GROUP_WIDGET_ID_PREFIX = 'gauge-group:'
/** Lamp strips (ADR 0052) — the indicator ribbon, as a placeable widget. */
export const LAMP_STRIP_WIDGET_ID_PREFIX = 'lamps:'
/** Engine clusters (ADR 0054) — a ticked ring with mask-cut corner cards. */
export const CLUSTER_WIDGET_ID_PREFIX = 'cluster:'
/** The poi-map widget (ADR 0091 phase 3b) — a moving map of nearby points of interest. */
export const POI_MAP_WIDGET_ID_PREFIX = 'poi-map:'
export type EmbedWidgetId = `${typeof EMBED_WIDGET_ID_PREFIX}${string}`
export type GaugeWidgetId = `${typeof GAUGE_WIDGET_ID_PREFIX}${string}`
export type GaugeGroupWidgetId = `${typeof GAUGE_GROUP_WIDGET_ID_PREFIX}${string}`
export type LampStripWidgetId = `${typeof LAMP_STRIP_WIDGET_ID_PREFIX}${string}`
export type ClusterWidgetId = `${typeof CLUSTER_WIDGET_ID_PREFIX}${string}`
export type PoiMapWidgetId = `${typeof POI_MAP_WIDGET_ID_PREFIX}${string}`

export type DashboardWidgetId =
  | BuiltinWidgetId
  | EmbedWidgetId
  | GaugeWidgetId
  | GaugeGroupWidgetId
  | LampStripWidgetId
  | ClusterWidgetId
  | PoiMapWidgetId

export const DASHBOARD_WIDGET_LABELS: Record<BuiltinWidgetId, string> = {
  'vessel': 'Vessel',
  'wind': 'Apparent Wind',
  'depth-tide': 'Depth & Tide',
  'position': 'Position',
  'today-now': 'Today & Now',
  'anchor-watch': 'Anchor Watch',
  'tanks': 'Tanks',
  'route': 'Route',
  'nearby-vessels': 'Nearby Vessels',
  'radar-targets': 'Radar Targets',
  'battery-power': 'Battery & Power',
  'solar': 'Solar',
  'alternator': 'Alternator',
  'generator': 'Generator',
  'czone-switches': 'Switches',
  'hot-water': 'Hot Water',
  'autopilot': 'Autopilot',
  'clock': 'Clock',
  'current-conditions': 'Current Conditions',
  'forecast-days': 'Forecast',
  'sea-state': 'Sea State',
}

export interface EmbedWidgetConfig {
  title: string
  url: string
  /**
   * Drops the Tile title bar and padding so the embed fills the widget, for
   * wall-display strips where chrome is wasted space. Ignored while the
   * dashboard is in layout-editing mode, since editing still needs the gear
   * icon and title reachable.
   */
  frameless?: boolean
}

export type GaugeDisplay = 'numeric' | 'radial' | 'bar' | 'lamp' | 'trend'

/** Windows the history endpoint allows; mirrors telemetryHistoryWindows in Go. */
export const GAUGE_TREND_WINDOWS = ['1h', '3h', '6h', '24h', '7d'] as const

/** A band of the range coloured by alarm severity (ADR 0038's vocabulary). */
export interface GaugeZone {
  from: number
  to: number
  state: 'normal' | 'alert' | 'warn' | 'alarm' | 'emergency'
}

/** Binds one widget to one SignalK path (ADR 0039). */
export interface GaugeWidgetConfig {
  path: string
  label: string
  display: GaugeDisplay
  quantity: string
  unit: string
  decimals?: number
  min?: number
  max?: number
  zones?: GaugeZone[]
  /** History window; `trend` only (ADR 0051). */
  window?: string
  /** `radial` only: `plain` is the bare arc, `instrument` adds ticks and a scale (ADR 0054). */
  ringStyle?: 'plain' | 'instrument'
  /** `radial` only: where the reading sits relative to the ring. */
  readout?: 'below' | 'inside'
  /** `instrument` rings only: divides the scale numbers, e.g. 100 for RPM. */
  labelDivisor?: number
}

/**
 * A named cluster of gauges rendered as one tile (ADR 0049).
 *
 * Members are `GaugeWidgetConfig` verbatim, so the renderers, the unit
 * conversion and the backend's per-gauge validation are shared rather than
 * forked for the grouped case.
 */
export interface GaugeGroupWidgetConfig {
  title: string
  /** Columns inside the tile; undefined derives a count from the member count. */
  columns?: number
  /** Index of the member rendered as the larger two-column hero readout. */
  hero?: number
  gauges: GaugeWidgetConfig[]
}

/** One indicator lamp bound to a path. */
export interface LampConfig {
  path: string
  label: string
  /** Lights when the value is zero or absent, for a signal whose healthy state is off. */
  invert?: boolean
}

/**
 * A dense row of indicator lamps plus an optional CHK rollup (ADR 0052) — the
 * N2KView indicator ribbon, placed per page and duplicated onto the others.
 */
export interface LampStripWidgetConfig {
  title: string
  lamps: LampConfig[]
  showCheck?: boolean
}

/** One corner card. Rows let a card stack several readings, as the temps box does. */
export interface ClusterCorner {
  label: string
  /** A name from CLUSTER_ICONS; inferred from the first row when unset. */
  icon?: string
  rows: GaugeWidgetConfig[]
}

/**
 * One tank on the fuel rail (ADR 0061).
 *
 * Two gauge slots, not one. `GaugeWidgetConfig` binds one widget to one path,
 * and litres needs two readings: a 0..1 ratio and a capacity in m3. Both are
 * that type verbatim, the decision that made gauge groups and cluster corners
 * cheap, so zones, units and the backend's per-gauge validation work unchanged.
 *
 * The capacity slot leaves min, max and zones meaningless, which is the price
 * of not forking the type and the Go struct and the validator alongside it. The
 * editor pays it by rendering a cut-down form for that slot.
 *
 * Both slots must declare their quantity, and the backend rejects a level that
 * is not `ratio` or a capacity that is not `volume`. Nothing on this vessel
 * publishes `meta.units` for a tank, so the path picker preselects Unitless and
 * an unnoticed default would turn 890 L into 1.2 on the helm.
 */
export interface FuelBarConfig {
  /** tanks.fuel.5.currentLevel. Quantity `ratio`, so the bar reads percent. */
  level: GaugeWidgetConfig
  /** tanks.fuel.5.capacity. Quantity `volume`, which is what makes litres. */
  capacity: GaugeWidgetConfig
}

/**
 * A fuel rail down one edge of the cluster (ADR 0061): a bar per tank against a
 * shared percentage scale, and the side's total in litres under it.
 *
 * `side` is which edge of the tile it sits on, so a pair of clusters can put
 * their rails outboard and mirror each other.
 */
export interface ClusterFuelRail {
  side: 'left' | 'right'
  bars: FuelBarConfig[]
  /** Caption over the total. "Total" when unset. */
  totalLabel?: string
}

/**
 * An engine cluster (ADR 0054): a ticked ring with the primary reading inside
 * it and up to four mask-cut cards in the corners.
 *
 * Every slot is a GaugeWidgetConfig verbatim — the decision that made gauge
 * groups cheap, so zones, units and scales all work per slot unchanged.
 */
export interface EngineClusterConfig {
  title: string
  ring: GaugeWidgetConfig
  centre: GaugeWidgetConfig
  /** A name from CLUSTER_ICONS; inferred from the centre slot when unset. */
  centreIcon?: string
  corners: ClusterCorner[]
  /**
   * Readings that get a telltale in the strip under the dial rather than a box
   * of their own: icon, short label, value, coloured by zone. For the ones you
   * check by glancing for a colour, not by reading a number.
   */
  telltales?: GaugeWidgetConfig[]
  /** A fuel rail down one edge (ADR 0061). */
  fuel?: ClusterFuelRail
}

/**
 * A moving map of nearby points of interest (ADR 0091 phase 3b): a range, a
 * category subset, a layout, and two rendering toggles. Per-instance and
 * per-page, like every other multi-instance widget config.
 */
export interface PoiMapWidgetConfig {
  title: string
  rangeNm: number
  categories: string[]
  /** "map" fills the tile with just the map; "split" adds the ranked list. */
  layout: 'map' | 'split'
  showAis?: boolean
  showTrail?: boolean
}

/** Mirrors poiRadiusNmMin/Max in backend/poi_providers.go — the same bounds GET /api/poi enforces. */
export const POI_MAP_RANGE_NM_MIN = 0.5
export const POI_MAP_RANGE_NM_MAX = 25

export const CLUSTER_MAX_CORNERS = 4
export const CLUSTER_MAX_CORNER_ROWS = 4
export const CLUSTER_MAX_TELLTALES = 6
/** Mirrors clusterMaxFuelBars in backend/dashboard_pages.go. */
export const CLUSTER_MAX_FUEL_BARS = 4

export const LAMP_STRIP_MAX_LAMPS = 16
export const LAMP_LABEL_MAX_LENGTH = 12

/** Caps mirroring gaugeGroupMaxGauges / gaugeGroupTitleMaxLen in backend/dashboard_pages.go. */
export const GAUGE_GROUP_MAX_GAUGES = 12
/**
 * Named for the gauge group, but the backend checks the same cap against the
 * cluster and indicator strip titles too, so all three dialogs share it.
 */
export const GAUGE_GROUP_TITLE_MAX_LENGTH = 48
export const GAUGE_GROUP_MAX_COLUMNS = 4

export interface DashboardLayoutItem {
  id: DashboardWidgetId
  x: number
  y: number
  w: number
  h: number
  /** Present only on `embed:` widgets; the backend rejects it on any other id. */
  embed?: EmbedWidgetConfig
  /** Present only on `gauge:` widgets; the backend rejects it on any other id. */
  gauge?: GaugeWidgetConfig
  /** Present only on `gauge-group:` widgets; the backend rejects it elsewhere. */
  gaugeGroup?: GaugeGroupWidgetConfig
  /** Present only on `lamps:` widgets; the backend rejects it elsewhere. */
  lamps?: LampStripWidgetConfig
  /** Present only on `cluster:` widgets; the backend rejects it elsewhere. */
  cluster?: EngineClusterConfig
  /** Present only on `poi-map:` widgets; the backend rejects it elsewhere. */
  poiMap?: PoiMapWidgetConfig
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
 * Mirrors validatePoiMapWidget in backend/dashboard_pages.go: title length via
 * the gauge-group cap, range within GET /api/poi's own bounds, categories a
 * non-empty subset of the known catalog, layout in the closed set. Duplicated
 * rather than shared for the same reason isValidEmbedUrl is: the config
 * dialog needs synchronous feedback while the server must not trust the
 * client.
 */
export function isValidPoiMapConfig(config: PoiMapWidgetConfig): boolean {
  if (config.title.length > GAUGE_GROUP_TITLE_MAX_LENGTH) return false
  if (config.rangeNm < POI_MAP_RANGE_NM_MIN || config.rangeNm > POI_MAP_RANGE_NM_MAX) return false
  if (config.categories.length === 0) return false
  if (!config.categories.every((id) => POI_CATEGORY_IDS.includes(id))) return false
  if (config.layout !== 'map' && config.layout !== 'split') return false
  return true
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

export function isGaugeWidgetId(id: string): id is GaugeWidgetId {
  return id.startsWith(GAUGE_WIDGET_ID_PREFIX)
}

/** Mints a gauge id. Same reasoning as newEmbedWidgetId, including why not crypto.randomUUID. */
export function newGaugeWidgetId(existing: readonly DashboardLayoutItem[]): GaugeWidgetId {
  for (;;) {
    const token = `${Date.now().toString(36)}${Math.random().toString(36).slice(2, 10).padEnd(8, '0')}`
    const id: GaugeWidgetId = `${GAUGE_WIDGET_ID_PREFIX}${token}`
    if (!existing.some((w) => w.id === id)) return id
  }
}

export function isGaugeGroupWidgetId(id: string): id is GaugeGroupWidgetId {
  return id.startsWith(GAUGE_GROUP_WIDGET_ID_PREFIX)
}

/** Mints a gauge group id. Same reasoning as newEmbedWidgetId, including why not crypto.randomUUID. */
export function newGaugeGroupWidgetId(existing: readonly DashboardLayoutItem[]): GaugeGroupWidgetId {
  for (;;) {
    const token = `${Date.now().toString(36)}${Math.random().toString(36).slice(2, 10).padEnd(8, '0')}`
    const id: GaugeGroupWidgetId = `${GAUGE_GROUP_WIDGET_ID_PREFIX}${token}`
    if (!existing.some((w) => w.id === id)) return id
  }
}

export function isClusterWidgetId(id: string): id is ClusterWidgetId {
  return id.startsWith(CLUSTER_WIDGET_ID_PREFIX)
}

/** Mints a cluster id. Same reasoning as newEmbedWidgetId. */
export function newClusterWidgetId(existing: readonly DashboardLayoutItem[]): ClusterWidgetId {
  for (;;) {
    const token = `${Date.now().toString(36)}${Math.random().toString(36).slice(2, 10).padEnd(8, '0')}`
    const id: ClusterWidgetId = `${CLUSTER_WIDGET_ID_PREFIX}${token}`
    if (!existing.some((w) => w.id === id)) return id
  }
}

export function isLampStripWidgetId(id: string): id is LampStripWidgetId {
  return id.startsWith(LAMP_STRIP_WIDGET_ID_PREFIX)
}

/** Mints a lamp strip id. Same reasoning as newEmbedWidgetId. */
export function newLampStripWidgetId(existing: readonly DashboardLayoutItem[]): LampStripWidgetId {
  for (;;) {
    const token = `${Date.now().toString(36)}${Math.random().toString(36).slice(2, 10).padEnd(8, '0')}`
    const id: LampStripWidgetId = `${LAMP_STRIP_WIDGET_ID_PREFIX}${token}`
    if (!existing.some((w) => w.id === id)) return id
  }
}

export function isPoiMapWidgetId(id: string): id is PoiMapWidgetId {
  return id.startsWith(POI_MAP_WIDGET_ID_PREFIX)
}

/** Mints a poi map id. Same reasoning as newEmbedWidgetId, including why not crypto.randomUUID. */
export function newPoiMapWidgetId(existing: readonly DashboardLayoutItem[]): PoiMapWidgetId {
  for (;;) {
    const token = `${Date.now().toString(36)}${Math.random().toString(36).slice(2, 10).padEnd(8, '0')}`
    const id: PoiMapWidgetId = `${POI_MAP_WIDGET_ID_PREFIX}${token}`
    if (!existing.some((w) => w.id === id)) return id
  }
}

/** True for the widget kinds whose ids carry a per-instance token. */
export function isMultiInstanceWidgetId(id: string): boolean {
  return isEmbedWidgetId(id) || isGaugeWidgetId(id) || isGaugeGroupWidgetId(id)
    || isLampStripWidgetId(id) || isClusterWidgetId(id) || isPoiMapWidgetId(id)
}

/**
 * Copies a multi-instance widget under a fresh id, or returns null for a
 * builtin — builtins are one per page, so there is nothing to duplicate.
 *
 * The copy is deep: a shallow one would leave both tiles sharing the same
 * `gauges` array, and retargeting the copy would silently rewrite the original.
 */
export function duplicateWidget(
  widget: DashboardLayoutItem,
  existing: readonly DashboardLayoutItem[],
): DashboardLayoutItem | null {
  if (isPoiMapWidgetId(widget.id)) {
    return {
      ...widget,
      id: newPoiMapWidgetId(existing),
      poiMap: widget.poiMap ? { ...widget.poiMap, categories: [...widget.poiMap.categories] } : undefined,
    }
  }
  if (isClusterWidgetId(widget.id)) {
    return {
      ...widget,
      id: newClusterWidgetId(existing),
      cluster: widget.cluster ? structuredClone(widget.cluster) : undefined,
    }
  }
  if (isLampStripWidgetId(widget.id)) {
    return {
      ...widget,
      id: newLampStripWidgetId(existing),
      lamps: widget.lamps
        ? { ...widget.lamps, lamps: widget.lamps.lamps.map((lamp) => ({ ...lamp })) }
        : undefined,
    }
  }
  if (isGaugeGroupWidgetId(widget.id)) {
    return {
      ...widget,
      id: newGaugeGroupWidgetId(existing),
      gaugeGroup: widget.gaugeGroup ? cloneGaugeGroup(widget.gaugeGroup) : undefined,
    }
  }
  if (isGaugeWidgetId(widget.id)) {
    return {
      ...widget,
      id: newGaugeWidgetId(existing),
      gauge: widget.gauge ? cloneGauge(widget.gauge) : undefined,
    }
  }
  if (isEmbedWidgetId(widget.id)) {
    return {
      ...widget,
      id: newEmbedWidgetId(existing),
      embed: widget.embed ? { ...widget.embed } : undefined,
    }
  }
  return null
}

function cloneGauge(gauge: GaugeWidgetConfig): GaugeWidgetConfig {
  return { ...gauge, zones: gauge.zones?.map((zone) => ({ ...zone })) }
}

function cloneGaugeGroup(group: GaugeGroupWidgetConfig): GaugeGroupWidgetConfig {
  return { ...group, gauges: group.gauges.map(cloneGauge) }
}

/**
 * Retargets a group's paths in one pass — the point of duplicating a tile.
 * Copy "Port", replace `port` with `starboard`, and five gauges move to the
 * other engine without five rounds of retyping.
 *
 * Paths only. Labels are left alone deliberately: "Port RPM" is a two-word
 * edit, while a wrong bulk label rewrite is silent and easy to miss.
 */
export function rewriteGaugePaths(
  gauges: readonly GaugeWidgetConfig[],
  from: string,
  to: string,
): GaugeWidgetConfig[] {
  if (from === '') return gauges.map(cloneGauge)
  return gauges.map((gauge) => ({ ...cloneGauge(gauge), path: gauge.path.split(from).join(to) }))
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
  if (isPoiMapWidgetId(widget.id)) {
    return widget.poiMap?.title.trim() || 'Nearby'
  }
  if (isClusterWidgetId(widget.id)) {
    return widget.cluster?.title.trim() || 'Engine'
  }
  if (isLampStripWidgetId(widget.id)) {
    return widget.lamps?.title.trim() || 'Indicators'
  }
  if (isGaugeGroupWidgetId(widget.id)) {
    return widget.gaugeGroup?.title.trim() || 'Gauges'
  }
  if (isGaugeWidgetId(widget.id)) {
    return widget.gauge?.label.trim() || widget.gauge?.path || 'Gauge'
  }
  return DASHBOARD_WIDGET_LABELS[widget.id]
}
