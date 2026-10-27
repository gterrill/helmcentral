/**
 * Render counts through the real stores (ADR 0215): the stream, the stores and
 * their equality rules run unchanged over the fake EventSource, so a reader's
 * re-renders are the ones it would have on the boat. A stand-in store that
 * hands out its state on every call could not show any of this.
 */
import { Profiler } from 'react'
import { render } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import { useElectricalFields } from '@/hooks/use-electrical-state'
import { useGaugeValue } from '@/hooks/use-gauge-values'
import { useVesselFields } from '@/hooks/use-vessel-state'
import * as live from './fixtures/live-stream-payloads'
import { installFakeTelemetryStream } from './helpers/fake-telemetry-stream'

const stream = installFakeTelemetryStream()

const DEPTH_FIELDS = ['depth'] as const
const SOC_FIELDS = ['batterySocPercent'] as const

function DepthReader() {
  const { depth } = useVesselFields(DEPTH_FIELDS)
  return <span>{depth ?? '--'}</span>
}

function SocReader() {
  const { batterySocPercent } = useElectricalFields(SOC_FIELDS)
  return <span>{batterySocPercent ?? '--'}</span>
}

function GaugeReader({ path }: { path: string }) {
  return <span>{useGaugeValue(path) ?? '--'}</span>
}

function profiled(node: React.ReactNode) {
  const onRender = vi.fn()
  const view = render(<Profiler id="reader" onRender={onRender}>{node}</Profiler>)
  return { onRender, ...view }
}

describe('a reader of the vessel-state store', () => {
  it('does not re-render on a tick that moves only fields it does not read', () => {
    const { onRender, container } = profiled(<DepthReader />)
    stream.emit('vessel-state', { depth: 16.3, latitude: -20.2747, longitude: 149.0737, heading_true: 329.3 })
    expect(container).toHaveTextContent('16.3')
    const after = onRender.mock.calls.length

    stream.emit('vessel-state', { depth: 16.3, latitude: -20.2748, longitude: 149.0738, heading_true: 330.1, wind_speed_apparent_kts: 6.2 })
    stream.emit('vessel-state', { depth: 16.3, latitude: -20.2749, longitude: 149.0739, heading_true: 331.4 })

    expect(onRender.mock.calls.length).toBe(after)
  })

  it('re-renders when a field it reads changes', () => {
    const { onRender, container } = profiled(<DepthReader />)
    stream.emit('vessel-state', { depth: 16.3 })
    const after = onRender.mock.calls.length

    stream.emit('vessel-state', { depth: 15.9 })

    expect(onRender.mock.calls.length).toBeGreaterThan(after)
    expect(container).toHaveTextContent('15.9')
  })
})

describe('a reader of the electrical store', () => {
  it('does not re-render on a tick that moves the current and power but not the charge it reads', () => {
    const { onRender, container } = profiled(<SocReader />)
    stream.emit('electrical-state', live.ELECTRICAL)
    expect(container).toHaveTextContent('82')
    const after = onRender.mock.calls.length

    stream.emit('electrical-state', live.ELECTRICAL_NEXT)

    expect(onRender.mock.calls.length).toBe(after)
  })

  it('re-renders when the charge changes', () => {
    const { onRender, container } = profiled(<SocReader />)
    stream.emit('electrical-state', live.ELECTRICAL)
    const after = onRender.mock.calls.length

    stream.emit('electrical-state', { ...live.ELECTRICAL, battery_soc_percent: 83 })

    expect(onRender.mock.calls.length).toBeGreaterThan(after)
    expect(container).toHaveTextContent('83')
  })
})

describe('a reader of one gauge path', () => {
  it('does not re-render when another path moves, and does when its own does', () => {
    const { onRender, container } = profiled(<GaugeReader path="a.path" />)
    stream.emit('gauge-values', { values: { 'a.path': 1, 'b.path': 1 }, ages: {} })
    const after = onRender.mock.calls.length

    stream.emit('gauge-values', { values: { 'a.path': 1, 'b.path': 2 }, ages: {} })
    expect(onRender.mock.calls.length).toBe(after)

    stream.emit('gauge-values', { values: { 'a.path': 2, 'b.path': 2 }, ages: {} })
    expect(onRender.mock.calls.length).toBeGreaterThan(after)
    expect(container).toHaveTextContent('2')
  })
})
