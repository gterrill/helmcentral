import { render, screen } from '@testing-library/react'
import { beforeEach, describe, expect, test, vi } from 'vitest'

import { GaugeBody, GaugeTile, majorStepFor } from '@/components/gauge-tile'
import type { GaugeDisplay, GaugeWidgetConfig } from '@/lib/dashboard-widgets'
import { severityFill } from '@/lib/severity'

function config(overrides: Partial<GaugeWidgetConfig> = {}): GaugeWidgetConfig {
  return {
    path: 'propulsion.port.oilPressure',
    label: 'Port oil',
    display: 'numeric',
    quantity: 'pressure',
    unit: 'psi',
    ...overrides,
  }
}

describe('GaugeTile', () => {
  test('titles itself from the label, falling back to the path', () => {
    const { rerender } = render(
      <GaugeTile config={config()} value={241325} editing={false} onConfigure={vi.fn()} />,
    )
    expect(screen.getByText('Port oil')).toBeInTheDocument()

    rerender(<GaugeTile config={config({ label: '  ' })} value={241325} editing={false} onConfigure={vi.fn()} />)
    expect(screen.getByText('propulsion.port.oilPressure')).toBeInTheDocument()
  })

  test('converts from the SI value SignalK publishes', () => {
    render(<GaugeTile config={config()} value={241325} editing={false} onConfigure={vi.fn()} />)
    expect(screen.getByText('35.0')).toBeInTheDocument()
    expect(screen.getByText('psi')).toBeInTheDocument()
  })

  test.each<GaugeDisplay>(['numeric', 'radial', 'bar', 'lamp'])('renders the %s display', (display) => {
    render(
      <GaugeTile
        config={config({ display, min: 0, max: 100 })}
        value={241325}
        editing={false}
        onConfigure={vi.fn()}
      />,
    )
    // Every display kind shows a reading rather than rendering nothing.
    expect(screen.getByText(display === 'lamp' ? 'ON' : '35.0')).toBeInTheDocument()
  })

  // A gauge reading 0 when it means "no data" is the dangerous failure.
  test.each<GaugeDisplay>(['numeric', 'radial', 'bar', 'lamp'])(
    'shows the structural dash, never a zero, for an absent %s value',
    (display) => {
      render(
        <GaugeTile config={config({ display })} value={null} editing={false} onConfigure={vi.fn()} />,
      )
      expect(screen.getByText('--')).toBeInTheDocument()
      expect(screen.queryByText('0')).not.toBeInTheDocument()
    },
  )

  test('a genuine zero still reads as zero', () => {
    render(
      <GaugeTile config={config({ quantity: 'raw', unit: 'raw', decimals: 0 })} value={0} editing={false} onConfigure={vi.fn()} />,
    )
    expect(screen.getByText('0')).toBeInTheDocument()
    expect(screen.queryByText('--')).not.toBeInTheDocument()
  })

  test('offers the config button only in layout mode', () => {
    const onConfigure = vi.fn()
    const { rerender } = render(
      <GaugeTile config={config()} value={241325} editing={false} onConfigure={onConfigure} />,
    )
    expect(screen.queryByLabelText('Configure Port oil')).not.toBeInTheDocument()

    rerender(<GaugeTile config={config()} value={241325} editing onConfigure={onConfigure} />)
    screen.getByLabelText('Configure Port oil').click()
    expect(onConfigure).toHaveBeenCalledOnce()
  })
})

describe('GaugeTile trend display (ADR 0051)', () => {
  const trendConfig = config({ display: 'trend', window: '3h', min: 0, max: 100 })

  beforeEach(() => {
    vi.unstubAllGlobals()
  })

  test('draws a line from the returned history', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => ({
        points: [
          { time: '2026-08-23T00:00:00Z', value: 200000 },
          { time: '2026-08-23T01:00:00Z', value: 241325 },
          { time: '2026-08-23T02:00:00Z', value: 220000 },
        ],
      }),
    }))

    render(<GaugeTile config={trendConfig} value={241325} editing={false} onConfigure={vi.fn()} />)

    // The hero readout is still the live value, converted; the line is secondary.
    expect(screen.getByText('35.0')).toBeInTheDocument()
    expect(await screen.findByTestId('gauge-trend-line')).toBeInTheDocument()
  })

  /**
   * The whole reason the endpoint 503s rather than returning an empty series:
   * a flat line drawn because no database exists reads exactly like a sensor
   * holding steady.
   */
  test('says history needs InfluxDB rather than drawing an empty chart', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
      ok: false,
      status: 503,
      json: async () => ({ error: 'history needs InfluxDB, which is not configured. Enable it in Settings.' }),
    }))

    render(<GaugeTile config={trendConfig} value={241325} editing={false} onConfigure={vi.fn()} />)

    expect(await screen.findByText(/needs InfluxDB/i)).toBeInTheDocument()
    expect(screen.queryByTestId('gauge-trend-line')).not.toBeInTheDocument()
  })

  test('still shows the live reading when history is unavailable', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
      ok: false, status: 503, json: async () => ({ error: 'history needs InfluxDB' }),
    }))

    render(<GaugeTile config={trendConfig} value={241325} editing={false} onConfigure={vi.fn()} />)
    expect(await screen.findByText(/needs InfluxDB/i)).toBeInTheDocument()
    expect(screen.getByText('35.0')).toBeInTheDocument()
  })
})

