import { fireEvent, render, screen, within } from '@testing-library/react'
import { beforeEach, describe, expect, test, vi } from 'vitest'

import { GaugeGroupConfigDialog } from '@/components/gauge-group-config-dialog'
import type { DashboardLayoutItem, GaugeGroupWidgetConfig } from '@/lib/dashboard-widgets'

const portEngine: GaugeGroupWidgetConfig = {
  title: 'Port',
  gauges: [
    { path: 'propulsion.port.revolutions', label: 'RPM', display: 'numeric', quantity: 'frequency', unit: 'rpm' },
    { path: 'propulsion.port.oilPressure', label: 'Oil Press', display: 'numeric', quantity: 'pressure', unit: 'psi' },
    { path: 'environment.depth.belowTransducer', label: 'Depth', display: 'numeric', quantity: 'length', unit: 'ft' },
  ],
}

function widget(config: GaugeGroupWidgetConfig): DashboardLayoutItem {
  return { id: 'gauge-group:m1x8abcd', x: 0, y: 0, w: 6, h: 8, gaugeGroup: config }
}

function pathInputs(): HTMLInputElement[] {
  return screen.getAllByLabelText('SignalK path') as HTMLInputElement[]
}

beforeEach(() => {
  vi.stubGlobal('fetch', vi.fn().mockImplementation((url: string) => {
    if (String(url).includes('/api/equipment-profiles') || String(url).includes('/api/engine-profiles')) {
      return Promise.resolve({
        ok: true,
        json: async () => ({
          profiles: [{
            id: 'alt',
            name: 'Alternator',
            kind: 'alternator',
            gauges: [
              { path_suffix: 'power', label: 'Output', hero: true, display: 'numeric', quantity: 'power', unit: 'W' },
              { path_suffix: 'temperature', label: 'Temp', display: 'numeric', quantity: 'temperature', unit: 'C' },
            ],
          }],
          problems: [],
        }),
      })
    }
    return Promise.resolve({ ok: true, json: async () => ({ paths: [{ path: 'propulsion.port.revolutions' }] }) })
  }))
})

