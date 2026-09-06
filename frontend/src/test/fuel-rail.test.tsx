import { render, screen, within } from '@testing-library/react'
import { describe, expect, test } from 'vitest'

import { FuelRail } from '@/components/ui/fuel-rail'
import type { ClusterFuelRail, GaugeWidgetConfig } from '@/lib/dashboard-widgets'

/** The live port side, in the SI the stream carries. Captured, not assumed. */
const values = {
  'tanks.fuel.5.currentLevel': 0.7416, // 74.2%
  'tanks.fuel.5.capacity': 1.2, // 1200 L, so 890 L aboard
  'tanks.fuel.4.currentLevel': 0.68348, // 68.3%
  'tanks.fuel.4.capacity': 1.3, // 1300 L, so 889 L aboard
}

const level = (path: string, label: string, zones?: GaugeWidgetConfig['zones']): GaugeWidgetConfig => ({
  path, label, display: 'bar', quantity: 'ratio', unit: 'percent', decimals: 0, min: 0, max: 100, zones,
})
const capacity = (path: string, unit = 'L'): GaugeWidgetConfig => ({
  path, label: '', display: 'numeric', quantity: 'volume', unit, decimals: 0,
})

function port(overrides: Partial<ClusterFuelRail> = {}): ClusterFuelRail {
  return {
    side: 'left',
    bars: [
      { level: level('tanks.fuel.5.currentLevel', 'Fwd'), capacity: capacity('tanks.fuel.5.capacity') },
      { level: level('tanks.fuel.4.currentLevel', 'Aft'), capacity: capacity('tanks.fuel.4.capacity') },
    ],
    ...overrides,
  }
}

function renderRail(config = port(), v: Record<string, number | null> = values) {
  return render(<FuelRail config={config} values={v} height={228} />)
}

/** Where a bar stops: its top end, in viewBox units. */
function barTop(el: Element): number {
  return Number(el.getAttribute('y2'))
}
function barColumn(el: Element): number {
  return Number(el.getAttribute('x1'))
}

describe('readings', () => {
  test('turns a ratio and a capacity into litres', () => {
    renderRail()
    const fwd = screen.getByTestId('fuel-readout-0')
    expect(fwd).toHaveTextContent('74')
    expect(fwd).toHaveTextContent('890')
  })

  /**
   * The total is summed before rounding, so it is the litres actually aboard
   * rather than the displayed figures added up. Those differ by one here: 890
   * and 889 are each rounded up, while the true 1778.4 rounds down.
   */
  test('sums the side from unrounded readings', () => {
    renderRail()
    expect(screen.getByTestId('fuel-total')).toHaveTextContent('1778')
  })

  test('names the side total', () => {
    renderRail(port({ totalLabel: 'Aboard' }))
    expect(screen.getByText('Aboard')).toBeInTheDocument()
  })

  test('honours the configured volume unit', () => {
    const gallons = port()
    gallons.bars = gallons.bars.map((b) => ({ ...b, capacity: capacity(b.capacity.path, 'gal') }))
    renderRail(gallons)
    // 1.778444 m3 is 470 US gallons.
    expect(screen.getByTestId('fuel-total')).toHaveTextContent('470')
  })
})

/**
 * A tank reading zero and a sender that has stopped talking draw the same
 * nothing, and on a fuel gauge that is a safety problem rather than a cosmetic
 * one. The bar carries the distinction as well as the readout.
 */
describe('absent data', () => {
  test('shows the structural dash for an absent level, never a zero', () => {
    renderRail(port(), { ...values, 'tanks.fuel.5.currentLevel': null })
    const fwd = screen.getByTestId('fuel-readout-0')
    expect(fwd).toHaveTextContent('--')
    expect(within(fwd).queryByText('0')).not.toBeInTheDocument()
  })

  test('draws no bar at all for an absent level', () => {
    const { container } = renderRail(port(), { ...values, 'tanks.fuel.5.currentLevel': null })
    expect(container.querySelector('[data-fuel-bar="0"]')).toBeNull()
    expect(container.querySelector('[data-fuel-bar="1"]')).not.toBeNull()
  })

  test('tells an empty tank apart from a silent sender', () => {
    const { container: empty } = renderRail(port(), { ...values, 'tanks.fuel.5.currentLevel': 0 })
    expect(empty.querySelector('[data-bar-state="empty"]')).not.toBeNull()

    const { container: absent } = renderRail(port(), { ...values, 'tanks.fuel.5.currentLevel': null })
    expect(absent.querySelector('[data-bar-state="absent"]')).not.toBeNull()
  })

  /** A total missing one tank is a plausible number that is wrong. */
  test('refuses a partial total', () => {
    renderRail(port(), { ...values, 'tanks.fuel.5.capacity': null })
    expect(screen.getByTestId('fuel-total')).toHaveTextContent('--')
    // The tank that did report still shows its own litres.
    expect(screen.getByTestId('fuel-readout-1')).toHaveTextContent('889')
  })
})

