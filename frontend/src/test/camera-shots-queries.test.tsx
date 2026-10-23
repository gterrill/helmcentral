import { renderHook, waitFor } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { useCameraShots } from '@/hooks/use-camera-shots'
import { cameraShotsApi } from '@/lib/camera-shots-api'
import { stubApi, type StubRequest } from './helpers/stub-api'

const shot = (id: string, t_ms: number) => ({
  id, t_ms, mode: 'follow', zoom: 14, pitch: 50, bearing_offset_deg: 0, orbit_deg_per_s: 0, pace: 1,
})

describe('camera shots', () => {
  it('reads the shots inside the replayed span', async () => {
    const mock = stubApi({ 'GET /api/passages/camera-shots?from=1000&to=9000': { shots: [shot('a', 2000)] } })
    const { result } = renderHook(() => useCameraShots(1000, 9000))
    await waitFor(() => expect(result.current.shots).toHaveLength(1))
    expect(mock).toHaveBeenCalledTimes(1)
    expect(result.current.error).toBeNull()
  })

  it('refetches after a write, and sends the shot as given', async () => {
    let list = [shot('a', 2000)]
    const mock = stubApi({
      'GET /api/passages/camera-shots?from=1000&to=9000': () => ({ shots: list }),
      'POST /api/passages/camera-shots': (req: StubRequest) => { list = [...list, { ...(req.body as object), id: 'b' } as ReturnType<typeof shot>]; return { shot: list[1] } },
      'PATCH /api/passages/camera-shots/b': (req: StubRequest) => ({ shot: { ...list[1], ...(req.body as object) } }),
      'DELETE /api/passages/camera-shots/b': () => { list = [list[0]]; return { deleted: true } },
    })
    const { result } = renderHook(() => useCameraShots(1000, 9000))
    await waitFor(() => expect(result.current.shots).toHaveLength(1))
    await cameraShotsApi.create({ t_ms: 3000, mode: 'orbit', zoom: 13, pitch: 40, bearing_offset_deg: 0, orbit_deg_per_s: 8, pace: 0.5 })
    await waitFor(() => expect(result.current.shots).toHaveLength(2))
    const post = mock.mock.calls.find(([, init]) => init?.method === 'POST')!
    expect(JSON.parse(post[1]!.body as string)).toMatchObject({ mode: 'orbit', pace: 0.5 })
    await cameraShotsApi.patch('b', { zoom: 16 })
    await cameraShotsApi.remove('b')
    await waitFor(() => expect(result.current.shots).toHaveLength(1))
  })

  it('says why when a write is refused, with the server words', async () => {
    stubApi({ 'POST /api/passages/camera-shots': () => new Response(JSON.stringify({ error: 'invalid passage request: t_ms is not inside any passage' }), { status: 400, headers: { 'Content-Type': 'application/json' } }) })
    await expect(cameraShotsApi.create({ t_ms: 1, mode: 'follow', zoom: 14, pitch: 50, bearing_offset_deg: 0, orbit_deg_per_s: 0, pace: 1 })).rejects.toThrow('not inside any passage')
  })
})
