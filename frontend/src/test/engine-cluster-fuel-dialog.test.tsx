import { fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, test, vi } from 'vitest'

import { EngineClusterConfigDialog } from '@/components/engine-cluster-config-dialog'
import type { DashboardLayoutItem, EngineClusterConfig } from '@/lib/dashboard-widgets'
import type { EquipmentTileContext } from '@/lib/equipment-tiles'

const ctx: EquipmentTileContext = {
  items: [
    { id: 'eng-port', name: 'Port engine', signalk_source: 'propulsion.port', source_kind: 'engine', profile_id: '' },
    { id: 'tank-5', name: 'Port forward fuel', signalk_source: 'tanks.fuel.5', source_kind: 'tank', profile_id: '' },
    { id: 'tank-4', name: 'Port aft fuel', signalk_source: 'tanks.fuel.4', source_kind: 'tank', profile_id: '' },
  ],
  profiles: [],
  reporting: null,
}
vi.mock('@/hooks/use-equipment-tile-context', () => ({ useEquipmentTileContext: () => ctx }))
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

const tankSelect = (index: number) => document.querySelector(`#cluster-fuel-${index}-equipment`) as HTMLSelectElement

describe('the fuel rail section', () => {
  test('has no low fuel alarm of its own: the tank page owns the levels', () => {
    open({ fuel: { side: 'left', bars: [{ tankId: 'tank-5', label: 'Fwd' }] } })
    expect(screen.queryByRole('button', { name: /add low fuel alarm/i })).not.toBeInTheDocument()
    expect(screen.getByText(/follow the tank's levels/i)).toBeInTheDocument()
  })

  test('starts a rail with a forward and an aft tank to pick', () => {
    open()
    fireEvent.click(screen.getByRole('button', { name: /add fuel rail/i }))
    expect(tankSelect(0)).toBeInTheDocument()
    expect(tankSelect(1)).toBeInTheDocument()
  })

  test('offers no more than two tanks, the two arcs the fan draws', () => {
    open()
    fireEvent.click(screen.getByRole('button', { name: /add fuel rail/i }))
    expect(screen.getByRole('button', { name: /add tank/i })).toBeDisabled()
  })

  /**
   * The fan sits on the outer edge of the tile, which is the engine's own
   * side. There is no separate edge to pick, so the two can never disagree.
   */
  function saveWithRail(onSave: ReturnType<typeof vi.fn>) {
    fireEvent.change(tankSelect(0), { target: { value: 'tank-5' } })
    fireEvent.change(tankSelect(1), { target: { value: 'tank-4' } })
    fireEvent.click(screen.getByRole('button', { name: /^save$/i }))
    return vi.waitFor(() => onSave.mock.calls[0][0] as EngineClusterConfig)
  }

  test("puts a port engine's fan on the left edge", async () => {
    const onSave = open({ side: 'port' })
    fireEvent.click(screen.getByRole('button', { name: /add fuel rail/i }))
    expect((await saveWithRail(onSave)).fuel!.side).toBe('left')
  })

  test("puts a starboard engine's fan on the right edge", async () => {
    const onSave = open({ side: 'stbd' })
    fireEvent.click(screen.getByRole('button', { name: /add fuel rail/i }))
    expect((await saveWithRail(onSave)).fuel!.side).toBe('right')
  })

  test('moves the fan with the engine side', async () => {
    const onSave = open({ side: 'port' })
    fireEvent.click(screen.getByRole('button', { name: /add fuel rail/i }))
    fireEvent.change(screen.getByLabelText(/engine side/i), { target: { value: 'stbd' } })
    expect((await saveWithRail(onSave)).fuel!.side).toBe('right')
  })

  test('offers the tanks and stores the one picked', () => {
    const onSave = open()
    fireEvent.click(screen.getByRole('button', { name: /add fuel rail/i }))
    fireEvent.change(tankSelect(0), { target: { value: 'tank-5' } })
    fireEvent.change(tankSelect(1), { target: { value: 'tank-4' } })
    fireEvent.click(screen.getByRole('button', { name: /^save$/i }))

    return vi.waitFor(() => {
      const saved = onSave.mock.calls[0][0] as EngineClusterConfig
      expect(saved.fuel!.bars.map((b) => b.tankId)).toEqual(['tank-5', 'tank-4'])
      expect(saved.fuel!.bars[0].label).toBe('Fwd')
    })
  })

  test('will not save a bar with no tank picked', () => {
    open()
    fireEvent.click(screen.getByRole('button', { name: /add fuel rail/i }))
    fireEvent.change(tankSelect(0), { target: { value: 'tank-5' } })
    expect(screen.getByRole('button', { name: /^save$/i })).toBeDisabled()
  })

  test('drops the rail again', () => {
    open()
    fireEvent.click(screen.getByRole('button', { name: /add fuel rail/i }))
    fireEvent.click(screen.getByRole('button', { name: /remove fuel rail/i }))
    expect(tankSelect(0)).toBeNull()
  })
})
