import { useCallback, useRef, useState, type MutableRefObject } from 'react'

import { CLUSTER_DEFAULT_SIZE, raiseGaugeHeight } from '@/components/dashboard-bento-grid'
import {
  duplicateWidget,
  isClusterWidgetId,
  isEmbedWidgetId,
  isGaugeGroupWidgetId,
  isGaugeWidgetId,
  isLampStripWidgetId,
  isPoiMapWidgetId,
  newClusterWidgetId,
  newEmbedWidgetId,
  newGaugeGroupWidgetId,
  newGaugeWidgetId,
  newLampStripWidgetId,
  newPoiMapWidgetId,
  type DashboardLayoutItem,
  type DashboardWidgetId,
  type EmbedWidgetConfig,
  type EngineClusterConfig,
  type GaugeGroupWidgetConfig,
  type GaugeWidgetConfig,
  type LampStripWidgetConfig,
  type PoiMapWidgetConfig,
} from '@/lib/dashboard-widgets'
import { POI_CATEGORY_IDS } from '@/lib/poi'

/**
 * The tile being configured (ADR 0012, 0049, 0054, 0109).
 *
 * A new tile exists only as a draft until the operator saves it: the backend
 * rejects an embed without a URL, a gauge without a path and an empty gauge
 * group, so a half-made tile must never reach the page. Cancelling therefore
 * just drops the draft. Opening a saved tile's gear uses the same draft, which
 * is why saving either replaces the tile's config in place or appends the
 * draft.
 *
 * Only one tile dialog is open at a time, so there is one draft.
 */

export type TileKind = 'gauge' | 'gaugeGroup' | 'cluster' | 'lamps' | 'embed' | 'poiMap'

/** The config each kind keeps, under the key the layout item carries it by. */
export interface TileConfigs {
  gauge: GaugeWidgetConfig
  gaugeGroup: GaugeGroupWidgetConfig
  cluster: EngineClusterConfig
  lamps: LampStripWidgetConfig
  embed: EmbedWidgetConfig
  poiMap: PoiMapWidgetConfig
}

type Existing = readonly DashboardLayoutItem[]

/** Which kind a tile id belongs to, or null for a built-in tile. */
export function tileKindOf(id: DashboardWidgetId): TileKind | null {
  if (isClusterWidgetId(id)) return 'cluster'
  if (isLampStripWidgetId(id)) return 'lamps'
  if (isGaugeGroupWidgetId(id)) return 'gaugeGroup'
  if (isGaugeWidgetId(id)) return 'gauge'
  if (isEmbedWidgetId(id)) return 'embed'
  if (isPoiMapWidgetId(id)) return 'poiMap'
  return null
}

function lowestEdge(existing: Existing): number {
  return existing.reduce((max, w) => Math.max(max, w.y + w.h), 0)
}

/** Left edge, below every tile on the page, so a new tile never lands on another. */
export function placeBelow(item: Omit<DashboardLayoutItem, 'x' | 'y'>, existing: Existing): DashboardLayoutItem {
  return { ...item, x: 0, y: lowestEdge(existing) }
}

const blankGauge = (): GaugeWidgetConfig => ({ path: '', label: '', display: 'numeric', quantity: 'raw', unit: 'raw' })

