import { afterEach, describe, expect, it, vi } from 'vitest'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { toast } from 'sonner'

import { Toaster } from '@/components/ui/sonner'
import { ReloadConfirmDialog, UpdateNotices } from '@/components/update-notices'
import type { VersionWatch } from '@/hooks/use-version-reload'

const watch = (overrides: Partial<VersionWatch> = {}): VersionWatch => ({
  current: { version: 'v1', revision: 'aaa' },
  available: null,
  chunkFailure: null,
  readError: null,
  reportChunkFailure: vi.fn(),
  clearChunkFailure: vi.fn(),
  retryHeldReload: vi.fn(),
  ...overrides,
})
const v2 = { version: 'v2', revision: 'bbb' }

function mount(w: VersionWatch, requestReload = vi.fn()) {
  const view = render(<><Toaster /><UpdateNotices watch={w} requestReload={requestReload} /></>)
  return { ...view, requestReload, again: (next: VersionWatch) => view.rerender(<><Toaster /><UpdateNotices watch={next} requestReload={requestReload} /></>) }
}

afterEach(() => { toast.dismiss() })

describe('UpdateNotices', () => {
  it('says nothing while the build is current', () => {
    mount(watch())
    expect(screen.queryByText('Update available')).not.toBeInTheDocument()
  })

  it('raises Update available with a Reload action that asks to reload', async () => {
    const { requestReload } = mount(watch({ available: v2 }))
    expect(await screen.findByText('Update available')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Reload' }))
    expect(requestReload).toHaveBeenCalledTimes(1)
  })

  it('does not auto-dismiss it', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true })
    try {
      mount(watch({ available: v2 }))
      await screen.findByText('Update available')
      await vi.advanceTimersByTimeAsync(10 * 60 * 1000)
      expect(screen.getByText('Update available')).toBeInTheDocument()
    } finally {
      vi.useRealTimers()
    }
  })

  it('does not bring it back for the same build once dismissed', async () => {
    const { again } = mount(watch({ available: v2 }))
    await screen.findByText('Update available')
    toast.dismiss()
    await waitFor(() => expect(screen.queryByText('Update available')).not.toBeInTheDocument())
    again(watch({ available: { ...v2 } }))
    again(watch({ available: v2, chunkFailure: null }))
    await new Promise((r) => setTimeout(r, 100))
    expect(screen.queryByText('Update available')).not.toBeInTheDocument()
  })

  it('raises it again for a newer build', async () => {
    const { again } = mount(watch({ available: v2 }))
    await screen.findByText('Update available')
    toast.dismiss()
    again(watch({ available: { version: 'v3', revision: 'ccc' } }))
    expect(await screen.findByText('Update available')).toBeInTheDocument()
  })

  it('a screen that failed to load after a deploy asks to reload', async () => {
    const { requestReload } = mount(watch({ chunkFailure: 'needs-update', available: v2 }))
    expect(await screen.findByText('This screen needs the update. Reload to open it.')).toBeInTheDocument()
    fireEvent.click(screen.getAllByRole('button', { name: 'Reload' })[0])
    expect(requestReload).toHaveBeenCalled()
  })

  it('a screen that failed to load on a current build offers Reload, which asks like any other reload', async () => {
    const { requestReload } = mount(watch({ chunkFailure: 'connection' }))
    expect(await screen.findByText("Couldn't load this screen. Check the connection.")).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Retry' })).not.toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Reload' }))
    expect(requestReload).toHaveBeenCalledTimes(1)
  })

  it('clears the failure once it has been shown, so a second one raises the toast again', async () => {
    const w = watch({ chunkFailure: 'connection' })
    mount(w)
    await screen.findByText("Couldn't load this screen. Check the connection.")
    expect(w.clearChunkFailure).toHaveBeenCalledTimes(1)
  })

  it('takes the Update available toast down when the build is current again', async () => {
    const { again } = mount(watch({ available: v2 }))
    await screen.findByText('Update available')
    again(watch({ available: null }))
    await waitFor(() => expect(screen.queryByText('Update available')).not.toBeInTheDocument())
  })
})

describe('ReloadConfirmDialog', () => {
  it('asks before dropping unsaved changes, and reloads only on Reload', () => {
    const onCancel = vi.fn()
    const onConfirm = vi.fn()
    render(<ReloadConfirmDialog open onCancel={onCancel} onConfirm={onConfirm} />)
    expect(screen.getByText('You have unsaved changes. Reload anyway?')).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: 'Stay' }))
    expect(onCancel).toHaveBeenCalled()
    expect(onConfirm).not.toHaveBeenCalled()

    fireEvent.click(screen.getByRole('button', { name: 'Reload' }))
    expect(onConfirm).toHaveBeenCalledTimes(1)
  })

  it('renders nothing while closed', () => {
    render(<ReloadConfirmDialog open={false} onCancel={vi.fn()} onConfirm={vi.fn()} />)
    expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument()
  })
})
