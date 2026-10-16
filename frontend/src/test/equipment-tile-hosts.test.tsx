import { render, screen } from '@testing-library/react'
import { describe, expect, test, vi } from 'vitest'

import { EngineClusterHost, GaugeGroupHost } from '@/components/equipment-tile-hosts'
import type { EngineClusterConfig, GaugeGroupWidgetConfig } from '@/lib/dashboard-widgets'
import type { EquipmentTileContext, TileItem } from '@/lib/equipment-tiles'

const item = (over: Partial<TileItem> = {}): TileItem => ({
  id: 'eng-port', name: 'Port engine', signalk_source: 'propulsion.port', source_kind: 'engine', profile_id: '', ...over,
})
const slot = (suffix: string) => ({ path: '', suffix, label: suffix, display: 'numeric' as const, quantity: 'raw', unit: 'raw' })
const cluster: EngineClusterConfig = { equipmentId: 'eng-port', title: '', ring: slot('revolutions'), centre: slot('runTime'), corners: [] }
const ctxOf = (items: TileItem[], reporting: ReadonlySet<string> | null = null): EquipmentTileContext => ({ items, profiles: [], reporting })

const common = { values: {}, editing: false, onConfigure: vi.fn() }

describe('a tile that shows equipment', () => {
  test('says Pick equipment when no item is chosen, and offers to choose', () => {
    render(<EngineClusterHost {...common} config={{ ...cluster, equipmentId: undefined }} ctx={ctxOf([item()])} />)
    expect(screen.getByText('Pick equipment')).toBeInTheDocument()
    screen.getByRole('button', { name: /choose the equipment/i }).click()
    expect(common.onConfigure).toHaveBeenCalled()
  })

  test('says Not mapped and links to the item', () => {
    render(<EngineClusterHost {...common} config={cluster} ctx={ctxOf([item({ signalk_source: '', source_kind: '' })])} />)
    expect(screen.getByText('Not mapped')).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'Port engine' })).toHaveAttribute('href', expect.stringContaining('eng-port'))
  })

  test('says Not reporting and links to the item', () => {
    render(<EngineClusterHost {...common} config={cluster} ctx={ctxOf([item()], new Set(['propulsion.starboard']))} />)
    expect(screen.getByText('Not reporting')).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'Port engine' })).toBeInTheDocument()
  })

  test('names the tile for the item when it has no title of its own', () => {
    render(<EngineClusterHost {...common} config={cluster} ctx={ctxOf([item()])} />)
    expect(screen.getByText('Port engine')).toBeInTheDocument()
  })

  test('a plain group of paths is drawn as it always was', () => {
    const plain: GaugeGroupWidgetConfig = {
      title: 'Wind', gauges: [{ path: 'environment.wind.speedApparent', label: 'AWS', display: 'numeric', quantity: 'speed', unit: 'kn' }],
    }
    render(<GaugeGroupHost {...common} config={plain} ctx={ctxOf([])} />)
    expect(screen.getByText('Wind')).toBeInTheDocument()
    expect(screen.queryByTestId('equipment-tile-notice')).toBeNull()
  })
})
