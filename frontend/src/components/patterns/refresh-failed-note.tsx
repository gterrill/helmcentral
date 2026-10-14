import { Button } from '@/components/ui/button'
import { cn } from '@/lib/utils'

export interface RefreshFailedNoteProps {
  /** The server's own words for why the refresh failed. */
  reason: string
  onRetry?: () => void
  className?: string
}

/**
 * The line a list or panel shows when it still holds good rows but the latest
 * refresh failed. The rows stay on screen, because the failure says nothing
 * about them; the error is reported next to them rather than hidden, and
 * replaces them only when there is nothing else to show.
 */
export function RefreshFailedNote({ reason, onRetry, className }: RefreshFailedNoteProps) {
  return (
    <p
      role="status"
      data-slot="refresh-failed-note"
      className={cn('flex flex-wrap items-center gap-x-2 text-xs text-muted-foreground', className)}
    >
      <span className="min-w-0">Couldn&apos;t refresh: {reason}</span>
      {onRetry && (
        <Button type="button" variant="ghost" size="sm" className="h-auto min-h-0 min-w-0 px-1 py-0 text-xs" onClick={onRetry}>
          Retry
        </Button>
      )}
    </p>
  )
}

/**
 * The same note as a corner pill that takes no layout space: for a screen
 * whose tiles are fitted to the space they have (the wall, the helm
 * dashboard), where a line in the flow would shift and clip them on every
 * blip.
 */
export function RefreshFailedOverlay({ reason, onRetry }: { reason: string; onRetry?: () => void }) {
  return (
    <div
      data-testid="refresh-failed-overlay"
      className={cn(
        'fixed bottom-2 left-2 z-20 rounded-md border border-border bg-background/90 px-3 py-1 shadow-md',
        onRetry ? 'pointer-events-auto' : 'pointer-events-none',
      )}
    >
      <RefreshFailedNote reason={reason} onRetry={onRetry} />
    </div>
  )
}

export interface LoadErrorProps {
  reason: string
  /** Whether rows from an earlier load are on screen beside this. */
  hasRows: boolean
  onRetry?: () => void
  className?: string
}

/**
 * The one rule for a list that failed to load: with rows on screen the failure
 * is a small note beside them; with none it is the full error, with Retry.
 */
export function LoadError({ reason, hasRows, onRetry, className }: LoadErrorProps) {
  if (hasRows) return <RefreshFailedNote reason={reason} onRetry={onRetry} className={className} />
  return (
    <div className={cn('flex flex-col items-start gap-2', className)}>
      <p role="alert" className="text-sm text-destructive">{reason}</p>
      {onRetry && (
        <Button type="button" variant="outline" size="sm" onClick={onRetry}>
          Retry
        </Button>
      )}
    </div>
  )
}
