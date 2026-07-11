// A small marker dot on the chart's primary series at the hovered/touched index.
export function ChartTooltipMarker({ x, y, color }: { x: number; y: number; color: string }) {
  return <circle pointerEvents="none" cx={x} cy={y} r="5" fill={color} stroke="white" strokeWidth="2" />
}

// The floating time / prominent-value / secondary-value bubble, positioned
// at the pointer's X (clamped so it can't run off either edge of the chart)
// right above the chart - never behind a touch point, never elsewhere on
// the page.
export function ChartTooltipBubble({ pixelX, time, primary, secondary, tertiary }: { pixelX: number; time: string; primary: string; secondary: string; tertiary?: string }) {
  return (
    <div
      className="pointer-events-none absolute top-1 z-10 -translate-x-1/2 whitespace-nowrap rounded-md border border-border/60 bg-card px-2.5 py-1.5 shadow-md"
      style={{ left: `clamp(58px, ${pixelX}px, calc(100% - 58px))` }}
    >
      <p className="text-[9px] font-medium uppercase tracking-wide text-muted-foreground">{time}</p>
      <p className="font-display text-base leading-tight text-foreground">{primary}</p>
      <p className="text-[10px] text-muted-foreground">{secondary}</p>
      {tertiary && <p className="text-[10px] text-muted-foreground">{tertiary}</p>}
    </div>
  )
}

// Shared "chart unavailable for this day/station" message, used by every
// hourly/tide chart card when there's no data to render.
export function ChartUnavailableMessage({ testId, message }: { testId: string; message: string }) {
  return (
    <p className="py-6 text-center text-xs text-muted-foreground" data-testid={testId}>
      {message}
    </p>
  )
}
