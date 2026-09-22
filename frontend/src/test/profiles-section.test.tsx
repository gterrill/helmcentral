import { beforeEach, describe, expect, it, vi } from 'vitest'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { EquipmentSection } from '@/components/settings/sections/equipment-section'

const profile = {
  schema_version: 1,
  kind: 'engine',
  id: 'cummins-qsb67-550',
  name: 'Cummins QSB 6.7 550',
  gauges: [
    {
      path_suffix: 'oilPressure',
      label: 'Oil Press',
      display: 'radial',
      quantity: 'pressure',
      unit: 'psi',
      min: 0,
      max: 100,
    },
  ],
}

// A second profile, so a test can select something other than profiles[0]
// and prove the UI is actually tracking that selection rather than always
// falling back to the first entry.
const secondProfile = {
  schema_version: 1,
  kind: 'generator',
  id: 'onan-mdkdm',
  name: 'Onan MDKDM',
  gauges: [
    {
      path_suffix: 'phase.A.frequency',
      label: 'Frequency',
      display: 'numeric',
      quantity: 'frequency',
      unit: 'Hz',
    },
  ],
}

/**
 * Clicks a button that the section disables while the profile list is still
 * loading. The list reaches the form a microtask before the hook clears
 * `loading`, so a bare click can land on a still-disabled control and do
 * nothing at all, silently, leaving the assertion after it to fail.
 */
async function clickWhenEnabled(name: string) {
  // Re-queried on every attempt, not captured once: a render between the
  // lookup and the click swaps the DOM node, and clicking the detached one
  // does nothing.
  await waitFor(() => expect(screen.getByRole('button', { name })).toBeEnabled())
  fireEvent.click(screen.getByRole('button', { name }))
}

// Waits on the name field rather than findByDisplayValue. While selectedID is
// still settling (value="" matches no <option>) the <select> shows its first
// option, which carries this same text, so a bare display-value match can
// resolve before the profile has been selected and loaded.
async function waitForFirstProfile() {
  await waitFor(() => expect(screen.getByLabelText('Profile name')).toHaveValue('Cummins QSB 6.7 550'))
}

const openMock = vi.fn()
const fetchMock = vi.fn()

beforeEach(() => {
  openMock.mockReset()
  vi.stubGlobal('open', openMock)
  fetchMock.mockReset()
  fetchMock.mockImplementation((url: string, init?: RequestInit) => {
    if (String(url).includes('/api/equipment-profiles') && (!init || init.method === undefined)) {
      return Promise.resolve({ ok: true, json: async () => ({ profiles: [profile, secondProfile], problems: [] }) })
    }

    if (String(url).includes('/api/equipment-profiles') && init?.method === 'PUT') {
      const body = typeof init.body === 'string' ? init.body : ''
      if (body.includes('schema-error-profile')) {
        return Promise.resolve({
          ok: false,
          status: 400,
          json: async () => ({
            error: 'profile failed schema validation',
            errors: [{ path: 'gauges.0.path_suffix', message: 'must start with phase. for generator profile' }],
          }),
        })
      }
      return Promise.resolve({ ok: true, json: async () => ({ profile }) })
    }

    if (String(url).includes('/api/equipment-profiles') && init?.method === 'POST') {
      const body = typeof init.body === 'string' ? init.body : ''
      if (body.includes('cummins-qsb67-550')) {
        return Promise.resolve({
          ok: false,
          status: 409,
          json: async () => ({ error: 'profile id already exists' }),
        })
      }
      return Promise.resolve({ ok: true, status: 201, json: async () => ({ profile }) })
    }

    if (String(url).includes('/api/equipment-profiles') && init?.method === 'DELETE') {
      return Promise.resolve({ ok: true, status: 204, json: async () => ({}) })
    }
    return Promise.resolve({ ok: false, json: async () => ({ error: 'not found' }) })
  })
  vi.stubGlobal('fetch', fetchMock)
})

