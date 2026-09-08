import { fireEvent, render, screen } from '@testing-library/react'
import { beforeEach, describe, expect, test, vi } from 'vitest'

import { LampStripConfigDialog } from '@/components/lamp-strip-config-dialog'
import type { SignalKPath } from '@/hooks/use-signalk-paths'
import { useSignalKPaths } from '@/hooks/use-signalk-paths'
import type { LampStripWidgetConfig } from '@/lib/dashboard-widgets'

vi.mock('@/hooks/use-signalk-paths', () => ({
  useSignalKPaths: vi.fn(),
}))

const mockedUseSignalKPaths = vi.mocked(useSignalKPaths)

const statusStrip: LampStripWidgetConfig = {
  title: 'Status',
  lamps: [{ path: 'electrical.generator.state', label: 'GEN' }],
  showCheck: true,
}

// A small slice of a live vessel's published paths (see the fixture at
// frontend/src/test/fixtures/signalk-paths-2026-09-08.json for the real
// shape), enough to exercise the engine and generator catalogue entries.
const enginesAndGenerator: SignalKPath[] = [
  { path: 'propulsion.port.revolutions', value: 33 },
  { path: 'propulsion.starboard.revolutions', value: 32.9 },
  { path: 'electrical.generator.0.stateNumber', value: 0 },
]

beforeEach(() => {
  mockedUseSignalKPaths.mockReturnValue({ paths: [] })
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

  // ADR 0085: a fresh ribbon prefills from what the vessel actually publishes.
  describe('suggested lamps (ADR 0085)', () => {
    test('a fresh ribbon prefills from the suggestions once paths have loaded', () => {
      mockedUseSignalKPaths.mockReturnValue({ paths: enginesAndGenerator })
      render(<LampStripConfigDialog widget={{ id: 'ribbon' }} onCancel={vi.fn()} onSave={vi.fn()} />)

      const paths = screen.getAllByLabelText('Path') as HTMLInputElement[]
      expect(paths.map((input) => input.value)).toEqual([
        'propulsion.port.revolutions',
        'propulsion.starboard.revolutions',
        'electrical.generator.0.stateNumber',
      ])
      const labels = screen.getAllByLabelText('Label') as HTMLInputElement[]
      expect(labels.map((input) => input.value)).toEqual(['Port', 'Stbd', 'Gen'])
    })

    test('a ribbon with an existing config does not prefill from suggestions', () => {
      mockedUseSignalKPaths.mockReturnValue({ paths: enginesAndGenerator })
      render(
        <LampStripConfigDialog
          widget={{ id: 'ribbon', lamps: statusStrip }}
          onCancel={vi.fn()}
          onSave={vi.fn()}
        />,
      )
      const paths = screen.getAllByLabelText('Path') as HTMLInputElement[]
      expect(paths.map((input) => input.value)).toEqual(['electrical.generator.state'])
    })

    test('a page widget does not prefill from suggestions', () => {
      mockedUseSignalKPaths.mockReturnValue({ paths: enginesAndGenerator })
      render(
        <LampStripConfigDialog widget={{ id: 'lamps:m1x8abcd' }} onCancel={vi.fn()} onSave={vi.fn()} />,
      )
      const paths = screen.getAllByLabelText('Path') as HTMLInputElement[]
      expect(paths.map((input) => input.value)).toEqual([''])
    })

    test('a page widget has no Suggest lamps button', () => {
      mockedUseSignalKPaths.mockReturnValue({ paths: enginesAndGenerator })
      render(
        <LampStripConfigDialog widget={{ id: 'lamps:m1x8abcd' }} onCancel={vi.fn()} onSave={vi.fn()} />,
      )
      expect(screen.queryByRole('button', { name: /suggest lamps/i })).toBeNull()
    })

    test('Suggest lamps is disabled while paths have not loaded', () => {
      mockedUseSignalKPaths.mockReturnValue({ paths: [] })
      render(
        <LampStripConfigDialog
          widget={{ id: 'ribbon', lamps: statusStrip }}
          onCancel={vi.fn()}
          onSave={vi.fn()}
        />,
      )
      expect(screen.getByRole('button', { name: /suggest lamps/i })).toBeDisabled()
    })

    test('Suggest lamps appends only the suggestions missing from the current list', () => {
      mockedUseSignalKPaths.mockReturnValue({ paths: enginesAndGenerator })
      const existing: LampStripWidgetConfig = {
        title: 'Indicators',
        lamps: [{ path: 'propulsion.port.revolutions', label: 'Port' }],
        showCheck: true,
      }
      render(
        <LampStripConfigDialog
          widget={{ id: 'ribbon', lamps: existing }}
          onCancel={vi.fn()}
          onSave={vi.fn()}
        />,
      )

      const button = screen.getByRole('button', { name: /suggest lamps/i })
      expect(button).not.toBeDisabled()
      fireEvent.click(button)

      const paths = screen.getAllByLabelText('Path') as HTMLInputElement[]
      expect(paths.map((input) => input.value)).toEqual([
        'propulsion.port.revolutions',
        'propulsion.starboard.revolutions',
        'electrical.generator.0.stateNumber',
      ])
    })

    test('Suggest lamps is disabled once nothing new would be added', () => {
      mockedUseSignalKPaths.mockReturnValue({ paths: enginesAndGenerator })
      render(<LampStripConfigDialog widget={{ id: 'ribbon' }} onCancel={vi.fn()} onSave={vi.fn()} />)
      // The dialog already auto-prefilled every suggestion, so there is
      // nothing left for the button to add.
      expect(screen.getByRole('button', { name: /suggest lamps/i })).toBeDisabled()
    })
  })
})
