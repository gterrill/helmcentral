import { decodeMateCards, type WindRoseCard } from '@/lib/mate-cards'

const row = (...n: number[]) => n
const zero = () => row(0, 0, 0, 0, 0, 0)

/** The wind_rose contract example: 16 sectors, six knot bands. 100 samples, of
 * which 10 are calm and 5 variable, 60 are from the east and 25 from the south. */
export function windRosePayload(over: Record<string, unknown> = {}): Record<string, unknown> {
  const counts = Array.from({ length: 16 }, zero)
  counts[4] = row(0, 20, 30, 10, 0, 0)
  counts[8] = row(0, 10, 10, 5, 0, 0)
  return {
    title: 'Where the wind came from, 21 Oct to 28 Oct',
    timezone: 'Etc/GMT-10',
    range: { from: '2026-10-21T07:23:00+10:00', to: '2026-10-28T07:23:00+10:00' },
    reference: 'true',
    sectors: 16,
    speed_unit: 'kn',
    bins: [
      { from: 0, to: 5 },
      { from: 5, to: 10 },
      { from: 10, to: 15 },
      { from: 15, to: 20 },
      { from: 20, to: 25 },
      { from: 25, to: null },
    ],
    counts,
    samples: 100,
    calm: 10,
    calm_below: 1,
    variable: 5,
    unpaired: 0,
    bucket_minutes: 10,
    mean_speed: 11.4,
    strongest: { speed: 24.6, at: '2026-10-24T16:10:00+10:00' },
    ...over,
  }
}

export function windRoseCard(over: Record<string, unknown> = {}): WindRoseCard {
  const [card] = decodeMateCards([{ kind: 'wind_rose', payload: windRosePayload(over) }])
  if (card.kind !== 'wind_rose') throw new Error(`fixture did not decode: ${JSON.stringify(card)}`)
  return card
}
