import { beforeEach, describe, expect, it, vi } from 'vitest'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'

import { VesselParticularsForm } from '@/components/settings/sections/vessel-particulars-form'

// Live vessel data rides a telemetry stream; the form only reads two values
// from it, so a fixed stand-in is enough.
vi.mock('@/hooks/use-vessel-identity', () => ({ useVesselIdentity: () => ({ boatName: 'M/V Pikorua' }) }))
vi.mock('@/hooks/use-vessel-state', () => ({ useVesselState: () => ({ vesselLengthOverallM: 17.9, vesselDraftM: null }) }))

// GET /api/vessel/particulars example from docs/adr/DRAFT-import-api-contract.md.
const stored = {
  builder: 'Granocean', model: 'W-60', year: 2024,
  hin: 'XXTST00001A000', flag: 'Cook Islands', hailing_port: 'Sydney, Australia',
  hull_type: 'Catamaran', hull_material: 'Fiberglass', displacement_kg: 24500,
  shore_power: 'Dual x 125/250 volt | 30/50 amp', system_voltage: '24v',
  registration: '', imo: '', epirb_id: '', date_acquired: '2025-01-27',
  updated_at: '2026-10-01T05:00:00Z',
}

const fetchMock = vi.fn()

beforeEach(() => {
  fetchMock.mockReset()
  vi.stubGlobal('fetch', fetchMock)
})

describe('VesselParticularsForm', () => {
  it('shows the stored particulars and the live values beside them', async () => {
    fetchMock.mockResolvedValue({ ok: true, json: async () => stored })
    render(<VesselParticularsForm />)

    expect(await screen.findByLabelText('Builder')).toHaveValue('Granocean')
    expect(screen.getByLabelText('Displacement (kg)')).toHaveValue(24500)
    expect(screen.getByText('M/V Pikorua')).toBeInTheDocument()
    expect(screen.getByText('17.9 m')).toBeInTheDocument()
  })

  it('saves the whole record with a PUT', async () => {
    fetchMock.mockImplementation((_url: string, init?: RequestInit) => {
      if (init?.method === 'PUT') {
        return Promise.resolve({ ok: true, json: async () => ({ ...JSON.parse(String(init.body)), updated_at: '2026-10-01T06:00:00Z' }) })
      }
      return Promise.resolve({ ok: true, json: async () => stored })
    })
    render(<VesselParticularsForm />)

    fireEvent.change(await screen.findByLabelText('Flag'), { target: { value: 'Australia' } })
    fireEvent.click(screen.getByRole('button', { name: 'Save particulars' }))

    await screen.findByText('Particulars saved')
    const put = fetchMock.mock.calls.find(([, init]) => (init as RequestInit | undefined)?.method === 'PUT')!
    expect(put[0]).toBe('/api/vessel/particulars')
    expect(JSON.parse(String((put[1] as RequestInit).body))).toMatchObject({ flag: 'Australia', builder: 'Granocean', year: 2024, displacement_kg: 24500 })
  })

  it('shows the server message when a value is refused', async () => {
    fetchMock.mockImplementation((_url: string, init?: RequestInit) => {
      if (init?.method === 'PUT') {
        return Promise.resolve({ ok: false, status: 400, json: async () => ({ field: 'year', message: 'year must be between 1800 and 2200' }) })
      }
      return Promise.resolve({ ok: true, json: async () => stored })
    })
    render(<VesselParticularsForm />)

    fireEvent.change(await screen.findByLabelText('Year built'), { target: { value: '3000' } })
    fireEvent.click(screen.getByRole('button', { name: 'Save particulars' }))

    await waitFor(() => expect(screen.getByRole('alert')).toHaveTextContent('year must be between 1800 and 2200'))
    expect(screen.queryByText('Particulars saved')).not.toBeInTheDocument()
  })
})
