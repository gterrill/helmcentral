import { describe, expect, it } from 'vitest'

import { buildTimeRows } from '@/lib/telemetry-chart'

import { bucket, seriesBody } from './helpers/telemetry-chart-fixture'

describe('buildTimeRows', () => {
  it("breaks only the series that has the gap, not the other series' line", () => {
    const body = seriesBody({ current: [0, 1, 2, 3], soc: [0, 1, 3] })
    const halfHour = new Date(Date.parse(bucket(1)) + 1_800_000).toISOString()
    body.series[0].gaps = [{ from: halfHour, to: bucket(2) }] as never
    const rows = buildTimeRows(body as never, 2)
    const at = rows.find((r) => r.t === Date.parse(halfHour))!
    expect(at.s0).toBeNull()
    // SOC has points either side of the half hour, so its line must run through.
    expect(at.s1).not.toBeNull()
  })

  it('leaves a series null where its own data really stops', () => {
    const body = seriesBody({ current: [0, 1, 5], soc: [0, 1, 2, 3, 4, 5] })
    const rows = buildTimeRows(body as never, 2)
    expect(rows.find((r) => r.t === Date.parse(bucket(2)))!.s0).toBeNull()
    expect(rows.find((r) => r.t === Date.parse(bucket(2)))!.s1).not.toBeNull()
  })
})
