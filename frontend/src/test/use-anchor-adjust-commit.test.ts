import { describe, it, expect, vi, beforeEach } from 'vitest'
import { renderHook, act } from '@testing-library/react'
import { toast } from 'sonner'
import { useAnchorAdjustCommit } from '@/hooks/use-anchor-adjust-commit'

vi.mock('sonner', () => ({
  toast: Object.assign(vi.fn(), { error: vi.fn() }),
}))

const draft = { lat: -21.21, lon: 149.31, radiusMeters: 24 }
const previous = { lat: -21.2, lon: 149.3, radiusMeters: 20 }

// sonner's own ExternalToast type allows `action` to be a plain ReactNode as
// well as an {label, onClick} object; this hook only ever passes the object
// form, so tests narrow to it here rather than threading `as` casts through
// every call site below.
interface ToastActionOptions {
  action: { label: string; onClick: () => void }
}
function actionOf(options: unknown): ToastActionOptions['action'] {
  return (options as ToastActionOptions).action
}

describe('useAnchorAdjustCommit', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('never starts anything merely from rendering — no adjustAnchor call, no timer, until commit() is invoked (StrictMode-safe)', () => {
    const adjustAnchor = vi.fn().mockResolvedValue(undefined)
    renderHook(() => useAnchorAdjustCommit({ adjustAnchor }))
    expect(adjustAnchor).not.toHaveBeenCalled()
    expect(toast).not.toHaveBeenCalled()
  })

  it('on success: calls adjustAnchor with the draft, calls onSuccess, and shows a 10s Undo toast', async () => {
    const adjustAnchor = vi.fn().mockResolvedValue(undefined)
    const onSuccess = vi.fn()
    const { result } = renderHook(() => useAnchorAdjustCommit({ adjustAnchor }))

    await act(async () => {
      result.current.commit({ draft, previous, onSuccess })
      await Promise.resolve()
      await Promise.resolve()
    })

    expect(adjustAnchor).toHaveBeenCalledWith(draft)
    expect(onSuccess).toHaveBeenCalledTimes(1)
    expect(toast).toHaveBeenCalledTimes(1)
    const [, options] = vi.mocked(toast).mock.calls[0]
    expect(options).toMatchObject({ duration: 10000, action: { label: 'Undo' } })
  })

  it('on failure: does not call onSuccess, keeps the draft (caller stays in Adjust), and shows an error toast with Retry', async () => {
    const adjustAnchor = vi.fn().mockRejectedValue(new Error('Request failed'))
    const onSuccess = vi.fn()
    const { result } = renderHook(() => useAnchorAdjustCommit({ adjustAnchor }))

    await act(async () => {
      result.current.commit({ draft, previous, onSuccess })
      await Promise.resolve()
      await Promise.resolve()
    })

    expect(onSuccess).not.toHaveBeenCalled()
    expect(toast.error).toHaveBeenCalledTimes(1)
    const [, options] = vi.mocked(toast.error).mock.calls[0]
    expect(options).toMatchObject({ description: 'Request failed', action: { label: 'Retry' } })
  })

  it('committing flips true during the write and back to false once it resolves', async () => {
    let resolveWrite: () => void = () => {}
    const adjustAnchor = vi.fn().mockImplementation(() => new Promise<void>((resolve) => { resolveWrite = resolve }))
    const { result } = renderHook(() => useAnchorAdjustCommit({ adjustAnchor }))

    expect(result.current.committing).toBe(false)
    act(() => { result.current.commit({ draft, previous, onSuccess: () => {} }) })
    expect(result.current.committing).toBe(true)

    await act(async () => {
      resolveWrite()
      await Promise.resolve()
      await Promise.resolve()
    })
    expect(result.current.committing).toBe(false)
  })

  it("the Undo toast's action calls adjustAnchor with the PRE-Adjust values", async () => {
    const adjustAnchor = vi.fn().mockResolvedValue(undefined)
    const { result } = renderHook(() => useAnchorAdjustCommit({ adjustAnchor }))

    await act(async () => {
      result.current.commit({ draft, previous, onSuccess: () => {} })
      await Promise.resolve()
      await Promise.resolve()
    })

    adjustAnchor.mockClear()
    const [, options] = vi.mocked(toast).mock.calls[0]
    await act(async () => {
      actionOf(options).onClick()
      await Promise.resolve()
    })
    expect(adjustAnchor).toHaveBeenCalledWith(previous)
  })

  it('an Undo failure shows its own error toast with Retry, not the commit one', async () => {
    const adjustAnchor = vi.fn().mockResolvedValue(undefined)
    const { result } = renderHook(() => useAnchorAdjustCommit({ adjustAnchor }))

    await act(async () => {
      result.current.commit({ draft, previous, onSuccess: () => {} })
      await Promise.resolve()
      await Promise.resolve()
    })

    const [, options] = vi.mocked(toast).mock.calls[0]
    adjustAnchor.mockRejectedValueOnce(new Error('Undo failed'))
    await act(async () => {
      actionOf(options).onClick()
      await Promise.resolve()
      await Promise.resolve()
    })
    expect(toast.error).toHaveBeenCalledWith('Could not undo the anchor move', expect.objectContaining({ description: 'Undo failed' }))
  })

  // Code-review finding: Enter (via the map's own scoped keydown) reaches
  // the commit path unguarded while a write is already in flight, firing a
  // duplicate PATCH and a second Undo toast. The guard belongs on commit()
  // itself, not just on the Set button's own `disabled`.
  it('a second commit() call while one is already in flight is ignored, not queued as a duplicate write', async () => {
    let resolveWrite: () => void = () => {}
    const adjustAnchor = vi.fn().mockImplementation(() => new Promise<void>((resolve) => { resolveWrite = resolve }))
    const onSuccess = vi.fn()
    const { result } = renderHook(() => useAnchorAdjustCommit({ adjustAnchor }))

    act(() => {
      result.current.commit({ draft, previous, onSuccess })
      result.current.commit({ draft, previous, onSuccess })
    })

    expect(adjustAnchor).toHaveBeenCalledTimes(1)

    await act(async () => {
      resolveWrite()
      await Promise.resolve()
      await Promise.resolve()
    })
    expect(onSuccess).toHaveBeenCalledTimes(1)
  })

  // Code-review finding: the "Anchor moved" toast always showed metres, even
  // under the imperial display setting.
  it('the "Anchor moved" toast follows the units setting', async () => {
    const adjustAnchor = vi.fn().mockResolvedValue(undefined)
    const { result } = renderHook(() => useAnchorAdjustCommit({ adjustAnchor, isImperial: true }))

    await act(async () => {
      result.current.commit({ draft, previous, onSuccess: () => {} })
      await Promise.resolve()
      await Promise.resolve()
    })

    // draft.radiusMeters is 24 m -> 24 * 3.28084 = 78.74 -> 79 ft.
    const [, options] = vi.mocked(toast).mock.calls[0]
    expect(options).toMatchObject({ description: 'Radius 79 ft' })
  })

  it('defaults to metres when isImperial is omitted', async () => {
    const adjustAnchor = vi.fn().mockResolvedValue(undefined)
    const { result } = renderHook(() => useAnchorAdjustCommit({ adjustAnchor }))

    await act(async () => {
      result.current.commit({ draft, previous, onSuccess: () => {} })
      await Promise.resolve()
      await Promise.resolve()
    })

    const [, options] = vi.mocked(toast).mock.calls[0]
    expect(options).toMatchObject({ description: 'Radius 24 m' })
  })

  it('Retry on a failed commit re-sends the same draft', async () => {
    const adjustAnchor = vi.fn().mockRejectedValueOnce(new Error('Request failed')).mockResolvedValueOnce(undefined)
    const onSuccess = vi.fn()
    const { result } = renderHook(() => useAnchorAdjustCommit({ adjustAnchor }))

    await act(async () => {
      result.current.commit({ draft, previous, onSuccess })
      await Promise.resolve()
      await Promise.resolve()
    })

    const [, options] = vi.mocked(toast.error).mock.calls[0]
    await act(async () => {
      actionOf(options).onClick()
      await Promise.resolve()
      await Promise.resolve()
    })
    expect(adjustAnchor).toHaveBeenCalledTimes(2)
    expect(adjustAnchor).toHaveBeenLastCalledWith(draft)
    expect(onSuccess).toHaveBeenCalledTimes(1)
  })
})