describe('GaugeGroupConfigDialog', () => {
  test('lists one row per member', () => {
    render(<GaugeGroupConfigDialog widget={widget(portEngine)} onCancel={vi.fn()} onSave={vi.fn()} />)
    expect(pathInputs()).toHaveLength(3)
    expect((screen.getByLabelText('Title') as HTMLInputElement).value).toBe('Port')
  })

  test('adds and removes members', () => {
    render(<GaugeGroupConfigDialog widget={widget(portEngine)} onCancel={vi.fn()} onSave={vi.fn()} />)

    fireEvent.click(screen.getByRole('button', { name: /add gauge/i }))
    expect(pathInputs()).toHaveLength(4)

    fireEvent.click(screen.getAllByRole('button', { name: /remove gauge/i })[3])
    expect(pathInputs()).toHaveLength(3)
  })

  test('will not save a group with a blank path, which the backend rejects', () => {
    render(<GaugeGroupConfigDialog widget={widget(portEngine)} onCancel={vi.fn()} onSave={vi.fn()} />)
    const save = screen.getByRole('button', { name: 'Save' })
    expect(save).not.toBeDisabled()

    fireEvent.click(screen.getByRole('button', { name: /add gauge/i }))
    expect(save).toBeDisabled()
  })

  test('will not save an untitled group', () => {
    render(<GaugeGroupConfigDialog widget={widget(portEngine)} onCancel={vi.fn()} onSave={vi.fn()} />)
    fireEvent.change(screen.getByLabelText('Title'), { target: { value: '  ' } })
    expect(screen.getByRole('button', { name: 'Save' })).toBeDisabled()
  })

  describe('replace in all paths', () => {
    function typeReplacement(from: string, to: string) {
      fireEvent.change(screen.getByLabelText('Replace'), { target: { value: from } })
      fireEvent.change(screen.getByLabelText('With'), { target: { value: to } })
    }

    test('previews the count before anything is applied', () => {
      render(<GaugeGroupConfigDialog widget={widget(portEngine)} onCancel={vi.fn()} onSave={vi.fn()} />)
      typeReplacement('port', 'starboard')

      const preview = screen.getByTestId('gauge-group-replace-preview')
      expect(preview).toHaveTextContent('2 of 3 paths will change')
      expect(within(preview).getByText(/propulsion\.starboard\.revolutions/)).toBeInTheDocument()
      // Nothing has actually changed yet.
      expect(pathInputs()[0].value).toBe('propulsion.port.revolutions')
    })

    test('says so when the search text matches nothing', () => {
      render(<GaugeGroupConfigDialog widget={widget(portEngine)} onCancel={vi.fn()} onSave={vi.fn()} />)
      typeReplacement('nothing-matches', 'x')
      expect(screen.getByTestId('gauge-group-replace-preview')).toHaveTextContent('No paths match')
      expect(screen.getByRole('button', { name: 'Apply' })).toBeDisabled()
    })

    test('rewrites every matching path and leaves labels alone', () => {
      const onSave = vi.fn()
      render(<GaugeGroupConfigDialog widget={widget(portEngine)} onCancel={vi.fn()} onSave={onSave} />)

      typeReplacement('port', 'starboard')
      fireEvent.click(screen.getByRole('button', { name: 'Apply' }))

      expect(pathInputs().map((i) => i.value)).toEqual([
        'propulsion.starboard.revolutions',
        'propulsion.starboard.oilPressure',
        'environment.depth.belowTransducer',
      ])

      fireEvent.click(screen.getByRole('button', { name: 'Save' }))
      expect(onSave).toHaveBeenCalledOnce()
      const saved = onSave.mock.calls[0][0] as GaugeGroupWidgetConfig
      expect(saved.gauges[0].path).toBe('propulsion.starboard.revolutions')
      expect(saved.gauges[0].label).toBe('RPM')
      expect(saved.gauges[1].label).toBe('Oil Press')
    })
  })

  test('duplicates the current config and closes', () => {
    const onDuplicate = vi.fn()
    render(
      <GaugeGroupConfigDialog
        widget={widget(portEngine)}
        onCancel={vi.fn()}
        onSave={vi.fn()}
        onDuplicate={onDuplicate}
      />,
    )

    fireEvent.click(screen.getByRole('button', { name: /duplicate this tile/i }))

    expect(onDuplicate).toHaveBeenCalledOnce()
    expect(onDuplicate.mock.calls[0][0]).toMatchObject({
      title: 'Port',
      gauges: expect.arrayContaining([
        expect.objectContaining({ path: 'propulsion.port.revolutions' }),
      ]),
    })
  })

  test('re-seeds per instance, so editing one group never shows another', () => {
    const other: GaugeGroupWidgetConfig = { title: 'Starboard', gauges: [portEngine.gauges[0]] }
    const { rerender } = render(
      <GaugeGroupConfigDialog widget={widget(portEngine)} onCancel={vi.fn()} onSave={vi.fn()} />,
    )
    rerender(
      <GaugeGroupConfigDialog
        widget={{ ...widget(other), id: 'gauge-group:zzzz9999' }}
        onCancel={vi.fn()}
        onSave={vi.fn()}
      />,
    )
    expect((screen.getByLabelText('Title') as HTMLInputElement).value).toBe('Starboard')
    expect(pathInputs()).toHaveLength(1)
  })

  test('saves the selected hero gauge index', () => {
    const onSave = vi.fn()
    render(<GaugeGroupConfigDialog widget={widget(portEngine)} onCancel={vi.fn()} onSave={onSave} />)

    fireEvent.click(screen.getByRole('button', { name: /set gauge 1 as hero/i }))
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))

    const saved = onSave.mock.calls[0][0] as GaugeGroupWidgetConfig
    expect(saved.hero).toBe(0)
  })

  test('moves the hero index with its gauge when reordered', () => {
    const onSave = vi.fn()
    render(<GaugeGroupConfigDialog widget={widget(portEngine)} onCancel={vi.fn()} onSave={onSave} />)

    fireEvent.click(screen.getByRole('button', { name: /set gauge 1 as hero/i }))
    fireEvent.click(screen.getByRole('button', { name: /move gauge 1 down/i }))
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))

    const saved = onSave.mock.calls[0][0] as GaugeGroupWidgetConfig
    expect(saved.hero).toBe(1)
  })

  test('clears the hero index when that gauge is removed', () => {
    const onSave = vi.fn()
    render(<GaugeGroupConfigDialog widget={widget(portEngine)} onCancel={vi.fn()} onSave={onSave} />)

    fireEvent.click(screen.getByRole('button', { name: /set gauge 2 as hero/i }))
    fireEvent.click(screen.getAllByRole('button', { name: /remove gauge/i })[1])
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))

    const saved = onSave.mock.calls[0][0] as GaugeGroupWidgetConfig
    expect(saved.hero).toBeUndefined()
  })

  test('adopts the profile hero when applying an equipment profile', async () => {
    const onSave = vi.fn()
    render(<GaugeGroupConfigDialog widget={widget(portEngine)} onCancel={vi.fn()} onSave={onSave} />)

    fireEvent.click(screen.getByRole('button', { name: /apply an equipment profile/i }))
    fireEvent.change(await screen.findByLabelText('Engine instance'), { target: { value: 'electrical.alternator.0' } })
    fireEvent.click(screen.getByRole('button', { name: /apply to these gauges/i }))
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))

    const saved = onSave.mock.calls[0][0] as GaugeGroupWidgetConfig
    expect(saved.hero).toBeDefined()
    expect(saved.gauges[saved.hero!].label).toBe('Output')
  })
})
