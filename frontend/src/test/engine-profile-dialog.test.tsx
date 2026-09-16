import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { beforeEach, describe, expect, test, vi } from 'vitest'

import { EngineProfileDialog } from '@/components/engine-profile-dialog'
import type { EngineProfile } from '@/lib/engine-profiles'

const bundled: EngineProfile = {
  id: 'cummins-qsb67-550',
  name: 'Cummins QSB 6.7 550',
  source: 'Seaboard Marine — advisory ranges, not factory setpoints',
  gauges: [
    {
      path_suffix: 'oilPressure', label: 'Oil Press', display: 'radial',
      quantity: 'pressure', unit: 'psi', min: 0, max: 100,
      zones: [
        { direction: 'above', threshold: 40, state: 'normal', source: 'sbmar.com' },
        { direction: 'below', threshold: null, state: 'alarm', note: 'from your manual' },
      ],
    },
    {
      path_suffix: 'coolantTemperature', label: 'Coolant', display: 'radial',
      quantity: 'temperature', unit: 'C', min: 0, max: 120,
    },
  ],
  service: [{ id: 'engine-oil', description: 'Engine oil & filter' }],
}

function stubProfiles(profiles: EngineProfile[], problems: { file: string; error: string }[] = []) {
  vi.stubGlobal('fetch', vi.fn().mockImplementation((url: string) => {
    if (String(url).includes('/api/equipment-profiles') || String(url).includes('/api/engine-profiles')) {
      return Promise.resolve({ ok: true, json: async () => ({ profiles, problems }) })
    }
    return Promise.resolve({ ok: true, json: async () => ({ paths: [{ path: 'propulsion.port.oilPressure' }] }) })
  }))
}

function renderDialog(onApply = vi.fn()) {
  render(<EngineProfileDialog open onCancel={vi.fn()} onApply={onApply} />)
  return onApply
}

beforeEach(() => {
  vi.unstubAllGlobals()
  stubProfiles([bundled])
})

