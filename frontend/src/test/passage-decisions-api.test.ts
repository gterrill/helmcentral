import { beforeEach, describe, expect, it, vi } from 'vitest'

import { resetQueryStore, useApiQuery } from '@/lib/api-query'
import { PASSAGES_KEY } from '@/lib/passages-api'
import { onPassageDecisionSaved, passageDecisionSaved, passageDecisionsApi, PassageDecisionUnreadableError } from '@/lib/passage-decisions-api'
import { renderHook, waitFor } from '@testing-library/react'
import { stubApi } from './helpers/stub-api'

describe('passageDecisionsApi', () => {
  beforeEach(() => { resetQueryStore() })

  it('throws an unreadable-answer error, distinct from a refusal, when the saved answer is malformed', async () => {
    const mock = stubApi({ 'PUT /api/passage-decisions/5': { decision: { id: 5, state: 'sailed' } } })
    await expect(passageDecisionsApi.put(5, { state: 'stayed' })).rejects.toBeInstanceOf(PassageDecisionUnreadableError)
    expect(mock.mock.calls.filter(([, i]) => i?.method === 'PUT')).toHaveLength(1)
  })

  it('PUTs the body and returns the decoded view', async () => {
    const mock = stubApi({
      'PUT /api/passage-decisions/5': { decision: { id: 5, state: 'went', row_date: '2026-10-24', inferred: false, passage_id: 'p1' } },
      'GET /api/passages': { passages: [] },
    })
    const view = await passageDecisionsApi.put(5, { state: 'went', row_date: '2026-10-24' })
    expect(view).toEqual({ id: 5, state: 'went', rowDate: '2026-10-24', inferred: false, passageId: 'p1' })
    const put = mock.mock.calls.find(([, i]) => i?.method === 'PUT')
    expect(JSON.parse(String(put?.[1]?.body))).toEqual({ state: 'went', row_date: '2026-10-24' })
  })

  it('reads again only the list and the passages the decision was and is linked to', async () => {
    const reads: Record<string, number> = { list: 0, p1: 0, p2: 0, p3: 0 }
    stubApi({
      'GET /api/passages': () => ({ passages: [{ n: ++reads.list }] }),
      'GET /api/passages/p1': () => ({ n: ++reads.p1 }),
      'GET /api/passages/p2': () => ({ n: ++reads.p2 }),
      'GET /api/passages/p3': () => ({ n: ++reads.p3 }),
    })
    const { result } = renderHook(() => ({
      list: useApiQuery<unknown>({ queryKey: PASSAGES_KEY, path: '/api/passages' }),
      p1: useApiQuery<unknown>({ queryKey: [...PASSAGES_KEY, 'p1'], path: '/api/passages/p1' }),
      p2: useApiQuery<unknown>({ queryKey: [...PASSAGES_KEY, 'p2'], path: '/api/passages/p2' }),
      p3: useApiQuery<unknown>({ queryKey: [...PASSAGES_KEY, 'p3'], path: '/api/passages/p3' }),
    }))
    await waitFor(() => expect(result.current.p3.data).toBeDefined())
    await waitFor(() => expect(result.current.p1.data).toBeDefined())
    await waitFor(() => expect(result.current.p2.data).toBeDefined())
    await waitFor(() => expect(result.current.list.data).toBeDefined())
    await passageDecisionSaved('p1', { id: 5, state: 'went', rowDate: '2026-10-24', inferred: false, passageId: 'p2' })
    expect(reads).toEqual({ list: 2, p1: 2, p2: 2, p3: 1 })
  })

  it('reads every passage again when the saved answer could not be read', async () => {
    const reads = { list: 0, p3: 0 }
    stubApi({
      'GET /api/passages': () => ({ n: ++reads.list }),
      'GET /api/passages/p3': () => ({ n: ++reads.p3 }),
    })
    const { result } = renderHook(() => ({
      list: useApiQuery<unknown>({ queryKey: PASSAGES_KEY, path: '/api/passages' }),
      p3: useApiQuery<unknown>({ queryKey: [...PASSAGES_KEY, 'p3'], path: '/api/passages/p3' }),
    }))
    await waitFor(() => expect(result.current.p3.data).toBeDefined())
    await waitFor(() => expect(result.current.list.data).toBeDefined())
    await passageDecisionSaved(null, null)
    expect(reads).toEqual({ list: 2, p3: 2 })
  })

  it('tells listeners a decision was saved until they stop listening', async () => {
    const heard = vi.fn()
    const stop = onPassageDecisionSaved(heard)
    await passageDecisionSaved(null, null)
    stop()
    await passageDecisionSaved(null, null)
    expect(heard).toHaveBeenCalledTimes(1)
  })

  it('throws the server message and does not refetch on failure', async () => {
    stubApi({ 'PUT /api/passage-decisions/5': () => new Response(JSON.stringify({ error: 'bad row' }), { status: 400 }) })
    await expect(passageDecisionsApi.put(5, { state: 'stayed' })).rejects.toThrow('bad row')
  })
})
