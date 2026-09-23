/**
 * Pure pixel-geometry constants and helpers for the wall display's sea-state
 * chart (ADR 0092, ADR 0125) - deliberately recharts-free.
 *
 * sea-state-chart.tsx statically imports recharts (~395 KB raw), so anything
 * that needs to import from it eagerly (rather than through the lazy()
 * boundary forecast-conditions-tile.tsx already puts around the chart
 * itself) drags recharts into every route's startup bundle, including
 * /kiosk pages that never render this tile at all - manualChunks
 * (vite.config.ts's dedicated `chart-vendor` group) only keeps recharts out
 * of the entry chunk as long as nothing eager statically imports a module
 * that imports recharts; it does not matter that the *symbols* being
 * imported (these constants/functions) never touch recharts themselves.
 * This module exists so forecast-conditions-tile.tsx (and any test) can use
 * the chart's geometry without ever importing sea-state-chart.tsx outside
 * its lazy() boundary. See that file's own imports of these same exports
 * for the recharts-specific half of this geometry (the <ComposedChart>
 * itself).
 */

const HOURS_PER_DAY = 24
export const SEA_STATE_DAY_COUNT = 5

// The barbs and wave arrows draw at this fraction of the forecast drawer's
// size. The drawer doubled them for reading at arm's length on a tall plot;
// in the wall tile they share a short strip with the day cards above, and at
// full size they crowded the plot and read louder than the lines themselves.
export const SEA_STATE_GLYPH_SCALE = 0.6

// top raised from 24 so the barb row (barbY, half of margin.top) sits clear
// of the top y-axis tick text now that the tick carries the unit ("50 kn")
// rather than a separate corner label; bottom raised to match so the
// wave-arrow row keeps its own clearance from the day-name ticks below.
// This is the object passed to <ComposedChart>'s own `margin` prop and to
// each <YAxis>'s `width` - it is NOT, on its own, how far the actual
// plotted x-range is inset from the chart's edges (see SEA_STATE_PLOT_INSET
// immediately below for that number).
export const SEA_STATE_CHART_MARGIN = { top: 40, right: 40, bottom: 34, left: 40 }

// Recharts adds each <YAxis>'s own `width` ON TOP of <ComposedChart>'s
// `margin` rather than inside it - confirmed against a rendered chart's own
// output (a Line's first data point at index 0 lands at x=84.33 for a
// 1200px-wide chart, which is margin.left(40) + Y_AXIS_WIDTH(40) + a
// half-hour-slot offset, not margin.left(40) alone). SEA_STATE_CHART_MARGIN
// alone therefore understates the real horizontal inset by exactly one
// y-axis width on each side; SEA_STATE_PLOT_INSET is the corrected number
// every horizontal pixel calculation in sea-state-chart.tsx (and
// forecast-conditions-tile.tsx's card-row padding) actually has to use.
const Y_AXIS_WIDTH = 40
export const SEA_STATE_PLOT_INSET = {
  left: SEA_STATE_CHART_MARGIN.left + Y_AXIS_WIDTH,
  right: SEA_STATE_CHART_MARGIN.right + Y_AXIS_WIDTH,
}

export function plotWidthFor(width: number): number {
  return Math.max(1, width - SEA_STATE_PLOT_INSET.left - SEA_STATE_PLOT_INSET.right)
}

/**
 * Pixel x for the centre of hour-slot `index`, across a fixed `dayCount`
 * day columns rather than the series' own length - this is what makes "pad
 * the x domain to `dayCount * 24` instead of stretching fewer real days
 * across the full width" (see sea-state-chart.tsx's SeaStateChartProps.dayCount)
 * an exact, checkable mapping rather than an eyeballed one: day `d` spans
 * pixels `[xForIndex(d*24 - 0.5, width), xForIndex((d+1)*24 - 0.5, width)]`
 * regardless of how far the real series data actually reaches into that
 * span.
 */
export function xForIndex(index: number, width: number, dayCount: number = SEA_STATE_DAY_COUNT): number {
  const totalPoints = dayCount * HOURS_PER_DAY
  return SEA_STATE_PLOT_INSET.left + ((index + 0.5) / totalPoints) * plotWidthFor(width)
}

/**
 * Pixel x for the boundary between day `day - 1` and day `day` (1..dayCount
 * - 1) - algebraically `xForIndex(day * 24 - 0.5, width, dayCount)`, kept as
 * its own function because it's the number forecast-conditions-tile.tsx's
 * card-row gaps have to land under, and a day/dayCount fraction of the
 * inset plot width is the more direct way to say that than routing through
 * an hour index.
 */
export function xForDayBoundary(day: number, width: number, dayCount: number = SEA_STATE_DAY_COUNT): number {
  return SEA_STATE_PLOT_INSET.left + (day / dayCount) * plotWidthFor(width)
}
