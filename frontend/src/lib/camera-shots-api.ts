import { apiRequest, invalidateQueries } from '@/lib/api-query'
import type { CameraShot } from '@/lib/replay-clock'

// Camera shot reads and writes (ADR 0173 pattern). A failed write throws with
// the server's own message; nothing retries or substitutes.

export const CAMERA_SHOTS_KEY = ['passage-camera-shots'] as const

const SHOTS_PATH = '/api/passages/camera-shots'

export const cameraShotsPath = (fromMs: number, toMs: number) => `${SHOTS_PATH}?from=${Math.round(fromMs)}&to=${Math.round(toMs)}`

export type NewCameraShot = Omit<CameraShot, 'id'>
export type CameraShotPatch = Partial<NewCameraShot>

export const cameraShotsApi = {
  create: async (shot: NewCameraShot) => {
    const { shot: created } = await apiRequest<{ shot: CameraShot }>(SHOTS_PATH, { method: 'POST', json: shot })
    await invalidateQueries(CAMERA_SHOTS_KEY)
    return created
  },
  patch: async (id: string, patch: CameraShotPatch) => {
    const { shot } = await apiRequest<{ shot: CameraShot }>(`${SHOTS_PATH}/${encodeURIComponent(id)}`, { method: 'PATCH', json: patch })
    await invalidateQueries(CAMERA_SHOTS_KEY)
    return shot
  },
  remove: async (id: string) => {
    await apiRequest<{ deleted: boolean }>(`${SHOTS_PATH}/${encodeURIComponent(id)}`, { method: 'DELETE' })
    await invalidateQueries(CAMERA_SHOTS_KEY)
  },
}
