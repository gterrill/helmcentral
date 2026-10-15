import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { act, render, screen } from '@testing-library/react'
import { StrictMode } from 'react'

import { DisplayUpdatedNotice } from '@/components/display-updated-notice'
import { SEEN_VERSION_KEY } from '@/lib/seen-version'

describe('DisplayUpdatedNotice', () => {
  beforeEach(() => { vi.useFakeTimers(); localStorage.clear() })
  afterEach(() => { vi.useRealTimers(); localStorage.clear() })

  it('shows briefly after a version change, then fades, and is never interactive', async () => {
    localStorage.setItem(SEEN_VERSION_KEY, 'v0.44.0')
    render(<DisplayUpdatedNotice current={{ version: 'v0.45.0', revision: 'a' }} />, { wrapper: StrictMode })

    const notice = screen.getByTestId('display-updated-notice')
    expect(notice).toHaveTextContent('Updated to v0.45.0')
    expect(notice).toHaveAttribute('aria-hidden', 'false')
    expect(notice).toHaveClass('pointer-events-none')

    await act(async () => { await vi.advanceTimersByTimeAsync(10_000) })
    expect(screen.getByTestId('display-updated-notice')).toHaveAttribute('aria-hidden', 'true')
  })

  it('says nothing on a first load or an unchanged version', () => {
    const { rerender } = render(<DisplayUpdatedNotice current={{ version: 'v0.45.0', revision: 'a' }} />)
    expect(screen.queryByTestId('display-updated-notice')).not.toBeInTheDocument()
    rerender(<DisplayUpdatedNotice current={{ version: 'v0.45.0', revision: 'b' }} />)
    expect(screen.queryByTestId('display-updated-notice')).not.toBeInTheDocument()
  })
})
