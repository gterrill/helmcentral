import { describe, expect, it } from 'vitest'

import { useApiQuery } from '@/lib/api-query'
import { PASSAGES_KEY } from '@/lib/passages-api'
import { passageDecisionsApi, PassageDecisionUnreadableError } from '@/lib/passage-decisions-api'
import { renderHook, waitFor } from '@testing-library/react'
import { stubApi } from './helpers/stub-api'

describe('passageDecisionsApi', () => {
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

  it('refetches the passages query after a successful PUT', async () => {
    let reads = 0
    stubApi({
      'PUT /api/passage-decisions/5': { decision: { id: 5, state: 'stayed', row_date: null, inferred: false, passage_id: null } },
      'GET /api/passages': () => ({ passages: [{ n: ++reads }] }),
    })
    const { result } = renderHook(() => useApiQuery<{ passages: unknown[] }>({ queryKey: PASSAGES_KEY, path: '/api/passages' }))
    await waitFor(() => expect(result.current.data).toBeDefined())
    const before = reads
    await passageDecisionsApi.put(5, { state: 'stayed' })
    await waitFor(() => expect(reads).toBeGreaterThan(before))
  })

  it('throws the server message and does not refetch on failure', async () => {
    stubApi({ 'PUT /api/passage-decisions/5': () => new Response(JSON.stringify({ error: 'bad row' }), { status: 400 }) })
    await expect(passageDecisionsApi.put(5, { state: 'stayed' })).rejects.toThrow('bad row')
  })
})
