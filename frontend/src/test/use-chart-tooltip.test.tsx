import { describe, it, expect } from 'vitest'
import { fireEvent, render, screen } from '@testing-library/react'

import { useChartTooltip } from '@/hooks/use-chart-tooltip'

/*
 * Scrubbing an overlay is the only way to read an exact value off any of the
 * five charts (the summary sentences give ranges, not per-hour numbers), so
 * with no pointer there was no read path at all. These cover the keyboard one:
 * arrows step, Home/End jump, Escape clears, focus reveals.
 *
 * Geometry is chosen so the index -> pixel math is checkable by hand: a
 * 800-unit viewBox rendered 400px wide, so pixelX is exactly half the viewBox
 * x, and a plot spanning 40..760 so index 0 lands at 20px and the last index
 * at 380px.
 */
const VIEW_BOX_WIDTH = 800
const CHART_LEFT = 40
const CHART_RIGHT = 760
const RENDERED_WIDTH = 400

function Harness({ count }: { count: number }) {
  const tooltip = useChartTooltip(count, 'fixed-key', CHART_LEFT, CHART_RIGHT)
  return (
    <>
      <svg
        data-testid="overlay"
        ref={tooltip.svgRef}
        viewBox={`0 0 ${VIEW_BOX_WIDTH} 175`}
        tabIndex={0}
        onKeyDown={tooltip.onKeyDown}
        onFocus={tooltip.onFocus}
        onPointerDown={tooltip.onPointerDown}
        onPointerMove={tooltip.onPointerMove}
        onPointerLeave={tooltip.onPointerLeave}
      />
      <output data-testid="active-index">{tooltip.activeIndex === null ? 'none' : String(tooltip.activeIndex)}</output>
      <output data-testid="pixel-x">{tooltip.tooltipPixelX === null ? 'none' : tooltip.tooltipPixelX.toFixed(3)}</output>
    </>
  )
}

// jsdom lays nothing out, so every getBoundingClientRect is 0x0 - which the
// hook (deliberately) treats as "not measurable yet" and bails on. Stub the
// one rect the hook reads so the pixel math has something real to work with.
function renderChart(count = 5, width = RENDERED_WIDTH) {
  render(<Harness count={count} />)
  const svg = screen.getByTestId('overlay')
  svg.getBoundingClientRect = () =>
    ({ width, height: 175, left: 0, top: 0, right: width, bottom: 175, x: 0, y: 0, toJSON: () => ({}) }) as DOMRect
  return svg
}

const activeIndex = () => screen.getByTestId('active-index').textContent
const pixelX = () => Number(screen.getByTestId('pixel-x').textContent)

// The inverse of the hook's pointer path: index -> fraction -> viewBox x -> px.
function expectedPixelX(index: number, count: number) {
  const fraction = count <= 1 ? 0 : index / (count - 1)
  return ((CHART_LEFT + fraction * (CHART_RIGHT - CHART_LEFT)) / VIEW_BOX_WIDTH) * RENDERED_WIDTH
}

