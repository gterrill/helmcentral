import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, test, vi } from 'vitest'

import { EquipmentPicker } from '@/components/equipment-picker'
import type { EquipmentTileContext, TileItem } from '@/lib/equipment-tiles'

const discovery = {
  connected: true,
  detectors: {},
  groups: [{
    kind: 'engine', mappable: true,
    rows: [
      { source: 'propulsion.port', instance: 'port', bus_name: '', charger_input: false, reporting: true, metrics: [{ label: 'RPM', text: '0 rpm' }], item: { id: 'x', name: 'Taken' } },
      { source: 'propulsion.starboard', instance: 'starboard', bus_name: '', charger_input: false, reporting: true, metrics: [], item: null },
    ],
  }],
}
vi.mock('@/hooks/use-vessel-discovery', () => ({ useVesselDiscovery: () => ({ discovery, loading: false, error: null }) }))

const items: TileItem[] = [
  { id: 'eng-port', name: 'Port engine', signalk_source: 'propulsion.port', source_kind: 'engine', profile_id: '' },
  { id: 'spare', name: 'Spare engine', signalk_source: '', source_kind: '', profile_id: '' },
  { id: 'bank', name: 'House bank', signalk_source: 'electrical.batteries.1', source_kind: 'battery', profile_id: '' },
]
const ctx: EquipmentTileContext = { items, profiles: [], reporting: null }

beforeEach(() => { vi.restoreAllMocks() })

describe('EquipmentPicker', () => {
  test('lists mapped items of the right kind, then items not mapped yet, and no other kind', () => {
    render(<EquipmentPicker idPrefix="p" label="Engine" kind="engine" value="" onChange={vi.fn()} ctx={ctx} />)
    const names = Array.from(screen.getByLabelText('Engine').querySelectorAll('option')).map((o) => o.textContent)
    expect(names).toEqual(['Pick engine…', 'Port engine', 'Spare engine'])
  })

  test('an unmapped item offers to map it right there, warning that it changes the item everywhere', () => {
    render(<EquipmentPicker idPrefix="p" label="Engine" kind="engine" value="spare" onChange={vi.fn()} ctx={ctx} />)
    const here = screen.getByTestId('map-here')
    expect(here).toHaveTextContent('changes Spare engine itself, for every tile and feature that uses it')
    const options = Array.from(here.querySelectorAll('option')).map((o) => o.value)
    // Only what is reporting and owned by no item.
    expect(options).toEqual(['', 'propulsion.starboard'])
  })

  test('a mapped item offers nothing to map', () => {
    render(<EquipmentPicker idPrefix="p" label="Engine" kind="engine" value="eng-port" onChange={vi.fn()} ctx={ctx} />)
    expect(screen.queryByTestId('map-here')).toBeNull()
  })

  test('mapping writes the item source, the same field as the item page', async () => {
    const calls: { url: string; method: string; body?: string }[] = []
    vi.stubGlobal('fetch', vi.fn().mockImplementation((url: string, init?: RequestInit) => {
      calls.push({ url: String(url), method: init?.method ?? 'GET', body: init?.body as string | undefined })
      return Promise.resolve({
        ok: true,
        json: async () => ({ item: { id: 'spare', name: 'Spare engine', system: 'propulsion', category: 'mechanical', status: 'deployed', quantity: 1, aliases: [], signalk_source: '' } }),
      })
    }))
    render(<EquipmentPicker idPrefix="p" label="Engine" kind="engine" value="spare" onChange={vi.fn()} ctx={ctx} />)
    fireEvent.change(screen.getByLabelText('Map Spare engine to'), { target: { value: 'propulsion.starboard' } })
    fireEvent.click(screen.getByRole('button', { name: 'Map Spare engine' }))
    await waitFor(() => expect(calls.some((c) => c.method === 'PUT')).toBe(true))
    const put = calls.find((c) => c.method === 'PUT')!
    expect(put.url).toContain('/api/inventory/equipment/spare')
    expect(JSON.parse(put.body!).signalk_source).toBe('propulsion.starboard')
  })
})
