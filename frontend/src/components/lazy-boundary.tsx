import { Component, type ReactNode } from 'react'

/** How browsers word a dynamic import or its CSS that would not load. */
const CHUNK_ERROR = /dynamically imported module|importing a module script failed|failed to load module script|unable to preload css|loading (css )?chunk/i

export function isChunkLoadError(error: unknown): boolean {
  return error instanceof Error && CHUNK_ERROR.test(error.message)
}

interface LazyBoundaryProps {
  children: ReactNode
  /** Called once when a lazy chunk fails to load; the caller re-checks the build. */
  onChunkError: (error: unknown) => void
  /** Clears the failure when it changes, so another screen gets its own try. */
  resetKey?: string | null
  /** Takes the place of the screen. Defaults to a small inline message. */
  fallback?: ReactNode
}

interface LazyBoundaryState {
  error: Error | null
  resetKey: string | null | undefined
}

/**
 * Stands between the lazy screens (panels, drawers, sheets) and the rest of
 * the shell. A chunk that will not load, typically because a deploy replaced
 * it, is shown in place of the screen and reported, instead of taking down
 * everything above it, the toast layer included. Any other error is not this
 * boundary's to hide and goes on up unchanged.
 */
export class LazyBoundary extends Component<LazyBoundaryProps, LazyBoundaryState> {
  state: LazyBoundaryState = { error: null, resetKey: this.props.resetKey }

  static getDerivedStateFromError(error: Error): Partial<LazyBoundaryState> {
    return { error }
  }

  static getDerivedStateFromProps(props: LazyBoundaryProps, state: LazyBoundaryState): Partial<LazyBoundaryState> | null {
    return props.resetKey !== state.resetKey ? { error: null, resetKey: props.resetKey } : null
  }

  componentDidCatch(error: Error) {
    if (isChunkLoadError(error)) this.props.onChunkError(error)
  }

  render() {
    const { error } = this.state
    if (error === null) return this.props.children
    if (!isChunkLoadError(error)) throw error
    return this.props.fallback ?? (
      <div role="alert" className="p-4 text-sm text-muted-foreground">
        This screen couldn&apos;t be loaded.
      </div>
    )
  }
}
