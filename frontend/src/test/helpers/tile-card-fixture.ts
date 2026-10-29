import { decodeMateCards, type TileCard } from '@/lib/mate-cards'

/** The stored depth-tide card payload, as the server writes it. */
export function tilePayload(over: Record<string, unknown> = {}, snapshotOver: Record<string, unknown> = {}): Record<string, unknown> {
  return {
    tile: 'depth-tide',
    captured_at: '2026-10-28T04:03:00Z',
    snapshot: {
      depth_m: 3.3,
      depth_age_s: 1.2,
      navigation_state: 'anchored',
      tide: {
        datetime: '2026-10-28T04:03:00Z',
        current_tide_height_ft: 6.56,
        tide_direction: 'Rising',
        high_tide_time: '2026-10-28T14:12:00+10:00',
        high_tide_height_ft: 9.84,
        low_tide_time: '2026-10-28T20:30:00+10:00',
        low_tide_height_ft: 1.64,
        station_name: 'Cape Upstart',
        provider: 'bom',
      },
      depth_trend: {
        points: [
          { time: '2026-10-28T03:00:00Z', depth_m: 3.1 },
          { time: '2026-10-28T03:30:00Z', depth_m: 3.2 },
          { time: '2026-10-28T04:00:00Z', depth_m: 3.3 },
        ],
        since: 'tide',
        tide_type: 'low',
        tide_depth_m: 3.1,
      },
      ...snapshotOver,
    },
    ...over,
  }
}

export function tileCard(over: Record<string, unknown> = {}, snapshotOver: Record<string, unknown> = {}): TileCard {
  const [c] = decodeMateCards([{ kind: 'tile', payload: tilePayload(over, snapshotOver) }])
  if (c.kind !== 'tile') throw new Error(`did not decode: ${JSON.stringify(c)}`)
  return c
}
