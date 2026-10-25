import { fireEvent, render, screen, within } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import { PassageReplayBar, type PassageReplayBarProps } from '@/components/passages/passage-replay-bar'
import { DEFAULT_INSET_VIEW, type InsetView } from '@/lib/passage-inset-view'

function bar(over: Partial<PassageReplayBarProps> = {}) {
  const onInsetViewChange = vi.fn<(v: InsetView) => void>()
  const props: PassageReplayBarProps = {
    clock: 0, total: 100, playing: false, readout: '--', speed: '15s', insetOn: true, disabled: false,
    onToggle: () => {}, onScrub: () => {}, onSpeed: () => {}, onInsetChange: () => {},
    insetView: DEFAULT_INSET_VIEW, onInsetViewChange, ...over,
  }
  render(<PassageReplayBar {...props} />)
  return { onInsetViewChange }
}

const open = () => fireEvent.click(screen.getByRole('button', { name: 'Place overview map' }))

describe('the overview map placement controls', () => {
  it('stay in place but disabled while the overview map is off', () => {
    bar({ insetOn: false })
    expect(screen.getByRole('button', { name: 'Place overview map' })).toBeDisabled()
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

describe('the overview map switch', () => {
  it('shows the map state and turns it on and off', () => {
    const onInsetChange = vi.fn()
    bar({ insetOn: false, onInsetChange })
    const toggle = screen.getByRole('switch', { name: 'Overview map' })
    expect(toggle).not.toBeChecked()
    fireEvent.click(toggle)
    expect(onInsetChange).toHaveBeenCalledWith(true)
  })

  it('turns on from its words as well as the switch itself', () => {
    const onInsetChange = vi.fn()
    bar({ insetOn: false, onInsetChange })
    fireEvent.click(screen.getByText('Overview map'))
    expect(onInsetChange).toHaveBeenCalledTimes(1)
    expect(onInsetChange).toHaveBeenCalledWith(true)
  })

  it('names the box that holds the switch and the placement button', () => {
    bar()
    expect(screen.getByRole('group', { name: 'Overview map' })).toBeInTheDocument()
  })
})

describe('the replay length select', () => {
  it('shows the current length and saves the one picked', async () => {
    const onSpeed = vi.fn()
    bar({ speed: '15s', onSpeed })
    const trigger = screen.getByRole('combobox', { name: 'Replay length' })
    expect(trigger).toHaveTextContent('15 s')
    fireEvent.click(trigger)
    const option = await screen.findByRole('option', { name: '8 s' })
    fireEvent.pointerDown(option)
    fireEvent.pointerUp(option)
    fireEvent.click(option)
    expect(onSpeed).toHaveBeenCalledWith('8s')
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

  it('grey out the retry too while the overview map is off', () => {
    bar({ insetOn: false, insetView: undefined, insetViewError: 'HTTP 500', onInsetViewRetry: () => {} })
    expect(screen.getByRole('button', { name: 'Retry reading the overview map placement' })).toBeDisabled()
  })
})