describe('useChartTooltip keyboard reading', () => {
  it('starts at the first sample on ArrowRight when nothing is showing', () => {
    const svg = renderChart(5)
    fireEvent.keyDown(svg, { key: 'ArrowRight' })

    expect(activeIndex()).toBe('0')
    expect(pixelX()).toBeCloseTo(expectedPixelX(0, 5), 3)
  })

  it('starts at the last sample on ArrowLeft when nothing is showing', () => {
    const svg = renderChart(5)
    fireEvent.keyDown(svg, { key: 'ArrowLeft' })

    expect(activeIndex()).toBe('4')
    expect(pixelX()).toBeCloseTo(expectedPixelX(4, 5), 3)
  })

  it('steps one sample at a time and clamps at both ends', () => {
    const svg = renderChart(5)

    fireEvent.keyDown(svg, { key: 'ArrowRight' }) // 0
    fireEvent.keyDown(svg, { key: 'ArrowRight' }) // 1
    expect(activeIndex()).toBe('1')
    expect(pixelX()).toBeCloseTo(expectedPixelX(1, 5), 3)

    fireEvent.keyDown(svg, { key: 'ArrowRight' })
    fireEvent.keyDown(svg, { key: 'ArrowRight' })
    fireEvent.keyDown(svg, { key: 'ArrowRight' })
    // Held past the end: stays on the last sample rather than wrapping.
    fireEvent.keyDown(svg, { key: 'ArrowRight' })
    expect(activeIndex()).toBe('4')
    expect(pixelX()).toBeCloseTo(expectedPixelX(4, 5), 3)

    fireEvent.keyDown(svg, { key: 'ArrowLeft' })
    expect(activeIndex()).toBe('3')

    fireEvent.keyDown(svg, { key: 'ArrowLeft' })
    fireEvent.keyDown(svg, { key: 'ArrowLeft' })
    fireEvent.keyDown(svg, { key: 'ArrowLeft' })
    fireEvent.keyDown(svg, { key: 'ArrowLeft' })
    expect(activeIndex()).toBe('0')
    expect(pixelX()).toBeCloseTo(expectedPixelX(0, 5), 3)
  })

  it('jumps to the first and last sample on Home and End', () => {
    const svg = renderChart(5)

    fireEvent.keyDown(svg, { key: 'End' })
    expect(activeIndex()).toBe('4')

    fireEvent.keyDown(svg, { key: 'Home' })
    expect(activeIndex()).toBe('0')
    expect(pixelX()).toBeCloseTo(expectedPixelX(0, 5), 3)
  })

  it('clears the tooltip on Escape', () => {
    const svg = renderChart(5)
    fireEvent.keyDown(svg, { key: 'End' })
    expect(activeIndex()).toBe('4')

    fireEvent.keyDown(svg, { key: 'Escape' })
    expect(activeIndex()).toBe('none')
    expect(screen.getByTestId('pixel-x').textContent).toBe('none')
  })

  // preventDefault on everything would trap focus on the chart. Only the keys
  // the hook actually acts on may be swallowed.
  it('only prevents the default for keys it handles', () => {
    const svg = renderChart(5)

    for (const key of ['ArrowRight', 'ArrowLeft', 'Home', 'End', 'Escape']) {
      expect(fireEvent.keyDown(svg, { key }), `${key} should be handled`).toBe(false)
    }
    for (const key of ['Tab', 'a', 'Enter', 'ArrowUp', 'ArrowDown']) {
      expect(fireEvent.keyDown(svg, { key }), `${key} should pass through`).toBe(true)
    }
  })

  // Escape is a shared dismissal key: it belongs to the tooltip only while a
  // tooltip is up. With nothing showing it has to reach whatever else is
  // listening (a drawer, a dialog) rather than being swallowed by the chart.
  it('lets Escape through when there is nothing to clear', () => {
    const svg = renderChart(5)

    expect(fireEvent.keyDown(svg, { key: 'Escape' })).toBe(true)

    fireEvent.keyDown(svg, { key: 'End' })
    expect(fireEvent.keyDown(svg, { key: 'Escape' })).toBe(false)
  })

  it('reveals the first sample on focus, and leaves an existing one alone', () => {
    const svg = renderChart(5)

    fireEvent.focus(svg)
    expect(activeIndex()).toBe('0')

    fireEvent.keyDown(svg, { key: 'End' })
    fireEvent.focus(svg)
    // Re-focusing must not throw away where the reader had got to.
    expect(activeIndex()).toBe('4')
  })

  // Deliberately no onBlur: clearing is pointer-type-aware (see the comment at
  // the top of the hook), and a blur-clear would fight it - a touch tooltip is
  // meant to survive until the next tap.
  it('keeps the value showing after focus moves away', () => {
    const svg = renderChart(5)
    fireEvent.keyDown(svg, { key: 'End' })

    fireEvent.blur(svg)
    expect(activeIndex()).toBe('4')
  })

  it('does nothing until the chart has been laid out', () => {
    render(<Harness count={5} />)
    const svg = screen.getByTestId('overlay') // real jsdom rect: 0 wide

    fireEvent.keyDown(svg, { key: 'ArrowRight' })
    fireEvent.focus(svg)
    expect(activeIndex()).toBe('none')
  })

  it('handles a one-sample chart without dividing by zero', () => {
    const svg = renderChart(1)

    fireEvent.keyDown(svg, { key: 'ArrowRight' })
    expect(activeIndex()).toBe('0')
    expect(pixelX()).toBeCloseTo(expectedPixelX(0, 1), 3)

    fireEvent.keyDown(svg, { key: 'End' })
    expect(activeIndex()).toBe('0')
    expect(pixelX()).toBeCloseTo(expectedPixelX(0, 1), 3)
  })

  it('ignores keys on an empty chart', () => {
    const svg = renderChart(0)

    fireEvent.keyDown(svg, { key: 'ArrowRight' })
    fireEvent.keyDown(svg, { key: 'End' })
    fireEvent.focus(svg)
    expect(activeIndex()).toBe('none')
  })
})
