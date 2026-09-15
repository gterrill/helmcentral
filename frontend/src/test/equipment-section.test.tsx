import { beforeEach, describe, expect, it, vi } from 'vitest'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { EquipmentSection } from '@/components/settings/sections/equipment-section'

const profile = {
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

beforeEach(() => {
  vi.stubGlobal(
    'fetch',
    vi.fn().mockImplementation((url: string, init?: RequestInit) => {
      if (String(url).includes('/api/engine-profiles') && (!init || init.method === undefined)) {
        return Promise.resolve({ ok: true, json: async () => ({ profiles: [profile], problems: [] }) })
      }
      if (String(url).includes('/api/engine-profiles') && init?.method === 'PUT') {
        return Promise.resolve({ ok: true, json: async () => ({ profile }) })
      }
      return Promise.resolve({ ok: false, json: async () => ({ error: 'not found' }) })
    }),
  )
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
      expect.stringContaining('/api/engine-profiles/cummins-qsb67-550'),
      expect.objectContaining({
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
      }),
    )
  })
})