// Each kind's own starting point: its id, footprint and blank config. The
// footprints differ on purpose (a chart wants room, a ribbon spans the page).
type Start = Omit<DashboardLayoutItem, 'x' | 'y'>
const START: { [K in TileKind]: (existing: Existing) => Start } = {
  // Wider and taller than the built-in default: a chart needs the room.
  embed: (e) => ({ id: newEmbedWidgetId(e), w: 6, h: 8, embed: { title: '', url: '' } }),
  gauge: (e) => ({ id: newGaugeWidgetId(e), w: 3, h: 6, gauge: blankGauge() }),
  // The footprint of the built-in Alternator tile, so a pair sits side by side.
  gaugeGroup: (e) => ({
    id: newGaugeGroupWidgetId(e), w: 4, h: 7,
    gaugeGroup: { title: '', gauges: [blankGauge()] },
  }),
  // Tall: the canvas is 460x300 plus the tile chrome.
  cluster: (e) => ({
    id: newClusterWidgetId(e), ...CLUSTER_DEFAULT_SIZE,
    cluster: {
      title: '',
      ring: { ...blankGauge(), label: 'RPM', display: 'radial' },
      centre: { ...blankGauge(), label: 'Hours' },
      corners: [],
    },
  }),
  // Wide and short: a ribbon spans the page rather than occupying a cell.
  lamps: (e) => ({
    id: newLampStripWidgetId(e), w: 12, h: 3,
    lamps: { title: 'Status', lamps: [{ path: '', label: '' }], showCheck: true },
  }),
  // Wide (12x7): the split layout needs the room; a narrower map-only tile is a
  // resize away.
  poiMap: (e) => ({
    id: newPoiMapWidgetId(e), w: 12, h: 7,
    poiMap: { title: '', rangeNm: 5, categories: [...POI_CATEGORY_IDS], layout: 'split', showAis: true, showTrail: false },
  }),
}

/** A blank tile of this kind, placed below the page. Nothing is written. */
export function newTileDraft(kind: TileKind, existing: Existing): DashboardLayoutItem {
  return placeBelow(START[kind](existing), existing)
}

// Puts a config on a tile. A gauge is also raised to the height its display
// needs, since changing the display in the dialog must not leave it cut off.
function withConfig<K extends TileKind>(item: DashboardLayoutItem, kind: K, config: TileConfigs[K]): DashboardLayoutItem {
  const next = { ...item, [kind]: config }
  if (kind === 'gauge') next.h = raiseGaugeHeight(item.h, (config as GaugeWidgetConfig).display)
  return next
}

// A dialog saves its own kind's config. A mismatch is a wiring bug, so it
// stops here rather than writing a config under the wrong key.
function assertKind(draft: DashboardLayoutItem, kind: TileKind): void {
  if (tileKindOf(draft.id) !== kind) throw new Error(`Tile ${draft.id} is not a ${kind} tile`)
}

/**
 * The page's tiles after saving `draft` with `config`: the tile's config is
 * replaced in place when it is already on the page. A draft that is not is
 * appended below the page as it stands now, which may have grown since the
 * dialog opened.
 */
export function saveTileDraft<K extends TileKind>(
  existing: Existing,
  draft: DashboardLayoutItem,
  kind: K,
  config: TileConfigs[K],
): DashboardLayoutItem[] {
  assertKind(draft, kind)
  if (existing.some((w) => w.id === draft.id)) {
    return existing.map((w) => (w.id === draft.id ? withConfig(w, kind, config) : w))
  }
  return [...existing, placeBelow(withConfig(draft, kind, config), existing)]
}

/**
 * Save As from inside a dialog: the edits land on a new tile below the page
 * and the original stays as it was when the dialog opened. A draft that was
 * never saved gives the one tile carrying the edits, not a saved draft plus a
 * copy.
 */
export function duplicateTileDraft<K extends TileKind>(
  existing: Existing,
  draft: DashboardLayoutItem,
  kind: K,
  config: TileConfigs[K],
): DashboardLayoutItem[] {
  assertKind(draft, kind)
  const copy = duplicateWidget(withConfig(draft, kind, config), existing)
  if (!copy) throw new Error(`Tile ${draft.id} cannot be copied`)
  return [...existing, placeBelow(copy, existing)]
}

/**
 * Copies a saved tile below the page. Null for a built-in tile, which is one
 * per page, or an id that is not on the page.
 */
