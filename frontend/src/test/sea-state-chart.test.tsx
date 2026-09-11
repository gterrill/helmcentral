import { render, screen } from '@testing-library/react'
import { describe, expect, test } from 'vitest'

import { SeaStateChart } from '@/components/forecast/sea-state-chart'
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
    // 120 points (lastIndex 119) at width 582: plot width is 502px after
    // margins, so 3-hourly spacing (12px apart) would overlap into a solid
    // band. The step must widen to the next multiple of 3 hours - 6 - giving
    // half as many glyphs.
    render(<SeaStateChart series={buildSeries()} width={582} height={280} waveUnit="m" />)

    expect(screen.getAllByTestId('forecast-wind-barb')).toHaveLength(20)
    expect(screen.getAllByTestId('forecast-wave-arrow')).toHaveLength(20)
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
