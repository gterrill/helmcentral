import { beforeEach, describe, expect, it, vi } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'

import { VesselParticularsForm } from '@/components/settings/sections/vessel-particulars-form'
import { VesselParticularsProvider } from '@/components/settings/vessel-particulars-context'

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

function renderForm() {
  return render(<VesselParticularsProvider><VesselParticularsForm /></VesselParticularsProvider>)
}

beforeEach(() => {
  fetchMock.mockReset()
  vi.stubGlobal('fetch', fetchMock)
})

describe('VesselParticularsForm', () => {
  it('shows the stored particulars and the live values beside them', async () => {
    fetchMock.mockResolvedValue({ ok: true, json: async () => stored })
    renderForm()

    expect(await screen.findByLabelText('Builder')).toHaveValue('Granocean')
    expect(screen.getByLabelText('Displacement (kg)')).toHaveValue(24500)
    expect(screen.getByText('M/V Pikorua')).toBeInTheDocument()
    expect(screen.getByText('17.9 m')).toBeInTheDocument()
  })

  it('has no Save button of its own: the page Save bar owns it', async () => {
    fetchMock.mockResolvedValue({ ok: true, json: async () => stored })
    renderForm()

    await screen.findByLabelText('Builder')
    expect(screen.queryByRole('button', { name: /save/i })).toBeNull()
  })

  it('shows the load failure instead of an empty form', async () => {
    fetchMock.mockResolvedValue({ ok: false, status: 500, json: async () => ({ error: 'database is locked' }) })
    renderForm()

    await waitFor(() => expect(screen.getByRole('alert')).toHaveTextContent('database is locked'))
    expect(screen.queryByLabelText('Builder')).toBeNull()
  })
})
