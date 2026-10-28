import type { TelemetryChartSeries } from '@/lib/mate-cards'
import type { TelemetrySeriesBody } from '@/hooks/use-telemetry-series'

const HOUR_MS = 3_600_000
export const DAY_MS = 24 * HOUR_MS

function parts(ms: number, timeZone: string, opts: Intl.DateTimeFormatOptions): Intl.DateTimeFormatPart[] {
  return new Intl.DateTimeFormat('en-GB', { timeZone, hourCycle: 'h23', ...opts }).formatToParts(new Date(ms))
}

function pick(ps: Intl.DateTimeFormatPart[], type: string): string {
  return ps.find((p) => p.type === type)?.value ?? ''
}

/** "12–19 Oct", "28 Sep – 3 Oct", or "12 Oct" for a single day: the local
 * dates of the two boundaries. */
export function formatRange(from: string, to: string, timeZone: string): string {
  const f = Date.parse(from)
  const t = Date.parse(to)
  const a = parts(f, timeZone, { day: 'numeric', month: 'short' })
  const b = parts(t, timeZone, { day: 'numeric', month: 'short' })
  const [ad, am, bd, bm] = [pick(a, 'day'), pick(a, 'month'), pick(b, 'day'), pick(b, 'month')]
  if (ad === bd && am === bm) return `${ad} ${am}`
  if (am === bm) return `${ad}–${bd} ${bm}`
  return `${ad} ${am} – ${bd} ${bm}`
}