describe('healthy zones (ADR 0053)', () => {
  const healthy = config({ display: 'radial', min: 0, max: 100, zones: [{ from: 40, to: 100, state: 'normal' }] })

  // An advisory band that renders in the border colour is invisible.
  test('renders a normal band visibly, not in the border colour', () => {
    const { container } = render(
      <GaugeTile config={healthy} value={241325} editing={false} onConfigure={vi.fn()} />,
    )
    const strokes = [...container.querySelectorAll('path')].map((p) => p.getAttribute('stroke') ?? '')
    expect(strokes.some((stroke) => stroke.startsWith('hsl(142'))).toBe(true)
    expect(strokes.some((stroke) => stroke.includes('--border'))).toBe(false)
  })

  // A healthy band must never colour the readout as though something is wrong.
  test('leaves the readout in the normal gauge colour', () => {
    render(<GaugeTile config={healthy} value={241325} editing={false} onConfigure={vi.fn()} />)
    const readout = screen.getByText('35.0')
    expect(readout.className).toContain('text-gauge-primary')
    expect(readout.className).not.toContain('red')
    expect(readout.className).not.toContain('amber')
  })
})

/**
 * The tile edge carries the gauge's own zone (ADR 0081).
 */
describe('tile state (ADR 0081)', () => {
  const zoned = config({
    display: 'numeric', min: 0, max: 100,
    zones: [{ from: 0, to: 20, state: 'alarm' }],
  })

  test('carries the active zone up to the tile edge', () => {
    const { container } = render(
      <GaugeTile config={zoned} value={68947} editing={false} onConfigure={vi.fn()} />,
    )
    expect(container.querySelector('[data-slot="card"]')).toHaveAttribute('data-state', 'alarm')
  })

  test('carries no state when the reading is in no zone', () => {
    const { container } = render(
      <GaugeTile config={zoned} value={689470} editing={false} onConfigure={vi.fn()} />,
    )
    expect(container.querySelector('[data-slot="card"]')).not.toHaveAttribute('data-state')
  })

  test('carries no state when the value is absent', () => {
    const { container } = render(
      <GaugeTile config={zoned} value={null} editing={false} onConfigure={vi.fn()} />,
    )
    expect(container.querySelector('[data-slot="card"]')).not.toHaveAttribute('data-state')
  })
})

/**
 * Ages ride the same gauge-values stream (ADR 0083): a frozen source marks
 * the tile stale and blanks the reading, the same treatment ADR 0068 gave
 * Solar and Battery & Power, extended to every configurable gauge.
 */
describe('staleness (ADR 0083)', () => {
  const zoned = config({
    display: 'numeric', min: 0, max: 100,
    zones: [{ from: 0, to: 20, state: 'alarm' }],
  })

  test('a frozen source blanks the reading and marks the tile stale, without a zone', () => {
    const { container } = render(
      <GaugeTile config={zoned} value={68947} ages={{ [zoned.path]: 300 }} editing={false} onConfigure={vi.fn()} />,
    )
    expect(screen.getByTestId('tile-stale-badge')).toBeInTheDocument()
    expect(screen.getByText('--')).toBeInTheDocument()
    expect(screen.queryByText('10.0')).not.toBeInTheDocument()
    expect(container.querySelector('[data-slot="card"]')).not.toHaveAttribute('data-state')
  })

  test('a fresh age leaves the reading and state untouched', () => {
    render(
      <GaugeTile config={config()} value={241325} ages={{ 'propulsion.port.oilPressure': 5 }} editing={false} onConfigure={vi.fn()} />,
    )
    expect(screen.queryByTestId('tile-stale-badge')).not.toBeInTheDocument()
    expect(screen.getByText('35.0')).toBeInTheDocument()
  })

  test('an unknown age is never stale', () => {
    render(<GaugeTile config={config()} value={241325} editing={false} onConfigure={vi.fn()} />)
    expect(screen.queryByTestId('tile-stale-badge')).not.toBeInTheDocument()
    expect(screen.getByText('35.0')).toBeInTheDocument()
  })
})

