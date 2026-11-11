import { beforeEach, describe, expect, it, vi } from 'vitest'

import { watchesApi } from '@/hooks/use-watches'
import { answerThenRefresh, UnusualSavedError } from '@/lib/watches'
import { resetQueryStore } from '@/lib/api-query'

// ADR 0276, Feedback: an answer about an unusual stretch goes to the server
// with the stretch's own times, so it can be used without scoring again.

describe('watchesApi.answerUnusual', () => {
  beforeEach(() => resetQueryStore())

  it('names the run and the line, and sends no times', async () => {
    const calls: Array<{ url: string; method?: string; body?: string }> = []
    vi.stubGlobal(
      'fetch',
      vi.fn(async (url: string, init?: RequestInit) => {
        calls.push({ url, method: init?.method, body: init?.body as string | undefined })
        return { ok: true, status: 200, json: async () => ({}) }
      }),
    )
    await watchesApi.answerUnusual({ id: 'r1', watch_id: 'w1' }, 1, 'normal')
    const put = calls.find((c) => c.method === 'PUT')
    expect(put?.url).toContain('/api/anomaly/normal/answers')
    expect(JSON.parse(put?.body ?? '{}')).toEqual({ watch_id: 'w1', run_id: 'r1', index: 1, answer: 'normal' })
  })

  it('lets a refusal through with the server words', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => ({ ok: false, status: 400, json: async () => ({ error: 'This engine is no longer set up. It may have been renumbered or removed.' }) })))
    await expect(watchesApi.answerUnusual({ id: 'r1', watch_id: 'w1' }, 0, 'real')).rejects.toThrow('This engine is no longer set up')
  })
})

describe('answerThenRefresh', () => {
  it('refreshes after a good save', async () => {
    const refresh = vi.fn().mockResolvedValue(undefined)
    await answerThenRefresh(async () => undefined, refresh)
    expect(refresh).toHaveBeenCalledTimes(1)
  })

  it('lets a failed save through as it is, and does not refresh', async () => {
    const refresh = vi.fn()
    await expect(answerThenRefresh(async () => { throw new Error('refused') }, refresh)).rejects.toThrow('refused')
    expect(refresh).not.toHaveBeenCalled()
  })

  it('says the answer was saved when only the refresh failed', async () => {
    const err = await answerThenRefresh(async () => undefined, async () => { throw new Error('offline') }).catch((e: unknown) => e)
    expect(err).toBeInstanceOf(UnusualSavedError)
    expect((err as Error).message).toBe('offline')
  })
})
