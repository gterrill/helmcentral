/**
 * Settings' navigation guard and route wiring (ADR 0171), with the page
 * itself stubbed: what is under test is that the panel registers a guard
 * exactly while the form is dirty, holds only a move out of Settings, and
 * saves through the page's own handle.
 */
import { forwardRef, useImperativeHandle } from 'react'
import { describe, it, expect, vi } from 'vitest'
import { render, screen, fireEvent, act } from '@testing-library/react'
import { SettingsRoutePanel } from '@/components/settings/settings-route-panel'
import { testRouter } from './helpers/router'

const save = vi.fn().mockResolvedValue(undefined)
const pageRef = vi.hoisted(() => ({ detach: false }))

vi.mock('@/components/settings/settings-page', () => ({
  SettingsPage: forwardRef(function SettingsPageStub(
    props: {
      onDirtyChange?: (dirty: boolean) => void
      activeSectionId?: string
      onSectionChange?: (id: string) => void
      importRunId?: string | null
      onImportRunChange?: (runId: string | null) => void
    },
    ref,
  ) {
    useImperativeHandle(ref, () => (pageRef.detach ? (null as never) : { save }))
    return (
      <div>
        <p data-testid="section">{props.activeSectionId}</p>
        <p data-testid="run">{props.importRunId ?? 'none'}</p>
        <button type="button" onClick={() => props.onDirtyChange?.(true)}>make-dirty</button>
        <button type="button" onClick={() => props.onDirtyChange?.(false)}>make-clean</button>
        <button type="button" onClick={() => props.onSectionChange?.('security')}>go-security</button>
        <button type="button" onClick={() => props.onImportRunChange?.('run-7')}>open-run</button>
      </div>
    )
  }),
}))

function mount(path = '/settings') {
  const harness = testRouter(path)
  render(<SettingsRoutePanel onAskMate={vi.fn()} />, { wrapper: harness.wrapper as never })
  return harness
}

describe('SettingsRoutePanel', () => {
  it('shows the section and run the location names', () => {
    mount('/settings/import/run-1')
    expect(screen.getByTestId('section')).toHaveTextContent('import')
    expect(screen.getByTestId('run')).toHaveTextContent('run-1')
  })

  it('moves between sections and into a run through the location', () => {
    const { history } = mount('/settings')
    fireEvent.click(screen.getByText('go-security'))
    expect(history.path()).toBe('/settings/security')
    fireEvent.click(screen.getByText('open-run'))
    expect(history.path()).toBe('/settings/import/run-7')
    expect(screen.getByTestId('section')).toHaveTextContent('import')
  })

  it('registers no guard while the form is clean, and one while it is dirty', () => {
    const { router } = mount()
    expect(router.hasGuards()).toBe(false)
    fireEvent.click(screen.getByText('make-dirty'))
    expect(router.getSnapshot().guardOwners).toEqual(['settings'])
    fireEvent.click(screen.getByText('make-clean'))
    expect(router.hasGuards()).toBe(false)
  })

  it('holds a move out of Settings while dirty, naming the Settings page', () => {
    const { router } = mount()
    fireEvent.click(screen.getByText('make-dirty'))
    let moved = true
    act(() => { moved = router.navigate({ panel: 'forecast' }) })
    expect(moved).toBe(false)
    expect(router.getSnapshot().pending).toMatchObject({ kind: 'dirty', label: 'Settings' })
  })

  it('does not hold a move between sections', () => {
    const { router } = mount()
    fireEvent.click(screen.getByText('make-dirty'))
    expect(router.navigate({ panel: 'settings', route: { view: 'section', section: 'signalk' } })).toBe(true)
  })

  it('Save and Continue saves through the page\'s own handle', async () => {
    const { router } = mount()
    fireEvent.click(screen.getByText('make-dirty'))
    act(() => { router.navigate({ panel: 'forecast' }) })
    await act(async () => { await router.saveAndContinue() })
    expect(save).toHaveBeenCalledTimes(1)
    expect(router.getSnapshot().location.panel).toBe('forecast')
  })

  it('Save and Continue stays put and keeps the move held when the page\'s save fails', async () => {
    save.mockRejectedValueOnce(new Error('boom'))
    const { router } = mount()
    fireEvent.click(screen.getByText('make-dirty'))
    act(() => { router.navigate({ panel: 'forecast' }) })
    await act(async () => { await expect(router.saveAndContinue()).rejects.toThrow('boom') })
    expect(router.getSnapshot().pending).toMatchObject({ kind: 'dirty' })
    expect(router.getSnapshot().location.panel).toBe('settings')
  })

  it('Save and Continue fails rather than moving on when the page is not mounted to save', async () => {
    pageRef.detach = true
    try {
      const { router } = mount()
      fireEvent.click(screen.getByText('make-dirty'))
      act(() => { router.navigate({ panel: 'forecast' }) })
      await act(async () => { await expect(router.saveAndContinue()).rejects.toThrow(/not available/i) })
      expect(router.getSnapshot().location.panel).toBe('settings')
    } finally {
      pageRef.detach = false
    }
  })

  it('stops guarding when the panel goes away', () => {
    const harness = testRouter('/settings')
    const { unmount } = render(<SettingsRoutePanel onAskMate={vi.fn()} />, { wrapper: harness.wrapper as never })
    fireEvent.click(screen.getByText('make-dirty'))
    expect(harness.router.hasGuards()).toBe(true)
    unmount()
    expect(harness.router.hasGuards()).toBe(false)
  })
})
