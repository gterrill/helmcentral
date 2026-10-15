import { fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, test, vi } from 'vitest'

import { EngineClusterConfigDialog } from '@/components/engine-cluster-config-dialog'
import type { DashboardLayoutItem, EngineClusterConfig } from '@/lib/dashboard-widgets'
import type { EquipmentTileContext } from '@/lib/equipment-tiles'

const profile = {
  id: 'yanmar-4jh', name: 'Yanmar 4JH', kind: 'engine' as const,
  gauges: [
    { path_suffix: 'revolutions', label: 'RPM', display: 'radial' as const, quantity: 'frequency', unit: 'rpm', min: 0, max: 3000 },
    { path_suffix: 'runTime', label: 'Hours', display: 'numeric' as const, quantity: 'duration', unit: 'h' },
    { path_suffix: 'oilPressure', label: 'Oil', display: 'radial' as const, quantity: 'pressure', unit: 'psi', min: 0, max: 100,
      zones: [{ direction: 'below' as const, threshold: 15, state: 'alarm' as const }] },
  ],
}
const ctx: EquipmentTileContext = {
  items: [{ id: 'eng-port', name: 'Port engine', signalk_source: 'propulsion.port', source_kind: 'engine', profile_id: 'yanmar-4jh' }],
  profiles: [profile],
  reporting: null,
}
vi.mock('@/hooks/use-equipment-tile-context', () => ({ useEquipmentTileContext: () => ctx }))

const blank: EngineClusterConfig = {
  title: '',
  ring: { path: '', suffix: '', label: 'RPM', display: 'radial', quantity: 'raw', unit: 'raw' },
  centre: { path: '', suffix: '', label: 'Hours', display: 'numeric', quantity: 'raw', unit: 'raw' },
  corners: [],
}
const widget = (cluster: EngineClusterConfig): DashboardLayoutItem => ({ id: 'cluster:abcd1234', x: 0, y: 0, w: 6, h: 8, cluster })

describe('EngineClusterConfigDialog', () => {
  test('a new cluster needs an engine before it can be saved', () => {
    render(<EngineClusterConfigDialog widget={widget(blank)} onCancel={vi.fn()} onSave={vi.fn()} />)
    expect(screen.getByRole('button', { name: /^save$/i })).toBeDisabled()
  })

  test('picking the engine and filling from its profile stores suffixes, never paths or zones', () => {
    const onSave = vi.fn()
    render(<EngineClusterConfigDialog widget={widget(blank)} onCancel={vi.fn()} onSave={onSave} />)
    fireEvent.change(screen.getByLabelText('Engine'), { target: { value: 'eng-port' } })
    fireEvent.click(screen.getByRole('button', { name: /fill slots from profile/i }))
    fireEvent.click(screen.getByRole('button', { name: /^save$/i }))

    const saved = onSave.mock.calls[0][0] as EngineClusterConfig
    expect(saved.equipmentId).toBe('eng-port')
    expect(saved.ring).toMatchObject({ suffix: 'revolutions', path: '', ringStyle: 'instrument' })
    expect(saved.corners[0].rows[0]).toMatchObject({ suffix: 'oilPressure', path: '' })
    expect(saved.corners[0].rows[0].zones).toBeUndefined()
  })

  test('the zone editor shows the profile zones and names who else they touch', () => {
    render(<EngineClusterConfigDialog widget={widget({ ...blank, equipmentId: 'eng-port' })} onCancel={vi.fn()} onSave={vi.fn()} />)
    fireEvent.click(screen.getByRole('button', { name: /fill slots from profile/i }))
    expect(screen.getAllByTestId('zone-notice')[0]).toHaveTextContent('Applies to every tile and alarm using Yanmar 4JH (Port engine)')
  })

  test('has no way to type a SignalK path', () => {
    render(<EngineClusterConfigDialog widget={widget({ ...blank, equipmentId: 'eng-port' })} onCancel={vi.fn()} onSave={vi.fn()} />)
    expect(screen.queryByLabelText(/signalk path/i)).toBeNull()
    expect(screen.queryByLabelText(/engine instance/i)).toBeNull()
  })
})
