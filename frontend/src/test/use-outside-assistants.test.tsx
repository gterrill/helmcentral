import { act, renderHook } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'

const api = vi.hoisted(() => ({
  apiRequest: vi.fn(),
  refetch: vi.fn(),
}))

vi.mock('@/lib/api-query', () => ({
  apiRequest: api.apiRequest,
  useApiQuery: () => ({ data: undefined, error: null, isLoading: false, isFetching: false, refetch: api.refetch }),
}))

import { useOutsideAssistants } from '@/hooks/use-outside-assistants'

beforeEach(() => {
  api.apiRequest.mockReset()
  api.refetch.mockReset()
})

describe('useOutsideAssistants.create', () => {
  it('returns the one-time secret even when the list refresh fails', async () => {
    api.apiRequest.mockResolvedValue({ token: { id: 't1', name: 'Laptop' }, secret: 'hcmcp_ONCE' })
    api.refetch.mockRejectedValue(new Error('network down'))
    const { result } = renderHook(() => useOutsideAssistants())

    let created: { secret: string } | undefined
    await act(async () => {
      created = await result.current.create('Laptop')
    })

    expect(created?.secret).toBe('hcmcp_ONCE')
    expect(result.current.refreshError).toMatch(/network down/)
  })

  it('does not claim success when the POST itself fails', async () => {
    api.apiRequest.mockRejectedValue(new Error('403'))
    const { result } = renderHook(() => useOutsideAssistants())
    await expect(act(async () => { await result.current.create('Laptop') })).rejects.toThrow('403')
  })
})