describe('EngineProfileDialog', () => {
  test('lists every gauge the profile will create', async () => {
    renderDialog()
    const preview = await screen.findByTestId('engine-profile-preview')
    expect(within(preview).getByText(/Oil Press/)).toBeInTheDocument()
    expect(within(preview).getByText(/Coolant/)).toBeInTheDocument()
  })

  test('shows the equipment kind in the selector for generator profiles', async () => {
    stubProfiles([{ ...bundled, kind: 'generator', id: 'generator-profile', name: 'Generator Profile' }])
    renderDialog()

    const option = await screen.findByRole('option', { name: /Generator Profile \(generator\)/i })
    expect(option).toBeInTheDocument()
  })

  /**
   * The safety gate. Published data gives advisory ranges, not factory
   * setpoints, so a bundled profile must read zero here — and the operator
   * sees that number before anything is applied.
   */
  test('says how many thresholds will raise an alarm', async () => {
    renderDialog()
    expect(await screen.findByTestId('engine-profile-alarm-count')).toHaveTextContent('0 alarm thresholds')
  })

  test('counts the thresholds that are set', async () => {
    stubProfiles([{
      ...bundled,
      gauges: [{
        ...bundled.gauges[0],
        zones: [
          { direction: 'below', threshold: 15, state: 'alarm' },
          { direction: 'below', threshold: 25, state: 'warn' },
        ],
      }],
    }])
    renderDialog()
    expect(await screen.findByTestId('engine-profile-alarm-count')).toHaveTextContent('2 alarm thresholds')
  })

  // A slot is a threshold the manufacturer defines and the profile does not
  // know. Showing it is how the operator learns there is a number to look up.
  test('shows an unset threshold as not set', async () => {
    renderDialog()
    const preview = await screen.findByTestId('engine-profile-preview')
    expect(within(preview).getByText(/not set/i)).toBeInTheDocument()
  })

  test('surfaces the profile source', async () => {
    renderDialog()
    expect(await screen.findByText(/not factory setpoints/i)).toBeInTheDocument()
  })

  test('will not apply without an instance prefix', async () => {
    renderDialog()
    await screen.findByTestId('engine-profile-preview')

    fireEvent.change(screen.getByLabelText(/engine instance/i), { target: { value: '  ' } })
    expect(screen.getByRole('button', { name: /^Add tile$/ })).toBeDisabled()
  })

  test('applies the composed gauges under the chosen instance', async () => {
    const onApply = renderDialog()
    await screen.findByTestId('engine-profile-preview')

    // The instance field seeds from published paths, a separate fetch from
    // the one the preview above waits on. An effect keyed off that seed
    // re-sets the field whenever it changes, so typing over it before the
    // seed has landed is a race: on an unlucky interleaving the seeding
    // effect runs after this test's own change event and clobbers it back to
    // 'propulsion.port', the default the mocked paths resolve to. Waiting for
    // that seed first (as the sibling 'suggests instances' test already
    // does) means the effect's dependencies are settled before this test
    // overwrites the value, so it can't fire again afterwards.
    await waitFor(() => {
      expect((screen.getByLabelText(/engine instance/i) as HTMLInputElement).value).toBe('propulsion.port')
    })

    fireEvent.change(screen.getByLabelText(/engine instance/i), { target: { value: 'propulsion.starboard' } })
    fireEvent.change(screen.getByLabelText(/tile title/i), { target: { value: 'Starboard' } })
    fireEvent.click(screen.getByRole('button', { name: /^Add tile$/ }))

    expect(onApply).toHaveBeenCalledOnce()
    const [title, gauges] = onApply.mock.calls[0]
    expect(title).toBe('Starboard')
    expect(gauges).toHaveLength(2)
    expect(gauges[0].path).toBe('propulsion.starboard.oilPressure')
    expect(gauges[0].zones).toEqual([{ from: 40, to: 100, state: 'normal' }])
  })

  test('suggests instances the server is publishing', async () => {
    const { container } = render(<EngineProfileDialog open onCancel={vi.fn()} onApply={vi.fn()} />)
    await screen.findByTestId('engine-profile-preview')

    await waitFor(() => {
      const options = [...container.ownerDocument.querySelectorAll('#engine-instance-options option')]
      expect(options.map((o) => o.getAttribute('value'))).toContain('propulsion.port')
    })
    // And it seeds the field, so the common case needs no typing at all.
    expect((screen.getByLabelText(/engine instance/i) as HTMLInputElement).value).toBe('propulsion.port')
  })

  // One bad drop-in file must not be silent.
  test('reports a profile that failed to load', async () => {
    stubProfiles([bundled], [{ file: 'broken.json', error: 'unexpected end of JSON input' }])
    renderDialog()
    expect(await screen.findByText(/broken\.json/)).toBeInTheDocument()
  })

  test('says so when there are no profiles at all', async () => {
    stubProfiles([])
    renderDialog()
    expect(await screen.findByText(/no engine profiles/i)).toBeInTheDocument()
  })
})

describe('applying to a tile that already exists', () => {
  const portGauges = [
    { path: 'propulsion.starboard.oilPressure', label: 'My oil', display: 'numeric' as const, quantity: 'raw', unit: 'raw' },
  ]

  /**
   * The suggestion list is whatever the server publishes, which need not be
   * the engine this tile is for. Seeding from the tile is what stops a profile
   * appending starboard gauges to the Port tile.
   */
  test('seeds the instance from the tile, not from the published paths', async () => {
    render(<EngineProfileDialog open existingGauges={portGauges} onCancel={vi.fn()} onApply={vi.fn()} />)
    await screen.findByTestId('engine-profile-preview')

    expect((screen.getByLabelText(/engine instance/i) as HTMLInputElement).value).toBe('propulsion.starboard')
  })

  test('says how many gauges will be updated and how many added', async () => {
    render(<EngineProfileDialog open existingGauges={portGauges} onCancel={vi.fn()} onApply={vi.fn()} />)
    const summary = await screen.findByTestId('engine-profile-change-summary')

    expect(summary).toHaveTextContent('1 gauge updated')
    expect(summary).toHaveTextContent('1 added')
  })

  test('falls back to the published paths when the tile is empty', async () => {
    render(<EngineProfileDialog open existingGauges={[]} onCancel={vi.fn()} onApply={vi.fn()} />)
    await screen.findByTestId('engine-profile-preview')

    expect((screen.getByLabelText(/engine instance/i) as HTMLInputElement).value).toBe('propulsion.port')
  })

  test('shows no update summary when building a new tile', async () => {
    renderDialog()
    await screen.findByTestId('engine-profile-preview')
    expect(screen.queryByTestId('engine-profile-change-summary')).not.toBeInTheDocument()
  })
})