export function copyTileBelow(
  existing: Existing,
  id: DashboardWidgetId,
): { widgets: DashboardLayoutItem[]; copy: DashboardLayoutItem } | null {
  const source = existing.find((w) => w.id === id)
  if (!source) return null
  const copy = duplicateWidget(source, existing)
  if (!copy) return null
  const placed = placeBelow(copy, existing)
  return { widgets: [...existing, placed], copy: placed }
}

export interface TileDraftHost {
  /** The page the tiles belong to. Read at the call, so the callbacks never change. */
  pageRef: MutableRefObject<{ id: string } | null>
  widgetsRef: MutableRefObject<DashboardLayoutItem[]>
  /** Resolves null when the write failed; the caller has already toasted it. */
  updatePage: (id: string, patch: { widgets: DashboardLayoutItem[] }) => Promise<object | null>
}

// One open dialog's draft. Each opening is a new entry, so a callback made for
// an earlier dialog can tell it is no longer the one on screen.
interface Entry { item: DashboardLayoutItem }

/**
 * Holds the tile whose dialog is open. A dialog can finish late: the cluster
 * dialog saves profile zones first, and Duplicate waits for the same. So every
 * finish (save, duplicate, cancel) applies only to the draft it began with. A
 * dialog cancelled or replaced meanwhile gets nothing written and nothing
 * closed.
 */
export function useTileDraft({ pageRef, widgetsRef, updatePage }: TileDraftHost) {
  const [entry, setEntry] = useState<Entry | null>(null)
  const currentRef = useRef<Entry | null>(null)
  // The entry whose write is out. A second Save, or the dialog closing itself
  // after Save, must not act on it before the answer lands.
  const writingRef = useRef<Entry | null>(null)

  const show = useCallback((item: DashboardLayoutItem) => {
    const next = { item }
    currentRef.current = next
    setEntry(next)
  }, [])

  const close = useCallback((from: Entry) => {
    if (currentRef.current !== from || writingRef.current === from) return
    currentRef.current = null
    setEntry(null)
  }, [])

  const write = useCallback(async (widgets: DashboardLayoutItem[]): Promise<boolean> => {
    const page = pageRef.current
    if (!page) return false
    return (await updatePage(page.id, { widgets })) !== null
  }, [pageRef, updatePage])

  // Writes what `build` gives for the draft, then closes it. A failed write
  // leaves the draft open so the operator can retry.
  const finish = async (from: Entry | null, build: (item: DashboardLayoutItem) => DashboardLayoutItem[]) => {
    if (!from || currentRef.current !== from || writingRef.current === from) return
    writingRef.current = from
    let ok = false
    try {
      ok = await write(build(from.item))
    } finally {
      writingRef.current = null
    }
    if (ok) close(from)
  }

  const addTile = useCallback((kind: TileKind) => {
    show(newTileDraft(kind, widgetsRef.current))
  }, [show, widgetsRef])

  const openTile = useCallback((widget: DashboardLayoutItem) => {
    if (tileKindOf(widget.id)) show(widget)
  }, [show])

  const duplicateTileById = useCallback((id: DashboardWidgetId) => {
    const result = copyTileBelow(widgetsRef.current, id)
    if (!result) return
    void write(result.widgets)
    show(result.copy)
  }, [show, widgetsRef, write])

  return {
    draft: entry?.item ?? null,
    /** The draft when it is of this kind, for that kind's own dialog. */
    draftOfKind: (kind: TileKind) => (entry && tileKindOf(entry.item.id) === kind ? entry.item : null),
    addTile,
    openTile,
    duplicateTileById,
    cancelTile: () => { if (entry) close(entry) },
    saveTile: <K extends TileKind>(kind: K, config: TileConfigs[K]) =>
      finish(entry, (item) => saveTileDraft(widgetsRef.current, item, kind, config)),
    /** Save As from the dialog (see duplicateTileDraft). */
    duplicateTile: <K extends TileKind>(kind: K, config: TileConfigs[K]) =>
      finish(entry, (item) => duplicateTileDraft(widgetsRef.current, item, kind, config)),
  }
}
