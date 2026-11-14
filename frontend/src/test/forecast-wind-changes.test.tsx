import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { render, screen, within, renderHook, waitFor } from '@testing-library/react'

import { ForecastDrawer } from '@/components/forecast-drawer'
import { useWeatherForecast, type WindChange } from '@/hooks/use-weather-forecast'
import { buildWindChangeSentence, windChangeChipText } from '@/lib/forecast-wind-changes'

// Wind changes on the forecast (ADR 0287).

function change(overrides: Partial<WindChange> = {}): WindChange {
  return {
    label: '2 PM',
    from: 'N',
    to: 'SE',
    speedMinKn: 15,
    speedMaxKn: 20,
    lowerConfidence: false,
    prominent: false,
    ...overrides,
  }
}

const NAMES = ['Sunday', 'Monday', 'Tuesday']

function day(idx: number, windChanges: WindChange[] = []) {
  return {
    dayKey: `2026-06-${14 + idx}`,
    date: `Jun ${14 + idx}`,
    dayName: NAMES[idx],
    condition: 'Clear',
    high: 76,
    low: 62,
    windSpeed: 10,
    windGust: 14,
    windDirection: 'NE',
    windSummary: null,
    precipitationSummary: null,
    precipitation: 5,
    humidityPct: 58,
    visibilityNm: 9.5,
    sunriseTime: null,
    sunsetTime: null,
    moonPhase: null,
    hourlyWind: [],
    hourlyPrecip: [],
    hourlyUV: [],
    hourlyCloud: [],
    windChanges,
  }
}

describe('forecast wind change wording', () => {
  it('writes the chip as from, to and time', () => {
    expect(windChangeChipText(change())).toBe('N→SE 2 PM')
    expect(windChangeChipText(change({ label: '1.30 PM' }))).toBe('N→SE 1.30 PM')
  })

  it('leads with the wind until the change, then the wind behind it', () => {
    expect(buildWindChangeSentence([day(0), day(1), day(2, [change()])], null)).toBe(
      'Northerlies until a SE change Tue 16 around 2 PM, 15–20 kn behind it.',
    )
  })

  it('says "an" before a change to a point that starts with E, read as "east"', () => {
    expect(buildWindChangeSentence([day(2, [change({ from: 'NNW', to: 'ESE' }), change({ from: 'ESE', to: 'E', label: '9 PM' })])], null)).toBe(
      'NNW winds until an ESE change Tue 16 around 2 PM, 15–20 kn behind it, then an E change Tue 16 around 9 PM.',
    )
  })

  it('words today and tomorrow from the boat\'s date at the time of drawing, not the time of the response', () => {
    const days = [day(0, [change({ speedMinKn: 18, speedMaxKn: 18 })]), day(1, [change({ from: 'SE', to: 'W', label: '1 AM' })])]
    expect(buildWindChangeSentence(days, '2026-06-14')).toBe(
      'Northerlies until a SE change today around 2 PM, 18 kn behind it, then a W change tomorrow around 1 AM.',
    )
    // The same response, drawn the next morning: Sunday's change is no longer today.
    expect(buildWindChangeSentence(days, '2026-06-15')).toBe(
      'Northerlies until a SE change Sun 14 around 2 PM, 18 kn behind it, then a W change today around 1 AM.',
    )
  })

  it('words a lower-confidence change as less certain and lists later ones', () => {
    const sentence = buildWindChangeSentence([
      day(0),
      day(1, [change({ lowerConfidence: true })]),
      day(2, [change({ from: 'SE', to: 'W', label: '1 AM' })]),
    ], '2026-06-14')
    expect(sentence).toBe(
      'Northerlies until a SE change tomorrow around 2 PM, 15–20 kn behind it (less certain this far out), then a W change Tue 16 around 1 AM.',
    )
  })

  it('says nothing when there is no change', () => {
    expect(buildWindChangeSentence([day(0), day(1)], null)).toBeNull()
  })

  it('does not invent a word for a point between the eight', () => {
    expect(buildWindChangeSentence([day(0, [change({ from: 'NNE' })])], null)).toMatch(/^NNE winds until/)
  })
})

