import { fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, test, vi } from 'vitest'

import { PageDisplaySelect } from '@/components/page-display-select'

const displays = [
  { id: 'd1', name: 'Flybridge' },
  { id: 'd2', name: 'Saloon TV' },
]

describe('PageDisplaySelect', () => {
  test('renders nothing without a page', () => {
    const { container } = render(<PageDisplaySelect page={null} displays={displays} onPatch={vi.fn()} onManageDisplays={vi.fn()} />)
    expect(container).toBeEmptyDOMElement()
  })

  test('shows "Not on a wall" selected and no dwell/condition controls for an unassigned page', () => {
    render(<PageDisplaySelect page={{ id: 'p1', name: 'Cluster preview' }} displays={displays} onPatch={vi.fn()} onManageDisplays={vi.fn()} />)
    expect(screen.getByLabelText('Wall display for Cluster preview')).toHaveValue('')
    expect(screen.queryByLabelText('Seconds for Cluster preview')).not.toBeInTheDocument()
    expect(screen.queryByLabelText('Wall condition for Cluster preview')).not.toBeInTheDocument()
  })

  test('lists "Not on a wall" plus one option per display', () => {
    render(<PageDisplaySelect page={{ id: 'p1', name: 'Cluster preview' }} displays={displays} onPatch={vi.fn()} onManageDisplays={vi.fn()} />)
    const select = screen.getByLabelText('Wall display for Cluster preview') as HTMLSelectElement
    const options = Array.from(select.options).map((option) => [option.value, option.textContent])
    expect(options).toEqual([
      ['', 'Not on a wall'],
      ['d1', 'Flybridge'],
      ['d2', 'Saloon TV'],
    ])
  })

  test('states up front that a wall display clears the hero tile', () => {
    render(<PageDisplaySelect page={{ id: 'p1', name: 'Cluster preview' }} displays={displays} onPatch={vi.fn()} onManageDisplays={vi.fn()} />)
    expect(screen.getByTitle(/clears its hero tile/i)).toBeInTheDocument()
  })

  test('picking a display sends display_id with the stored seconds, or 30 as a default', () => {
    const onPatch = vi.fn()
    const { rerender } = render(<PageDisplaySelect page={{ id: 'p1', name: 'Cluster preview' }} displays={displays} onPatch={onPatch} onManageDisplays={vi.fn()} />)
    fireEvent.change(screen.getByLabelText('Wall display for Cluster preview'), { target: { value: 'd1' } })
    expect(onPatch).toHaveBeenCalledWith('p1', { display_id: 'd1', dwell_seconds: 30 })

    onPatch.mockClear()
    rerender(<PageDisplaySelect page={{ id: 'p1', name: 'Cluster preview', dwell_seconds: 45 }} displays={displays} onPatch={onPatch} onManageDisplays={vi.fn()} />)
    fireEvent.change(screen.getByLabelText('Wall display for Cluster preview'), { target: { value: 'd2' } })
    expect(onPatch).toHaveBeenCalledWith('p1', { display_id: 'd2', dwell_seconds: 45 })
  })

  test('picking "Not on a wall" sends only display_id, leaving dwell and condition alone', () => {
    const onPatch = vi.fn()
    render(<PageDisplaySelect page={{ id: 'p1', name: 'Cluster preview', display_id: 'd1', dwell_seconds: 30, show_when: 'anchored' }} displays={displays} onPatch={onPatch} onManageDisplays={vi.fn()} />)
    fireEvent.change(screen.getByLabelText('Wall display for Cluster preview'), { target: { value: '' } })
    expect(onPatch).toHaveBeenCalledWith('p1', { display_id: '' })
  })

  test('shows the duration and condition controls once assigned', () => {
    render(<PageDisplaySelect page={{ id: 'p1', name: 'Cluster preview', display_id: 'd1', dwell_seconds: 30, show_when: 'anchored' }} displays={displays} onPatch={vi.fn()} onManageDisplays={vi.fn()} />)
    expect(screen.getByLabelText('Seconds for Cluster preview')).toHaveValue(30)
    expect(screen.getByLabelText('Wall condition for Cluster preview')).toHaveValue('anchored')
  })

  test('offers autostate navigation conditions', () => {
    render(<PageDisplaySelect page={{ id: 'p1', name: 'Cluster preview', display_id: 'd1', dwell_seconds: 30 }} displays={displays} onPatch={vi.fn()} onManageDisplays={vi.fn()} />)
    const select = screen.getByLabelText('Wall condition for Cluster preview') as HTMLSelectElement
    const options = Array.from(select.options).map((option) => option.value)
    expect(options).toEqual(['always', 'anchored', 'motoring', 'sailing', 'moored'])
  })

  test('commits an in-range duration on blur', () => {
    const onPatch = vi.fn()
    render(<PageDisplaySelect page={{ id: 'p1', name: 'Cluster preview', display_id: 'd1', dwell_seconds: 30 }} displays={displays} onPatch={onPatch} onManageDisplays={vi.fn()} />)
    const input = screen.getByLabelText('Seconds for Cluster preview')
    fireEvent.change(input, { target: { value: '60' } })
    fireEvent.blur(input)
    expect(onPatch).toHaveBeenCalledWith('p1', { dwell_seconds: 60 })
  })

  test('commits an in-range duration on Enter', () => {
    const onPatch = vi.fn()
    render(<PageDisplaySelect page={{ id: 'p1', name: 'Cluster preview', display_id: 'd1', dwell_seconds: 30 }} displays={displays} onPatch={onPatch} onManageDisplays={vi.fn()} />)
    const input = screen.getByLabelText('Seconds for Cluster preview')
    fireEvent.change(input, { target: { value: '90' } })
    fireEvent.keyDown(input, { key: 'Enter' })
    expect(onPatch).toHaveBeenCalledWith('p1', { dwell_seconds: 90 })
  })

  test('rejects an out-of-range duration: no patch, destructive border', () => {
    const onPatch = vi.fn()
    render(<PageDisplaySelect page={{ id: 'p1', name: 'Cluster preview', display_id: 'd1', dwell_seconds: 30 }} displays={displays} onPatch={onPatch} onManageDisplays={vi.fn()} />)
    const input = screen.getByLabelText('Seconds for Cluster preview')
    fireEvent.change(input, { target: { value: '3601' } })
    fireEvent.blur(input)
    expect(onPatch).not.toHaveBeenCalled()
    expect(input.className).toMatch(/destructive/)
  })

  test('never patches on every keystroke', () => {
    const onPatch = vi.fn()
    render(<PageDisplaySelect page={{ id: 'p1', name: 'Cluster preview', display_id: 'd1', dwell_seconds: 30 }} displays={displays} onPatch={onPatch} onManageDisplays={vi.fn()} />)
    fireEvent.change(screen.getByLabelText('Seconds for Cluster preview'), { target: { value: '99' } })
    expect(onPatch).not.toHaveBeenCalled()
  })

  test('saves the condition immediately on change', () => {
    const onPatch = vi.fn()
    render(<PageDisplaySelect page={{ id: 'p1', name: 'Cluster preview', display_id: 'd1', dwell_seconds: 30 }} displays={displays} onPatch={onPatch} onManageDisplays={vi.fn()} />)
    fireEvent.change(screen.getByLabelText('Wall condition for Cluster preview'), { target: { value: 'anchored' } })
    expect(onPatch).toHaveBeenCalledWith('p1', { show_when: 'anchored' })
  })

  describe('with no displays configured', () => {
    test('offers a single escape hatch instead of a dead-end select', () => {
      const onManageDisplays = vi.fn()
      render(<PageDisplaySelect page={{ id: 'p1', name: 'Cluster preview' }} displays={[]} onPatch={vi.fn()} onManageDisplays={onManageDisplays} />)
      expect(screen.queryByLabelText('Wall display for Cluster preview')).not.toBeInTheDocument()
      fireEvent.click(screen.getByRole('button', { name: /no displays yet/i }))
      expect(onManageDisplays).toHaveBeenCalled()
    })

    test('still states the hero-clearing rule up front', () => {
      render(<PageDisplaySelect page={{ id: 'p1', name: 'Cluster preview' }} displays={[]} onPatch={vi.fn()} onManageDisplays={vi.fn()} />)
      expect(screen.getByTitle(/clears its hero tile/i)).toBeInTheDocument()
    })
  })
})
