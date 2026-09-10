import { render } from '@testing-library/react'
import { describe, expect, test, vi, afterEach } from 'vitest'

import { KioskShell } from '@/components/kiosk-shell'

vi.mock('@/hooks/use-telemetry-stream', () => ({
  useTelemetryStatus: () => 'connected' as const,
}))

describe('KioskShell', () => {
  afterEach(() => {
    document.documentElement.style.cursor = ''
    document.body.style.overflow = ''
  })

  test('renders its children with no rotation by default', () => {
    const { getByTestId, getByText } = render(
      <KioskShell rotate={0} alarms={[]}>
        <div>content</div>
      </KioskShell>,
    )
    expect(getByText('content')).toBeInTheDocument()
    const root = getByTestId('kiosk-root')
    expect(root).toHaveAttribute('data-rotate', '0')
    expect(root.style.transform).toBe('')
  })

  test('rotates the root 180 degrees under rotate=180', () => {
    const { getByTestId } = render(
      <KioskShell rotate={180} alarms={[]}>
        <div>content</div>
      </KioskShell>,
    )
    const root = getByTestId('kiosk-root')
    expect(root).toHaveAttribute('data-rotate', '180')
    expect(root.style.transform).toBe('rotate(180deg)')
  })

  test('hides the cursor and locks body scroll while mounted, restoring both on unmount', () => {
    const { unmount } = render(
      <KioskShell rotate={0} alarms={[]}>
        <div>content</div>
      </KioskShell>,
    )
    expect(document.documentElement.style.cursor).toBe('none')
    expect(document.body.style.overflow).toBe('hidden')

    unmount()
    expect(document.documentElement.style.cursor).toBe('')
    expect(document.body.style.overflow).toBe('')
  })
})
