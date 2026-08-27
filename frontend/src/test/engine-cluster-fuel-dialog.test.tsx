import { fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, test, vi } from 'vitest'

import { EngineClusterConfigDialog } from '@/components/engine-cluster-config-dialog'
import type { DashboardLayoutItem, EngineClusterConfig } from '@/lib/dashboard-widgets'

vi.mock('@/hooks/use-signalk-paths', () => ({
  useSignalKPaths: () => ({ paths: [{ path: 'tanks.fuel.5.currentLevel' }, { path: 'tanks.fuel.5.capacity' }], loading: false }),
}))
vi.mock('@/hooks/use-engine-profiles', () => ({ useEngineProfiles: () => ({ profiles: [] }) }))

const cluster: EngineClusterConfig = {
  title: 'Port',
  ring: { path: 'propulsion.port.revolutions', label: 'RPM', display: 'radial', quantity: 'frequency', unit: 'rpm' },
  centre: { path: 'propulsion.port.runTime', label: 'Hours', display: 'numeric', quantity: 'duration', unit: 'h' },
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

/**
 * The bar's own level input, by id. A cluster has a path field per slot, so
 * indexing the whole form would have picked up the ring.
 */
function levelInput(index: number) {
  return document.querySelector(`#cluster-fuel-${index}-path`) as HTMLInputElement
}

describe('the fuel rail section', () => {
  test('starts a rail with a forward and an aft tank', () => {
    open()
    fireEvent.click(screen.getByRole('button', { name: /add fuel rail/i }))
    expect(screen.getByLabelText(/tile edge/i)).toBeInTheDocument()
    expect(screen.getAllByLabelText(/capacity path/i)).toHaveLength(2)
  })

  /**
   * A cluster titled Port puts its rail outboard without being asked. The
   * facing pair is the whole reason the edge is configurable at all.
   */
  test('seeds the edge from the cluster title', () => {
    open({ title: 'Port' })
    fireEvent.click(screen.getByRole('button', { name: /add fuel rail/i }))
    expect(screen.getByLabelText(/tile edge/i)).toHaveValue('left')
  })

  test('seeds a starboard cluster to the other edge', () => {
    open({ title: 'Starboard' })
    fireEvent.click(screen.getByRole('button', { name: /add fuel rail/i }))
    expect(screen.getByLabelText(/tile edge/i)).toHaveValue('right')
  })

  /** SignalK names them as siblings, so one path all but names the other. */
  test('fills the capacity path from the level path', () => {
    open()
    fireEvent.click(screen.getByRole('button', { name: /add fuel rail/i }))
    fireEvent.change(levelInput(0), { target: { value: 'tanks.fuel.5.currentLevel' } })
    expect(screen.getAllByLabelText(/capacity path/i)[0]).toHaveValue('tanks.fuel.5.capacity')
  })

  /**
   * The quantities the backend insists on, set without the operator being asked.
   * No tank path here publishes meta.units, so a picker left on its default
   * would make the rail read 1.2 where it should read 890.
   */
  test('sets the quantities the backend requires', () => {
    const onSave = open()
    fireEvent.click(screen.getByRole('button', { name: /add fuel rail/i }))
    for (const [i, path] of ['tanks.fuel.5.currentLevel', 'tanks.fuel.4.currentLevel'].entries()) {
      fireEvent.change(levelInput(i), { target: { value: path } })
    }
    fireEvent.click(screen.getByRole('button', { name: /^save$/i }))

    const saved = onSave.mock.calls[0][0] as EngineClusterConfig
    for (const bar of saved.fuel!.bars) {
      expect(bar.level.quantity).toBe('ratio')
      expect(bar.capacity.quantity).toBe('volume')
    }
  })

  test('will not save a tank with no capacity path', () => {
    open()
    fireEvent.click(screen.getByRole('button', { name: /add fuel rail/i }))
    fireEvent.change(levelInput(0), { target: { value: 'a.b.c' } })
    expect(screen.getByRole('button', { name: /^save$/i })).toBeDisabled()
  })

  test('drops the rail again', () => {
    open()
    fireEvent.click(screen.getByRole('button', { name: /add fuel rail/i }))
    fireEvent.click(screen.getByRole('button', { name: /remove fuel rail/i }))
    expect(screen.queryByLabelText(/tile edge/i)).not.toBeInTheDocument()
  })
})
