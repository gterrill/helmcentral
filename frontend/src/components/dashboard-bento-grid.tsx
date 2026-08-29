import { useCallback, useMemo } from 'react'
import GridLayout, { WidthProvider, type LayoutItem } from 'react-grid-layout/legacy'
import 'react-grid-layout/css/styles.css'
import 'react-resizable/css/styles.css'
import '@/styles/dashboard-bento-grid.css'
import { Copy, GripVertical, X } from 'lucide-react'
import { cn } from '@/lib/utils'
import { BREAKPOINTS, useMinWidth } from '@/lib/breakpoints'
import { CLUSTER_CANVAS } from '@/lib/cluster-canvas'
import { isClusterWidgetId, isGaugeGroupWidgetId, isGaugeWidgetId, isEmbedWidgetId, isLampStripWidgetId, isMultiInstanceWidgetId, mergeLayoutGeometry, widgetDisplayName, type BuiltinWidgetId, type DashboardLayoutItem, type DashboardWidgetId } from '@/lib/dashboard-widgets'

const ReactGridLayout = WidthProvider(GridLayout)

// RGL's row geometry. Shared with the narrow CSS grid below, which derives each tile's
// minimum height from the same numbers so a tile keeps roughly its authored proportions.
const GRID_COLUMNS = 12
export const GRID_ROW_HEIGHT = 32
export const GRID_MARGIN = 16

/** What a row count is worth in pixels: n rows and the n-1 margins between them. */
export function gridPixelHeight(rows: number): number {
  return rows * GRID_ROW_HEIGHT + Math.max(0, rows - 1) * GRID_MARGIN
}

/** The fewest whole rows that will hold a given height. */
function rowsForHeight(px: number): number {
  return Math.ceil((px + GRID_MARGIN) / (GRID_ROW_HEIGHT + GRID_MARGIN))
}

// At half the 12-column grid or wider, a tile was authored as a "big" one and stays
// full-bleed in the two-column narrow layout instead of being squeezed into a half.
const NARROW_FULL_SPAN_MIN_W = GRID_COLUMNS / 2

// Embeds share one constraint rather than having per-id entries, since their ids
// carry a per-instance token (see ADR 0031).
const GAUGE_WIDGET_CONSTRAINTS = { minW: 2, minH: 4 }

const EMBED_WIDGET_CONSTRAINTS = { minW: 3, minH: 6 }

// A cluster needs the room a single gauge does not.
const GAUGE_GROUP_WIDGET_CONSTRAINTS = { minW: 3, minH: 6 }

// A ribbon is wide and short by nature.
const LAMP_STRIP_WIDGET_CONSTRAINTS = { minW: 3, minH: 2 }

// Tile header, its top padding, and the card's bottom padding: everything the
// cluster canvas sits inside. Measured rather than derived, since it comes out
// of the Card and CardHeader utility classes rather than a number this file
// could import.
const TILE_CHROME_H = 54 + 16

/**
 * Derived from the canvas rather than written down beside it. The number here
 * used to be nine rows against a comment claiming a 460x300 canvas, and it
 * outlived two rebuilds of the cluster: by the time the canvas settled at 228px
 * tall the floor was 120px above what the tile needed, and the tile could not be
 * resized down at all.
 *
 * The canvas only ever scales down (see useFitScale), so its design height is
 * the tallest the tile ever gets and a wider column costs no extra rows.
 */
const CLUSTER_WIDGET_CONSTRAINTS = {
  minW: 4,
  minH: rowsForHeight(TILE_CHROME_H + CLUSTER_CANVAS.height),
}

/**
 * A cluster carrying a fuel rail needs a wider column (ADR 0061). The rail adds
 * 126 to a 520-wide design, so at the bare floor of 4 the whole canvas scales
 * to about 0.6 and the corner labels drop under the legibility floor. Applied
 * per widget rather than raising the constant, so a cluster with no rail keeps
 * the width it has always had.
 */
export const CLUSTER_FUEL_MIN_W = 5

export { CLUSTER_WIDGET_CONSTRAINTS }

const WIDGET_CONSTRAINTS: Partial<Record<BuiltinWidgetId, { minW?: number; minH?: number }>> = {
  'vessel': { minW: 4, minH: 2 },
  'wind': { minW: 3, minH: 6 },
  'anchor-watch': { minW: 3, minH: 6 },
  'battery-power': { minW: 3, minH: 6 },
  'solar': { minW: 3, minH: 6 },
  'depth-tide': { minW: 2, minH: 4 },
  'position': { minW: 2, minH: 3 },
  'today-now': { minW: 2, minH: 4 },
  'tanks': { minW: 2, minH: 3 },
  'route': { minW: 2, minH: 3 },
  'nearby-vessels': { minW: 2, minH: 3 },
  'radar-targets': { minW: 2, minH: 3 },
  'alternator': { minW: 2, minH: 4 },
  'generator': { minW: 2, minH: 4 },
  'czone-switches': { minW: 2, minH: 3 },
  // The control pad needs finger-sized hold-to-confirm targets (ADR 0041).
  'autopilot': { minW: 4, minH: 6 },
}

export interface DashboardBentoGridProps {
  widgets: DashboardLayoutItem[]
  editing: boolean
  // Takes the whole layout item, not just the id: embed widgets carry their
  // per-instance config alongside their position.
  renderWidget: (widget: DashboardLayoutItem) => React.ReactNode
  onRemoveWidget: (id: DashboardWidgetId) => void
  // Only the token-id kinds can be duplicated; builtins are one per page.
  onDuplicateWidget: (id: DashboardWidgetId) => void
  onLayoutSettle: (next: DashboardLayoutItem[]) => void
}

