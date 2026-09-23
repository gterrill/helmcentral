import { render, screen } from '@testing-library/react'
import { describe, expect, test } from 'vitest'

import { SeaStateChart } from '@/components/forecast/sea-state-chart'
import { SEA_STATE_GLYPH_SCALE, SEA_STATE_PLOT_INSET, SEA_STATE_DAY_COUNT, xForIndex, xForDayBoundary } from '@/lib/sea-state-geometry'
import type { SeaStatePoint } from '@/lib/sea-state-series'

// Five days, real ISO dayKeys (2026-06-14 is a Sunday) so day-name ticks are
// derivable from the point data alone, matching the component's four-prop
// signature (series, width, height, waveUnit) - no separate day-labels prop.
const DAY_KEYS = ['2026-06-14', '2026-06-15', '2026-06-16', '2026-06-17', '2026-06-18']

function buildSeries(): SeaStatePoint[] {
  const points: SeaStatePoint[] = []
  DAY_KEYS.forEach((dayKey, dayIndex) => {
    for (let hourOfDay = 0; hourOfDay < 24; hourOfDay++) {
      points.push({
        index: dayIndex * 24 + hourOfDay,
        dayKey,
        hourOfDay,
        windKts: 10 + hourOfDay * 0.1,
        gustKts: 14 + hourOfDay * 0.1,
        windDirDeg: (hourOfDay * 15) % 360,
        waveM: 1 + hourOfDay * 0.02,
        waveDirDeg: (hourOfDay * 20) % 360,
        steepnessBand: hourOfDay % 8 < 6 ? 'rolling' : 'steep',
      })
    }
  })
  return points
}

describe('SeaStateChart', () => {
  test('draws one wind barb and one wave arrow every 3 hours', () => {
    render(<SeaStateChart series={buildSeries()} width={1200} height={280} waveUnit="m" />)

    expect(screen.getAllByTestId('forecast-wind-barb')).toHaveLength(40)
    expect(screen.getAllByTestId('forecast-wave-arrow')).toHaveLength(40)
  })

  test('draws its barbs and arrows smaller than the forecast drawer does', () => {
    expect(SEA_STATE_GLYPH_SCALE).toBeGreaterThan(0)
    expect(SEA_STATE_GLYPH_SCALE).toBeLessThan(1)

    const { container } = render(<SeaStateChart series={buildSeries()} width={1200} height={280} waveUnit="m" />)
    const staff = container.querySelector('[data-testid="forecast-wind-barb"] > line') as SVGLineElement
    const length = Math.hypot(
      Number(staff.getAttribute('x2')) - Number(staff.getAttribute('x1')),
      Number(staff.getAttribute('y2')) - Number(staff.getAttribute('y1')),
    )
    expect(length).toBeCloseTo(24 * SEA_STATE_GLYPH_SCALE)
  })

  test('draws a day boundary between each pair of adjacent days', () => {
    render(<SeaStateChart series={buildSeries()} width={1200} height={280} waveUnit="m" />)

    expect(screen.getAllByTestId('sea-state-day-boundary')).toHaveLength(4)
  })

  test('labels each day tick with its weekday', () => {
    render(<SeaStateChart series={buildSeries()} width={1200} height={280} waveUnit="m" />)

    // 2026-06-14 is a Sunday; five consecutive days from there.
    for (const label of ['Sun', 'Mon', 'Tue', 'Wed', 'Thu']) {
      expect(screen.getByText(label)).toBeInTheDocument()
    }
  })

  test('renders a null wave hour as a gap, never as a fabricated calm reading', () => {
    const series = buildSeries()
    series[10] = { ...series[10], waveM: null, waveDirDeg: null, steepnessBand: null }

    // Should not throw, and should not draw an arrow for the null hour
    // (hour 10 is not a multiple of 3, so this also exercises the general
    // "skip nulls silently" path rather than the barb/arrow sampling path).
    render(<SeaStateChart series={series} width={1200} height={280} waveUnit="m" />)
    expect(screen.getAllByTestId('forecast-wave-arrow')).toHaveLength(40)
  })

  test('skips a barb/arrow at a sampled index whose reading is null', () => {
    const series = buildSeries()
    // Index 0, 3, 6... are sampled; null out index 3's wind reading.
    series[3] = { ...series[3], windKts: null, windDirDeg: null }
    render(<SeaStateChart series={series} width={1200} height={280} waveUnit="m" />)

    // WindBarb itself renders null (no element) for a negative/absent
    // direction or speed, so the count drops by exactly one.
    expect(screen.getAllByTestId('forecast-wind-barb')).toHaveLength(39)
  })

  test('widens the glyph step as the chart narrows, so glyphs never pack closer than ~22px apart', () => {
    // 120 points at width 582: the real plot width is 422px (582 minus the
    // 80px SEA_STATE_PLOT_INSET on each side - see that constant's own
    // comment for why it's not just the 40px margin), so 3-hourly spacing
    // would pack well under 22px apart. The step must widen to the next
    // multiple of 3 hours - 9 - leaving 14 glyphs (ceil(120/9)).
    render(<SeaStateChart series={buildSeries()} width={582} height={280} waveUnit="m" />)

    expect(screen.getAllByTestId('forecast-wind-barb')).toHaveLength(14)
    expect(screen.getAllByTestId('forecast-wave-arrow')).toHaveLength(14)
  })

  test('folds the axis unit into the top tick only, leaving the rest of the ticks bare', () => {
    const series = buildSeries().map((p) => ({ ...p, windKts: 20, gustKts: 46, waveM: 2 }))
    render(<SeaStateChart series={series} width={1200} height={280} waveUnit="m" />)

    // windMax rounds up to 50 (niceMax), waveMax to 5 - the top tick on each
    // axis carries the unit, folded in rather than a separate axis label.
    expect(screen.getByText('50 kn')).toBeInTheDocument()
    expect(screen.getByText('5 m')).toBeInTheDocument()
    // The old standalone 'kn'/'m' corner label is gone - no bare unit text
    // floating apart from a tick value.
    expect(screen.queryByText('kn')).not.toBeInTheDocument()
    expect(screen.queryByText('m')).not.toBeInTheDocument()
  })
})