describe('ForecastDrawer wind changes', () => {
  it('shows one chip on the day the change happens, coloured only when prominent', () => {
    render(
      <ForecastDrawer
        forecast={[day(0), day(1, [change({ prominent: true })]), day(2, [change({ from: 'SE', to: 'S', label: '9 PM' })])]}
        loading={false}
        error={null}
        unit="metric"
      />,
    )
    const chips = screen.getAllByTestId('forecast-wind-change-chip')
    expect(chips).toHaveLength(2)
    expect(chips[0]).toHaveTextContent('N→SE 2 PM')
    expect(chips[0]).toHaveAttribute('data-prominent', 'true')
    expect(chips[0].className).toContain('amber')
    expect(chips[1]).toHaveTextContent('SE→S 9 PM')
    expect(chips[1]).not.toHaveAttribute('data-prominent')
    expect(chips[1].className).toContain('text-muted-foreground')
    expect(chips[0].className).toContain('truncate')
    // The day's own direction is still on the card.
    expect(screen.getAllByTestId('forecast-day-headline')[1]).toHaveTextContent('NE')
  })

  it('never wraps the chip row, and shows +N for further changes that name them all to a screen reader', () => {
    render(
      <ForecastDrawer
        forecast={[day(0), day(1, [change(), change({ from: 'SE', to: 'S', label: '9 PM' })])]}
        loading={false}
        error={null}
        unit="metric"
      />,
    )
    expect(screen.getAllByTestId('forecast-wind-change-chip')).toHaveLength(1)
    expect(screen.getByTestId('forecast-wind-change-more')).toHaveTextContent('+1')
    expect(screen.getByTestId('forecast-day-wind-changes').className).not.toContain('flex-wrap')
    expect(screen.getByRole('button', { name: /N to SE around 2 PM.*SE to S around 9 PM/ })).toBeInTheDocument()
  })

  it('says why changes were not judged against the limits, under the wind sentence', () => {
    render(
      <ForecastDrawer
        forecast={[day(0), day(1, [change()])]}
        windChangesProblem="assistant.passage_limits is unreadable"
        loading={false}
        error={null}
        unit="metric"
      />,
    )
    expect(screen.getByTestId('forecast-wind-changes-problem')).toHaveTextContent(
      'Wind changes not judged against your limits: assistant.passage_limits is unreadable',
    )
    expect(screen.getByTestId('forecast-wind-change-chip')).not.toHaveAttribute('data-prominent')
  })

  it('says nothing about limits when there is no change to judge', () => {
    render(
      <ForecastDrawer forecast={[day(0), day(1)]} windChangesProblem="assistant.passage_limits is unreadable" loading={false} error={null} unit="metric" />,
    )
    expect(screen.queryByTestId('forecast-wind-changes-problem')).not.toBeInTheDocument()
  })

  it('says how far the changes were checked, only when the hourly forecast stops early', () => {
    const { rerender } = render(
      <ForecastDrawer forecast={[day(0), day(1), day(2)]} windChangesCheckedTo="Mon 15 3 AM" windChangesCheckedReason="the forecast skips an hour here" loading={false} error={null} unit="metric" />,
    )
    expect(screen.getByTestId('forecast-wind-changes-checked-to')).toHaveTextContent('Wind changes checked to Mon 15 3 AM: the forecast skips an hour here')
    rerender(<ForecastDrawer forecast={[day(0), day(1), day(2)]} windChangesCheckedTo={null} loading={false} error={null} unit="metric" />)
    expect(screen.queryByTestId('forecast-wind-changes-checked-to')).not.toBeInTheDocument()
  })

  it('shows no problem line when the limits were read', () => {
    render(<ForecastDrawer forecast={[day(0), day(1, [change()])]} loading={false} error={null} unit="metric" />)
    expect(screen.queryByTestId('forecast-wind-changes-problem')).not.toBeInTheDocument()
  })

  it('names the change in the day card label for a screen reader', () => {
    render(<ForecastDrawer forecast={[day(0), day(1, [change()])]} loading={false} error={null} unit="metric" />)
    expect(screen.getByRole('button', { name: /Monday.*wind change N to SE around 2 PM/ })).toBeInTheDocument()
  })

  it('shows the chip amber when any change that day is prominent, not only the first', () => {
    const quiet = change({ from: 'N', to: 'NE', label: '8 AM' })
    const loud = change({ from: 'NE', to: 'SE', label: '4 PM', prominent: true })
    render(<ForecastDrawer forecast={[day(0), day(1, [quiet, loud])]} loading={false} error={null} unit="metric" />)
    const chip = screen.getByTestId('forecast-wind-change-chip')
    expect(chip).toHaveAttribute('data-prominent', 'true')
    expect(chip.textContent).toBe('NE→SE 4 PM')
    expect(screen.getByTestId('forecast-wind-change-more').textContent).toBe('+1')
    expect(chip.className).toMatch(/amber/)
  })

  it('names every change in the day label and which reach the marginal wind', () => {
    const quiet = change({ from: 'N', to: 'NE', label: '8 AM' })
    const loud = change({ from: 'NE', to: 'SE', label: '4 PM', prominent: true })
    render(<ForecastDrawer forecast={[day(0), day(1, [quiet, loud])]} loading={false} error={null} unit="metric" />)
    const label = screen.getByRole('button', { name: /Monday/ }).getAttribute('aria-label') ?? ''
    expect(label).toMatch(/wind change N to NE around 8 AM(?!, reaches your marginal wind)/)
    expect(label).toContain('wind change NE to SE around 4 PM, reaches your marginal wind')
  })

  it('says when a wind change could not be read, never "no change"', () => {
    render(<ForecastDrawer forecast={[day(0), day(1)]} loading={false} error={null} unit="metric" windChangesUnreadable="1 of the forecast's wind changes could not be read" />)
    expect(screen.getByTestId('forecast-wind-changes-unreadable').textContent).toContain("1 of the forecast's wind changes could not be read")
  })

  it('leads the extended summary with the wind sentence', () => {
    render(<ForecastDrawer forecast={[day(0), day(1), day(2, [change()])]} loading={false} error={null} unit="metric" />)
    const intro = screen.getByTestId('forecast-extended-intro')
    expect(intro.textContent?.startsWith('Northerlies until a SE change Tue 16 around 2 PM, 15–20 kn behind it.')).toBe(true)
    expect(within(screen.getByTestId('forecast-panel-extended')).getByTestId('forecast-extended-intro')).toBe(intro)
  })

  it('shows no chip and no wind sentence without a change', () => {
    render(<ForecastDrawer forecast={[day(0), day(1)]} loading={false} error={null} unit="metric" />)
    expect(screen.queryByTestId('forecast-wind-change-chip')).not.toBeInTheDocument()
    expect(screen.getByTestId('forecast-extended-intro').textContent).not.toMatch(/until a/)
  })
})

