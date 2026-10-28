import { describe, expect, it } from 'vitest'

import { angleAxis, angleTickLabel, axisTicks, buildTimeRows, panelsOf, seriesValues } from '@/lib/telemetry-chart'

import { bucket, seriesBody, telemetryChartCard } from './helpers/telemetry-chart-fixture'

describe('panelsOf', () => {
  it('groups series into panels in panel order, keeping each series index', () => {
    const series = [
      { panel: 0, label: 'a' },
      { panel: 1, label: 'b' },
      { panel: 0, label: 'c' },
    ]
    expect(panelsOf(series as never)).toEqual([
      { panel: 0, indices: [0, 2] },
      { panel: 1, indices: [1] },
    ])
  })

  it('gives a chart whose series all share a panel one panel', () => {
    expect(panelsOf([{ panel: 0 }, { panel: 0 }] as never)).toEqual([{ panel: 0, indices: [0, 1] }])
  })
})

describe('angleAxis', () => {
  it('draws a bearing on 0 to 360 with north at both ends', () => {
    const a = angleAxis('bearing')
    expect(a.domain).toEqual([0, 360])
    expect(a.ticks).toEqual([0, 90, 180, 270, 360])
    expect(a.ticks.map((t) => angleTickLabel('bearing', t))).toEqual(['N', 'E', 'S', 'W', 'N'])
  })

  it('draws a relative angle on -180 to 180 in plain degrees', () => {
    const a = angleAxis('relative')
    expect(a.domain).toEqual([-180, 180])
    expect(a.ticks.map((t) => angleTickLabel('relative', t))).toEqual(['-180', '-90', '0', '90', '180'])
  })
})

describe('band rows', () => {
  it('carries each bucket as a min-to-max pair beside the mean, and no band for a bucket without both', () => {
    const body = seriesBody({ current: [0, 1, 2], soc: [0] })
    body.series[0].points = body.series[0].points.map((p, i) => (i === 1 ? p : { ...p, min: p.mean - 2, max: p.mean + 3 })) as never
    const rows = buildTimeRows(body as never, 2)
    const r0 = rows.find((r) => r.t === Date.parse(bucket(0)))!
    expect(r0.s0).toBe(10)
    expect(r0.b0).toEqual([8, 13])
    expect(rows.find((r) => r.t === Date.parse(bucket(1)))!.b0).toBeNull()
  })

  it('leaves the band null where the mean line breaks for a gap', () => {
    const body = seriesBody({ current: [0, 1, 5], soc: [0, 1, 2, 3, 4, 5] })
    body.series[0].points = body.series[0].points.map((p) => ({ ...p, min: p.mean - 1, max: p.mean + 1 })) as never
    const rows = buildTimeRows(body as never, 2)
    const gap = rows.find((r) => r.t === Date.parse(bucket(2)))!
    expect(gap.s0).toBeNull()
    expect(gap.b0).toBeNull()
  })

  it('has no band at all for a series without min and max, such as an angle', () => {
    const rows = buildTimeRows(seriesBody() as never, 2)
    expect(rows.every((r) => r.b0 === null && r.b1 === null)).toBe(true)
  })
})

describe('seriesValues with the band', () => {
  it('includes the band edges so the axis shows the gusts, not just the mean', () => {
    const body = seriesBody({ current: [0, 1], soc: [0] })
    body.series[0].points = body.series[0].points.map((p) => ({ ...p, min: p.mean - 5, max: p.mean + 9 })) as never
    expect(seriesValues([body as never], 0, true).sort((a, b) => a - b)).toEqual([5, 6, 10, 11, 19, 20])
    expect(seriesValues([body as never], 0)).toEqual([10, 11])
  })

  it('lets axisTicks cover a gust above the mean', () => {
    const ticks = axisTicks([4, 6, 24], 'kn')
    expect(ticks[ticks.length - 1]).toBeGreaterThanOrEqual(24)
  })
})

describe('a decoded card with panels', () => {
  it('reads the panel of each series', () => {
    const card = telemetryChartCard()
    expect(card.series.map((s) => s.panel)).toEqual([0, 1])
  })
})
