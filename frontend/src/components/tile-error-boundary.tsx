import { Component, type ErrorInfo, type ReactNode } from 'react'

import { Tile } from '@/components/ui/tile'
import { widgetDisplayName, type DashboardLayoutItem } from '@/lib/dashboard-widgets'

interface TileErrorBoundaryProps {
  /** The widget this boundary wraps, for the fallback card's title. */
  widget: DashboardLayoutItem
  children: ReactNode
}

interface TileErrorBoundaryState {
  error: Error | null
}

/**
 * Catches a render error from exactly one widget rather than letting it
 * propagate to React's root: React 19 unmounts the whole app on an uncaught
 * render error, which on the unattended wall-display kiosk (ADR 0089) would
 * blank every page of the feed until someone physically reloads it. One
 * widget throwing degrades to a failed-tile card in its own grid slot; the
 * rest of the dashboard, and the kiosk rotation, keep going.
 *
 * Must be a class component — `getDerivedStateFromError` /
 * `componentDidCatch` have no hook equivalent.
 *
 * Callers key each instance by the widget's id (dashboard-bento-grid.tsx),
 * so swapping in a different widget at the same grid position mounts a
 * fresh boundary with no memory of the old widget's error, while an
 * ordinary re-render of the same widget (a config change, a prop update)
 * does not clear a real, still-current failure.
 */
export class TileErrorBoundary extends Component<TileErrorBoundaryProps, TileErrorBoundaryState> {
  state: TileErrorBoundaryState = { error: null }

  static getDerivedStateFromError(error: Error): TileErrorBoundaryState {
    return { error }
  }

  componentDidCatch(error: Error, errorInfo: ErrorInfo) {
    console.error(`Tile "${widgetDisplayName(this.props.widget)}" failed to render:`, error, errorInfo)
  }

  render() {
    const { error } = this.state
    if (error) {
      return (
        <Tile title={widgetDisplayName(this.props.widget)}>
          <div
            data-testid="tile-error-fallback"
            className="flex h-full flex-col items-center justify-center gap-1 text-center"
          >
            <p className="text-sm text-muted-foreground">This tile failed to render</p>
            <p className="text-xs text-muted-foreground">{error.message}</p>
          </div>
        </Tile>
      )
    }
    return this.props.children
  }
}
