import { render, screen, act } from '@testing-library/react'
import { describe, it, expect, vi, afterEach } from 'vitest'
import { DisplayRemoteToast } from '@/components/display-remote-toast'

describe('DisplayRemoteToast', () => {
  afterEach(() => {
    vi.useRealTimers()
  })

  it('shows "Paused · <name> (n of total)" while paused', () => {
    render(<DisplayRemoteToast announcementId={1} pageName="Engines" position={{ index: 1, total: 5 }} paused={true} />)
    expect(screen.getByTestId('display-remote-toast')).toHaveTextContent('Paused · Engines (2 of 5)')
  })

  it('shows "<name> (n of total)" with no "Paused" prefix while running', () => {
    render(<DisplayRemoteToast announcementId={1} pageName="Engines" position={{ index: 2, total: 5 }} paused={false} />)
    const toast = screen.getByTestId('display-remote-toast')
    expect(toast).toHaveTextContent('Engines (3 of 5)')
    expect(toast).not.toHaveTextContent('Paused')
  })

  // These bump the id on an already-mounted component rather than mounting
  // with one, because that is the only way it happens: the toast mounts once
  // with the wall shell, before any key has been pressed, and a press then
  // changes the id. Asserting visibility straight off a mount was asserting
  // the boot flash.
  it('is visible immediately on a new announcement', () => {
    vi.useFakeTimers()
    const { rerender } = render(<DisplayRemoteToast announcementId={0} pageName="Engines" position={{ index: 0, total: 2 }} paused={false} />)
    rerender(<DisplayRemoteToast announcementId={1} pageName="Engines" position={{ index: 0, total: 2 }} paused={false} />)
    expect(screen.getByTestId('display-remote-toast')).toHaveAttribute('aria-hidden', 'false')
  })

  it('fades out about 2s after the announcement', () => {
    vi.useFakeTimers()
    const { rerender } = render(<DisplayRemoteToast announcementId={0} pageName="Engines" position={{ index: 0, total: 2 }} paused={false} />)
    rerender(<DisplayRemoteToast announcementId={1} pageName="Engines" position={{ index: 0, total: 2 }} paused={false} />)

    act(() => { vi.advanceTimersByTime(1_999) })
    expect(screen.getByTestId('display-remote-toast')).toHaveAttribute('aria-hidden', 'false')

    act(() => { vi.advanceTimersByTime(1) })
    expect(screen.getByTestId('display-remote-toast')).toHaveAttribute('aria-hidden', 'true')
  })

  it('reappears when announcementId changes again, even to the same page/position', () => {
    vi.useFakeTimers()
    const { rerender } = render(<DisplayRemoteToast announcementId={0} pageName="Engines" position={{ index: 0, total: 2 }} paused={false} />)
    rerender(<DisplayRemoteToast announcementId={1} pageName="Engines" position={{ index: 0, total: 2 }} paused={false} />)
    act(() => { vi.advanceTimersByTime(2_000) })
    expect(screen.getByTestId('display-remote-toast')).toHaveAttribute('aria-hidden', 'true')

    rerender(<DisplayRemoteToast announcementId={2} pageName="Engines" position={{ index: 0, total: 2 }} paused={false} />)
    expect(screen.getByTestId('display-remote-toast')).toHaveAttribute('aria-hidden', 'false')
  })

  it('does not reset its fade timer when position/paused change without a new announcementId', () => {
    // An ordinary dwell-driven page change also changes position/paused as
    // props, but must never re-trigger the toast on its own - only an
    // explicit remote action (a new announcementId) does.
    vi.useFakeTimers()
    const { rerender } = render(<DisplayRemoteToast announcementId={1} pageName="Engines" position={{ index: 0, total: 2 }} paused={false} />)
    act(() => { vi.advanceTimersByTime(1_500) })

    rerender(<DisplayRemoteToast announcementId={1} pageName="Anchor" position={{ index: 1, total: 2 }} paused={false} />)
    act(() => { vi.advanceTimersByTime(500) })
    expect(screen.getByTestId('display-remote-toast')).toHaveAttribute('aria-hidden', 'true')
  })

  it('stays hidden on the first render, so a wall boot does not flash a toast nobody asked for', () => {
    // The effect runs once on mount with the initial announcementId, which
    // is not a remote press. Before the guard this showed "(1 of 0)" for two
    // seconds on every boot, while the feed was still resolving.
    render(
      <DisplayRemoteToast announcementId={0} pageName="" position={{ index: 0, total: 0 }} paused={false} />,
    )

    expect(screen.getByTestId('display-remote-toast')).toHaveAttribute('aria-hidden', 'true')
  })
})
