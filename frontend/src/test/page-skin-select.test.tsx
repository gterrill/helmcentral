import { fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, test, vi } from 'vitest'

import { PageSkinSelect } from '@/components/page-skin-select'

/**
 * The skin used to live at the bottom of the page-switcher popover, three
 * clicks from the board it re-paints. It belongs with the other layout
 * controls, alongside Add Widget, where the operator already is when they are
 * deciding what the page looks like (ADR 0060).
 */
describe('PageSkinSelect', () => {
  test('shows the page it is editing and the skin that page is on', () => {
    render(<PageSkinSelect page={{ id: 'p1', name: 'Cluster preview', skin: 'instrument' }} onSetSkin={vi.fn()} />)
    expect(screen.getByLabelText('Skin for Cluster preview')).toHaveValue('instrument')
  })

  test('treats a page with no skin as following the app theme', () => {
    render(<PageSkinSelect page={{ id: 'p1', name: 'Anchored' }} onSetSkin={vi.fn()} />)
    expect(screen.getByLabelText('Skin for Anchored')).toHaveValue('default')
  })

  test('reports the chosen skin against the page it belongs to', () => {
    const onSetSkin = vi.fn()
    render(<PageSkinSelect page={{ id: 'p2', name: 'Underway' }} onSetSkin={onSetSkin} />)

    fireEvent.change(screen.getByLabelText('Skin for Underway'), { target: { value: 'instrument' } })
    expect(onSetSkin).toHaveBeenCalledWith('p2', 'instrument')
  })

  // Nothing to skin, nothing to show — rather than a control wired to no page.
  test('renders nothing without a page', () => {
    const { container } = render(<PageSkinSelect page={null} onSetSkin={vi.fn()} />)
    expect(container).toBeEmptyDOMElement()
  })
})
