import { useCallback, useMemo } from 'react'
import GridLayout, { WidthProvider, type LayoutItem } from 'react-grid-layout/legacy'
import 'react-grid-layout/css/styles.css'
import 'react-resizable/css/styles.css'
import '@/styles/dashboard-bento-grid.css'
import { GripVertical, X } from 'lucide-react'
import { cn } from '@/lib/utils'
import { BREAKPOINTS, useMinWidth } from '@/lib/breakpoints'
import { isGaugeWidgetId, isEmbedWidgetId, mergeLayoutGeometry, widgetDisplayName, type BuiltinWidgetId, type DashboardLayoutItem, type DashboardWidgetId } from '@/lib/dashboard-widgets'

const ReactGridLayout = WidthProvider(GridLayout)

// RGL's row geometry. Shared with the narrow CSS grid below, which derives each tile's
// minimum height from the same numbers so a tile keeps roughly its authored proportions.
const GRID_COLUMNS = 12
const GRID_ROW_HEIGHT = 32
const GRID_MARGIN = 16

// At half the 12-column grid or wider, a tile was authored as a "big" one and stays
// full-bleed in the two-column narrow layout instead of being squeezed into a half.
const NARROW_FULL_SPAN_MIN_W = GRID_COLUMNS / 2

// Embeds share one constraint rather than having per-id entries, since their ids
// carry a per-instance token (see ADR 0031).
const GAUGE_WIDGET_CONSTRAINTS = { minW: 2, minH: 4 }

const EMBED_WIDGET_CONSTRAINTS = { minW: 3, minH: 6 }

const WIDGET_CONSTRAINTS: Partial<Record<BuiltinWidgetId, { minW?: number; minH?: number }>> = {
  'vessel': { minW: 4, minH: 2 },
  'wind': { minW: 3, minH: 6 },
  'anchor-watch': { minW: 3, minH: 6 },
  'battery-power': { minW: 3, minH: 6 },
  'solar': { minW: 3, minH: 6 },
  'depth-tide': { minW: 2, minH: 4 },
  'position': { minW: 2, minH: 3 },
  'today-now': { minW: 2, minH: 4 },
  'rode-scope': { minW: 2, minH: 4 },
  'tanks': { minW: 2, minH: 3 },
  'route': { minW: 2, minH: 3 },
  'nearby-vessels': { minW: 2, minH: 3 },
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
  onLayoutSettle: (next: DashboardLayoutItem[]) => void
}

export function DashboardBentoGrid({ widgets, editing, renderWidget, onRemoveWidget, onLayoutSettle }: DashboardBentoGridProps) {
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
      ...(isEmbedWidgetId(w.id) ? EMBED_WIDGET_CONSTRAINTS : isGaugeWidgetId(w.id) ? GAUGE_WIDGET_CONSTRAINTS : WIDGET_CONSTRAINTS[w.id as BuiltinWidgetId]),
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
            className={cn(w.w >= NARROW_FULL_SPAN_MIN_W && 'sm:col-span-2')}
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
