import { fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, test, vi } from 'vitest'

import { PageKioskSelect } from '@/components/page-kiosk-select'

describe('PageKioskSelect', () => {
  test('renders nothing without a page', () => {
    const { container } = render(<PageKioskSelect page={null} onPatch={vi.fn()} />)
    expect(container).toBeEmptyDOMElement()
  })

  test('shows the checkbox unticked and no duration/condition controls for an unflagged page', () => {
    render(<PageKioskSelect page={{ id: 'p1', name: 'Cluster preview' }} onPatch={vi.fn()} />)
    expect(screen.getByLabelText('Kiosk for Cluster preview')).not.toBeChecked()
    expect(screen.queryByLabelText('Seconds for Cluster preview')).not.toBeInTheDocument()
  })

  test('ticking sends kiosk:true with the stored seconds, or 30 as a default', () => {
    const onPatch = vi.fn()
    const { rerender } = render(<PageKioskSelect page={{ id: 'p1', name: 'Cluster preview' }} onPatch={onPatch} />)
    fireEvent.click(screen.getByLabelText('Kiosk for Cluster preview'))
    expect(onPatch).toHaveBeenCalledWith('p1', { kiosk: true, kiosk_seconds: 30 })

    onPatch.mockClear()
    rerender(<PageKioskSelect page={{ id: 'p1', name: 'Cluster preview', kiosk_seconds: 45 }} onPatch={onPatch} />)
    fireEvent.click(screen.getByLabelText('Kiosk for Cluster preview'))
    expect(onPatch).toHaveBeenCalledWith('p1', { kiosk: true, kiosk_seconds: 45 })
  })

  test('unticking sends only kiosk:false', () => {
    const onPatch = vi.fn()
    render(<PageKioskSelect page={{ id: 'p1', name: 'Cluster preview', kiosk: true, kiosk_seconds: 30 }} onPatch={onPatch} />)
    fireEvent.click(screen.getByLabelText('Kiosk for Cluster preview'))
    expect(onPatch).toHaveBeenCalledWith('p1', { kiosk: false })
  })

  test('shows the duration and condition controls once flagged', () => {
    render(<PageKioskSelect page={{ id: 'p1', name: 'Cluster preview', kiosk: true, kiosk_seconds: 30, kiosk_when: 'anchored' }} onPatch={vi.fn()} />)
    expect(screen.getByLabelText('Seconds for Cluster preview')).toHaveValue(30)
    expect(screen.getByLabelText('Kiosk condition for Cluster preview')).toHaveValue('anchored')
  })

  test('commits an in-range duration on blur', () => {
    const onPatch = vi.fn()
    render(<PageKioskSelect page={{ id: 'p1', name: 'Cluster preview', kiosk: true, kiosk_seconds: 30 }} onPatch={onPatch} />)
    const input = screen.getByLabelText('Seconds for Cluster preview')
    fireEvent.change(input, { target: { value: '60' } })
    fireEvent.blur(input)
    expect(onPatch).toHaveBeenCalledWith('p1', { kiosk_seconds: 60 })
  })

  test('commits an in-range duration on Enter', () => {
    const onPatch = vi.fn()
    render(<PageKioskSelect page={{ id: 'p1', name: 'Cluster preview', kiosk: true, kiosk_seconds: 30 }} onPatch={onPatch} />)
    const input = screen.getByLabelText('Seconds for Cluster preview')
    fireEvent.change(input, { target: { value: '90' } })
    fireEvent.keyDown(input, { key: 'Enter' })
    expect(onPatch).toHaveBeenCalledWith('p1', { kiosk_seconds: 90 })
  })

  test('rejects an out-of-range duration: no patch, destructive border', () => {
    const onPatch = vi.fn()
    render(<PageKioskSelect page={{ id: 'p1', name: 'Cluster preview', kiosk: true, kiosk_seconds: 30 }} onPatch={onPatch} />)
    const input = screen.getByLabelText('Seconds for Cluster preview')
    fireEvent.change(input, { target: { value: '3601' } })
    fireEvent.blur(input)
    expect(onPatch).not.toHaveBeenCalled()
    expect(input.className).toMatch(/destructive/)
  })

  test('never patches on every keystroke', () => {
    const onPatch = vi.fn()
    render(<PageKioskSelect page={{ id: 'p1', name: 'Cluster preview', kiosk: true, kiosk_seconds: 30 }} onPatch={onPatch} />)
    fireEvent.change(screen.getByLabelText('Seconds for Cluster preview'), { target: { value: '99' } })
    expect(onPatch).not.toHaveBeenCalled()
  })

  test('saves the condition immediately on change', () => {
    const onPatch = vi.fn()
    render(<PageKioskSelect page={{ id: 'p1', name: 'Cluster preview', kiosk: true, kiosk_seconds: 30 }} onPatch={onPatch} />)
    fireEvent.change(screen.getByLabelText('Kiosk condition for Cluster preview'), { target: { value: 'anchored' } })
    expect(onPatch).toHaveBeenCalledWith('p1', { kiosk_when: 'anchored' })
  })
})
