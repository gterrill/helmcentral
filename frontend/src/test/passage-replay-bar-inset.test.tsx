import { fireEvent, render, screen, within } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import { PassageReplayBar, type PassageReplayBarProps } from '@/components/passages/passage-replay-bar'
import { DEFAULT_INSET_VIEW, type InsetView } from '@/lib/passage-inset-view'

function bar(over: Partial<PassageReplayBarProps> = {}) {
  const onInsetViewChange = vi.fn<(v: InsetView) => void>()
  const props: PassageReplayBarProps = {
    clock: 0, total: 100, playing: false, readout: '--', speed: 'normal', insetOn: true, disabled: false,
    onToggle: () => {}, onScrub: () => {}, onSpeed: () => {}, onInsetChange: () => {},
    insetView: DEFAULT_INSET_VIEW, onInsetViewChange, ...over,
  }
  render(<PassageReplayBar {...props} />)
  return { onInsetViewChange }
}

const open = () => fireEvent.click(screen.getByRole('button', { name: 'Place overview map' }))

describe('the overview map placement controls', () => {
  it('are offered only while the overview map is on', () => {
    bar({ insetOn: false })
    expect(screen.queryByRole('button', { name: 'Place overview map' })).toBeNull()
  })

  it('are absent when the caller gives no way to save', () => {
    bar({ onInsetViewChange: undefined })
    expect(screen.queryByRole('button', { name: 'Place overview map' })).toBeNull()
  })

  it('offer the four corners with the current one pressed', () => {
    bar({ insetView: { corner: 'bottom-left', zoomOffset: 0 } })
    open()
    const group = screen.getByRole('group', { name: 'Overview map corner' })
    const names = within(group).getAllByRole('button').map((b) => [b.textContent, b.getAttribute('aria-pressed')])
    expect(names).toEqual([['Top left', 'false'], ['Top right', 'false'], ['Bottom left', 'true'], ['Bottom right', 'false']])
  })

  it('save the chosen corner and keep the zoom step', () => {
    const { onInsetViewChange } = bar({ insetView: { corner: 'top-right', zoomOffset: 2 } })
    open()
    fireEvent.click(screen.getByRole('button', { name: 'Bottom right' }))
    expect(onInsetViewChange).toHaveBeenCalledWith({ corner: 'bottom-right', zoomOffset: 2 })
  })

  it('step the zoom wider and closer, showing the step', () => {
    const { onInsetViewChange } = bar({ insetView: { corner: 'top-left', zoomOffset: 1 } })
    open()
    expect(screen.getByTestId('inset-zoom-step')).toHaveTextContent('+1')
    fireEvent.click(screen.getByRole('button', { name: 'Wider' }))
    expect(onInsetViewChange).toHaveBeenLastCalledWith({ corner: 'top-left', zoomOffset: 0 })
    fireEvent.click(screen.getByRole('button', { name: 'Closer' }))
    expect(onInsetViewChange).toHaveBeenLastCalledWith({ corner: 'top-left', zoomOffset: 2 })
  })

  it('stop at three steps either way', () => {
    bar({ insetView: { corner: 'top-left', zoomOffset: 3 } })
    open()
    expect(screen.getByRole('button', { name: 'Closer' })).toBeDisabled()
    expect(screen.getByRole('button', { name: 'Wider' })).toBeEnabled()
  })

  it('stop at the wide end too', () => {
    bar({ insetView: { corner: 'top-left', zoomOffset: -3 } })
    open()
    expect(screen.getByTestId('inset-zoom-step')).toHaveTextContent('-3')
    expect(screen.getByRole('button', { name: 'Wider' })).toBeDisabled()
  })
})

describe('the placement controls before and without the saved view', () => {
  it('wait, disabled, while the saved placement is still being read', () => {
    bar({ insetView: undefined })
    expect(screen.getByRole('button', { name: 'Place overview map' })).toBeDisabled()
  })

  it('offer a retry, not a quiet default, when the saved placement could not be read', () => {
    const onRetry = vi.fn()
    bar({ insetView: undefined, insetViewError: 'HTTP 500', onInsetViewRetry: onRetry })
    expect(screen.queryByRole('button', { name: 'Place overview map' })).toBeNull()
    fireEvent.click(screen.getByRole('button', { name: 'Retry reading the overview map placement' }))
    expect(onRetry).toHaveBeenCalledTimes(1)
  })
})
