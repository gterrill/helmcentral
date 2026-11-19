import { beforeEach, describe, expect, it, vi } from 'vitest'
import { fireEvent, render, screen } from '@testing-library/react'
import { MaintenanceRuleDialog } from '@/components/inventory/maintenance-rule-dialog'

// The item picker must tell two same-named items apart, or a rule can be
// moved to the wrong engine.

const fetchMock = vi.fn()
function jsonResponse(status: number, body: unknown) {
  return Promise.resolve({ ok: status >= 200 && status < 300, status, json: async () => body } as Response)
}

beforeEach(() => {
  fetchMock.mockReset()
  vi.stubGlobal('fetch', fetchMock)
})

describe('MaintenanceRuleDialog item picker', () => {
  it('labels same-named items by location or alias, in the list and on the trigger', async () => {
    const items = [
      { id: 'eq-p', name: 'Cummins QSB 6.7', aliases: [], zone_name: 'ER - Port' },
      { id: 'eq-s', name: 'Cummins QSB 6.7', aliases: ['Starboard engine'], zone_name: 'ER - Starboard' },
      { id: 'eq-g', name: 'Genset', aliases: [], zone_name: 'ER' },
    ]
    fetchMock.mockImplementation((url: string) => {
      if (String(url).includes('/api/inventory/equipment')) return jsonResponse(200, { items })
      return jsonResponse(200, {})
    })
    render(<MaintenanceRuleDialog rule={undefined} presetEquipmentId="eq-p" open onCancel={() => {}} onSave={async () => { throw new Error('unused') }} />)
    const trigger = await screen.findByRole('combobox', { name: 'Item' })
    expect(await screen.findByText('Cummins QSB 6.7 (ER - Port)')).toBeInTheDocument()
    fireEvent.click(trigger)
    expect(await screen.findByRole('option', { name: 'Cummins QSB 6.7 (ER - Port)' })).toBeInTheDocument()
    expect(screen.getByRole('option', { name: 'Cummins QSB 6.7 (Starboard engine)' })).toBeInTheDocument()
    expect(screen.getByRole('option', { name: 'Genset' })).toBeInTheDocument()
  })
})
