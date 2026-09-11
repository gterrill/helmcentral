import { describe, it, expect, afterEach, vi } from 'vitest'
import { renderHook, waitFor } from '@testing-library/react'
import { useAssistantStatus } from '@/hooks/use-assistant-status'

// ADR 0093: the assistant panel checks readiness before it lets the operator
// type anything. `problem` carries the actionable reason (disabled, no key,
// blank model) and is absent once the assistant can actually run.
describe('useAssistantStatus', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('fetches the status shape on mount', async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      json: async () => ({ enabled: true, configured: true, model: 'anthropic/claude-sonnet-4.5' }),
    })
    vi.stubGlobal('fetch', fetchMock)

    const { result } = renderHook(() => useAssistantStatus())

    await waitFor(() => expect(result.current.loading).toBe(false))
    expect(fetchMock).toHaveBeenCalledWith('/api/assistant/status')
    expect(result.current.status).toEqual({ enabled: true, configured: true, model: 'anthropic/claude-sonnet-4.5' })
    expect(result.current.error).toBeNull()
  })

  it('carries a problem sentence when the assistant cannot run', async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      json: async () => ({ enabled: false, configured: false, model: '', problem: 'Enable the assistant in Settings → Assistant.' }),
    })
    vi.stubGlobal('fetch', fetchMock)

    const { result } = renderHook(() => useAssistantStatus())

    await waitFor(() => expect(result.current.loading).toBe(false))
    expect(result.current.status?.problem).toBe('Enable the assistant in Settings → Assistant.')
  })

  it('sets an error message when the fetch fails', async () => {
    const fetchMock = vi.fn().mockResolvedValue({ ok: false, status: 500 })
    vi.stubGlobal('fetch', fetchMock)

    const { result } = renderHook(() => useAssistantStatus())

    await waitFor(() => expect(result.current.loading).toBe(false))
    expect(result.current.error).toContain('500')
    expect(result.current.status).toBeNull()
  })

  it('refresh re-fetches the status', async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      json: async () => ({ enabled: true, configured: true, model: 'x' }),
    })
    vi.stubGlobal('fetch', fetchMock)

    const { result } = renderHook(() => useAssistantStatus())
    await waitFor(() => expect(result.current.loading).toBe(false))

    await result.current.refresh()

    expect(fetchMock).toHaveBeenCalledTimes(2)
  })
})