describe('EquipmentSection', () => {
  it('lets the operator edit the selected profile', async () => {
    render(<EquipmentSection />)

    await waitForFirstProfile()

    const nameInput = screen.getByLabelText('Profile name')
    expect(nameInput).toHaveAttribute('readonly')

    await clickWhenEnabled('Edit')

    expect(screen.getByRole('button', { name: 'Save changes' })).toBeInTheDocument()
    expect(screen.getByLabelText('Profile name')).not.toHaveAttribute('readonly')

    fireEvent.change(screen.getByLabelText('Profile name'), {
      target: { value: 'Cummins QSB 6.7 600' },
    })

    expect(screen.getByLabelText('Profile name')).toHaveValue('Cummins QSB 6.7 600')
  })

  it('validates profile JSON before saving and does not call the API on parse failure', async () => {
    render(<EquipmentSection />)
    await waitForFirstProfile()

    await clickWhenEnabled('Edit')
    fireEvent.change(screen.getByLabelText('Profile JSON'), {
      target: { value: '{"id":' },
    })

    fireEvent.click(screen.getByRole('button', { name: 'Save changes' }))

    const errors = await screen.findAllByText('Profile JSON is invalid. Fix parsing errors before saving.')
    expect(errors.length).toBeGreaterThan(0)
    expect(globalThis.fetch).toHaveBeenCalledTimes(1)
  })

  it('saves valid JSON to the profile update endpoint', async () => {
    render(<EquipmentSection />)
    await waitForFirstProfile()

    await clickWhenEnabled('Edit')
    fireEvent.change(screen.getByLabelText('Profile JSON'), {
      target: {
        value: JSON.stringify({ ...profile, name: 'Cummins QSB 6.7 600' }, null, 2),
      },
    })
    fireEvent.change(screen.getByLabelText('Profile name'), {
      target: { value: 'Cummins QSB 6.7 600' },
    })

    fireEvent.click(screen.getByRole('button', { name: 'Save changes' }))

    await waitFor(() => expect(screen.queryByRole('button', { name: 'Save changes' })).not.toBeInTheDocument())
    expect(globalThis.fetch).toHaveBeenCalledWith(
      expect.stringContaining('/api/equipment-profiles/cummins-qsb67-550'),
      expect.objectContaining({
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
      }),
    )
  })

  it('creates a new profile from the New profile action', async () => {
    render(<EquipmentSection />)
    await waitForFirstProfile()

    fireEvent.click(screen.getByRole('button', { name: 'New profile' }))
    fireEvent.change(screen.getByLabelText('Profile name'), {
      target: { value: 'New equipment profile' },
    })
    fireEvent.click(screen.getByRole('button', { name: 'Save changes' }))

    await waitFor(() => {
      expect(globalThis.fetch).toHaveBeenCalledWith(
        expect.stringContaining('/api/equipment-profiles'),
        expect.objectContaining({ method: 'POST' }),
      )
    })
  })

  it('switches new profile template based on selected kind', async () => {
    render(<EquipmentSection />)
    await waitForFirstProfile()

    fireEvent.click(screen.getByRole('button', { name: 'New profile' }))

    const kindSelect = screen.getByLabelText('Profile kind')
    expect(kindSelect).toHaveValue('engine')
    expect((screen.getByLabelText('Profile JSON') as HTMLTextAreaElement).value).toContain('"kind": "engine"')

    fireEvent.change(kindSelect, { target: { value: 'alternator' } })

    expect(screen.getByLabelText('Profile name')).toHaveValue('New alternator profile')
    expect((screen.getByLabelText('Profile JSON') as HTMLTextAreaElement).value).toContain('"kind": "alternator"')
    expect((screen.getByLabelText('Profile JSON') as HTMLTextAreaElement).value).toContain('"path_suffix": "voltage"')
    expect((screen.getByLabelText('Profile JSON') as HTMLTextAreaElement).value).toContain('"hero": true')

    fireEvent.change(kindSelect, { target: { value: 'generator' } })

    expect(screen.getByLabelText('Profile name')).toHaveValue('New generator profile')
    expect((screen.getByLabelText('Profile JSON') as HTMLTextAreaElement).value).toContain('"kind": "generator"')
    expect((screen.getByLabelText('Profile JSON') as HTMLTextAreaElement).value).toContain('"path_suffix": "phase.A.frequency"')
  })

  it('asks for confirmation before deleting, naming the profile, and does not delete on cancel', async () => {
    render(<EquipmentSection />)
    await waitForFirstProfile()

    await clickWhenEnabled('Delete equipment profile')

    // The DELETE must not fire until the operator confirms.
    expect(screen.getByText('Delete "Cummins QSB 6.7 550"?')).toBeInTheDocument()
    expect(fetchMock.mock.calls.some(([, init]) => (init as RequestInit | undefined)?.method === 'DELETE')).toBe(false)

    fireEvent.click(screen.getByRole('button', { name: 'Cancel' }))

    await waitFor(() => {
      expect(screen.queryByText('Delete "Cummins QSB 6.7 550"?')).not.toBeInTheDocument()
    })
    expect(fetchMock.mock.calls.some(([, init]) => (init as RequestInit | undefined)?.method === 'DELETE')).toBe(false)
  })

  it('deletes the selected profile via API once the confirmation dialog is accepted', async () => {
    render(<EquipmentSection />)
    await waitForFirstProfile()

    await clickWhenEnabled('Delete equipment profile')
    await screen.findByText('Delete "Cummins QSB 6.7 550"?')
    fireEvent.click(screen.getByRole('button', { name: 'Delete' }))

    await waitFor(() => {
      expect(globalThis.fetch).toHaveBeenCalledWith(
        expect.stringContaining('/api/equipment-profiles/cummins-qsb67-550'),
        expect.objectContaining({ method: 'DELETE' }),
      )
    })
  })

  it('opens download endpoint for the selected profile', async () => {
    render(<EquipmentSection />)
    await waitForFirstProfile()

    fireEvent.click(screen.getByRole('button', { name: 'Download equipment profile' }))

    expect(openMock).toHaveBeenCalledWith(
      expect.stringContaining('/api/equipment-profiles/cummins-qsb67-550/download'),
      '_blank',
    )
  })

  it('uploads a profile file through the equipment create endpoint', async () => {
    render(<EquipmentSection />)
    await waitForFirstProfile()

    const uploadInput = screen.getByLabelText('Upload equipment profile file') as HTMLInputElement
    const uploadFile = new File([
      JSON.stringify({
        schema_version: 1,
        kind: 'generator',
        id: 'uploaded-generator',
        name: 'Uploaded Generator',
        gauges: [
          {
            path_suffix: 'phase.A.frequency',
            label: 'Hz',
            display: 'numeric',
            quantity: 'frequency',
            unit: 'Hz',
          },
        ],
      }),
    ], 'uploaded-generator.json', { type: 'application/json' })

    fireEvent.change(uploadInput, { target: { files: [uploadFile] } })

    await waitFor(() => {
      expect(fetchMock).toHaveBeenCalledWith(
        expect.stringContaining('/api/equipment-profiles'),
        expect.objectContaining({ method: 'POST' }),
      )
    })
  })

  it('shows duplicate-id upload conflict returned by the backend', async () => {
    render(<EquipmentSection />)
    await waitForFirstProfile()

    const uploadInput = screen.getByLabelText('Upload equipment profile file') as HTMLInputElement
    const uploadFile = new File([
      JSON.stringify({
        schema_version: 1,
        kind: 'engine',
        id: 'cummins-qsb67-550',
        name: 'Duplicate',
        gauges: [
          {
            path_suffix: 'oilPressure',
            label: 'Oil Press',
            display: 'radial',
            quantity: 'pressure',
            unit: 'psi',
            min: 0,
            max: 100,
          },
        ],
      }),
    ], 'duplicate.json', { type: 'application/json' })

    fireEvent.change(uploadInput, { target: { files: [uploadFile] } })

    await screen.findByText('profile id already exists')
  })

  it('renders schema validation path details returned on save', async () => {
    render(<EquipmentSection />)
    await waitForFirstProfile()

    await clickWhenEnabled('Edit')
    fireEvent.change(screen.getByLabelText('Profile JSON'), {
      target: {
        value: JSON.stringify({ ...profile, notes: 'schema-error-profile' }, null, 2),
      },
    })

    fireEvent.click(screen.getByRole('button', { name: 'Save changes' }))

    await screen.findByText('profile failed schema validation')
    await screen.findByText('gauges.0.path_suffix: must start with phase. for generator profile')
  })

  it('rejects an uploaded file with invalid JSON before API create', async () => {
    render(<EquipmentSection />)
    await waitForFirstProfile()

    const uploadInput = screen.getByLabelText('Upload equipment profile file') as HTMLInputElement
    const uploadFile = new File(['{"id":'], 'bad.json', { type: 'application/json' })

    fireEvent.change(uploadInput, { target: { files: [uploadFile] } })

    const errors = await screen.findAllByText('Uploaded file is not valid JSON.')
    expect(errors.length).toBeGreaterThan(0)

    const postCalls = fetchMock.mock.calls.filter(([, init]) => (init as RequestInit | undefined)?.method === 'POST')
    expect(postCalls).toHaveLength(0)
  })

  // "Nothing selected" was overloaded onto selectedID === '', which also
  // happens to match no profile and so fell back to profiles[0]. Selecting
  // any profile other than the first, then starting a new one, is the exact
  // repro: the fallback silently substituted the first profile back in and
  // the fresh draft never appeared.
  it('shows a blank new-profile draft after selecting a profile other than the first', async () => {
    render(<EquipmentSection />)
    await waitForFirstProfile()

    fireEvent.change(screen.getByLabelText('Profile'), { target: { value: secondProfile.id } })
    // Scoped to the name field specifically: the <select>'s own selected
    // option carries this same text, and matching on display value alone is
    // ambiguous between the two.
    await waitFor(() => expect(screen.getByLabelText('Profile name')).toHaveValue(secondProfile.name))

    fireEvent.click(screen.getByRole('button', { name: 'New profile' }))

    expect(screen.getByLabelText('Profile name')).toHaveValue('New engine profile')
    expect(screen.getByLabelText('Profile name')).not.toHaveAttribute('readonly')
    expect(screen.getByRole('button', { name: 'Save changes' })).toBeInTheDocument()
  })

  it('shows the uploaded profile immediately, and still shows it once the list reload lands', async () => {
    render(<EquipmentSection />)
    await waitForFirstProfile()

    // Select a profile that is not profiles[0] first — otherwise the bug
    // (falling back to the first profile while the upload is in flight)
    // would be invisible, since the first profile is already on screen.
    fireEvent.change(screen.getByLabelText('Profile'), { target: { value: secondProfile.id } })
    await waitFor(() => expect(screen.getByLabelText('Profile name')).toHaveValue(secondProfile.name))

    const uploadedProfile = {
      schema_version: 1,
      kind: 'generator',
      id: 'uploaded-generator',
      name: 'Uploaded Generator',
      gauges: [
        { path_suffix: 'phase.A.frequency', label: 'Hz', display: 'numeric', quantity: 'frequency', unit: 'Hz' },
      ],
    }

    // The reload() fetch that follows a successful upload is held open here,
    // so the test can prove the display neither waits on it nor reverts once
    // it lands.
    let releaseReload: () => void = () => {}
    const reloadGate = new Promise<void>((resolve) => { releaseReload = resolve })

    fetchMock.mockImplementation((url: string, init?: RequestInit) => {
      if (String(url).includes('/api/equipment-profiles') && init?.method === 'POST') {
        return Promise.resolve({ ok: true, status: 201, json: async () => ({ profile: uploadedProfile }) })
      }
      if (String(url).includes('/api/equipment-profiles') && (!init || init.method === undefined)) {
        return reloadGate.then(() => ({
          ok: true,
          json: async () => ({ profiles: [profile, secondProfile, uploadedProfile], problems: [] }),
        }))
      }
      return Promise.resolve({ ok: false, json: async () => ({ error: 'not found' }) })
    })

    const uploadInput = screen.getByLabelText('Upload equipment profile file') as HTMLInputElement
    const uploadFile = new File([JSON.stringify(uploadedProfile)], 'uploaded-generator.json', { type: 'application/json' })
    fireEvent.change(uploadInput, { target: { files: [uploadFile] } })

    await waitFor(() => expect(screen.getByLabelText('Profile name')).toHaveValue('Uploaded Generator'))

    releaseReload()
    await waitFor(() => expect(fetchMock.mock.calls.filter(([u, i]) =>
      String(u).includes('/api/equipment-profiles') && (!i || (i as RequestInit).method === undefined),
    ).length).toBeGreaterThan(1))

    // Still the uploaded profile once the reload lands — it must not revert.
    expect(screen.getByLabelText('Profile name')).toHaveValue('Uploaded Generator')
  })

  it('surfaces a failed profile fetch as an error instead of an empty profile list', async () => {
    fetchMock.mockImplementation((url: string, init?: RequestInit) => {
      if (String(url).includes('/api/equipment-profiles') && (!init || init.method === undefined)) {
        return Promise.resolve({ ok: false, status: 500, json: async () => ({ error: 'profiles directory unreadable' }) })
      }
      return Promise.resolve({ ok: false, json: async () => ({ error: 'not found' }) })
    })

    render(<EquipmentSection />)

    expect(await screen.findByText(/profiles directory unreadable/i)).toBeInTheDocument()
  })
})
