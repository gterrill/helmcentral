import { decodeMateCards, type TelemetryChartCard } from '@/lib/mate-cards'

/** The telemetry_chart contract example (plan: mate-cards-telemetry-chart-contract)
 * with two series on two units. */
export function telemetryChartPayload(over: Record<string, unknown> = {}): Record<string, unknown> {
  return {
    title: 'Alternator current against house bank SOC',
    view: 'time',
    xy: null,
    timezone: 'Etc/GMT-10',
    series: [
      {
        path: 'electrical.alternators.12.current',
        source: 'venus.c0619ab9d0ee',
        label: 'Starboard alternator current',
        unit: 'A',
        si_unit: 'A',
        panel: 0,
      },
      { path: 'electrical.batteries.512.capacity.stateOfCharge', source: null, label: 'House bank SOC', unit: '%', si_unit: '%', panel: 1 },
    ],
    range: { from: '2026-10-12T00:00:00+10:00', to: '2026-10-19T00:00:00+10:00' },
    compare: [],
    ...over,
  }
}

export function telemetryChartCard(over: Record<string, unknown> = {}): TelemetryChartCard {
  const [card] = decodeMateCards([{ kind: 'telemetry_chart', payload: telemetryChartPayload(over) }])
  if (card.kind !== 'telemetry_chart') throw new Error(`fixture did not decode: ${JSON.stringify(card)}`)
  return card
}

const HOUR = 3_600_000
const T0 = Date.parse('2026-10-18T00:00:00+10:00')
export const bucket = (i: number) => new Date(T0 + i * HOUR).toISOString()

/** A /api/telemetry/series body. `current` and `soc` are bucket indexes present for each series. */
export function seriesBody(opts: { current?: number[]; soc?: number[]; socFound?: boolean } = {}) {
  const current = opts.current ?? [0, 1, 2, 3, 4]
  const soc = opts.soc ?? [0, 1, 2, 3, 5]
  return {
    from: '2026-10-12T00:00:00+10:00',
    to: '2026-10-19T00:00:00+10:00',
    bucket: '1h',
    series: [
      {
        path: 'electrical.alternators.12.current',
        found: true,
        unit: 'A',
        points: current.map((i) => ({ t: bucket(i), mean: 10 + i })),
        gaps: [],
      },
      {
        path: 'electrical.batteries.512.capacity.stateOfCharge',
        found: opts.socFound ?? true,
        unit: '%',
        points: opts.socFound === false ? [] : soc.map((i) => ({ t: bucket(i), mean: 50 + i })),
        gaps: [],
      },
    ],
  }
}
