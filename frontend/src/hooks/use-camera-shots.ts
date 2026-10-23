import { useApiQuery } from '@/lib/api-query'
import { CAMERA_SHOTS_KEY, cameraShotsPath } from '@/lib/camera-shots-api'
import type { CameraShot } from '@/lib/replay-clock'

const NO_SHOTS: CameraShot[] = []

/** The camera shots whose time falls between two Unix-millisecond instants:
 * the span the replay covers. Disabled while there is nothing to replay. */
export function useCameraShots(fromMs: number | null, toMs: number | null) {
  const enabled = fromMs !== null && toMs !== null
  const query = useApiQuery({
    queryKey: [...CAMERA_SHOTS_KEY, fromMs, toMs],
    path: enabled ? cameraShotsPath(fromMs, toMs) : '',
    enabled,
    select: (raw: { shots?: CameraShot[] }) => raw.shots ?? NO_SHOTS,
  })
  return { shots: query.data ?? NO_SHOTS, loading: query.isLoading, error: query.error?.message ?? null }
}
