import { render, screen, fireEvent, within } from '@testing-library/react'
import { describe, expect, it, vi, beforeEach } from 'vitest'

import { AnchorDropRaiseButton } from '@/components/anchor-drop-raise-button'

const primeAudioContextForAlarmMock = vi.fn().mockResolvedValue(undefined)

vi.mock('@/lib/audio-utils', () => ({
  primeAudioContextForAlarm: () => primeAudioContextForAlarmMock(),
}))

describe('AnchorDropRaiseButton', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  describe('inactive (no anchor watch)', () => {
    it('renders an enabled Drop button that primes audio and calls onDrop when clicked', () => {
      const onDrop = vi.fn()
      const onRaise = vi.fn()
      render(<AnchorDropRaiseButton anchorActive={false} canDrop onDrop={onDrop} onRaise={onRaise} />)

      const dropButton = screen.getByRole('button', { name: 'Drop' })
      expect(dropButton).toBeEnabled()

      fireEvent.click(dropButton)

      expect(primeAudioContextForAlarmMock).toHaveBeenCalledTimes(1)
      expect(onDrop).toHaveBeenCalledTimes(1)
    })

    it('disables the Drop button when canDrop is false', () => {
      render(<AnchorDropRaiseButton anchorActive={false} canDrop={false} onDrop={vi.fn()} onRaise={vi.fn()} />)

      expect(screen.getByRole('button', { name: 'Drop' })).toBeDisabled()
    })

    // DESIGN.md's Two Accents Rule: teal is the instrument-readout token
    // (text-gauge-secondary), not a colour for interactive chrome. Drop is a
    // button, so it takes the standard primary variant like every other
    // button, not a bespoke teal that also measured under the 4.5:1 text
    // contrast floor.
    it('uses the standard primary button colour, not the teal readout token', () => {
      render(<AnchorDropRaiseButton anchorActive={false} canDrop onDrop={vi.fn()} onRaise={vi.fn()} />)

      const dropButton = screen.getByRole('button', { name: 'Drop' })
      expect(dropButton.className).not.toMatch(/teal/)
      expect(dropButton.className).toMatch(/\bbg-primary\b/)
      // Prominence comes from full width and h-11, not hue.
      expect(dropButton.className).toMatch(/\bh-11\b/)
    })

    it('does not render a Raise button', () => {
      render(<AnchorDropRaiseButton anchorActive={false} canDrop onDrop={vi.fn()} onRaise={vi.fn()} />)

      expect(screen.queryByRole('button', { name: 'Raise' })).toBeNull()
    })
  })

  describe('active (anchor watch running)', () => {
    it('renders Raise and no Drop button', () => {
      render(<AnchorDropRaiseButton anchorActive canDrop onDrop={vi.fn()} onRaise={vi.fn()} />)

      expect(screen.getByRole('button', { name: 'Raise' })).toBeInTheDocument()
      expect(screen.queryByRole('button', { name: 'Drop' })).toBeNull()
    })

    it('opens a confirmation dialog mentioning stopping the watch, and clearing the trail and placemarks', () => {
      render(<AnchorDropRaiseButton anchorActive canDrop onDrop={vi.fn()} onRaise={vi.fn()} />)

      fireEvent.click(screen.getByRole('button', { name: 'Raise' }))

      const dialog = screen.getByRole('alertdialog')
      expect(within(dialog).getByText(/Raise anchor\?/i)).toBeInTheDocument()
      expect(within(dialog).getByText(/stops the anchor watch/i)).toBeInTheDocument()
      expect(within(dialog).getByText(/trail/i)).toBeInTheDocument()
      expect(within(dialog).getByText(/placemarks/i)).toBeInTheDocument()
    })

    it('Cancel closes the dialog without calling onRaise', () => {
      const onRaise = vi.fn()
      render(<AnchorDropRaiseButton anchorActive canDrop onDrop={vi.fn()} onRaise={onRaise} />)

      fireEvent.click(screen.getByRole('button', { name: 'Raise' }))
      const dialog = screen.getByRole('alertdialog')
      fireEvent.click(within(dialog).getByRole('button', { name: 'Cancel' }))

      expect(screen.queryByRole('alertdialog')).toBeNull()
      expect(onRaise).not.toHaveBeenCalled()
    })

    it('confirming calls onRaise exactly once', () => {
      const onRaise = vi.fn()
      render(<AnchorDropRaiseButton anchorActive canDrop onDrop={vi.fn()} onRaise={onRaise} />)

      fireEvent.click(screen.getByRole('button', { name: 'Raise' }))
      const dialog = screen.getByRole('alertdialog')
      fireEvent.click(within(dialog).getByRole('button', { name: 'Raise' }))

      expect(onRaise).toHaveBeenCalledTimes(1)
    })
  })
})
