import { describe, it, expect, vi } from 'vitest'
import { render, screen, within } from '@testing-library/react'

import { EquipmentSourceSection } from '@/components/inventory/equipment-source-section'
import { BLANK_DRAFT } from '@/hooks/use-inventory'
import { loggedDay, type DiscoveryRow, type VesselDiscoveryResponse } from '@/lib/vessel-settings'

const YOUNG_NOTE = 'Helmcentral has been watching the boat since 18 Oct. A source that is switched off is listed once it has reported since then.'

// The Source select on an equipment item's page is fed by the same discovery
// list as Settings -> SignalK. On 2026-10-21 the engines were off and the Port
// Cummins was on propulsion.0, which carries only exhaust temperature: the
// select must offer propulsion.port and .starboard from Helmcentral's record,
// not offer .0/.1 as new choices, and warn about the current choice.

const row = (source: string, extra: Partial<DiscoveryRow>): DiscoveryRow => ({
  source, instance: source.split('.')[1], bus_name: '', charger_input: false, reporting: false, metrics: [],
  item: null, last_seen: null, signature: 'carried', ...extra,
})

const discovery: VesselDiscoveryResponse = {
  connected: true,
  detectors: {},
  tracking: { since: '2026-10-01T00:00:00Z', since_day: '1 Oct', note: '', seeded: true, covers: true, error: '' },
  groups: [{
    kind: 'engine', mappable: true, signature_label: 'RPM', rows: [
      row('propulsion.0', { item: { id: 'c0', name: 'Cummins QSB 6.7' }, signature: 'missing' }),
      row('propulsion.1', { item: { id: 'c1', name: 'Cummins QSB 6.7' }, signature: 'missing' }),
      row('propulsion.port', { last_seen: '2026-10-16T12:00:00Z' }),
      row('propulsion.starboard', { last_seen: '2026-10-16T12:00:00Z' }),
    ],
  }],
}

describe('EquipmentSourceSection: Source select', () => {
  it('offers the logged engines, not the sources without RPM, and warns about the current choice', () => {
    render(<EquipmentSourceSection id="c0" draft={{ ...BLANK_DRAFT, signalk_source: 'propulsion.0' }} setDraft={vi.fn()} discovery={discovery} canWrite />)

    const select = screen.getByLabelText('Source')
    expect(within(select).getByRole('option', { name: 'propulsion.port · not reporting now · last seen 16 Oct' })).not.toBeDisabled()
    expect(within(select).getByRole('option', { name: 'propulsion.starboard · not reporting now · last seen 16 Oct' })).not.toBeDisabled()
    // propulsion.1 belongs to another item: shown as taken, never as a choice.
    expect(within(select).getByRole('option', { name: 'propulsion.1 · assigned to Cummins QSB 6.7' })).toBeDisabled()
    expect(screen.getByRole('alert')).toHaveTextContent('propulsion.0 reports other readings but has never reported RPM')
    expect(screen.getByText(/now or when Helmcentral last saw them/)).toBeInTheDocument()
  })

  it('shows no warning on a source that carries its signature, and says since when it has been watching', () => {
    render(
      <EquipmentSourceSection
        id="c0"
        draft={{ ...BLANK_DRAFT, signalk_source: 'propulsion.port' }}
        setDraft={vi.fn()}
        discovery={{ ...discovery, tracking: { since: '2026-10-18T12:00:00Z', since_day: '18 Oct', note: YOUNG_NOTE, seeded: false, covers: false, error: '' } }}
        canWrite
      />,
    )

    expect(screen.queryByRole('alert')).toBeNull()
    expect(screen.getByText(/Helmcentral has been watching the boat since 18 Oct/)).toBeInTheDocument()
  })

  it('says plainly when Helmcentral could not read its record', () => {
    render(
      <EquipmentSourceSection
        id="c0"
        draft={{ ...BLANK_DRAFT, signalk_source: 'propulsion.port' }}
        setDraft={vi.fn()}
        discovery={{ ...discovery, tracking: { since: null, since_day: '', note: "Helmcentral couldn't read its record of what the boat has reported (database is locked), so only sources reporting now are listed.", seeded: false, covers: false, error: 'database is locked' } }}
        canWrite
      />,
    )

    expect(screen.getByText("Helmcentral couldn't read its record of what the boat has reported (database is locked), so only sources reporting now are listed.")).toBeInTheDocument()
  })

  it('says one thing, not two, when SignalK has no data at all', () => {
    render(
      <EquipmentSourceSection
        id="c0"
        draft={{ ...BLANK_DRAFT, signalk_source: '' }}
        setDraft={vi.fn()}
        discovery={{ connected: false, groups: [], detectors: {}, tracking: { since: '2026-10-18T12:00:00Z', since_day: '18 Oct', note: YOUNG_NOTE, seeded: false, covers: false, error: '' } }}
        canWrite
      />,
    )

    expect(screen.getByText(/no data from SignalK yet/)).toBeInTheDocument()
    expect(screen.queryByText(/Helmcentral has been watching/)).toBeNull()
    expect(screen.queryByText(/now or when Helmcentral last saw them/)).toBeNull()
  })
})

describe('loggedDay', () => {
  it('names the day in the time zone it is given', () => {
    expect(loggedDay('2026-10-16T23:30:00Z', 'UTC')).toBe('16 Oct')
    expect(loggedDay('2026-10-16T23:30:00Z', 'Australia/Brisbane')).toBe('17 Oct')
  })
})
