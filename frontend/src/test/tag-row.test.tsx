import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { TagRow } from '@/components/inventory/tag-row'

beforeEach(() => {
  vi.stubGlobal('isSecureContext', true)
  Object.defineProperty(navigator, 'clipboard', {
    value: { writeText: vi.fn().mockResolvedValue(undefined) },
    configurable: true,
  })
})

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('TagRow', () => {
  it('shows the full URL built from the given path and the page origin', () => {
    render(<TagRow path="/inventory/bins/LAZ-02" />)
    expect(screen.getByText(`${window.location.origin}/inventory/bins/LAZ-02`)).toBeInTheDocument()
  })

  it('copies the URL to the clipboard', async () => {
    render(<TagRow path="/inventory/bins/LAZ-02" />)
    fireEvent.click(screen.getByRole('button', { name: /copy/i }))

    await waitFor(() => expect(navigator.clipboard.writeText).toHaveBeenCalledWith(`${window.location.origin}/inventory/bins/LAZ-02`))
    await screen.findByRole('button', { name: /copied/i })
  })

  it('shows the Chrome-on-Android message and no Write tag button when NFC is unsupported', () => {
    render(<TagRow path="/inventory/bins/LAZ-02" />)
    expect(screen.getByText(/Writing a tag needs Chrome on Android/)).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Write tag' })).not.toBeInTheDocument()
  })

  it('shows the tailnet https note when not in a secure context', () => {
    vi.stubGlobal('isSecureContext', false)
    render(<TagRow path="/inventory/bins/LAZ-02" />)
    expect(screen.getByText(/tailnet https address/)).toBeInTheDocument()
  })

  it('does not show the tailnet https note in a secure context', () => {
    render(<TagRow path="/inventory/bins/LAZ-02" />)
    expect(screen.queryByText(/tailnet https address/)).not.toBeInTheDocument()
  })

  it('writes the tag and shows Written on success, when NFC is supported', async () => {
    const write = vi.fn().mockResolvedValue(undefined)
    vi.stubGlobal('NDEFReader', class { write = write })

    render(<TagRow path="/inventory/bins/LAZ-02" />)
    fireEvent.click(screen.getByRole('button', { name: 'Write tag' }))

    await screen.findByRole('button', { name: 'Written' })
    // An NDEF URL record, not a text record (lib/nfc.ts's writeUrlTag) - a
    // bare string here is what used to leave a phone opening nothing when
    // it tapped the written tag.
    expect(write).toHaveBeenCalledWith(
      { records: [{ recordType: 'url', data: `${window.location.origin}/inventory/bins/LAZ-02` }] },
      { signal: undefined },
    )
  })

  it('shows the thrown error when writing the tag fails', async () => {
    vi.stubGlobal('NDEFReader', class {
      write = vi.fn().mockRejectedValue(new Error('NotAllowedError: permission denied'))
    })

    render(<TagRow path="/inventory/bins/LAZ-02" />)
    fireEvent.click(screen.getByRole('button', { name: 'Write tag' }))

    await screen.findByText('NotAllowedError: permission denied')
  })
})
