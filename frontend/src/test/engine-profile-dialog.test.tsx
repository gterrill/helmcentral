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

    // Same seeding race the sibling tests below document: the instance
    // field is seeded from a fetch (useSignalKPaths) separate from the one
    // the preview above waits on. Waiting for that seed first - rather than
    // typing over it immediately - means it can't still be in flight when
    // this test ends, which otherwise left it to resolve during whichever
    // test ran next.
    await waitFor(() => {
      expect((screen.getByLabelText(/engine instance/i) as HTMLInputElement).value).toBe('propulsion.port')
    })

    fireEvent.change(screen.getByLabelText(/engine instance/i), { target: { value: '  ' } })
    expect(screen.getByRole('button', { name: /^Add tile$/ })).toBeDisabled()
  })

  // Deterministic repro for the race the seeding effect used to lose: the
  // instance field is seeded from useSignalKPaths, a fetch entirely separate
  // from the one the preview above waits on. Holding that fetch open lets
  // the test land a keystroke in the exact window where the real bug used to
  // fire - the seed arriving *after* the operator has already typed - without
  // depending on real scheduling luck.
  test('does not clobber a typed instance once the published-paths seed arrives late', async () => {
    let releasePaths: (paths: { path: string }[]) => void = () => {}
    const pathsGate = new Promise<{ path: string }[]>((resolve) => { releasePaths = resolve })

    vi.stubGlobal('fetch', vi.fn().mockImplementation((url: string) => {
      if (String(url).includes('/api/equipment-profiles') || String(url).includes('/api/engine-profiles')) {
        return Promise.resolve({ ok: true, json: async () => ({ profiles: [bundled], problems: [] }) })
      }
      return pathsGate.then((paths) => ({ ok: true, json: async () => ({ paths }) }))
    }))

    renderDialog()
    await screen.findByTestId('engine-profile-preview')

    // The preview appearing only means `profile` resolved - the seeding
    // effect is a *separate* effect off that same profile, and with the
    // profiles fetch resolving outside of any act() the two aren't
    // guaranteed to land in the same commit. Waiting for the title field
    // (which that same effect sets to the profile's own name whenever there
    // is no prefix to seed yet) confirms the effect's first pass, with
    // seededProfileIdRef primed, has already happened - otherwise a keystroke
    // landed here could race that first pass instead of the late paths seed
    // this test means to exercise.
    await waitFor(() => {
      expect(screen.getByLabelText(/tile title/i)).toHaveValue(bundled.name)
    })

    // Nothing has seeded the instance field itself yet - the paths fetch is
    // still held open. Type into it now, before the seed lands.
    fireEvent.change(screen.getByLabelText(/engine instance/i), { target: { value: 'propulsion.starboard' } })

    // Now let the seed land.
    releasePaths([{ path: 'propulsion.port.oilPressure' }])
    await waitFor(() => {
      const options = [...document.querySelectorAll('#engine-instance-options option')]
      expect(options.map((o) => o.getAttribute('value'))).toContain('propulsion.port')
    })

    // The operator's typed value must survive the late-arriving seed.
    expect((screen.getByLabelText(/engine instance/i) as HTMLInputElement).value).toBe('propulsion.starboard')
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

  test('passes the profile-designated hero gauge index on apply', async () => {
    stubProfiles([{
      ...bundled,
      gauges: [
        { ...bundled.gauges[0] },
        { ...bundled.gauges[1], hero: true },
      ],
    }])

    const onApply = renderDialog()
    await screen.findByTestId('engine-profile-preview')
    // The preview appearing only means the profile resolved; the instance
    // seeding effect is separate and can still be in flight (see the seeding
    // race documented on the sibling tests above), which would otherwise
    // leave Add tile disabled here.
    await waitFor(() => expect(screen.getByRole('button', { name: /^Add tile$/ })).toBeEnabled())
    fireEvent.click(screen.getByRole('button', { name: /^Add tile$/ }))

    const [, , , hero] = onApply.mock.calls[0]
    expect(hero).toBe(1)
  })

  test('suggests instances the server is publishing', async () => {
    const { container } = render(<EngineProfileDialog open onCancel={vi.fn()} onApply={vi.fn()} />)
    await screen.findByTestId('engine-profile-preview')

    await waitFor(() => {
      const options = [...container.ownerDocument.querySelectorAll('#engine-instance-options option')]
      expect(options.map((o) => o.getAttribute('value'))).toContain('propulsion.port')
    })
    // And it seeds the field, so the common case needs no typing at all. The
    // datalist above is a plain useMemo and updates the moment `paths`
    // lands; the field's own value comes from a separate effect off that
    // same candidate list, which can still be one commit behind - so this
    // needs its own wait rather than a bare assertion right after the one
    // above resolves.
    await waitFor(() => {
      expect((screen.getByLabelText(/engine instance/i) as HTMLInputElement).value).toBe('propulsion.port')
    })
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
    expect(await screen.findByText(/no equipment profiles/i)).toBeInTheDocument()
  })

  // A failed fetch and an empty profiles directory must not read the same —
  // one is "nothing installed", the other is "the backend is broken" — or the
  // operator has no way to tell which one they are looking at.
  test('surfaces a failed profile fetch as an error, not as an empty list', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
      ok: false,
      status: 500,
      json: async () => ({ error: 'profiles directory unreadable' }),
    }))
    renderDialog()

    expect(await screen.findByText(/profiles directory unreadable/i)).toBeInTheDocument()
    expect(screen.queryByText(/no equipment profiles/i)).not.toBeInTheDocument()
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

    // The preview appearing only means the profile resolved; the instance
    // seeding effect (which computes this value from existingGauges) is a
    // separate effect off that same profile and can still be one commit
    // behind - see the seeding race documented on the sibling tests above.
    await waitFor(() => {
      expect((screen.getByLabelText(/engine instance/i) as HTMLInputElement).value).toBe('propulsion.starboard')
    })
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

    // Unlike the "seeds from the tile" case above, an empty tile falls
    // through to the published-paths candidates - a fetch separate from the
    // one the preview waits on, so the value can still be seeding in when
    // the preview first appears.
    await waitFor(() => {
      expect((screen.getByLabelText(/engine instance/i) as HTMLInputElement).value).toBe('propulsion.port')
    })
  })

  test('shows no update summary when building a new tile', async () => {
    renderDialog()
    await screen.findByTestId('engine-profile-preview')
    expect(screen.queryByTestId('engine-profile-change-summary')).not.toBeInTheDocument()
  })
})
