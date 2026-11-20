import { useApiQuery } from '@/lib/api-query'
import { finiteOrNull } from '@/lib/finite-or-null'

/**
 * The motion stream's state (GET /api/signalk/motion-feed): whether it is open,
 * the attitude and heave rates the recorder stored on its last finished window
 * (rounded once, by the server, for display) and how long ago that was, and the
 * seconds since a sample last reached Helmcentral. A figure the server does not
 * know is null, never zero. The server judges each rate against the slowest one
 * the recorder keeps figures from and gives a plain reason per signal when it
 * has none or something is wrong, so this page holds no copy of the rule.
 */
export interface MotionFeed {
  connected: boolean
  attitude_hz: number | null
  attitude_recorded_ago_s: number | null
  attitude_last_sample_s: number | null
  attitude_reason?: string
  heave_hz: number | null
  heave_recorded_ago_s: number | null
  heave_last_sample_s: number | null
  heave_reason?: string
  /** Why the stream is not open. */
  reason?: string
}

const KEY = ['/api/signalk/motion-feed']

const text = (v: unknown) => (typeof v === 'string' && v !== '' ? v : undefined)

/** Re-read every 5 s while the Settings page is open. */
export function useMotionFeed() {
  const { data, error } = useApiQuery<MotionFeed>({
    queryKey: KEY,
    path: '/api/signalk/motion-feed',
    refetchInterval: 5_000,
    select: (body: MotionFeed): MotionFeed => ({
      connected: body.connected === true,
      attitude_hz: finiteOrNull(body.attitude_hz),
      attitude_recorded_ago_s: finiteOrNull(body.attitude_recorded_ago_s),
      attitude_last_sample_s: finiteOrNull(body.attitude_last_sample_s),
      attitude_reason: text(body.attitude_reason),
      heave_hz: finiteOrNull(body.heave_hz),
      heave_recorded_ago_s: finiteOrNull(body.heave_recorded_ago_s),
      heave_last_sample_s: finiteOrNull(body.heave_last_sample_s),
      heave_reason: text(body.heave_reason),
      reason: text(body.reason),
    }),
  })
  return { feed: data ?? null, error: error?.message ?? null }
}
