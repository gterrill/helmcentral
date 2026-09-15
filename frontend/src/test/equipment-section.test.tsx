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

const openMock = vi.fn()
const fetchMock = vi.fn()

beforeEach(() => {
  openMock.mockReset()
  vi.stubGlobal('open', openMock)
  fetchMock.mockReset()
  fetchMock.mockImplementation((url: string, init?: RequestInit) => {
    if (String(url).includes('/api/equipment-profiles') && (!init || init.method === undefined)) {
      return Promise.resolve({ ok: true, json: async () => ({ profiles: [profile], problems: [] }) })
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

    await screen.findByDisplayValue('Cummins QSB 6.7 550')

    const nameInput = screen.getByLabelText('Profile name')
    expect(nameInput).toHaveAttribute('readonly')

    fireEvent.click(screen.getByRole('button', { name: 'Edit' }))

    expect(screen.getByRole('button', { name: 'Save changes' })).toBeInTheDocument()
    expect(screen.getByLabelText('Profile name')).not.toHaveAttribute('readonly')

    fireEvent.change(screen.getByLabelText('Profile name'), {
      target: { value: 'Cummins QSB 6.7 600' },
    })

    expect(screen.getByLabelText('Profile name')).toHaveValue('Cummins QSB 6.7 600')
  })

  it('validates profile JSON before saving and does not call the API on parse failure', async () => {
    render(<EquipmentSection />)
    await screen.findByDisplayValue('Cummins QSB 6.7 550')

    fireEvent.click(screen.getByRole('button', { name: 'Edit' }))
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
    await screen.findByDisplayValue('Cummins QSB 6.7 550')

    fireEvent.click(screen.getByRole('button', { name: 'Edit' }))
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
    await screen.findByDisplayValue('Cummins QSB 6.7 550')

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
    await screen.findByDisplayValue('Cummins QSB 6.7 550')

    fireEvent.click(screen.getByRole('button', { name: 'New profile' }))

    const kindSelect = screen.getByLabelText('Profile kind')
    expect(kindSelect).toHaveValue('engine')
    expect((screen.getByLabelText('Profile JSON') as HTMLTextAreaElement).value).toContain('"kind": "engine"')

    fireEvent.change(kindSelect, { target: { value: 'generator' } })

    expect(screen.getByLabelText('Profile name')).toHaveValue('New generator profile')
    expect((screen.getByLabelText('Profile JSON') as HTMLTextAreaElement).value).toContain('"kind": "generator"')
    expect((screen.getByLabelText('Profile JSON') as HTMLTextAreaElement).value).toContain('"path_suffix": "phase.A.frequency"')
  })

  it('deletes the selected profile via API', async () => {
    render(<EquipmentSection />)
    await screen.findByDisplayValue('Cummins QSB 6.7 550')

    fireEvent.click(screen.getByRole('button', { name: 'Delete equipment profile' }))

    await waitFor(() => {
      expect(globalThis.fetch).toHaveBeenCalledWith(
        expect.stringContaining('/api/equipment-profiles/cummins-qsb67-550'),
        expect.objectContaining({ method: 'DELETE' }),
      )
    })
  })

  it('opens download endpoint for the selected profile', async () => {
    render(<EquipmentSection />)
    await screen.findByDisplayValue('Cummins QSB 6.7 550')

    fireEvent.click(screen.getByRole('button', { name: 'Download equipment profile' }))

    expect(openMock).toHaveBeenCalledWith(
      expect.stringContaining('/api/equipment-profiles/cummins-qsb67-550/download'),
      '_blank',
    )
  })

  it('uploads a profile file through the equipment create endpoint', async () => {
    render(<EquipmentSection />)
    await screen.findByDisplayValue('Cummins QSB 6.7 550')

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
    await screen.findByDisplayValue('Cummins QSB 6.7 550')

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
    await screen.findByDisplayValue('Cummins QSB 6.7 550')

    fireEvent.click(screen.getByRole('button', { name: 'Edit' }))
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
    await screen.findByDisplayValue('Cummins QSB 6.7 550')

    const uploadInput = screen.getByLabelText('Upload equipment profile file') as HTMLInputElement
    const uploadFile = new File(['{"id":'], 'bad.json', { type: 'application/json' })

    fireEvent.change(uploadInput, { target: { files: [uploadFile] } })

    const errors = await screen.findAllByText('Uploaded file is not valid JSON.')
    expect(errors.length).toBeGreaterThan(0)

    const postCalls = fetchMock.mock.calls.filter(([, init]) => (init as RequestInit | undefined)?.method === 'POST')
    expect(postCalls).toHaveLength(0)
  })
})
