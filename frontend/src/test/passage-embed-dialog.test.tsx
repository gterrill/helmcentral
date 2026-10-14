import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { PassageEmbedDialog } from '@/components/passages/passage-embed-dialog'

const toast = vi.hoisted(() => ({ success: vi.fn(), error: vi.fn() }))
vi.mock('sonner', () => ({ toast }))

const EMBED = '<div id="hcp-x">card</div>'

function setClipboard(value: unknown) {
  Object.defineProperty(navigator, 'clipboard', { value, configurable: true })
}
function setExec(fn: ((cmd: string) => boolean) | undefined) {
  Object.defineProperty(document, 'execCommand', { value: fn, configurable: true, writable: true })
}

beforeEach(() => { vi.clearAllMocks() })
afterEach(() => { setClipboard(undefined); setExec(undefined) })

describe('PassageEmbedDialog', () => {
  it('shows the embed read-only and already selected', async () => {
    render(<PassageEmbedDialog embed={EMBED} onClose={() => {}} />)
    const box = (await screen.findByRole('textbox', { name: 'Blog embed' })) as HTMLTextAreaElement
    expect(box.readOnly).toBe(true)
    expect(box.value).toBe(EMBED)
    expect(box.className).toContain('font-mono')
    expect(box.selectionStart).toBe(0)
    expect(box.selectionEnd).toBe(EMBED.length)
  })

  it('stays closed with no embed', () => {
    render(<PassageEmbedDialog embed={null} onClose={() => {}} />)
    expect(screen.queryByRole('textbox')).toBeNull()
  })

  it('copies through the clipboard API when the page is secure', async () => {
    const writeText = vi.fn().mockResolvedValue(undefined)
    setClipboard({ writeText })
    render(<PassageEmbedDialog embed={EMBED} onClose={() => {}} />)
    fireEvent.click(await screen.findByRole('button', { name: 'Copy' }))
    await waitFor(() => expect(screen.getByRole('button', { name: 'Copied' })).toBeTruthy())
    expect(writeText).toHaveBeenCalledWith(EMBED)
    expect(toast.success).toHaveBeenCalled()
  })

  it('falls back to copying the selection over plain http', async () => {
    setClipboard(undefined)
    const exec = vi.fn().mockReturnValue(true)
    setExec(exec)
    render(<PassageEmbedDialog embed={EMBED} onClose={() => {}} />)
    fireEvent.click(await screen.findByRole('button', { name: 'Copy' }))
    await waitFor(() => expect(screen.getByRole('button', { name: 'Copied' })).toBeTruthy())
    expect(exec).toHaveBeenCalledWith('copy')
  })

  it('tries the selection when the clipboard API refuses', async () => {
    setClipboard({ writeText: vi.fn().mockRejectedValue(new Error('denied')) })
    const exec = vi.fn().mockReturnValue(true)
    setExec(exec)
    render(<PassageEmbedDialog embed={EMBED} onClose={() => {}} />)
    fireEvent.click(await screen.findByRole('button', { name: 'Copy' }))
    await waitFor(() => expect(exec).toHaveBeenCalled())
    expect(await screen.findByRole('button', { name: 'Copied' })).toBeTruthy()
  })

  it('asks for a manual copy, leaving the text selected, when both routes fail', async () => {
    setClipboard(undefined)
    setExec(() => false)
    render(<PassageEmbedDialog embed={EMBED} onClose={() => {}} />)
    fireEvent.click(await screen.findByRole('button', { name: 'Copy' }))
    expect((await screen.findByRole('status')).textContent).toBe('Press ⌘C (Ctrl+C) to copy the selected text')
    expect(toast.success).not.toHaveBeenCalled()
    expect(toast.error).not.toHaveBeenCalled()
    const box = screen.getByRole('textbox', { name: 'Blog embed' }) as HTMLTextAreaElement
    expect([box.selectionStart, box.selectionEnd]).toEqual([0, EMBED.length])
  })
})
