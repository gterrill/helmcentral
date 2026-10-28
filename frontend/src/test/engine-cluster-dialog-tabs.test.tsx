import { fireEvent, render, screen, within } from '@testing-library/react'
import { describe, expect, test, vi } from 'vitest'

import { EngineClusterConfigDialog } from '@/components/engine-cluster-config-dialog'
import type { DashboardLayoutItem, EngineClusterConfig } from '@/lib/dashboard-widgets'
import type { EquipmentTileContext } from '@/lib/equipment-tiles'

const ctx: EquipmentTileContext = {
  items: [
    { id: 'eng-port', name: 'Port engine', signalk_source: 'propulsion.port', source_kind: 'engine', profile_id: '' },
    { id: 'batt', name: 'Port start battery', signalk_source: 'electrical.batteries.292', source_kind: 'battery', profile_id: '' },
  ],
  profiles: [],
  reporting: null,
}
vi.mock('@/hooks/use-equipment-tile-context', () => ({ useEquipmentTileContext: () => ctx }))
vi.mock('@/hooks/use-signalk-paths', () => ({ useSignalKPaths: () => ({ paths: [], loading: false }) }))
vi.mock('@/hooks/use-vessel-roles', () => ({ useVesselRoles: () => null }))
vi.mock('@/hooks/use-vessel-discovery', () => ({
  useVesselDiscovery: () => ({ discovery: { connected: true, groups: [], detectors: {} }, loading: false, error: null }),
}))

const cluster: EngineClusterConfig = {
  equipmentId: 'eng-port',
  title: 'Port',
  ring: { path: '', suffix: 'revolutions', label: 'RPM', display: 'radial', quantity: 'frequency', unit: 'rpm' },
  centre: { path: '', suffix: 'runTime', label: 'Hours', display: 'numeric', quantity: 'duration', unit: 'h' },
  corners: [],
}
const widget: DashboardLayoutItem = { id: 'cluster:abcd1234', x: 0, y: 0, w: 6, h: 8, cluster }

function open(overrides: Partial<EngineClusterConfig> = {}) {
  const onSave = vi.fn()
  render(<EngineClusterConfigDialog
    widget={{ ...widget, cluster: { ...cluster, ...overrides } }}
    onCancel={vi.fn()} onSave={onSave} />)
  return onSave
}

const emptyRing = { ...cluster.ring, suffix: '' }

describe('the tab strip', () => {
  test('opens on the Dial tab with no warning when ring and centre are set', () => {
    open()
    expect(screen.getByRole('tab', { name: /^dial$/i })).toHaveAttribute('aria-selected', 'true')
  })

  test('flags the Dial tab when the ring has no reading', () => {
    open({ ring: emptyRing })
    expect(screen.getByRole('tab', { name: 'Dial needs attention' })).toBeInTheDocument()
  })

  test('flags the Dial tab when the centre has no reading', () => {
    open({ centre: { ...cluster.centre, suffix: '' } })
    expect(screen.getByRole('tab', { name: 'Dial needs attention' })).toBeInTheDocument()
  })

  test('counts the footer slots that are bound', () => {
    open({
      footer: {
        battery: { path: '', suffix: 'voltage', label: 'Battery', display: 'numeric', quantity: 'potential', unit: 'V', equipmentId: 'batt' },
        // A foreign slot with a reading but no item reads nothing: not counted.
        altAmps: { path: '', suffix: 'current', label: 'Alt', display: 'numeric', quantity: 'current', unit: 'A' },
        transPressure: { path: '', suffix: 'transmission.oilPressure', label: 'Gear', display: 'numeric', quantity: 'pressure', unit: 'Pa' },
      },
    })
    expect(screen.getByRole('tab', { name: /footer/i })).toHaveTextContent('2/8')
  })
})

describe('the footer tab', () => {
  test('lists the eight slots closed, with the item named on a bound one', () => {
    open({
      footer: {
        battery: { path: '', suffix: 'voltage', label: 'Battery', display: 'numeric', quantity: 'potential', unit: 'V', equipmentId: 'batt' },
      },
    })
    fireEvent.click(screen.getByRole('tab', { name: /footer/i }))
    const rows = document.querySelectorAll('details')
    expect(rows).toHaveLength(8)
    rows.forEach((row) => expect(row).not.toHaveAttribute('open'))
    const battery = screen.getByText('Starter battery voltage').closest('details') as HTMLElement
    expect(within(battery.querySelector('summary') as HTMLElement).getByText('Port start battery')).toBeInTheDocument()
    const egt = screen.getByText('Exhaust temperature').closest('details') as HTMLElement
    expect(within(egt).getByText('Not shown')).toBeInTheDocument()
  })

  // A slot whose item has been deleted reads nothing; the row says so rather than showing a blank name.
  test('says the equipment is missing when the bound item no longer exists', () => {
    open({
      footer: {
        battery: { path: '', suffix: 'voltage', label: 'Battery', display: 'numeric', quantity: 'potential', unit: 'V', equipmentId: 'gone' },
      },
    })
    fireEvent.click(screen.getByRole('tab', { name: /footer/i }))
    const battery = screen.getByText('Starter battery voltage').closest('details') as HTMLElement
    expect(within(battery.querySelector('summary') as HTMLElement).getByText('Equipment missing')).toBeInTheDocument()
  })

  test('names what the tab count counts', () => {
    open()
    expect(screen.getByRole('tab', { name: /^footer 0 of 8 shown$/i })).toBeInTheDocument()
  })
})

describe('why Save is disabled', () => {
  test('names the ring and centre when one has no reading', () => {
    open({ ring: emptyRing })
    expect(screen.getByRole('button', { name: /^save$/i })).toBeDisabled()
    expect(screen.getByText('Choose a reading for the ring and centre on the Dial tab.')).toBeInTheDocument()
  })

  test('names the fuel rail when a bar has no tank', () => {
    open({ fuel: { side: 'left', bars: [{ tankId: '', label: 'Fwd' }] } })
    expect(screen.getByText('Choose a tank for every fuel bar, or remove the rail.')).toBeInTheDocument()
  })

  test('says nothing when Save is enabled', () => {
    open()
    expect(screen.getByRole('button', { name: /^save$/i })).toBeEnabled()
    expect(screen.queryByText(/^Choose a/)).not.toBeInTheDocument()
  })
})