export function DashboardBentoGrid({ widgets, editing, renderWidget, onRemoveWidget, onDuplicateWidget, onLayoutSettle }: DashboardBentoGridProps) {
  // Below `lg`, render a plain reflowed stack instead of the RGL grid — never both at once.
  // Toggling between them via CSS (rather than this JS media query) would mount both layouts
  // simultaneously, leaving duplicate DOM nodes per widget: wasted render cost for real users,
  // and it breaks any `getByText`-style single-match query in tests.
  const isDesktopGrid = useMinWidth(BREAKPOINTS.lg)

  const rglLayout = useMemo<LayoutItem[]>(
    () => widgets.map((w) => ({
      i: w.id,
      x: w.x,
      y: w.y,
      w: w.w,
      h: w.h,
      ...(isClusterWidgetId(w.id)
        ? { ...CLUSTER_WIDGET_CONSTRAINTS, ...(w.cluster?.fuel ? { minW: CLUSTER_FUEL_MIN_W } : {}) }
        : isLampStripWidgetId(w.id)
        ? LAMP_STRIP_WIDGET_CONSTRAINTS
        : isEmbedWidgetId(w.id)
        ? EMBED_WIDGET_CONSTRAINTS
        : isGaugeGroupWidgetId(w.id)
          ? GAUGE_GROUP_WIDGET_CONSTRAINTS
          : isGaugeWidgetId(w.id)
            ? GAUGE_WIDGET_CONSTRAINTS
            : WIDGET_CONSTRAINTS[w.id as BuiltinWidgetId]),
    })),
    [widgets],
  )

  const commit = useCallback((layout: readonly LayoutItem[]) => {
    onLayoutSettle(mergeLayoutGeometry(widgets, layout))
  }, [widgets, onLayoutSettle])

  if (!isDesktopGrid) {
    // Below `lg`, reflow the *same* persisted 12-column layout into a CSS grid — one
    // column on a phone, two from `sm` up (iPad portrait, where a single 900px-wide
    // column wasted the screen). Nothing extra is persisted, so ADR 0012's one-global-
    // layout constraint holds; see ADR 0032 for why RGL isn't reused at fewer columns.
    //
    // Layout mode stays unavailable regardless of `editing`: a narrow-view drag says
    // "put this third", which has no non-arbitrary mapping back to (x, y, w, h), and
    // the backend validator doesn't enforce X+W <= 12 — a bad write-back would be
    // persisted silently and corrupt the desktop layout.
    return (
      <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
        {[...widgets].sort((a, b) => a.y - b.y || a.x - b.x).map((w) => (
          <div
            key={w.id}
            // min-w-0 so a `1fr` track cannot be widened by its content. A
            // grid column is minmax(auto, 1fr) by default, and auto resolves to
            // min-content: the engine cluster lays out on a fixed 520px canvas
            // that it scales down to fit, so without this the track grew to 520,
            // the whole grid overflowed the viewport and the tile never scaled
            // at all because the width it measured was already 520.
            className={cn('min-w-0', w.w >= NARROW_FULL_SPAN_MIN_W && 'sm:col-span-2')}
            // The operator's sizing intent as a floor, not a fixed height: text wraps
            // more at phone width, so a height copied straight from the desktop grid
            // would clip. Mirrors RGL's own row maths (rowHeight + margin).
            style={{ minHeight: w.h * GRID_ROW_HEIGHT + (w.h - 1) * GRID_MARGIN }}
          >
            {renderWidget(w)}
          </div>
        ))}
      </div>
    )
  }

  return (
    <ReactGridLayout
      className="layout"
      layout={rglLayout}
      cols={GRID_COLUMNS}
      rowHeight={GRID_ROW_HEIGHT}
      margin={[GRID_MARGIN, GRID_MARGIN]}
      containerPadding={[0, 0]}
      isDraggable={editing}
      isResizable={editing}
      draggableHandle=".bento-drag-handle"
      onDragStop={commit}
      onResizeStop={commit}
    >
      {widgets.map((w) => (
        <div
          key={w.id}
          className={cn('relative rounded-xl', editing && 'select-none outline-dashed outline-2 outline-primary/30')}
        >
          <div className="h-full [&>*]:h-full">{renderWidget(w)}</div>
          {editing && (
            <>
              <button
                type="button"
                onClick={() => onRemoveWidget(w.id)}
                className="absolute -right-2 -top-2 z-10 inline-flex h-6 w-6 items-center justify-center rounded-full border bg-background text-muted-foreground shadow-sm hover:text-foreground"
                aria-label={`Remove ${widgetDisplayName(w)} widget`}
              >
                <X className="h-3.5 w-3.5" />
              </button>
              {isMultiInstanceWidgetId(w.id) && (
                <button
                  type="button"
                  onClick={() => onDuplicateWidget(w.id)}
                  className="absolute -right-2 top-6 z-10 inline-flex h-6 w-6 items-center justify-center rounded-full border bg-background text-muted-foreground shadow-sm hover:text-foreground"
                  aria-label={`Duplicate ${widgetDisplayName(w)} widget`}
                >
                  <Copy className="h-3 w-3" />
                </button>
              )}
              <div className="bento-drag-handle absolute -left-2 -top-2 z-10 inline-flex h-6 w-6 cursor-grab items-center justify-center rounded-full border bg-background text-muted-foreground shadow-sm active:cursor-grabbing">
                <GripVertical className="h-3.5 w-3.5" />
              </div>
            </>
          )}
        </div>
      ))}
    </ReactGridLayout>
  )
}