// ADR 0125: the merged forecast-conditions tile's card row and this chart
// must line up "by construction" - the day-card grid cells are inset by
// SEA_STATE_PLOT_INSET (the chart's REAL horizontal plot inset, not its raw
// margin object - Recharts adds each y-axis's own width on top of the
// margin, so the real inset is bigger; see that constant's own comment),
// and both divide the same width into SEA_STATE_DAY_COUNT equal columns.
// These pure functions are the one source of truth for where a column
// boundary or an hour-slot centre actually lands in pixels, so they're
// worth testing directly rather than only indirectly through a rendered
// chart.
describe('xForIndex / xForDayBoundary (pure chart geometry)', () => {
  const width = 1200

  test('xForDayBoundary divides the inset plot width into exactly dayCount equal columns', () => {
    const plotWidth = width - SEA_STATE_PLOT_INSET.left - SEA_STATE_PLOT_INSET.right
    for (let day = 1; day < SEA_STATE_DAY_COUNT; day++) {
      expect(xForDayBoundary(day, width)).toBeCloseTo(SEA_STATE_PLOT_INSET.left + (day / SEA_STATE_DAY_COUNT) * plotWidth, 6)
    }
  })

  test('xForIndex at a day boundary index (24d - 0.5) lands on the same pixel as xForDayBoundary(d, width)', () => {
    for (let day = 1; day < SEA_STATE_DAY_COUNT; day++) {
      expect(xForIndex(day * 24 - 0.5, width)).toBeCloseTo(xForDayBoundary(day, width), 6)
    }
  })

  test('xForIndex places hour-slot 0 half a slot in from the left inset', () => {
    const plotWidth = width - SEA_STATE_PLOT_INSET.left - SEA_STATE_PLOT_INSET.right
    const totalPoints = SEA_STATE_DAY_COUNT * 24
    expect(xForIndex(0, width)).toBeCloseTo(SEA_STATE_PLOT_INSET.left + (0.5 / totalPoints) * plotWidth, 6)
  })

  test('a narrower dayCount still divides evenly (the fewer-than-5-real-days case elsewhere pads dayCount itself, not this function)', () => {
    const plotWidth = width - SEA_STATE_PLOT_INSET.left - SEA_STATE_PLOT_INSET.right
    expect(xForDayBoundary(1, width, 2)).toBeCloseTo(SEA_STATE_PLOT_INSET.left + 0.5 * plotWidth, 6)
  })
})

describe('the rendered day-boundary lines land exactly where xForDayBoundary says they should', () => {
  test('four boundary lines at the 1/5, 2/5, 3/5 and 4/5 marks of the plot width', () => {
    const { container } = render(<SeaStateChart series={buildSeries()} width={1200} height={280} waveUnit="m" />)
    const lines = Array.from(container.querySelectorAll('.recharts-reference-line line'))
    expect(lines).toHaveLength(4)

    lines.forEach((line, i) => {
      const day = i + 1
      const expectedX = xForDayBoundary(day, 1200)
      expect(Number(line.getAttribute('x1'))).toBeCloseTo(expectedX, 0)
      expect(Number(line.getAttribute('x2'))).toBeCloseTo(expectedX, 0)
    })
  })

  // The fewer-than-5-real-days case (ADR 0125): the chart still divides its
  // width into 5 day columns even when `series` covers fewer real days,
  // rather than stretching what real data exists across the full tile -
  // otherwise the card row above (always 5 cells) and the chart below would
  // disagree about where a day starts and ends.
  test('still divides the width into 5 day columns when the series covers only 2 real days', () => {
    const shortSeries = buildSeries().slice(0, 48) // 2 real days, 48 hourly points
    const { container } = render(<SeaStateChart series={shortSeries} width={1200} height={280} waveUnit="m" />)
    const lines = Array.from(container.querySelectorAll('.recharts-reference-line line'))
    // Still 4 internal boundaries across 5 columns, not 1 boundary across 2.
    expect(lines).toHaveLength(4)

    lines.forEach((line, i) => {
      const day = i + 1
      const expectedX = xForDayBoundary(day, 1200)
      expect(Number(line.getAttribute('x1'))).toBeCloseTo(expectedX, 0)
    })
  })

  test('does not throw and renders no glyphs past the real data when the series is shorter than dayCount * 24', () => {
    const shortSeries = buildSeries().slice(0, 24) // 1 real day
    render(<SeaStateChart series={shortSeries} width={1200} height={280} waveUnit="m" />)
    // 24 points at the default (wide) glyph step of 3h -> 8 glyphs, never
    // reaching into the 4 padding days the chart still reserves columns for.
    expect(screen.getAllByTestId('forecast-wind-barb')).toHaveLength(8)
  })
})
