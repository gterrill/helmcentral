import { fireEvent, render, screen } from '@testing-library/react'
import { beforeEach, describe, expect, test, vi } from 'vitest'

import { LampStripConfigDialog } from '@/components/lamp-strip-config-dialog'
import { useSignalKPaths } from '@/hooks/use-signalk-paths'
import type { LampStripWidgetConfig } from '@/lib/dashboard-widgets'
import type { EquipmentTileContext, TileItem } from '@/lib/equipment-tiles'

vi.mock('@/hooks/use-signalk-paths', () => ({
  useSignalKPaths: vi.fn(),
}))

const mockedUseSignalKPaths = vi.mocked(useSignalKPaths)

// The equipment the dialog suggests lamps from (ADR 0185).
let items: TileItem[] = []
vi.mock('@/hooks/use-equipment-tile-context', () => ({
  useEquipmentTileContext: (): EquipmentTileContext => ({ items, profiles: [], reporting: null }),
}))

const statusStrip: LampStripWidgetConfig = {
  title: 'Status',
  lamps: [{ path: 'electrical.generator.state', label: 'GEN' }],
  showCheck: true,
}

const mapped = (id: string, name: string, source: string, kind: TileItem['source_kind']): TileItem => ({
  id, name, signalk_source: source, source_kind: kind, profile_id: '',
})
const enginesAndGenerator: TileItem[] = [
  mapped('eng-p', 'Port', 'propulsion.port', 'engine'),
  mapped('eng-s', 'Stbd', 'propulsion.starboard', 'engine'),
  mapped('gen', 'Gen', 'electrical.generator.0', 'generator'),
]

