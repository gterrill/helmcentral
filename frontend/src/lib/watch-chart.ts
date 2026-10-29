import type { WatchChartData } from '@/lib/watches'

/** One row of a panel's data: a time and, for each series, its value or null at a gap. */
export type PanelRow = { t: number } & Record<string, number | null>

/** A gap wider than this many median steps is left open. The PNG uses the same rule. */
const GAP_STEPS = 4

function medianStep(times: number[]): number {
  const steps = times.slice(1).map((t, i) => t - times[i]).sort((a, b) => a - b)
  return steps.length === 0 ? 0 : steps[Math.floor(steps.length / 2)]
}

/**
 * The rows a panel plots for the given series. Where a series has a gap wider
 * than four times its usual step, a row with a null for it sits in the middle of
 * the gap, so the line breaks there instead of joining the two sides.
 */
export function panelRows(chart: WatchChartData, indices: number[]): PanelRow[] {
  const rows = new Map<number, PanelRow>()
  const put = (t: number, key: string, v: number | null) => rows.set(t, { ...(rows.get(t) ?? { t }), [key]: v } as PanelRow)
  for (const i of indices) {
    const points = [...chart.series[i].points].sort((a, b) => a[0] - b[0])
    const limit = GAP_STEPS * medianStep(points.map((p) => p[0]))
    points.forEach(([t, v], k) => {
      put(t, `s${i}`, v)
      const next = points[k + 1]
      if (next && limit > 0 && next[0] - t > limit) put(t + (next[0] - t) / 2, `s${i}`, null)
    })
  }
  return [...rows.values()].sort((a, b) => a.t - b.t)
}
