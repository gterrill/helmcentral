import { Button } from '@/components/ui/button'

// A small marker dot on the chart's primary series at the hovered/touched index.
export function ChartTooltipMarker({ x, y, color }: { x: number; y: number; color: string }) {
  return <circle pointerEvents="none" cx={x} cy={y} r="5" fill={color} stroke="white" strokeWidth="2" />
}

// The floating time / prominent-value / secondary-value bubble, positioned
// at the pointer's X (clamped so it can't run off either edge of the chart)
// right above the chart - never behind a touch point, never elsewhere on
// the page.
//
// DESIGN.md's Still Digits Rule: every number here changes fastest exactly
// while the reader is scrubbing, so time/primary/secondary/tertiary all carry
// tabular-nums even though only primary is the bold headline value.
export function ChartTooltipBubble({ pixelX, time, primary, secondary, tertiary }: { pixelX: number; time: string; primary: string; secondary: string; tertiary?: string }) {
  return (
    <div
      className="pointer-events-none absolute top-1 z-10 whitespace-nowrap rounded-md border border-border/60 bg-card px-2.5 py-1.5 shadow-md"
      style={{ left: `${pixelX}px`, transform: 'translateX(-50%)' }}
    >
      <p className="text-[9px] font-medium uppercase tracking-wide tabular-nums text-muted-foreground">{time}</p>
      <p className="font-display text-base leading-tight tabular-nums text-foreground">{primary}</p>
      <p className="text-[10px] tabular-nums text-muted-foreground">{secondary}</p>
      {tertiary && <p className="text-[10px] tabular-nums text-muted-foreground">{tertiary}</p>}
    </div>
  )
}

/**
 * Shared "chart unavailable for this day/station" message, used by every
 * hourly/tide chart card when there's no data to render.
 *
 * Defaults to a loud amber treatment - border, wash and text-sm, mirroring
 * forecast-drawer.tsx's ForecastStaleBadge idiom - because PRODUCT.md's first
 * principle is "a missing feed is an alarm, not a blank tile": this must not
 * render smaller and fainter than the live prose sitting next to it.
 *
 * `tone="quiet"` is the one documented exception. tide-chart.tsx's "no
 * extremes fall inside this day's window" is a real tidal state, not an
 * absent feed, and a genuine tide fetch failure already gets its own louder
 * red-and-Retry treatment in forecast-tide-section.tsx - the amber alarm here
 * would be sounding for nothing.
 *
 * `onRetry`, when provided, renders a Retry button beside the message. Wire
 * it only where the message means an actual failed fetch (see
 * forecast-drawer.tsx's waveUnavailableDueToError) - a day that legitimately
 * has no data will still have none after a retry.
 */
export function ChartUnavailableMessage({
  testId,
  message,
  tone = 'alarm',
  onRetry,
}: {
  testId: string
  message: string
  tone?: 'alarm' | 'quiet'
  onRetry?: () => void
}) {
  if (tone === 'quiet') {
    return (
      <p className="py-6 text-center text-xs text-muted-foreground" data-testid={testId}>
        {message}
      </p>
    )
  }

  return (
    <div
      data-testid={testId}
      className="flex flex-col items-center gap-2 rounded-md border border-amber-500/40 bg-amber-500/10 py-6 text-center"
    >
      <p className="text-sm font-medium text-amber-600 dark:text-amber-400">{message}</p>
      {onRetry && (
        <Button type="button" size="sm" variant="outline" className="h-10 min-w-24" onClick={onRetry}>
          Retry
        </Button>
      )}
    </div>
  )
}