beforeEach(() => {
  items = []
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

  test('icon and group round-trip through the dialog', () => {
    const onSave = vi.fn()
    render(
      <LampStripConfigDialog
        widget={{ id: 'ribbon', lamps: { ...statusStrip, lamps: [{ path: 'electrical.generator.state', label: 'GEN', group: 'Power', icon: 'generator' }] } }}
        onCancel={vi.fn()}
        onSave={onSave}
      />,
    )
    expect((screen.getByLabelText('Group') as HTMLInputElement).value).toBe('Power')
    expect((screen.getByLabelText('Icon') as HTMLSelectElement).value).toBe('generator')

    fireEvent.change(screen.getByLabelText('Group'), { target: { value: 'Propulsion' } })
    fireEvent.change(screen.getByLabelText('Icon'), { target: { value: 'engine' } })
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))
    expect(onSave.mock.calls[0][0].lamps[0]).toMatchObject({ group: 'Propulsion', icon: 'engine' })
  })

  test('leaving icon on automatic saves no icon', () => {
    const onSave = vi.fn()
    render(<LampStripConfigDialog widget={{ id: 'ribbon', lamps: statusStrip }} onCancel={vi.fn()} onSave={onSave} />)
    expect((screen.getByLabelText('Icon') as HTMLSelectElement).value).toBe('')
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))
    expect(onSave.mock.calls[0][0].lamps[0].icon).toBeUndefined()
  })

  // ADR 0085, ADR 0185: a fresh ribbon prefills from the equipment that is mapped.
  describe('suggested lamps', () => {
    const equipmentIds = () => (screen.getAllByLabelText('Equipment') as HTMLSelectElement[]).map((select) => select.value)

    test('a fresh ribbon prefills from the mapped equipment', () => {
      items = enginesAndGenerator
      render(<LampStripConfigDialog widget={{ id: 'ribbon' }} onCancel={vi.fn()} onSave={vi.fn()} />)

      expect(equipmentIds()).toEqual(['eng-p', 'eng-s', 'gen'])
      const labels = screen.getAllByLabelText('Label') as HTMLInputElement[]
      expect(labels.map((input) => input.value)).toEqual(['Port', 'Stbd', 'Gen'])
    })

    test('saves the suggested lamps as references to the equipment', () => {
      items = enginesAndGenerator
      const onSave = vi.fn()
      render(<LampStripConfigDialog widget={{ id: 'ribbon' }} onCancel={vi.fn()} onSave={onSave} />)
      fireEvent.click(screen.getByRole('button', { name: 'Save' }))
      expect(onSave.mock.calls[0][0].lamps[0]).toMatchObject({ equipmentId: 'eng-p', suffix: 'revolutions', path: '' })
    })

    test('a ribbon with an existing config does not prefill from suggestions', () => {
      items = enginesAndGenerator
      render(<LampStripConfigDialog widget={{ id: 'ribbon', lamps: statusStrip }} onCancel={vi.fn()} onSave={vi.fn()} />)
      const paths = screen.getAllByLabelText('Path') as HTMLInputElement[]
      expect(paths.map((input) => input.value)).toEqual(['electrical.generator.state'])
    })

    test('a page widget does not prefill from suggestions', () => {
      items = enginesAndGenerator
      render(<LampStripConfigDialog widget={{ id: 'lamps:m1x8abcd' }} onCancel={vi.fn()} onSave={vi.fn()} />)
      const paths = screen.getAllByLabelText('Path') as HTMLInputElement[]
      expect(paths.map((input) => input.value)).toEqual([''])
    })

    test('a page widget has no Suggest lamps button', () => {
      items = enginesAndGenerator
      render(<LampStripConfigDialog widget={{ id: 'lamps:m1x8abcd' }} onCancel={vi.fn()} onSave={vi.fn()} />)
      expect(screen.queryByRole('button', { name: /suggest lamps/i })).toBeNull()
    })

    test('Suggest lamps is disabled while no equipment is mapped', () => {
      render(<LampStripConfigDialog widget={{ id: 'ribbon', lamps: statusStrip }} onCancel={vi.fn()} onSave={vi.fn()} />)
      expect(screen.getByRole('button', { name: /suggest lamps/i })).toBeDisabled()
    })

    test('Suggest lamps appends only the suggestions missing from the current list', () => {
      items = enginesAndGenerator
      const existing: LampStripWidgetConfig = {
        title: 'Indicators',
        lamps: [{ path: '', equipmentId: 'eng-p', suffix: 'revolutions', label: 'Port' }],
        showCheck: true,
      }
      render(<LampStripConfigDialog widget={{ id: 'ribbon', lamps: existing }} onCancel={vi.fn()} onSave={vi.fn()} />)

      const button = screen.getByRole('button', { name: /suggest lamps/i })
      expect(button).not.toBeDisabled()
      fireEvent.click(button)
      expect(equipmentIds()).toEqual(['eng-p', 'eng-s', 'gen'])
    })

    test('Suggest lamps is disabled once nothing new would be added', () => {
      items = enginesAndGenerator
      render(<LampStripConfigDialog widget={{ id: 'ribbon' }} onCancel={vi.fn()} onSave={vi.fn()} />)
      // The dialog already auto-prefilled every suggestion.
      expect(screen.getByRole('button', { name: /suggest lamps/i })).toBeDisabled()
    })
  })

  test('a lamp can be switched to read equipment, and will not save without a reading', () => {
    items = enginesAndGenerator
    const onSave = vi.fn()
    render(<LampStripConfigDialog widget={{ id: 'lamps:m1x8abcd' }} onCancel={vi.fn()} onSave={onSave} />)
    fireEvent.change(screen.getByLabelText('Reads'), { target: { value: 'equipment' } })
    expect(screen.getByRole('button', { name: 'Save' })).toBeDisabled()
    fireEvent.change(screen.getByLabelText('Equipment'), { target: { value: 'eng-s' } })
    expect((screen.getByLabelText('Reading') as HTMLInputElement).value).toBe('revolutions')
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))
    expect(onSave.mock.calls[0][0].lamps[0]).toMatchObject({ equipmentId: 'eng-s', suffix: 'revolutions' })
  })

  test('the lamp reading is picked from the usual signal and what the boat reports under the source, never typed', () => {
    items = enginesAndGenerator
    mockedUseSignalKPaths.mockReturnValue({
      paths: [{ path: 'propulsion.port.revolutions', value: 1 }, { path: 'propulsion.port.coolantTemperature', value: 1 }, { path: 'propulsion.starboard.state', value: 1 }],
    })
    render(<LampStripConfigDialog widget={{ id: 'lamps:m1x8abcd', lamps: { title: 'S', lamps: [{ path: '', equipmentId: 'eng-p', suffix: 'revolutions', label: 'Port' }], showCheck: true } }} onCancel={vi.fn()} onSave={vi.fn()} />)
    const reading = screen.getByLabelText('Reading') as HTMLSelectElement
    expect(reading.tagName).toBe('SELECT')
    expect(Array.from(reading.querySelectorAll('option')).map((o) => o.value)).toEqual(['', 'coolantTemperature', 'revolutions'])
  })
})
