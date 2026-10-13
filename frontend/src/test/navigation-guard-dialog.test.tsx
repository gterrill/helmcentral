/**
 * NavigationGuardDialog (ADR 0171): the question a guard asks, in the words
 * the guard gave when it held the move.
 */
import { describe, it, expect, vi } from 'vitest'
import { render, screen, fireEvent, waitFor, act } from '@testing-library/react'
import { NavigationGuardDialog } from '@/components/navigation-guard-dialog'
import type { NavigationGuard } from '@/lib/app-router'
import { testRouter } from './helpers/router'

function setup(path: string, guard: Partial<NavigationGuard>) {
  const harness = testRouter(path)
  harness.router.registerGuard(() => ({ owner: 'x', label: 'Settings', blocks: () => ({ kind: 'dirty' }), ...guard }))
  render(<NavigationGuardDialog />, { wrapper: harness.wrapper as never })
  return harness
}

const hold = (router: ReturnType<typeof testRouter>['router']) => act(() => { router.navigate({ panel: 'radar' }) })

describe('NavigationGuardDialog for unsaved changes', () => {
  it('is not showing until a move is held', () => {
    setup('/settings', {})
    expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument()
  })

  it('names the page and offers Cancel, Discard and Save and Continue', async () => {
    const { router } = setup('/settings', { label: 'Details' })
    hold(router)
    expect(await screen.findByRole('alertdialog', { name: 'Unsaved changes' })).toBeInTheDocument()
    expect(screen.getByText(/unsaved changes on the Details page/i)).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Cancel' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Discard' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Save and Continue' })).toBeInTheDocument()
  })

  it('Cancel drops the move and stays', async () => {
    const { router } = setup('/settings', {})
    hold(router)
    fireEvent.click(await screen.findByRole('button', { name: 'Cancel' }))
    expect(router.getSnapshot().pending).toBeNull()
    expect(router.getSnapshot().location.panel).toBe('settings')
    await waitFor(() => expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument())
  })

  it('Discard makes the move without saving', async () => {
    const save = vi.fn()
    const { router } = setup('/settings', { save })
    hold(router)
    fireEvent.click(await screen.findByRole('button', { name: 'Discard' }))
    expect(router.getSnapshot().location.panel).toBe('radar')
    expect(save).not.toHaveBeenCalled()
  })

  it('Save and Continue saves, then makes the move', async () => {
    const save = vi.fn().mockResolvedValue(undefined)
    const { router } = setup('/settings', { save })
    hold(router)
    fireEvent.click(await screen.findByRole('button', { name: 'Save and Continue' }))
    await waitFor(() => expect(router.getSnapshot().location.panel).toBe('radar'))
    expect(save).toHaveBeenCalledTimes(1)
  })

  it('disables Cancel and Discard while the save is running', async () => {
    let finish: () => void = () => {}
    const save = vi.fn(() => new Promise<void>((resolve) => { finish = resolve }))
    const { router } = setup('/settings', { save })
    hold(router)
    fireEvent.click(await screen.findByRole('button', { name: 'Save and Continue' }))
    expect(await screen.findByRole('button', { name: 'Cancel' })).toBeDisabled()
    expect(screen.getByRole('button', { name: 'Discard' })).toBeDisabled()
    await act(async () => { finish() })
    await waitFor(() => expect(router.getSnapshot().location.panel).toBe('radar'))
  })

  it('Save and Continue stays on the page and shows the reason when the save fails', async () => {
    const save = vi.fn().mockRejectedValue(new Error('title already used'))
    const { router } = setup('/settings', { save })
    hold(router)
    fireEvent.click(await screen.findByRole('button', { name: 'Save and Continue' }))
    expect(await screen.findByText('title already used')).toBeInTheDocument()
    expect(router.getSnapshot().location.panel).toBe('settings')
    expect(screen.getByRole('alertdialog', { name: 'Unsaved changes' })).toBeInTheDocument()
  })
})

describe('NavigationGuardDialog for work with nothing to save', () => {
  it('words stocktake work with Stay and Leave, and no Save', async () => {
    const { router } = setup('/inventory/stocktake', { blocks: () => ({ kind: 'stocktake-work' }) })
    hold(router)
    expect(await screen.findByRole('alertdialog', { name: 'Leave stocktake?' })).toBeInTheDocument()
    expect(screen.getByText('The scans from this pass will be cleared.')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Save and Continue' })).not.toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Leave' }))
    expect(router.getSnapshot().location.panel).toBe('radar')
  })

  it('uses the guard\'s own detail for a staged quick-add, and the generic line without one', async () => {
    const { router } = setup('/inventory/bins/LAZ-02', {
      blocks: () => ({ kind: 'quick-add-work', detail: "1 photo for Gaffer tape hasn't uploaded yet." }),
    })
    hold(router)
    expect(await screen.findByRole('alertdialog', { name: 'Leave this bin?' })).toBeInTheDocument()
    expect(screen.getByText("1 photo for Gaffer tape hasn't uploaded yet.")).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Stay' }))
    expect(router.getSnapshot().location.panel).toBe('inventory')
  })

  it('falls back to the generic line when the guard gave no detail', async () => {
    const { router } = setup('/inventory/bins/LAZ-02', { blocks: () => ({ kind: 'quick-add-work' }) })
    hold(router)
    expect(await screen.findByText('The name and photos you have added will be cleared.')).toBeInTheDocument()
  })
})