/** Offset of `timeZone` from UTC at an instant, in ms. */
function offsetMs(ms: number, timeZone: string): number {
  const ps = parts(ms, timeZone, { year: 'numeric', month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit', second: '2-digit' })
  const asUtc = Date.UTC(+pick(ps, 'year'), +pick(ps, 'month') - 1, +pick(ps, 'day'), +pick(ps, 'hour'), +pick(ps, 'minute'), +pick(ps, 'second'))
  return asUtc - Math.floor(ms / 1000) * 1000
}

/** The instant a local `YYYY-MM-DD` starts in `timeZone`. */
export function zonedDayStart(ymd: string, timeZone: string): number {
  const [y, m, d] = ymd.split('-').map(Number)
  const guess = Date.UTC(y, m - 1, d)
  const first = guess - offsetMs(guess, timeZone)
  return guess - offsetMs(first, timeZone)
}

/** The local `YYYY-MM-DD` of an instant in `timeZone`. */
export function zonedYmd(ms: number, timeZone: string): string {
  const ps = parts(ms, timeZone, { year: 'numeric', month: '2-digit', day: '2-digit' })
  return `${pick(ps, 'year')}-${pick(ps, 'month')}-${pick(ps, 'day')}`
}

export interface TimeRow {
  t: number
  /** `s{i}` is series i's mean; `b{i}` its [min, max] band, or null. */
  [seriesKey: string]: number | null | [number, number]
}

export const seriesKey = (i: number) => `s${i}`
export const bandKey = (i: number) => `b${i}`

/** A card's series grouped into its stacked panels, in panel order. */
export function panelsOf(series: Pick<TelemetryChartSeries, 'panel'>[]): { panel: number; indices: number[] }[] {
  const by = new Map<number, number[]>()
  series.forEach((s, i) => by.set(s.panel, [...(by.get(s.panel) ?? []), i]))
  return [...by.entries()].sort((a, b) => a[0] - b[0]).map(([panel, indices]) => ({ panel, indices }))
}

export type AngleKind = 'bearing' | 'relative'

/**
 * The y axis of an angle panel. A bearing runs 0 to 360 with north at both
 * ends, so a wrap through north is a jump between dots, not a line across the
 * chart. A relative angle runs -180 to 180.
 */
export function angleAxis(kind: AngleKind): { domain: [number, number]; ticks: number[] } {
  return kind === 'bearing'
    ? { domain: [0, 360], ticks: [0, 90, 180, 270, 360] }
    : { domain: [-180, 180], ticks: [-180, -90, 0, 90, 180] }
}

/** The kind of angle a panel draws: set only when every series in it is an angle of one kind. */
export function panelAngleKind(bodies: (TelemetrySeriesBody | undefined)[], indices: number[]): AngleKind | null {
  const kinds = indices.map((i) => {
    const a = bodies.find((b) => b?.series[i]?.angle)?.series[i]?.angle
    return a === 'bearing' || a === 'relative' ? a : null
  })
  return kinds.length > 0 && kinds.every((k) => k !== null && k === kinds[0]) ? kinds[0] : null
}

/** The unit series i is drawn in: what the server says for an angle, the card's own otherwise. */
export function drawnUnit(series: Pick<TelemetryChartSeries, 'unit'>[], bodies: (TelemetrySeriesBody | undefined)[], i: number): string {
  return bodies.find((b) => b?.series[i]?.angle)?.series[i]?.unit || series[i].unit
}

export function angleTickLabel(kind: AngleKind, v: number): string {
  if (kind === 'relative') return String(v)
  return ({ 0: 'N', 90: 'E', 180: 'S', 270: 'W', 360: 'N' } as Record<number, string>)[v] ?? String(v)
}

function bucketMs(body: TelemetrySeriesBody): number {
  const m = /^(\d+)(s|m|h|d)$/.exec(body.bucket)
  if (!m) return 0
  return Number(m[1]) * { s: 1000, m: 60_000, h: HOUR_MS, d: DAY_MS }[m[2] as 's' | 'm' | 'h' | 'd']
}

/**
 * One row per bucket for a time chart. The line breaks (a null row) at every
 * server-reported gap and wherever two neighbouring points are further apart
 * than two buckets, so recharts never draws across missing data.
 */
export function buildTimeRows(body: TelemetrySeriesBody, nSeries: number): TimeRow[] {
  const step = bucketMs(body)
  const rows = new Map<number, TimeRow>()
  const row = (t: number): TimeRow => {
    let r = rows.get(t)
    if (!r) {
      r = { t }
      for (let i = 0; i < nSeries; i++) {
        r[seriesKey(i)] = null
        r[bandKey(i)] = null
      }
      rows.set(t, r)
    }
    return r
  }
  const kept = body.series.slice(0, nSeries).map((s) => ({
    pts: s.points.map((p) => ({ t: Date.parse(p.t), mean: p.mean, min: p.min, max: p.max })).sort((a, b) => a.t - b.t),
    gaps: s.gaps.map((g) => ({ from: Date.parse(g.from), to: Date.parse(g.to) })),
  }))
  // breaks[i] is every timestamp at which series i is deliberately null.
  const breaks = kept.map(() => new Set<number>())
  kept.forEach(({ pts, gaps }, i) => {
    const key = seriesKey(i)
    pts.forEach((p, j) => {
      const r = row(p.t)
      r[key] = p.mean
      if (p.min !== undefined && p.max !== undefined) r[bandKey(i)] = [p.min, p.max]
      const next = pts[j + 1]
      if (next && step > 0 && next.t - p.t > step * 2) {
        row(p.t + step)[key] = null
        breaks[i].add(p.t + step)
      }
    })
    for (const g of gaps) {
      if (!Number.isNaN(g.from)) {
        row(g.from)[key] = null
        breaks[i].add(g.from)
      }
    }
  })
  const out = [...rows.values()].sort((a, b) => a.t - b.t)
  // A row made for one series' break must not cut another series that runs
  // through that instant: where it has points on both sides, close together
  // and outside any gap of its own, it continues on the straight line it
  // would have been drawn along. An angle is never continued this way: the
  // line between 350 and 10 degrees passes through 180, a direction nothing
  // measured, so it exists only at its own timestamps.
  kept.forEach(({ pts, gaps }, i) => {
    if (body.series[i]?.angle) return
    const key = seriesKey(i)
    for (const r of out) {
      if (r[key] !== null || breaks[i].has(r.t)) continue
      if (gaps.some((g) => r.t >= g.from && r.t <= g.to)) continue
      const after = pts.findIndex((p) => p.t > r.t)
      if (after <= 0) continue
      const a = pts[after - 1]
      const b = pts[after]
      if (step > 0 && b.t - a.t > step * 2) continue
      const f = (r.t - a.t) / (b.t - a.t)
      r[key] = a.mean + (b.mean - a.mean) * f
      if (a.min !== undefined && a.max !== undefined && b.min !== undefined && b.max !== undefined) {
        r[bandKey(i)] = [a.min + (b.min - a.min) * f, a.max + (b.max - a.max) * f]
      }
    }
  })
  return out
}

export interface XYResult {
  points: { x: number; y: number }[]
  paired: number
  total: number
}

/** Join two series on identical bucket `t`. A bucket where either is missing
 * is dropped and counted: `total` is every distinct bucket either has. */
export function pairXY(body: TelemetrySeriesBody, xi: number, yi: number): XYResult {
  const xs = new Map((body.series[xi]?.points ?? []).map((p) => [p.t, p.mean]))
  const ys = new Map((body.series[yi]?.points ?? []).map((p) => [p.t, p.mean]))
  const all = new Set([...xs.keys(), ...ys.keys()])
  const points: { x: number; y: number }[] = []
  for (const t of all) {
    const x = xs.get(t)
    const y = ys.get(t)
    if (x !== undefined && y !== undefined) points.push({ x, y })
  }
  return { points, paired: points.length, total: all.size }
}

/** Y-axis domain [min, max] across every range, padded a little. */
export function extent(values: number[]): [number, number] {
  if (values.length === 0) return [0, 1]
  let lo = Math.min(...values)
  let hi = Math.max(...values)
  if (lo === hi) {
    lo -= 1
    hi += 1
  }
  const pad = (hi - lo) * 0.05
  return [lo - pad, hi + pad]
}

/** Every plotted value of series i across the ranges. With `band`, the min and
 * max of each bucket too, so the axis reaches the gusts and not only the mean. */
export function seriesValues(bodies: (TelemetrySeriesBody | undefined)[], i: number, band = false): number[] {
  return bodies.flatMap(
    (b) =>
      b?.series[i]?.points.flatMap((p) => (band ? [p.mean, ...(p.min !== undefined ? [p.min] : []), ...(p.max !== undefined ? [p.max] : [])] : [p.mean])) ?? [],
  )
}

/** Round, evenly spaced tick values covering [lo, hi]: 1, 2 or 5 times a power of ten. */
export function niceTicks(lo: number, hi: number, target = 4): number[] {
  const span = hi - lo || 1
  const raw = span / target
  const mag = 10 ** Math.floor(Math.log10(raw))
  const step = ([1, 2, 5, 10].find((m) => m * mag >= raw) ?? 10) * mag
  const first = Math.floor(lo / step) * step
  const last = Math.ceil(hi / step) * step
  const out: number[] = []
  for (let v = first; v <= last + step / 2; v += step) out.push(Number(v.toPrecision(12)))
  return out
}

/** Local-midnight ticks across [from, to], thinned to at most `max`; null for a span of two days or less. */
export function dayTicks(from: number, to: number, timeZone: string, max = 6): number[] | null {
  if (to - from <= 2 * DAY_MS) return null
  const [y, m, d] = zonedYmd(from, timeZone).split('-').map(Number)
  const all: number[] = []
  for (let k = 0; k < 400; k++) {
    const t = zonedDayStart(new Date(Date.UTC(y, m - 1, d + k)).toISOString().slice(0, 10), timeZone)
    if (t > to) break
    if (t >= from) all.push(t)
  }
  const every = Math.max(1, Math.ceil(all.length / max))
  return all.filter((_, i) => i % every === 0)
}

/**
 * Axis ticks for a series' values: padded and rounded, but never into a range
 * the quantity cannot have. Data with no negative value floors at 0; a
 * percentage stays within 0-100.
 */
export function axisTicks(values: number[], unit: string | null): number[] {
  let [lo, hi] = extent(values)
  const nonNegative = values.length === 0 || Math.min(...values) >= 0
  const percent = unit === '%'
  if (nonNegative || percent) lo = Math.max(0, lo)
  if (percent) hi = Math.min(100, hi)
  let ticks = niceTicks(lo, hi)
  if (nonNegative || percent) ticks = ticks.filter((t) => t >= 0)
  if (percent) ticks = [...new Set(ticks.map((t) => Math.min(100, t)))]
  return ticks
}
