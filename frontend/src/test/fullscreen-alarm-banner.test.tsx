import { act, fireEvent, render, screen } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { FullscreenAlarmBanner, OverlayCloseContext } from '@/components/fullscreen-alarm-banner'

import { testRouter } from './helpers/router'

const mockAlarms = vi.hoisted(() => ({ list: [] as unknown[] }))
vi.mock('@/hooks/use-alarms', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/hooks/use-alarms')>()),
  useAlarmListComparedOn: () => mockAlarms.list,
  acknowledgeAlarm: vi.fn(),
}))
vi.mock('@/hooks/use-forecast-warnings', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/hooks/use-forecast-warnings')>()),
  useForecastWarnings: () => ({ activeWarning: null }),
}))
const alarm = { rule_id: 'r', label: 'Bilge high', path: 'notifications.bilge', phase: 'active', state: 'alarm', value: 1, message: 'Bilge high', silenced: false, can_silence: false, can_acknowledge: true }

describe('FullscreenAlarmBanner View', () => {
  const t = testRouter('/')
  beforeEach(() => {
    mockAlarms.list = [alarm]
    t.router.navigate({ panel: null, pageId: null }, { force: true })
  })
  const setup = () => {
    const onClose = vi.fn()
    const closeOverlay = vi.fn()
    render(
      <OverlayCloseContext.Provider value={closeOverlay}>
        <FullscreenAlarmBanner onClose={onClose} />
      </OverlayCloseContext.Provider>,
      { wrapper: t.wrapper },
    )
    return { onClose, closeOverlay }
  }
  const hold = () => t.router.registerGuard(() => ({ owner: 'test', label: 'Test', blocks: () => ({ kind: 'dirty' }) }))

  it('closes after it has moved to Alarms', () => {
    const { onClose, closeOverlay } = setup()
    fireEvent.click(screen.getByRole('button', { name: /view/i }))
    expect(t.router.getSnapshot().location.panel).toBe('alarms')
    expect(onClose).toHaveBeenCalledTimes(1)
    expect(closeOverlay).toHaveBeenCalledTimes(1)
  })

  it('closes nothing while a guard holds the move, and closes once the operator leaves', () => {
    const unregister = hold()
    const { onClose, closeOverlay } = setup()
    fireEvent.click(screen.getByRole('button', { name: /view/i }))
    expect(t.router.getSnapshot().pending).not.toBeNull()
    expect(onClose).not.toHaveBeenCalled()
    expect(closeOverlay).not.toHaveBeenCalled()
    act(() => t.router.confirmPending())
    expect(t.router.getSnapshot().location.panel).toBe('alarms')
    expect(onClose).toHaveBeenCalledTimes(1)
    expect(closeOverlay).toHaveBeenCalledTimes(1)
    unregister()
  })

  it('closes nothing when the operator stays', () => {
    const unregister = hold()
    const { onClose, closeOverlay } = setup()
    fireEvent.click(screen.getByRole('button', { name: /view/i }))
    act(() => t.router.cancelPending())
    expect(onClose).not.toHaveBeenCalled()
    expect(closeOverlay).not.toHaveBeenCalled()
    unregister()
  })
})