/**
 * The plain radial's zone wash follows the same rule as DialRing's rim
 * (ADR 0080): thin at rest, full width only while the reading sits in that
 * zone, so a healthy gauge does not carry a permanent amber or red band.
 */
describe('zone wash width (ADR 0080)', () => {
  const zoned = config({
    display: 'radial', min: 0, max: 100,
    zones: [{ from: 0, to: 20, state: 'alarm' }],
  })

  test('is a thin wash when the reading is outside the zone', () => {
    // 68947 Pa converts to 10.0 psi, inside the 0-20 alarm zone here — use a
    // reading well clear of it instead.
    const { container } = render(
      <GaugeTile config={zoned} value={689470} editing={false} onConfigure={vi.fn()} />,
    )
    const wash = [...container.querySelectorAll('path')]
      .find((p) => p.getAttribute('stroke') === severityFill('alarm'))!
    expect(wash.getAttribute('stroke-width')).toBe('3')
    expect(wash.getAttribute('opacity')).toBe('0.4')
  })

  test('widens to full width when the reading is inside the zone', () => {
    const { container } = render(
      <GaugeTile config={zoned} value={68947} editing={false} onConfigure={vi.fn()} />,
    )
    const wash = [...container.querySelectorAll('path')]
      .find((p) => p.getAttribute('stroke') === severityFill('alarm'))!
    expect(wash.getAttribute('stroke-width')).toBe('8')
    expect(wash.getAttribute('opacity')).toBe('0.4')
  })
})

describe('instrument ring style (ADR 0054)', () => {
  const instrument = config({
    display: 'radial', ringStyle: 'instrument', min: 0, max: 3000,
    quantity: 'frequency', unit: 'rpm', decimals: 0, labelDivisor: 100,
  })

  test('draws ticks and a divided scale', () => {
    const { container } = render(
      <GaugeTile config={instrument} value={11.638} editing={false} onConfigure={vi.fn()} />,
    )
    expect(container.querySelectorAll('[data-tick="major"]').length).toBeGreaterThan(2)
    // A divided scale has to say so, or 0/10/20/30 is just wrong.
    expect(screen.getByText('RPM x100')).toBeInTheDocument()
    // 11.638 Hz is 698 RPM — the live idle reading.
    expect(screen.getByText('698')).toBeInTheDocument()
  })

  // Every gauge configured before this option existed must render as it did.
  test('leaves the plain arc alone by default', () => {
    const { container } = render(
      <GaugeTile config={config({ display: 'radial', min: 0, max: 100 })} value={241325} editing={false} onConfigure={vi.fn()} />,
    )
    expect(container.querySelectorAll('[data-tick]')).toHaveLength(0)
    expect(screen.getByText('35.0')).toBeInTheDocument()
  })

  test('picks round major steps rather than arbitrary ones', () => {
    expect(majorStepFor(0, 3000)).toBe(500)
    // A redlined tachometer's range does not end on a round number. Rounding
    // span/6 straight up took 550 to 1000 and left a four-tick dial.
    expect(majorStepFor(0, 3300)).toBe(500)
    expect(majorStepFor(0, 100)).toBe(20)
    expect(majorStepFor(0, 1)).toBe(0.2)
  })
})

// A numeric gauge has no dial or bar taking up room, so it was already at its
// largest readout size at 'full' density before 'hero' existed. The fallback
// ternary once repeated that same 'text-4xl' as a separate 'hero' arm, which
// no branch of it could ever tell apart from 'full' — collapsed here to the
// two sizes that are actually distinct.
describe('numeric readout size without an explicit readoutSizeClass', () => {
  const numeric = config({ display: 'numeric' })

  test('hero and full density render the same, largest size', () => {
    const { container: full } = render(<GaugeBody config={numeric} value={241325} density="full" />)
    const { container: hero } = render(<GaugeBody config={numeric} value={241325} density="hero" />)
    expect(full.querySelector('.font-display')).toHaveClass('text-4xl')
    expect(hero.querySelector('.font-display')).toHaveClass('text-4xl')
  })

  test('compact density renders smaller', () => {
    const { container } = render(<GaugeBody config={numeric} value={241325} density="compact" />)
    expect(container.querySelector('.font-display')).toHaveClass('text-2xl')
  })
})