describe('useWeatherForecast wind changes', () => {
  beforeEach(() => vi.clearAllMocks())
  afterEach(() => vi.unstubAllGlobals())

  it('maps a day\'s wind_changes and flags a malformed one instead of showing no change', async () => {
    const errorSpy = vi.spyOn(console, 'error').mockImplementation(() => {})
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue({
        ok: true,
        json: async () => ({
          wind_changes_problem: 'assistant.passage_limits is unreadable',
          wind_changes_checked_to: 'Fri 19 3 AM',
          wind_changes_checked_reason: 'the forecast is no longer hourly',
          wind_changes_unplaced: 2,
          days: [
            {
              day_key: '2026-06-16',
              wind_changes: [
                { label: '2 PM', from: 'N', to: 'SE', speed_min_kn: 15, speed_max_kn: 20, lower_confidence: true, prominent: true },
                { label: 'broken' },
              ],
            },
          ],
        }),
      }),
    )
    const { result } = renderHook(() => useWeatherForecast())
    await waitFor(() => expect(result.current.forecast).toHaveLength(1))
    expect(result.current.forecast[0].windChanges).toEqual([
      { label: '2 PM', from: 'N', to: 'SE', speedMinKn: 15, speedMaxKn: 20, lowerConfidence: true, prominent: true },
    ])
    expect(result.current.windChangesProblem).toBe('assistant.passage_limits is unreadable')
    expect(result.current.windChangesCheckedTo).toBe('Fri 19 3 AM')
    expect(result.current.windChangesCheckedReason).toBe('the forecast is no longer hourly')
    expect(result.current.windChangesUnreadable).toMatch(/could not be read/)
    expect(result.current.windChangesUnreadable).toMatch(/2 wind changes fall on a day the forecast has no day card for/)
    errorSpy.mockRestore()
  })

  it('counts a null or number entry as unreadable instead of breaking the forecast', async () => {
    const errorSpy = vi.spyOn(console, 'error').mockImplementation(() => {})
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue({
        ok: true,
        json: async () => ({
          days: [{ day_key: '2026-06-16', wind_changes: [null, 5, { label: '2 PM', from: 'N', to: 'SE', speed_min_kn: 15, speed_max_kn: 20 }] }],
        }),
      }),
    )
    const { result } = renderHook(() => useWeatherForecast())
    await waitFor(() => expect(result.current.forecast).toHaveLength(1))
    expect(result.current.error).toBeNull()
    expect(result.current.forecast[0].windChanges).toHaveLength(1)
    expect(result.current.windChangesUnreadable).toMatch(/^2 of the forecast's wind changes could not be read/)
    errorSpy.mockRestore()
  })

  it('says one change with no day card in the singular', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue({
        ok: true,
        json: async () => ({ wind_changes_unplaced: 1, days: [{ day_key: '2026-06-16' }] }),
      }),
    )
    const { result } = renderHook(() => useWeatherForecast())
    await waitFor(() => expect(result.current.forecast).toHaveLength(1))
    expect(result.current.windChangesUnreadable).toBe('1 wind change falls on a day the forecast has no day card for')
  })
})
