import { useCallback, useEffect, useMemo, useState } from 'react'
import GridLayout, { WidthProvider, type LayoutItem } from 'react-grid-layout/legacy'
import 'react-grid-layout/css/styles.css'
import 'react-resizable/css/styles.css'
import '@/styles/dashboard-bento-grid.css'
import { GripVertical, X } from 'lucide-react'
import { cn } from '@/lib/utils'
import type { DashboardLayoutItem, DashboardWidgetId } from '@/lib/dashboard-widgets'

const ReactGridLayout = WidthProvider(GridLayout)

// Matches Tailwind's `lg` breakpoint (1024px), used elsewhere in App.tsx for the same
// mobile-stack-vs-desktop-grid split. A JS media query (not CSS-only hiding) so only one
// of the two layouts is ever mounted — rendering both simultaneously (toggled via CSS
// classes) leaves duplicate DOM nodes per widget, which is wasted render cost for real
// users and breaks any `getByText`-style single-match query in tests.
const DESKTOP_GRID_BREAKPOINT = 1024

function useIsDesktopGrid() {
  const [isDesktop, setIsDesktop] = useState(
    () => typeof window !== 'undefined' && window.innerWidth >= DESKTOP_GRID_BREAKPOINT,
  )

  useEffect(() => {
    const mql = window.matchMedia(`(min-width: ${DESKTOP_GRID_BREAKPOINT}px)`)
    const onChange = () => setIsDesktop(mql.matches)
    onChange()
    mql.addEventListener('change', onChange)
    return () => mql.removeEventListener('change', onChange)
  }, [])

  return isDesktop
}

const WIDGET_CONSTRAINTS: Partial<Record<DashboardWidgetId, { minW?: number; minH?: number }>> = {
  'vessel': { minW: 4, minH: 2 },
  'wind': { minW: 3, minH: 6 },
  'anchor-watch': { minW: 3, minH: 6 },
  'battery-power': { minW: 3, minH: 6 },
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
}

export interface DashboardBentoGridProps {
  widgets: DashboardLayoutItem[]
  editing: boolean
  renderWidget: (id: DashboardWidgetId) => React.ReactNode
  onRemoveWidget: (id: DashboardWidgetId) => void
  onLayoutSettle: (next: DashboardLayoutItem[]) => void
}

export function DashboardBentoGrid({ widgets, editing, renderWidget, onRemoveWidget, onLayoutSettle }: DashboardBentoGridProps) {
  const isDesktopGrid = useIsDesktopGrid()

  const rglLayout = useMemo<LayoutItem[]>(
    () => widgets.map((w) => ({ i: w.id, x: w.x, y: w.y, w: w.w, h: w.h, ...WIDGET_CONSTRAINTS[w.id] })),
    [widgets],
  )

  const commit = useCallback((layout: readonly LayoutItem[]) => {
    onLayoutSettle(
      Array.from(layout).map((l) => ({
        id: l.i as DashboardWidgetId,
        x: l.x,
        y: l.y,
        w: l.w,
        h: l.h,
      }))
    )
  }, [onLayoutSettle])

  if (!isDesktopGrid) {
    // Below `lg`, layout mode is unavailable — always show every configured widget as a
    // plain reflowed stack in (y, x) order, regardless of `editing`.
    return (
      <div className="flex flex-col gap-4">
        {[...widgets].sort((a, b) => a.y - b.y || a.x - b.x).map((w) => (
          <div key={w.id}>{renderWidget(w.id)}</div>
        ))}
      </div>
    )
  }

  return (
    <ReactGridLayout
      className="layout"
      layout={rglLayout}
      cols={12}
      rowHeight={32}
      margin={[16, 16]}
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
          {renderWidget(w.id)}
          {editing && (
            <>
              <button
                type="button"
                onClick={() => onRemoveWidget(w.id)}
                className="absolute -right-2 -top-2 z-10 inline-flex h-6 w-6 items-center justify-center rounded-full border bg-background text-muted-foreground shadow-sm hover:text-foreground"
                aria-label={`Remove ${w.id} widget`}
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
