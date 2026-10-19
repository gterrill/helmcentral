import { decodeMateCards, type PassageWindowCard } from '@/lib/mate-cards'

/** The contract example (plan: mate-cards-passage-window-contract) grown to
 * two departure dates so a grid has more than one row. */
export function passageWindowPayload(): Record<string, unknown> {
  return {
    timezone: 'Australia/Brisbane',
    forecast_fetched_at: '2026-10-19T09:00:00+10:00',
    provider: 'open-meteo',
    wave_provider: '',
    legs: [
      { name: 'Townsville → Cape Upstart', stay_nights_after: 2 },
      { name: 'Cape Upstart → Bowen', stay_nights_after: null },
    ],
    rows: [
      {
        depart_date: '2026-10-23',
        verdict: 'no_go',
        cells: [
          {
            leg: 0,
            date: '2026-10-23',
            depart: '2026-10-23T06:00:00+10:00',
            arrive: '2026-10-23T14:30:00+10:00',
            state: 'no_go',
            limiting: { factor: 'wind', sector: 'ahead', text: 'Wind SE 27 kn ahead · Cape Bowling Green → Cape Upstart' },
            not_checked: ['wave', 'wind_against_tide'],
          },
          {
            leg: 1,
            date: '2026-10-25',
            depart: '2026-10-25T06:00:00+10:00',
            arrive: '2026-10-25T12:00:00+10:00',
            state: 'go',
            limiting: null,
            not_checked: ['wave'],
          },
        ],
      },
      {
        depart_date: '2026-10-24',
        verdict: 'no_data',
        cells: [
          {
            leg: 0,
            date: '2026-10-24',
            depart: '2026-10-24T06:00:00+10:00',
            arrive: '2026-10-24T14:30:00+10:00',
            state: 'marginal',
            limiting: { factor: 'gust', sector: null, text: 'Gusts 24 kn · Halifax Bay' },
            not_checked: [],
          },
          {
            leg: 1,
            date: '2026-10-26',
            depart: '2026-10-26T06:00:00+10:00',
            arrive: '2026-10-26T12:00:00+10:00',
            state: 'no_data',
            limiting: null,
            note: 'No forecast this far ahead',
            not_checked: [],
          },
        ],
      },
    ],
  }
}

/** The decoded card; throws if the decoder rejects the contract example. */
export function passageWindowCard(): PassageWindowCard {
  const [card] = decodeMateCards([{ kind: 'passage_window', payload: passageWindowPayload() }])
  if (card.kind !== 'passage_window') throw new Error(card.kind === 'error' ? card.reason : 'not decoded')
  return card
}
