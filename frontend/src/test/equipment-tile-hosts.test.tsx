import { render, screen } from '@testing-library/react'
import { describe, expect, test, vi } from 'vitest'
import { installFakeTelemetryStream } from './helpers/fake-telemetry-stream'

// The hosts read live readings from the gauge-values store, so the store is
// seeded through the stream it listens to.
const stream = installFakeTelemetryStream()
const emitGaugeValues = (payload: unknown) => stream.emit('gauge-values', payload)

import { EngineClusterHost, GaugeGroupHost } from '@/components/equipment-tile-hosts'
import type { EngineClusterConfig, GaugeGroupWidgetConfig } from '@/lib/dashboard-widgets'
import type { EquipmentTileContext, TileItem } from '@/lib/equipment-tiles'

const item = (over: Partial<TileItem> = {}): TileItem => ({
  id: 'eng-port', name: 'Port engine', signalk_source: 'propulsion.port', source_kind: 'engine', profile_id: '', ...over,
})
const slot = (suffix: string) => ({ path: '', suffix, label: suffix, display: 'numeric' as const, quantity: 'raw', unit: 'raw' })
const cluster: EngineClusterConfig = { equipmentId: 'eng-port', title: '', ring: slot('revolutions'), centre: slot('runTime'), corners: [] }
const ctxOf = (items: TileItem[], reporting: ReadonlySet<string> | null = null): EquipmentTileContext => ({ items, profiles: [], reporting })

const common = { editing: false, onConfigure: vi.fn() }

describe('a tile that shows equipment', () => {
  test('says Pick equipment when no item is chosen, and offers to choose', () => {
    render(<EngineClusterHost {...common} config={{ ...cluster, equipmentId: undefined }} ctx={ctxOf([item()])} />)
    expect(screen.getByText('Pick equipment')).toBeInTheDocument()
    screen.getByRole('button', { name: /choose the equipment/i }).click()
    expect(common.onConfigure).toHaveBeenCalled()
  })

  test('says Equipment removed for a deleted item, and Choose different equipment works outside Layout mode', () => {
    const onConfigure = vi.fn()
    render(<EngineClusterHost {...common} editing={false} onConfigure={onConfigure} config={cluster} ctx={ctxOf([])} />)
    expect(screen.getByText('Equipment removed')).toBeInTheDocument()
    screen.getByRole('button', { name: /choose different equipment/i }).click()
    expect(onConfigure).toHaveBeenCalled()
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

  // OFF follows the propulsion id the cluster reads, as it always has, not the
  // kind the item was given in Inventory.
  test('shows OFF for a cluster on a non-engine item mapped to an engine that is off', () => {
    const sensor = item({ source_kind: '' })
    render(<EngineClusterHost {...common} config={cluster} ctx={ctxOf([sensor])} />)
    emitGaugeValues({ values: {}, ages: {}, engines: { port: { state: 'off', last_update: '2026-10-06T04:30:00Z' } } })
    expect(screen.getByTestId('tile-off-badge')).toBeInTheDocument()
  })

  test('only this engine being off turns the tile off', () => {
    render(<EngineClusterHost {...common} config={cluster} ctx={ctxOf([item()])} />)
    emitGaugeValues({ values: {}, ages: {}, engines: { starboard: { state: 'off', last_update: '2026-10-06T04:30:00Z' }, port: { state: 'running' } } })
    expect(screen.queryByTestId('tile-off-badge')).toBeNull()
    emitGaugeValues({ values: {}, ages: {}, engines: { starboard: { state: 'off', last_update: '2026-10-06T04:30:00Z' }, port: { state: 'off', last_update: '2026-10-06T04:31:00Z' } } })
    expect(screen.getByTestId('tile-off-badge')).toBeInTheDocument()
  })

  test('a silent non-engine item reading propulsion.port.* still says Not reporting while port is off', () => {
    const sensor = item({ source_kind: '' })
    render(<EngineClusterHost {...common} config={cluster} ctx={ctxOf([sensor], new Set(['propulsion.starboard']))} />)
    emitGaugeValues({ values: {}, ages: {}, engines: { port: { state: 'off', last_update: '2026-10-06T04:30:00Z' } } })
    expect(screen.getByText('Not reporting')).toBeInTheDocument()
    expect(screen.queryByTestId('tile-off-badge')).toBeNull()
  })
})