describe('scale', () => {
  test('draws no hairline beside the bars', () => {
    const { container } = renderRail()
    expect(container.querySelector('[data-fuel-guide]')).toBeNull()
  })

  test('labels every quarter of the range', () => {
    renderRail()
    for (const label of ['0', '25', '50', '75', '100']) {
      expect(screen.getByText(label)).toBeInTheDocument()
    }
  })

  test('subdivides to a tick every five percent', () => {
    const { container } = renderRail()
    // 0 to 100 in fives is 21 ticks, of which 5 are majors.
    expect(container.querySelectorAll('[data-tick]')).toHaveLength(21)
    expect(container.querySelectorAll('[data-tick="major"]')).toHaveLength(5)
  })
})

describe('bars', () => {
  test('gives each tank its own column', () => {
    const { container } = renderRail()
    const columns = [0, 1].map((i) => barColumn(container.querySelector(`[data-fuel-bar="${i}"]`)!))
    expect(columns[0]).not.toBe(columns[1])
  })

  test('runs each bar straight up its column', () => {
    const { container } = renderRail()
    const bar = container.querySelector('[data-fuel-bar="0"]')!
    expect(bar.getAttribute('x1')).toBe(bar.getAttribute('x2'))
  })

  test('ends a bar at its own reading rather than the end of the scale', () => {
    const { container: partial } = renderRail()
    const { container: full } = renderRail(port(), { ...values, 'tanks.fuel.5.currentLevel': 1 })

    // 0 percent is the foot of the scale and 100 its head, so a fuller tank
    // ends at a smaller y.
    expect(barTop(full.querySelector('[data-fuel-bar="0"]')!))
      .toBeLessThan(barTop(partial.querySelector('[data-fuel-bar="0"]')!))
  })

  test('clamps a level past the top rather than overdrawing', () => {
    const { container: over } = renderRail(port(), { ...values, 'tanks.fuel.5.currentLevel': 9 })
    const { container: full } = renderRail(port(), { ...values, 'tanks.fuel.5.currentLevel': 1 })
    expect(barTop(over.querySelector('[data-fuel-bar="0"]')!))
      .toBe(barTop(full.querySelector('[data-fuel-bar="0"]')!))
  })
})

describe('both edges', () => {
  /**
   * The rail used to mirror, so a facing pair put their scales inboard. It does
   * not any more: a scale that changes hand between the two engines makes an
   * operator re-learn it at the second tile. `side` now only says which edge of
   * the tile the rail attaches to, which is the cluster's business.
   */
  test('draws exactly the same on either edge', () => {
    const { container: left } = renderRail(port({ side: 'left' }))
    const { container: right } = renderRail(port({ side: 'right' }))
    expect(right.querySelector('svg')!.innerHTML).toBe(left.querySelector('svg')!.innerHTML)
  })

  test('keeps the scale numbers upright, never flipped', () => {
    const { container } = renderRail(port({ side: 'right' }))
    for (const text of container.querySelectorAll('text')) {
      expect(text.getAttribute('transform')).toBeNull()
    }
  })

  // The reading side of the scale, on both tiles.
  test('puts the percent mark to the right of the numbers', () => {
    const { container } = renderRail()
    const unit = Number(container.querySelector('[data-fuel-unit]')!.getAttribute('x'))
    const labels = [...container.querySelectorAll('text')]
      .filter((t) => t.textContent === '50')
      .map((t) => Number(t.getAttribute('x')))
    expect(unit).toBeGreaterThan(Math.max(...labels))
  })
})

describe('zones', () => {
  test('colours the readout of the tank the zone belongs to', () => {
    const low = port()
    low.bars[0].level = level('tanks.fuel.5.currentLevel', 'Fwd', [{ from: 0, to: 90, state: 'warn' }])
    renderRail(low)

    expect(screen.getByTestId('fuel-readout-0').innerHTML).toContain('text-amber-600')
    expect(screen.getByTestId('fuel-readout-1').innerHTML).not.toContain('text-amber-600')
  })
})

describe('theming', () => {
  test('uses theme tokens rather than hardcoded colours', () => {
    const { container } = renderRail()
    const svg = container.querySelector('svg')!.outerHTML
    expect(svg).toContain('hsl(var(--')
    expect(svg).not.toMatch(/#[0-9a-f]{6}/i)
  })

  /**
   * A custom property with no value makes the whole declaration invalid at
   * computed-value time and is dropped in silence, so every var the component
   * reads needs a :root default. The same check dial-ring.tsx carries.
   */
  test('every --fuel-* var the component reads has a :root default', async () => {
    const [fs, path] = await Promise.all([import('node:fs'), import('node:path')])
    const read = (rel: string) => fs.readFileSync(path.resolve(process.cwd(), rel), 'utf8')

    const component = read('src/components/ui/fuel-rail.tsx')
    const css = read('src/index.css')
    const root = css.slice(css.indexOf(':root {'), css.indexOf('}', css.indexOf(':root {')))

    const declared = new Set([...root.matchAll(/--([\w-]+):/g)].map((m) => m[1]))
    const used = new Set([...component.matchAll(/var\(--(fuel-[\w-]+)/g)].map((m) => m[1]))

    expect(used.size).toBeGreaterThan(0)
    expect([...used].filter((t) => !declared.has(t))).toEqual([])
  })
})
