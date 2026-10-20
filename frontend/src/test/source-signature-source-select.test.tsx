import { describe, it, expect, vi } from 'vitest'
import { render, screen, within } from '@testing-library/react'

import { EquipmentSourceSection } from '@/components/inventory/equipment-source-section'
import { BLANK_DRAFT } from '@/hooks/use-inventory'
import { loggedDay, type DiscoveryRow, type VesselDiscoveryResponse } from '@/lib/vessel-settings'

// The Source select on an equipment item's page is fed by the same discovery
// list as Settings -> SignalK. On 2026-10-21 the engines were off and the Port
// Cummins was on propulsion.0, which carries only exhaust temperature: the
// select must offer propulsion.port and .starboard from the logged history,
// not offer .0/.1 as new choices, and warn about the current choice.

const row = (source: string, extra: Partial<DiscoveryRow>): DiscoveryRow => ({
  source, instance: source.split('.')[1], bus_name: '', charger_input: false, reporting: false, metrics: [],
  item: null, last_logged: null, signature: 'carried', ...extra,
})

const discovery: VesselDiscoveryResponse = {
  connected: true,
  detectors: {},
  history_error: '',
  groups: [{
    kind: 'engine', mappable: true, signature_label: 'RPM', rows: [
      row('propulsion.0', { item: { id: 'c0', name: 'Cummins QSB 6.7' }, signature: 'missing' }),
      row('propulsion.1', { item: { id: 'c1', name: 'Cummins QSB 6.7' }, signature: 'missing' }),
      row('propulsion.port', { last_logged: '2026-10-16T12:00:00Z' }),
      row('propulsion.starboard', { last_logged: '2026-10-16T12:00:00Z' }),
    ],
  }],
}

describe('EquipmentSourceSection: Source select', () => {
  it('offers the logged engines, not the sources without RPM, and warns about the current choice', () => {
    render(<EquipmentSourceSection id="c0" draft={{ ...BLANK_DRAFT, signalk_source: 'propulsion.0' }} setDraft={vi.fn()} discovery={discovery} canWrite />)

    const select = screen.getByLabelText('Source')
    expect(within(select).getByRole('option', { name: 'propulsion.port · not reporting now · last logged 16 Oct' })).not.toBeDisabled()
    expect(within(select).getByRole('option', { name: 'propulsion.starboard · not reporting now · last logged 16 Oct' })).not.toBeDisabled()
    // propulsion.1 belongs to another item: shown as taken, never as a choice.
    expect(within(select).getByRole('option', { name: 'propulsion.1 · assigned to Cummins QSB 6.7' })).toBeDisabled()
    expect(screen.getByRole('alert')).toHaveTextContent('propulsion.0 has not reported RPM in the logged history')
    expect(screen.getByText(/now or in the logged history/)).toBeInTheDocument()
  })

  it('shows no warning on a source that carries its signature, and says when history is unavailable', () => {
    render(
      <EquipmentSourceSection
        id="c0"
        draft={{ ...BLANK_DRAFT, signalk_source: 'propulsion.port' }}
        setDraft={vi.fn()}
        discovery={{ ...discovery, history_error: 'influxdb is not configured' }}
        canWrite
      />,
    )

    expect(screen.queryByRole('alert')).toBeNull()
    expect(screen.getByText(/Logged history isn't available \(influxdb is not configured\)/)).toBeInTheDocument()
  })

  it('says one thing, not two, when SignalK has no data at all', () => {
    render(
      <EquipmentSourceSection
        id="c0"
        draft={{ ...BLANK_DRAFT, signalk_source: '' }}
        setDraft={vi.fn()}
        discovery={{ connected: false, groups: [], detectors: {}, history_error: 'influxdb is not configured' }}
        canWrite
      />,
    )

    expect(screen.getByText(/no data from SignalK yet/)).toBeInTheDocument()
    expect(screen.queryByText(/Logged history isn't available/)).toBeNull()
    expect(screen.queryByText(/now or in the logged history/)).toBeNull()
  })
})

describe('loggedDay', () => {
  it('names the day in the time zone it is given', () => {
    expect(loggedDay('2026-10-16T23:30:00Z', 'UTC')).toBe('16 Oct')
    expect(loggedDay('2026-10-16T23:30:00Z', 'Australia/Brisbane')).toBe('17 Oct')
  })
})
