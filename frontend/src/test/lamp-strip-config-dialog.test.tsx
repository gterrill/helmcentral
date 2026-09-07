import { fireEvent, render, screen } from '@testing-library/react'
import { beforeEach, describe, expect, test, vi } from 'vitest'

import { LampStripConfigDialog } from '@/components/lamp-strip-config-dialog'
import type { LampStripWidgetConfig } from '@/lib/dashboard-widgets'

const statusStrip: LampStripWidgetConfig = {
  title: 'Status',
  lamps: [{ path: 'electrical.generator.state', label: 'GEN' }],
  showCheck: true,
}

beforeEach(() => {
  vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: true, json: async () => ({ paths: [] }) }))
})

describe('LampStripConfigDialog', () => {
  test('seeds from an existing per-page widget unchanged', () => {
    render(
      <LampStripConfigDialog
        widget={{ id: 'lamps:m1x8abcd', lamps: statusStrip }}
        onCancel={vi.fn()}
        onSave={vi.fn()}
      />,
    )
    expect((screen.getByLabelText('Title') as HTMLInputElement).value).toBe('Status')
  })

  test('defaults a fresh per-page widget to "Status"', () => {
    render(
      <LampStripConfigDialog widget={{ id: 'lamps:m1x8abcd' }} onCancel={vi.fn()} onSave={vi.fn()} />,
    )
    expect((screen.getByLabelText('Title') as HTMLInputElement).value).toBe('Status')
  })

  // ADR 0082: the ribbon reuses this dialog under a synthetic { id: 'ribbon' }
  // widget rather than a real DashboardLayoutItem, so a fresh ribbon must get
  // its own default title rather than the per-page widget's "Status".
  test('defaults a fresh ribbon to "Indicators"', () => {
    render(
      <LampStripConfigDialog widget={{ id: 'ribbon' }} onCancel={vi.fn()} onSave={vi.fn()} />,
    )
    expect((screen.getByLabelText('Title') as HTMLInputElement).value).toBe('Indicators')
  })

  test('seeds from an existing ribbon unchanged, not the ribbon default', () => {
    render(
      <LampStripConfigDialog
        widget={{ id: 'ribbon', lamps: statusStrip }}
        onCancel={vi.fn()}
        onSave={vi.fn()}
      />,
    )
    expect((screen.getByLabelText('Title') as HTMLInputElement).value).toBe('Status')
  })

  test('has no remove button for a per-page widget, which has no onRemove', () => {
    render(
      <LampStripConfigDialog
        widget={{ id: 'lamps:m1x8abcd', lamps: statusStrip }}
        onCancel={vi.fn()}
        onSave={vi.fn()}
      />,
    )
    expect(screen.queryByRole('button', { name: /remove ribbon/i })).toBeNull()
  })

  test('shows a Remove ribbon button when onRemove is provided, and calls it', () => {
    const onRemove = vi.fn()
    render(
      <LampStripConfigDialog
        widget={{ id: 'ribbon', lamps: statusStrip }}
        onCancel={vi.fn()}
        onSave={vi.fn()}
        onRemove={onRemove}
      />,
    )
    const button = screen.getByRole('button', { name: /remove ribbon/i })
    // Outline variant, distinct from Cancel's ghost and Save's default.
    expect(button.className).toMatch(/border-border/)
    fireEvent.click(button)
    expect(onRemove).toHaveBeenCalledTimes(1)
  })

  test('still saves the edited config from the ribbon path', () => {
    const onSave = vi.fn()
    render(
      <LampStripConfigDialog
        widget={{ id: 'ribbon', lamps: statusStrip }}
        onCancel={vi.fn()}
        onSave={onSave}
        onRemove={vi.fn()}
      />,
    )
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))
    expect(onSave).toHaveBeenCalledWith(statusStrip)
  })
})
