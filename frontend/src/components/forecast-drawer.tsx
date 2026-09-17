import { useEffect, useId, useMemo, useRef, useState } from 'react'
import type { ReactNode } from 'react'

import { Area, Bar, CartesianGrid, Cell, ComposedChart, Customized, Line, ReferenceDot, XAxis, YAxis } from 'recharts'

import { Cloud, Moon, Sunrise, Sunset, Wind, Waves } from 'lucide-react'

import { Button } from '@/components/ui/button'
import { ChartTooltipBubble, ChartTooltipMarker, ChartUnavailableMessage } from '@/components/chart-tooltip'
import { ForecastTideSection } from '@/components/forecast-tide-section'
import { ForecastWarningNotice } from '@/components/forecast-warning-notice'
import { WeatherConditionIcon } from '@/components/forecast/weather-condition-icon'
import { WindBarb, WaveDirectionArrow } from '@/components/forecast/direction-glyphs'
import type { ChartConfig } from '@/components/ui/chart'
import { useChartTooltip } from '@/hooks/use-chart-tooltip'
import type { WeatherHourlyCloudPoint, WeatherHourlyEntry, WeatherHourlyPrecipPoint, WeatherHourlyUVPoint, WeatherHourlyWindPoint } from '@/hooks/use-weather-forecast'
import type { WaveForecastDay } from '@/hooks/use-wave-forecast'
import type { UpperAirDay, UpperAirSample, UpperAirWindow } from '@/hooks/use-upper-air'
import type { ForecastWarnings } from '@/hooks/use-forecast-warnings'
import { useMeasuredWidth } from '@/hooks/use-measured-width'
import { buildConditionsSummary } from '@/lib/forecast-condition-summary'
import { compassPointFor } from '@/lib/format'
import { moonPhaseEmoji, moonPhaseLabel } from '@/lib/moon-phase'
import { fahrenheitToCelsius } from '@/lib/units'
import { formatDataAge, isStale } from '@/lib/staleness'

interface ForecastDay {
  dayKey: string
  date: string
  dayName: string
  condition: string
  high: number
  low: number
  windSpeed: number
  windGust: number
  windDirection: string
  windSummary: string | null
  precipitationSummary: string | null
  /** null when the provider reported no chance-of-precipitation data at all - distinct from a real 0%. */
  precipitation: number | null
  /** null when the provider reported no humidity/visibility data at all - distinct from a real 0. */
  humidityPct: number | null
  visibilityNm: number | null
  sunriseTime: string | null
  sunsetTime: string | null
  moonPhase: string | null
  hourlyWind: WeatherHourlyWindPoint[]
  hourlyPrecip: WeatherHourlyPrecipPoint[]
  hourlyUV: WeatherHourlyUVPoint[]
  hourlyCloud: WeatherHourlyCloudPoint[]
}

interface ForecastDrawerProps {
  forecast: ForecastDay[]
  hourlyToday?: WeatherHourlyEntry[]
  summary?: string | null
  loading?: boolean
  error?: string | null
  provider?: string | null
  isCached?: boolean
  updatedAt?: string | null
  ttlSeconds?: number | null
  onRetry?: () => void
  unit: 'imperial' | 'metric'
  activeForecastWarning?: ForecastWarnings | null
  waveDays?: WaveForecastDay[]
  /** 500mb outlook, empty when no upper-air provider is installed. */
  upperAirDays?: UpperAirDay[]
  /** Sub-daily 500mb trace across the whole window, empty when there is no provider. */
  upperAirSeries?: UpperAirSample[]
  /** The range the window's days are judged against, absent when there is no provider. */
  upperAirWindow?: UpperAirWindow
  /** When the upper-air provider last answered - drives the panel's own stale badge, independent of the weather and wave feeds. */
  upperAirUpdatedAt?: string | null
  upperAirTtlSeconds?: number | null
  waveSeaTemperatureF?: number | null
  waveLoading?: boolean
  waveError?: string | null
  waveProvider?: string | null
  waveIsCached?: boolean
  waveUpdatedAt?: string | null
  waveTtlSeconds?: number | null
  /**
   * Retries the wave feed alone (its own fetch, distinct from onRetry's
   * whole-forecast retry) - wired to the amber wave-error state's Retry
   * button. A day with no wave data for a non-error reason gets no Retry at
   * all, since retrying would not produce anything different.
   */
  onWaveRetry?: () => void
}

function getHourlyWeatherIcon(entry: WeatherHourlyEntry, isNight: boolean) {
  if (entry.kind === 'sunset') {
		return <Sunset size={24} className="text-gauge-primary" />
  }

  const normalized = entry.condition.toLowerCase()
  if (isNight && (normalized.includes('clear') || normalized.includes('sunny'))) {
		return <Moon size={24} className="text-gauge-secondary" />
  }

  return <WeatherConditionIcon condition={entry.condition} size={24} />
}

// Same clear-night-becomes-moon override as getHourlyWeatherIcon, but keyed
// off the cloud chart's own per-hour isDaylight flag (sourced directly from
// WeatherKit) rather than re-deriving day/night from sunrise/sunset.
function getCloudChartIcon(condition: string, isDaylight: boolean, size: number) {
  const normalized = condition.toLowerCase()
  if (!isDaylight && (normalized.includes('clear') || normalized.includes('sunny'))) {
    return <Moon size={size} className="text-gauge-secondary" />
  }
  return <WeatherConditionIcon condition={condition} size={size} />
}

// Renders a small double-headed arrow pointing in the direction the swell
// is heading (the API reports the direction it's coming from, so this is
// rotated 180 degrees to match the convention used by most swell forecasts).
/*
 * Wave statistics and thresholds from Surviving the Storm (Dashew, 1999).
 *
 * The highest wave in a sea runs 1.87 times the significant height (page
 * 243), which is why a forecast read as its headline number understates what
 * you will actually meet. The 0.8 ceiling (page 233) says wave height in feet
 * does not normally exceed 0.8 times the wind in knots; a sea past that was
 * not built by the wind you can see, which is the book's cue to look harder.
 */
const HIGHEST_WAVE_MULTIPLE = 1.87
const METRES_TO_FEET = 3.28084
const WIND_WAVE_CEILING = 0.8
// "As little as 3 or 4 degrees Fahrenheit (1 or 2 degrees Celsius)" (page 310).
const AIR_SEA_DELTA_F = 4

/*
 * Steepness band colours.
 *
 * Only the two escalating bands get an alert colour. Rolling and building
 * stay the ordinary wave colour, because most days are one of those and a
 * line that is always painted "a colour" teaches the eye to ignore it. Amber
 * and red are the app's existing alert tokens rather than new hues, so they
 * keep looking like warnings under the instrument skin.
 *
 * Colour never carries the band on its own: the ratio is written beside every
 * direction arrow and the band is named in the summary and the tooltip. In
 * the light theme both the wave and gust tokens fall under 3:1 against the
 * card, so that text is a requirement rather than a nicety.
 */
// The 500mb height trace, and the colour it takes over a day the outlook
// flagged. Red rather than amber: amber is the surface-gust series on the same
// frame, and --destructive is already this app's alert token (the wave chart
// uses it for its breaking band).
const UPPER_AIR_TRACE_STROKE = 'hsl(var(--chart-wave) / 0.95)'
const UPPER_AIR_TROUGH_STROKE = 'hsl(var(--destructive))'

const WAVE_STEEPNESS_STROKE: Record<string, string> = {
  rolling: 'hsl(var(--chart-wave) / 0.9)',
  building: 'hsl(var(--chart-wave) / 0.9)',
  steep: 'hsl(var(--chart-gust))',
  breaking: 'hsl(var(--destructive))',
}

// "1 in N", floored so a 1-in-9.8 sea reads as the steeper 1:9 rather than
// the flatter-sounding 1:10. Mirrors formatSteepnessRatio in the backend.
function formatSteepnessRatio(ratio: number | null): string | null {
  if (ratio === null || ratio <= 0) return null
  return `1:${Math.floor(1 / ratio)}`
}

// A small inline swatch used by the chart legends below, drawing an actual
// stroked <line> (or, for the precipitation bar series, a filled <rect>) in
// the series' real color/width/dasharray - replacing the old fake swatches
// that stood in an em-dash for a solid line and two hyphens for a dashed one
// without tracking the real strokeDasharray at all.
/**
 * One horizon of the forecast page.
 *
 * The page answers the same question at three ranges: what is happening on
 * deck today, what the surface forecast holds over ten days, and what the
 * upper pattern is doing over sixteen. Those are peers, so they get one shell
 * rather than the accidental hierarchy that came from each being built at a
 * different time.
 *
 * Span is the only thing that actually differs between them, and it is stated
 * in words (spanLabel) rather than drawn. A meter used to sit in the header
 * slot showing the same proportion as a bar, on the argument Surviving the
 * Storm makes for the 500mb chart: you plan on the fortnight, not on today.
 * The stale badge took that slot, because a panel that has stopped updating
 * is a louder thing to say than how long its window is. Restoring the meter
 * means finding it a second slot, not sharing this one.
 */
/**
 * Amber outline badge marking a feed that has stopped updating. Mirrors
 * ui/tile.tsx's stale badge exactly (same classes, same "Stale {age}" text,
 * same title) so the vocabulary for "this is frozen" reads the same whether
 * it is a gauge tile or a forecast panel - the one difference is the named
 * text-2xs scale step in place of tile.tsx's arbitrary 10px value, because
 * this file's own guard test ('ForecastDrawer design tokens') forbids
 * arbitrary bracketed font-size utilities and text-2xs already aliases to
 * the same 10px in the Tailwind config.
 */
function ForecastStaleBadge({ testId, staleLabel }: { testId?: string; staleLabel?: string }) {
  return (
    <span
      data-testid={testId}
      title={staleLabel ? `No update for ${staleLabel}` : 'Source has stopped updating'}
      className="ml-1 shrink-0 rounded-xs border border-amber-500/40 bg-amber-500/10 px-1.5 py-0.5 text-2xs leading-none text-amber-600 dark:text-amber-400"
    >
      Stale{staleLabel ? ` ${staleLabel}` : ''}
    </span>
  )
}

function ForecastPanel({
  title,
  spanLabel,
  intro,
  testId,
  stale = false,
  staleLabel,
  children,
}: {
  title: string
  spanLabel: string
  intro?: ReactNode
  testId?: string
  /**
   * The feed behind this panel has stopped updating. A three-day-old cached
   * forecast must not render pixel-identical to a live one, so this panel
   * gets the same loud, colour-blind-safe treatment as every stale Tile:
   * an amber badge in the header and a grayscale body - never opacity, see
   * the comment on the body wrapper below.
   */
  stale?: boolean
  /** Age of the last update, already formatted (`1h 39m`). */
  staleLabel?: string
  children: ReactNode
}) {
  return (
    <section
      data-testid={testId}
      className="rounded-2xl border border-gauge-secondary/15 bg-card"
    >
      {/* No overflow-hidden here (see the day-selector sticky fix below) - the
          section's own background still respects rounded-2xl because
          border-radius clips an element's own background/border painting
          regardless of overflow. This header's background is a CHILD
          element flush against the top edge, which border-radius does NOT
          clip for free, so it carries its own rounded-t-2xl instead.

          DESIGN.md's Flat Board Rule: the header/body split is a tonal step
          (bg-muted on bg-card) plus the border-b below, not a gradient. */}
      <div className="rounded-t-2xl border-b border-gauge-secondary/14 bg-muted/60 px-4 py-3.5">
        <div className="flex items-center justify-between gap-3">
          <h3 className="text-xs font-semibold uppercase tracking-[0.16em] text-foreground/70">{title}</h3>
          <div className="flex shrink-0 items-center gap-2">
            {stale && (
              <ForecastStaleBadge testId={testId ? `${testId}-stale-badge` : undefined} staleLabel={staleLabel} />
            )}
            <span className="whitespace-nowrap text-xs font-medium uppercase tracking-[0.1em] text-muted-foreground">
              {spanLabel}
            </span>
          </div>
        </div>
        {intro && <div className="mt-2">{intro}</div>}
      </div>
      {/* Grayscale only, never opacity - an opacity fade reads as "dim screen
          in the sun," not "this feed is dead," and this is the system's
          loudest state, not its quietest. Matches ui/tile.tsx's CardContent. */}
      <div className={stale ? 'grayscale' : undefined} data-testid={testId ? `${testId}-body` : undefined}>
        {children}
      </div>
    </section>
  )
}

function LegendSwatch({
  color,
  strokeWidth = 2,
  dasharray,
  kind = 'line',
}: {
  color: string
  strokeWidth?: number
  dasharray?: string
  kind?: 'line' | 'bar'
}) {
  return (
    <svg width="14" height="8" viewBox="0 0 14 8" className="inline-block align-middle" aria-hidden="true">
      {kind === 'bar' ? (
        <rect x="2" y="1" width="10" height="6" rx="1" fill={color} />
      ) : (
        <line x1="0" y1="4" x2="14" y2="4" stroke={color} strokeWidth={strokeWidth} strokeDasharray={dasharray} strokeLinecap="round" />
      )}
    </svg>
  )
}

// Colors a precipitation intensity bar by probability percentage. One hue
// (the --chart-precip token) at a rising opacity - the legend already reads
// "opacity = probability", so a hue shift on top of that would contradict
// what it tells the reader.
function precipBarColor(chancePct: number | null) {
  if (chancePct === null || chancePct <= 0) {
    return 'hsl(var(--chart-precip) / 0.2)'
  }
  if (chancePct < 25) {
    return 'hsl(var(--chart-precip) / 0.35)'
  }
  if (chancePct < 50) {
    return 'hsl(var(--chart-precip) / 0.55)'
  }
  if (chancePct < 75) {
    return 'hsl(var(--chart-precip) / 0.75)'
  }
  return 'hsl(var(--chart-precip) / 0.92)'
}

// Shared axis label styling, used for every value/tick/band label across the
// Wind, Wave, Precipitation and UV charts so they read consistently.
const AXIS_LABEL_FONT_SIZE = '10'
const AXIS_LABEL_COLOR = 'hsl(var(--muted-foreground))'

/*
 * THE AXIS IDIOM, one set of rules for all five charts - Cloud, Wind, Wave,
 * Upper Air here, and Tide in tide-chart.tsx. The five had drifted apart on
 * four separate dimensions, and a reader moving between them was relearning
 * the conventions on each one. Settled as:
 *
 * 1. COLOUR BY OWNERSHIP. An axis whose labels describe ONE series is drawn
 *    in that series' colour - it is what says which number belongs to which
 *    line, and is what made the upper-air swatch legend redundant. An axis
 *    serving several series stays AXIS_LABEL_COLOR, because tinting it any
 *    one series' colour would be a lie about the other two. So: Cloud left
 *    --chart-temp, Cloud right --chart-precip, Upper-air left --chart-wave,
 *    Upper-air right --chart-gust, Tide --chart-wave; Wind left stays muted
 *    (wind and gust), Wave left stays muted (total, wind-wave and swell).
 *
 *    The label takes the -label variant of that token, not the series token
 *    itself: a label is text and owes 4.5:1 against the card, where the line
 *    it names only owes the 3:1 graphics bar. On the light card all four
 *    series colours failed that as text (2.14:1 to 3.63:1). The two dark
 *    themes alias the variant straight back to the series colour, which
 *    already passes there - see the block beside --chart-uv in index.css.
 *
 * 2. UNIT PLACEMENT FOLLOWS AXIS COUNT. On a SINGLE-axis chart the unit lives
 *    in the <h4> header ("Wind (kts)", "Wave (m)", "Tide (m)") and every tick
 *    is a bare number - a long unit suffix on the topmost tick runs past the
 *    plot's left gutter at fontSize 10. On a DUAL-axis chart one header cannot
 *    disambiguate two scales, so each axis carries its own unit, ONCE, on its
 *    extreme (top) tick: "22°C" over "20", "1.0mm" over "0".
 *
 * 3. EVERY LABEL IS POSITIONED THROUGH axisTickLabelY, over the same *YFor
 *    mapping its own series is drawn with. Hardcoded pixel ys survive the
 *    scale change that invalidates them and stay right only by coincidence.
 *
 * 4. TICK DENSITY FOLLOWS HOW THE AXIS IS READ. A ladder where the value is
 *    read quantitatively - Wind, Wave, and the upper-air gust axis, where "is
 *    that 20 or 35 knots" is the question. Extremes only where the range is
 *    read qualitatively and the shape carries the meaning - Cloud temperature,
 *    Cloud precipitation, and upper-air height, where the trough shape is the
 *    point and the numbers only bound it.
 */

// recharts' <XAxis> reserves its own `height` (default 30) via its internal
// offset calculation IN ADDITION TO whatever `margin.bottom` a <ComposedChart>
// is given, for any axis that isn't `hide`-den (calculateOffset in
// generateCategoricalChart.js only skips this for hidden/mirrored axes) - so
// the chart's true rendered plot-rectangle bottom edge sits this many pixels
// higher than `margin.bottom` alone would suggest. Every hourly chart below
// needs its visible tick-label XAxis to actually render ticks (which recharts
// refuses to do at all when given `height={0}` - CartesianAxis.render() bails
// out early), so each chart's `margin.bottom` is deliberately reduced by this
// same amount to compensate, keeping the ACTUAL plot rectangle aligned with
// the existing yFor-family pixel math (and, in turn, with the tooltip marker
// and every ReferenceDot, which are positioned by that same pixel math, not
// by recharts' own scale).
const RECHARTS_XAXIS_HEIGHT = 30

// Shared top/bottom pixel bounds for every hourly chart's plot rectangle -
// Wind, Wave, Precipitation and Cloud & Temperature all use the identical
// band before each chart's own per-value Y scaling is applied. Exported
// (test-only use) so the glyph-band clipping regression tests below assert
// against the real source of truth rather than a copy that can drift out of
// sync with it.
export const HOURLY_CHART_TOP = 50
const HOURLY_CHART_BOTTOM = 215

// 6-hour-block ticks (12AM/6AM/12PM/6PM), used by every hourly chart below
// instead of spacing ticks dynamically by count. Hoisted to module scope
// (rather than declared inside ForecastDrawer) since it's a pure function of
// its argument, which lets call sites safely memoize on it.
function hourTicksFor<T extends { hourOfDay: number }>(hourly: T[]) {
  return hourly.map((entry, idx) => ({ entry, idx })).filter(({ entry }) => entry.hourOfDay % 6 === 0)
}

// Recharts' XAxis tickFormatter only receives the raw hourOfDay value, not
// the hourly entry - this looks the real API-provided `.label` text up by
// hour rather than re-deriving a 12-hour-clock string, so the tick text is
// guaranteed to match whatever the backend sends. Shared by every hourly
// chart below.
function buildLabelByHour<T extends { hourOfDay: number; label: string }>(hourly: T[]): Map<number, string> {
  return new Map(hourly.map((entry) => [entry.hourOfDay, entry.label]))
}

// Computes a y position for a left-axis tick label, nudging the top and
// bottom ticks inward so the text isn't clipped by the chart edges.
function axisTickLabelY(yFor: (value: number) => number, value: number, top: number, bottom: number) {
  const y = yFor(value)
  if (Math.abs(y - top) < 0.5) return top + 8
  if (Math.abs(y - bottom) < 0.5) return bottom - 2
  return y + 3
}

/*
 * A centred moving average over the upper-air height series, three samples
 * wide. The provider's 6-hourly sampling carries a tight ripple - the diurnal
 * atmospheric tide plus model sample-interval chatter - that holds no synoptic
 * information and competes visually with the trough shape the panel exists to
 * show. Three samples is 18 hours, wide enough to flatten that and narrow
 * enough to leave a two-day fall where it is.
 *
 * The window shrinks at the two ends rather than dropping the points, so the
 * trace still spans the full axis instead of leaving the frame's first and
 * last day blank.
 *
 * This is DISPLAY ONLY. ADR 0071 section 5 requires the drawn band and the
 * marked days to agree, and both of those come from the backend's unsmoothed
 * numbers - so nothing here may feed a threshold, the quintile band, the
 * trough spans, or the scrub tooltip's reported height.
 */
export function smoothUpperAirHeights(values: number[]): number[] {
  return values.map((_, idx) => {
    const from = Math.max(0, idx - 1)
    const to = Math.min(values.length - 1, idx + 1)
    let total = 0
    for (let i = from; i <= to; i += 1) total += values[i]
    return total / (to - from + 1)
  })
}

// Converts a 0-360 bearing to a 16-point compass label (just the direction,
// e.g. "ESE"), for the wave/swell direction shown in the scrub tooltip (wind
// already gets this as a string straight from the API). Returns '' for the
// negative sentinel meaning "no data". Shares its bucketing lookup with
// format.ts's formatHeading via compassPointFor, but keeps this shorter
// output format (no degree number) — do not replace this with formatHeading,
// that would change the displayed tooltip text.
function compassLabel(directionDeg: number): string {
  if (directionDeg < 0) return ''
  return compassPointFor(directionDeg % 360)
}

// WHO UV Index risk bands, matching the thresholds already used for the UV
// chart's gradient stops.
function uvRiskLabel(value: number): string {
  if (value >= 11) return 'Extreme'
  if (value >= 8) return 'Very High'
  if (value >= 6) return 'High'
  if (value >= 3) return 'Moderate'
  return 'Low'
}


export function formatRefreshAge(value: string | null | undefined, nowMs: number) {
  if (!value) {
    return 'Unknown'
  }

  const parsed = new Date(value)
  if (Number.isNaN(parsed.getTime())) {
    return 'Unknown'
  }

  const elapsedMs = Math.max(0, nowMs - parsed.getTime())
  const elapsedMinutes = Math.floor(elapsedMs / 60000)

  if (elapsedMinutes <= 0) {
    return 'just now'
  }

  if (elapsedMinutes === 1) {
    return '1 min ago'
  }

  if (elapsedMinutes < 60) {
    return `${elapsedMinutes} mins ago`
  }

  const elapsedHours = Math.floor(elapsedMinutes / 60)
  if (elapsedHours === 1) {
    return '1 hour ago'
  }

  return `${elapsedHours} hours ago`
}

// Age of `updatedAt` in seconds, off the browser clock - the same math
// formatRefreshAge already uses, so the freshness line and the stale badge
// never disagree about how old a forecast is. Null when there's no
// timestamp to measure, which isStale below treats as "unknown," not "stale."
function ageSecondsFromUpdatedAt(value: string | null | undefined, nowMs: number): number | null {
  if (!value) return null
  const parsed = new Date(value)
  if (Number.isNaN(parsed.getTime())) return null
  return Math.max(0, (nowMs - parsed.getTime()) / 1000)
}

// Whether a forecast feed has gone stale. staleness.ts's 120s default is a
// SignalK-refresh assumption (a boat instrument that publishes every few
// seconds); a weather forecast's TTL runs from 15 minutes (WeatherKit) to 12
// hours (BOM), so this derives the threshold from the feed's own TTL instead
// - 3x it, so an on-time refresh that lands a poll cycle late doesn't trip
// it. A missing ttlSeconds carries no cache-lifetime signal at all, so per
// isStale's own reasoning that reads as "unknown," not "stale" - never
// falling back to the SignalK default here would misreport a feed we simply
// have no TTL for as frozen forever.
function isForecastStale(ageSeconds: number | null, ttlSeconds: number | null | undefined): boolean {
  if (ttlSeconds === null || ttlSeconds === undefined) return false
  return isStale(ageSeconds, ttlSeconds * 3)
}

// Fallback used to compute a "selected day" when the forecast list is empty
// (loading/error/no-data states). These states render one of the early
// returns below instead of the chart JSX, so the values derived from this
// are never displayed - it only needs to be safe to compute, not accurate.
const EMPTY_DAY: ForecastDay = {
  dayKey: '',
  date: '',
  dayName: '',
  condition: '',
  high: 0,
  low: 0,
  windSpeed: 0,
  windGust: 0,
  windDirection: '',
  windSummary: null,
  precipitationSummary: null,
  precipitation: null,
  humidityPct: null,
  visibilityNm: null,
  sunriseTime: null,
  sunsetTime: null,
  moonPhase: null,
  hourlyWind: [],
  hourlyPrecip: [],
  hourlyUV: [],
  hourlyCloud: [],
}

export function ForecastDrawer({
  forecast,
  hourlyToday = [],
  summary = null,
  loading = false,
  error = null,
  provider = null,
  isCached = false,
  updatedAt = null,
  ttlSeconds = null,
  onRetry,
  unit,
  activeForecastWarning = null,
  waveDays = [],
  upperAirDays = [],
  upperAirSeries = [],
  upperAirWindow,
  upperAirUpdatedAt = null,
  upperAirTtlSeconds = null,
  waveSeaTemperatureF = null,
  waveLoading = false,
  waveError = null,
  waveProvider = null,
  waveIsCached = false,
  waveUpdatedAt = null,
  waveTtlSeconds = null,
  onWaveRetry,
}: ForecastDrawerProps) {
  const hasForecast = Boolean(forecast && forecast.length > 0)
  const [selectedDayIndex, setSelectedDayIndex] = useState(0)
  const windAreaGradientId = useId()
  const waveAreaGradientId = useId()
  const waveSteepnessGradientId = useId()
  const tempAreaGradientId = useId()
  const uvAreaGradientId = useId()
  const upperAirGustGradientId = useId()
  const upperAirTroughGradientId = useId()
  const detailsCardRef = useRef<HTMLDivElement>(null)
  const dayTabsRowRef = useRef<HTMLDivElement>(null)

  const tempUnit = unit === 'metric' ? '°C' : '°F'
  const windUnit = 'kts'
  const displayTemp = (tempF: number) => (unit === 'metric' ? fahrenheitToCelsius(tempF) : tempF)
  const days = forecast.slice(0, 10)
  // Matches spanLabel="Next 24 hours" below - the strip used to cap at 12,
  // which read as a full day's plan while actually stopping around 9PM for
  // anyone reading it at midday: daylight hours only, no overnight, on the
  // one panel a skipper uses to judge whether tonight is safe.
  const hourlyEntries = hourlyToday.slice(0, 24)

  useEffect(() => {
    setSelectedDayIndex((prev) => (prev < days.length ? prev : 0))
  }, [days.length])

  // Bring the daily summary back into view when the user picks a day, so
  // selecting a new one doesn't leave the details card scrolled down to
  // wherever the previous day's chart happened to be. Triggered directly
  // from the click handler (not a `selectedDayIndex` effect) so it only
  // fires on an actual selection, not on mount or on the index-clamping
  // effect above.
  const selectDay = (idx: number) => {
    setSelectedDayIndex(idx)
    // The day-tabs row is sticky at the top of the scroll container, so
    // without this the details card would scroll to right behind it instead
    // of just below it.
    if (detailsCardRef.current && dayTabsRowRef.current) {
      detailsCardRef.current.style.scrollMarginTop = `${dayTabsRowRef.current.offsetHeight}px`
    }
    detailsCardRef.current?.scrollIntoView({ behavior: 'smooth', block: 'start' })
  }

  const selectedDay = days[selectedDayIndex] ?? days[0] ?? EMPTY_DAY
  const selectedWaveDay = waveDays.find((d) => d.dayKey === selectedDay.dayKey) ?? null
  const selectedUpperAir = upperAirDays.find((d) => d.dayKey === selectedDay.dayKey)?.outlook ?? null
  const upperAirFlaggedDayKeys = useMemo(
    () => new Set(upperAirDays.filter((d) => d.outlook.present && d.outlook.troughSupport).map((d) => d.dayKey)),
    [upperAirDays],
  )


  // A wave-provider outage must read as visibly different from "this
  // location just has no wave data" - if the whole wave fetch failed AND we
  // have no matching day for the selected forecast day, that's the outage
  // case (rendered via ChartUnavailableMessage further below); an empty
  // hourlyWave array with no error is the legitimate "no data" case.
  const waveUnavailableDueToError = Boolean(waveError) && selectedWaveDay === null

  // The forecast panel's defining risk: a frozen feed has to look visibly
  // different from a live one, not pixel-identical. The weather feed drives
  // both the Today and 10-Day panels; the wave feed carries its own TTL and
  // can go stale on its own schedule, so it gets a second, independent check.
  const weatherAgeSeconds = ageSecondsFromUpdatedAt(updatedAt, Date.now())
  const weatherStale = isForecastStale(weatherAgeSeconds, ttlSeconds)
  const weatherStaleLabel = weatherStale ? formatDataAge(weatherAgeSeconds) : undefined

  const waveAgeSeconds = ageSecondsFromUpdatedAt(waveUpdatedAt, Date.now())
  const waveStale = isForecastStale(waveAgeSeconds, waveTtlSeconds)
  const waveStaleLabel = waveStale ? formatDataAge(waveAgeSeconds) : undefined

  // Upper air runs on the model cadence (four times a day) rather than the
  // weather/wave poll interval, so it carries its own TTL and its own
  // independent stale check, same shape as the wave one above.
  const upperAirAgeSeconds = ageSecondsFromUpdatedAt(upperAirUpdatedAt, Date.now())
  const upperAirStale = isForecastStale(upperAirAgeSeconds, upperAirTtlSeconds)
  const upperAirStaleLabel = upperAirStale ? formatDataAge(upperAirAgeSeconds) : undefined

  const precipitationPct = selectedDay.precipitation
  const humidityPct = selectedDay.humidityPct
  const visibilityNm = selectedDay.visibilityNm

  const windHourly = selectedDay.hourlyWind ?? []
  const waveHourly = selectedWaveDay?.hourlyWave ?? []
  const precipHourly = selectedDay.hourlyPrecip ?? []
  const uvHourly = selectedDay.hourlyUV ?? []
  const cloudHourly = selectedDay.hourlyCloud ?? []

  // All the hourly chart cards below share the same rounded-md border
  // bg-card/70 p-2 styling and the same parent width, so measuring just one
  // of them (the Cloud & Temperature card, which always renders regardless
  // of data availability) and reusing it for the rest is equivalent to
  // measuring each independently. p-2 (16px) + border (2px) is the fixed
  // inset between the card's outer width and its actual content/SVG width.
  const [chartCardRef, chartCardWidth] = useMeasuredWidth()
  const forecastChartWidth = chartCardWidth > 18 ? chartCardWidth - 18 : 1000

  const hourlyChartLeft = 30
  const hourlyChartRight = forecastChartWidth - 20
  const hourlyChartWidth = hourlyChartRight - hourlyChartLeft

  // Matches the XAxis's own domain={[0, 23]} continuous number scale exactly
  // (not a band scale), so a series entry renders at the same pixel recharts
  // itself computes for that entry's real hourOfDay - unlike the old
  // index/count-based formula, this stays correct even if the hourly array is
  // ever shorter or non-uniformly spaced than a full 24-hour sequence.
  const hourlyXForHour = (hourOfDay: number) => hourlyChartLeft + (hourOfDay / 23) * hourlyChartWidth

  // Shared margin formula for every hourly chart below: same left/right/top,
  // and a bottom derived from the chart's own SVG viewBox height (265, shared
  // by all four stacked charts) so recharts' plot rectangle lands exactly
  // where the yFor-family pixel math and tooltip overlay already expect it.
  function hourlyChartMargin(viewboxHeight: number) {
    return {
      left: hourlyChartLeft,
      right: forecastChartWidth - hourlyChartRight,
      top: HOURLY_CHART_TOP,
      bottom: viewboxHeight - HOURLY_CHART_BOTTOM - RECHARTS_XAXIS_HEIGHT,
    }
  }

  // --- The 500mb trace across the whole window ---
  //
  // A per-day figure like "500mb 5899 m" cannot be correlated against
  // anything, and Surviving the Storm's method is not a per-day read: it is
  // reading the shape across successive charts (p62). So the window gets drawn
  // as a window, at the sub-daily resolution the provider already returns.
  //
  // Surface gust rides on the same x-axis deliberately. The book's claim is
  // causal and lagged - the upper trough is what vents a surface low - and the
  // only way to show a lag is to put both series on one time axis. The surface
  // forecast is shorter than the upper-air one, so its later samples are null
  // and the area simply stops rather than being extrapolated.
  const surfaceGustByDayKey = useMemo(
    () => new Map(days.map((day) => [day.dayKey, day.windGust])),
    [days],
  )
  const upperAirDayLabels = useMemo(
    () => new Map(upperAirDays.map((day) => [day.dayKey, `${day.dayName.slice(0, 3)} ${day.date.replace(/^[A-Za-z]+ /, '')}`])),
    [upperAirDays],
  )

  // --- What the ten days add up to ---
  //
  // The strip shows ten cards and four charts and never says which day is the
  // window. This resolves that, and only from what upperAirFlaggedDayKeys
  // already holds: no new claim is made here.
  //
  // Four outcomes, and the difference between the first and the last is the
  // point. No provider installed (ADR 0071 calls that a normal
  // configuration, and the Upper Air panel is absent too) is silence, because
  // nothing was looked at. Checked and found clear is a sentence, because
  // something was. Days the provider had no outlook for count as the former:
  // "no upper support" off an absent outlook would be a reassurance nobody
  // measured.
  const upperAirIntro = useMemo(() => {
    if (!upperAirDays.some((day) => day.outlook.present)) return null

    const visibleDayKeys = new Set(days.map((day) => day.dayKey))
    const namedDays = days
      .filter((day) => upperAirFlaggedDayKeys.has(day.dayKey))
      .map((day) => upperAirDayLabels.get(day.dayKey) ?? `${day.dayName.slice(0, 3)} ${day.date.replace(/^[A-Za-z]+ /, '')}`)
    const hasFlaggedBeyondStrip = [...upperAirFlaggedDayKeys].some((dayKey) => !visibleDayKeys.has(dayKey))

    // The window length is whatever the provider returned, not a constant.
    // The panel header beside this already derives its span the same way, and
    // a hardcoded "sixteen" goes quietly wrong the first time it is fourteen.
    const windowDays = upperAirDays.length

    let text: string
    if (namedDays.length > 0) {
      const named =
        namedDays.length === 1
          ? namedDays[0]
          : `${namedDays.slice(0, -1).join(', ')} and ${namedDays[namedDays.length - 1]}`
      text = hasFlaggedBeyondStrip
        ? `Upper air supports a surface low developing on ${named}, and again later in the ${windowDays} day trace below.`
        : `Upper air supports a surface low developing on ${named}.`
    } else if (hasFlaggedBeyondStrip) {
      // Naming a day the reader cannot select in this strip is a dead
      // reference, so the trace gets pointed at instead.
      text = `Upper air supports a surface low developing later in the ${windowDays} day trace below.`
    } else {
      text = `No upper support for a surface low in the next ${windowDays} days.`
    }

    return (
      <p data-testid="forecast-upper-air-intro" className="pr-2 text-base font-medium leading-relaxed text-foreground/90">
        {text}
      </p>
    )
  }, [days, upperAirDays, upperAirDayLabels, upperAirFlaggedDayKeys])

  // --- What the ten days add up to, in plain conditions ---
  //
  // The strip shows ten cards, one condition each, and never says what they
  // add up to as a run - a reader has to scan all ten to notice "it's wet
  // today and tomorrow, then clear." buildConditionsSummary does that
  // scanning once, off days[].condition, and is unit-tested on its own in
  // lib/forecast-condition-summary.ts; this just wires the sentence in as
  // this panel's intro. Trough days ride along too - the same
  // upperAirFlaggedDayKeys/upperAirDayLabels the Upper Air intro above uses,
  // just folded into this sentence instead of a second one.
  const extendedIntro = useMemo(() => {
    const summaryDays = days.map((day) => ({
      condition: day.condition,
      dayName: day.dayName,
      label: upperAirDayLabels.get(day.dayKey) ?? `${day.dayName.slice(0, 3)} ${day.date.replace(/^[A-Za-z]+ /, '')}`,
      trough: upperAirFlaggedDayKeys.has(day.dayKey),
    }))
    const summary = buildConditionsSummary(summaryDays)
    if (!summary) return null

    return (
      <p data-testid="forecast-extended-intro" className="pr-2 text-base font-medium leading-relaxed text-foreground/90">
        {summary}
      </p>
    )
  }, [days, upperAirDayLabels, upperAirFlaggedDayKeys])

  const upperAirChartData = useMemo(() => {
    // Carried alongside the raw height rather than replacing it: the trace is
    // drawn from the smoothed value and every number the reader is shown -
    // the scrub tooltip especially - still comes off the raw sample.
    const smoothed = smoothUpperAirHeights(upperAirSeries.map((sample) => sample.height500M))
    return upperAirSeries.map((sample, idx) => ({
      idx,
      dayKey: sample.dayKey,
      localHour: sample.localHour,
      height500M: sample.height500M,
      height500Smoothed: smoothed[idx],
      thicknessM: sample.thicknessM,
      wind500Kts: sample.wind500Kts,
      surfaceGustKts: surfaceGustByDayKey.get(sample.dayKey) ?? null,
    }))
  }, [upperAirSeries, surfaceGustByDayKey])

  const hasUpperAirTrace = upperAirChartData.length > 1

  // The height axis is scaled to the window itself, not to a fixed range. At
  // this vessel 500mb heights run around 5900m and in the Southern Ocean they
  // are hundreds of metres lower; a fixed domain would flatten one of them into
  // a straight line. Padding keeps the trace off the frame.
  const upperAirHeights = upperAirChartData.map((d) => d.height500M)
  const upperAirRawLow = upperAirHeights.length > 0 ? Math.min(...upperAirHeights) : 0
  const upperAirRawHigh = upperAirHeights.length > 0 ? Math.max(...upperAirHeights) : 0
  const upperAirPad = Math.max(5, (upperAirRawHigh - upperAirRawLow) * 0.15)
  const upperAirMin = upperAirRawLow - upperAirPad
  const upperAirMax = upperAirRawHigh + upperAirPad

  const upperAirGusts = upperAirChartData.map((d) => d.surfaceGustKts ?? 0)
  const upperAirGustDataMax = upperAirGusts.length > 0 ? Math.max(...upperAirGusts) : 0
  // Same rule as the wind and wave frames. A fixed 30kt floor draws a 12kt
  // window as a flat line along the bottom of a frame that has nothing to do
  // with it, which is the whole reason the other two charts stopped using one.
  //
  // Math.max(step, ...) matters: a window whose surface-gust series is null
  // throughout - the designed state for the tail of a 16-day window, and for
  // one that does not overlap the 10-day surface forecast at all - would
  // otherwise give 0 and make upperAirGustYFor divide by zero.
  const upperAirGustTickStep = upperAirGustDataMax <= 20 ? 5 : 10
  const upperAirGustMax = Math.max(
    upperAirGustTickStep,
    Math.ceil(upperAirGustDataMax / upperAirGustTickStep) * upperAirGustTickStep,
  )

  const upperAirChartTop = HOURLY_CHART_TOP
  const upperAirChartBottom = HOURLY_CHART_BOTTOM
  const upperAirHeightYFor = (value: number) =>
    upperAirMax === upperAirMin
      ? upperAirChartBottom
      : upperAirChartTop + (1 - (value - upperAirMin) / (upperAirMax - upperAirMin)) * (upperAirChartBottom - upperAirChartTop)

  /*
   * What the height axis STATES is the window, not the frame. ADR 0071
   * section 2: each day is scored against the rest of the window and flagged
   * when it lands in the lowest quintile of it, so the window's own low and
   * high are the two numbers a reader needs to judge any day on the trace.
   * upperAirMin/upperAirMax are the series extremes plus 15% padding - an
   * artefact of drawing the trace clear of the frame - and they sit close
   * enough to the real window to be misread as it.
   *
   * The frame is unchanged; only the labels move, positioned by
   * upperAirHeightYFor so they land at the heights they name. The window is
   * derived from the same fetch the series is, so its bounds sit inside the
   * padded frame and both labels stay visible.
   *
   * A provider can return a series with no window at all (the contract's
   * window is optional and lands absent rather than zeroed), and there is
   * nothing to state then, so the axis falls back to the frame it draws.
   */
  const upperAirAxisHighM = upperAirWindow?.present ? upperAirWindow.highM : upperAirMax
  const upperAirAxisLowM = upperAirWindow?.present ? upperAirWindow.lowM : upperAirMin

  // The gust axis is drawn now rather than hidden. A hidden axis forced its
  // full scale into the legend, which is a magnitude written a long way from
  // the line it belongs to.
  const upperAirGustYFor = (value: number) =>
    upperAirGustMax === 0
      ? upperAirChartBottom
      : upperAirChartTop + (1 - value / upperAirGustMax) * (upperAirChartBottom - upperAirChartTop)
  const upperAirGustTicks = Array.from(
    { length: upperAirGustMax / upperAirGustTickStep + 1 },
    (_, i) => i * upperAirGustTickStep,
  )
  const upperAirXFor = (idx: number) =>
    upperAirChartData.length <= 1
      ? hourlyChartLeft
      : hourlyChartLeft + (idx / (upperAirChartData.length - 1)) * upperAirPlotWidth

  // One tick per local day, thinned once the window gets long enough that
  // sixteen labels would collide.
  const upperAirDayStarts = useMemo(() => {
    const starts: Array<{ idx: number; dayKey: string }> = []
    upperAirChartData.forEach((point, idx) => {
      if (idx === 0 || point.dayKey !== upperAirChartData[idx - 1].dayKey) {
        starts.push({ idx, dayKey: point.dayKey })
      }
    })
    return starts
  }, [upperAirChartData])

  const upperAirTickStride = upperAirDayStarts.length > 9 ? 2 : 1
  const upperAirTicks = upperAirDayStarts.filter((_, i) => i % upperAirTickStride === 0).map((d) => d.idx)
  const upperAirTickLabels = useMemo(
    () => new Map(upperAirDayStarts.map((d) => [d.idx, upperAirDayLabels.get(d.dayKey) ?? d.dayKey.slice(5)])),
    [upperAirDayStarts, upperAirDayLabels],
  )

  // A flagged day is drawn as the extent of time it actually covers, so the
  // approach, the bottom and the recovery read as a shape. The card marker
  // says which day; this says how long.
  const upperAirTroughSpans = useMemo(
    () =>
      upperAirDayStarts
        .map((start, i) => ({
          dayKey: start.dayKey,
          from: start.idx,
          // A band runs to where the next day begins rather than to this
          // day's own last sample. Two flagged days in a row are one stretch
          // of weather, and stopping at the last sample would leave a gap
          // between them that reads as the trough letting up in the middle.
          to: upperAirDayStarts[i + 1]?.idx ?? upperAirChartData.length - 1,
        }))
        .filter((span) => upperAirFlaggedDayKeys.has(span.dayKey)),
    [upperAirDayStarts, upperAirFlaggedDayKeys, upperAirChartData.length],
  )

  /*
   * Paired hard-edged gradient stops across the plot, the same shape as
   * waveSteepnessStops: each span emits its colour at its own offset and again
   * at the next, so the transition is a step rather than a blend. A day either
   * has upper support for a surface low or it does not, and a smeared gradient
   * would invent a state between them.
   *
   * This replaced full-height bands behind the plot. A band is a claim about
   * the whole column of the chart, including the surface-gust trace it sits
   * behind, when the claim is only about the height line.
   *
   * The flagged colour is --destructive, not --chart-gust: amber is already
   * the surface-gust series on this same frame, so an amber height line would
   * read as the other series.
   *
   * Offsets are fractions of the plot rectangle (x1=hourlyChartLeft to
   * x2=upperAirChartRight in user space), and upperAirXFor is linear in idx
   * across exactly that span, so an index converts straight to idx/(n-1).
   */
  const upperAirTroughStops = useMemo(() => {
    if (upperAirTroughSpans.length === 0) return []
    const lastIdx = Math.max(1, upperAirChartData.length - 1)
    const fractionOf = (idx: number) => Math.min(1, Math.max(0, idx / lastIdx))

    const stops: Array<{ key: string; offset: number; colour: string; band: string }> = []
    let cursor = 0
    upperAirTroughSpans.forEach((span, i) => {
      const start = fractionOf(span.from)
      const end = fractionOf(span.to)
      if (start > cursor) {
        stops.push({ key: `base-${i}-a`, offset: cursor, colour: UPPER_AIR_TRACE_STROKE, band: 'base' })
        stops.push({ key: `base-${i}-b`, offset: start, colour: UPPER_AIR_TRACE_STROKE, band: 'base' })
      }
      stops.push({ key: `${span.dayKey}-a`, offset: start, colour: UPPER_AIR_TROUGH_STROKE, band: 'trough' })
      stops.push({ key: `${span.dayKey}-b`, offset: end, colour: UPPER_AIR_TROUGH_STROKE, band: 'trough' })
      cursor = end
    })
    if (cursor < 1) {
      stops.push({ key: 'base-tail-a', offset: cursor, colour: UPPER_AIR_TRACE_STROKE, band: 'base' })
      stops.push({ key: 'base-tail-b', offset: 1, colour: UPPER_AIR_TRACE_STROKE, band: 'base' })
    }
    return stops
  }, [upperAirTroughSpans, upperAirChartData.length])

  // The per-day charts sit two containers deep inside the extended panel; this
  // one sits directly in its own panel body, so it is wider and has to measure
  // itself rather than borrow the day charts' width.
  //
  // The ref goes on an element with no padding or border of its own, so the
  // width needs no inset arithmetic. useMeasuredWidth settles on the observed
  // contentRect, and subtracting a padding that measurement has already
  // excluded is how this first came out 20px narrow than its own panel.
  const [upperAirCardRef, upperAirCardWidth] = useMeasuredWidth()
  const upperAirChartWidth = upperAirCardWidth > 0 ? upperAirCardWidth : forecastChartWidth
  const upperAirChartRight = upperAirChartWidth - 20
  const upperAirPlotWidth = upperAirChartRight - hourlyChartLeft
  const upperAirChartMargin = {
    left: hourlyChartLeft,
    right: upperAirChartWidth - upperAirChartRight,
    top: HOURLY_CHART_TOP,
    bottom: 265 - HOURLY_CHART_BOTTOM - RECHARTS_XAXIS_HEIGHT,
  }

  const upperAirTooltip = useChartTooltip(upperAirChartData.length, upperAirChartData.length, hourlyChartLeft, upperAirChartRight)
  const upperAirTooltipEntry = upperAirTooltip.activeIndex === null ? null : upperAirChartData[upperAirTooltip.activeIndex] ?? null

  /*
   * The wind frame is derived from the WHOLE visible window, not the selected
   * day, and it is deliberately constant as the reader moves between day
   * tabs. A rough day late in the window raises the frame for every earlier
   * day, so the calm ones read as calm relative to what is coming - which is
   * the comparison a passage plan actually turns on. Do not make this
   * per-day: that is the bug this replaced, where a fixed 0-30kt floor
   * rendered a 6kt day and a 16kt day as near-identical flat lines.
   */
  const windWindowMax = useMemo(
    () =>
      days.reduce(
        (max, day) =>
          (day.hourlyWind ?? []).reduce(
            (dayMax, entry) => Math.max(dayMax, entry.windSpeed, entry.windGust),
            max,
          ),
        0,
      ),
    [days],
  )
  const windTickStep = windWindowMax <= 20 ? 5 : 10
  // Math.max(step, ...) matters: a window with no wind data at all would
  // otherwise give windMax = 0 and make windYFor divide by zero.
  const windMax = Math.max(windTickStep, Math.ceil(windWindowMax / windTickStep) * windTickStep)
  const windChartTop = HOURLY_CHART_TOP
  const windChartBottom = HOURLY_CHART_BOTTOM
  const windYFor = (value: number) => windChartTop + (1 - value / windMax) * (windChartBottom - windChartTop)
  const windAxisTicks = Array.from({ length: windMax / windTickStep + 1 }, (_, idx) => idx * windTickStep)
  const windHourTicks = useMemo(() => hourTicksFor(windHourly), [windHourly])
  const windChartData = useMemo(
    () =>
      windHourly.map((entry) => ({
        hourOfDay: entry.hourOfDay,
        windSpeed: Math.max(0, entry.windSpeed),
        windGust: Math.max(0, entry.windGust),
      })),
    [windHourly],
  )
  const windLabelByHour = useMemo(() => buildLabelByHour(windHourly), [windHourly])
  const windChartMargin = hourlyChartMargin(265)

  /*
   * Same framing policy as wind, over the wave window: constant across day
   * tabs so a 0.6m day reads as calm against the 3m one waiting on Thursday.
   * waveDays can be shorter than `days` or empty - the reduce handles both,
   * and the step floor keeps waveYFor out of a divide by zero.
   */
  const waveWindowMax = useMemo(
    () =>
      waveDays.reduce(
        (max, day) =>
          (day.hourlyWave ?? []).reduce(
            (dayMax, entry) => Math.max(dayMax, entry.waveHeightM, entry.windWaveHeightM, entry.swellWaveHeightM),
            max,
          ),
        0,
      ),
    [waveDays],
  )
  const waveTickStep = waveWindowMax <= 2 ? 0.5 : 1
  // Counted in whole steps rather than dividing waveMax back out, so a
  // half-metre step can't lose a tick to floating-point drift.
  const waveTickCount = Math.max(1, Math.ceil(waveWindowMax / waveTickStep))
  const waveMax = waveTickCount * waveTickStep
  const waveChartTop = HOURLY_CHART_TOP
  const waveChartBottom = HOURLY_CHART_BOTTOM
  const waveYFor = (value: number) => waveChartTop + (1 - value / waveMax) * (waveChartBottom - waveChartTop)
  const waveAxisTicks = Array.from({ length: waveTickCount + 1 }, (_, idx) => idx * waveTickStep)
  const waveHourTicks = useMemo(() => hourTicksFor(waveHourly), [waveHourly])
  const waveChartData = useMemo(
    () =>
      waveHourly.map((entry) => ({
        hourOfDay: entry.hourOfDay,
        waveHeightM: Math.max(0, entry.waveHeightM),
        windWaveHeightM: Math.max(0, entry.windWaveHeightM),
        swellWaveHeightM: Math.max(0, entry.swellWaveHeightM),
      })),
    [waveHourly],
  )
  /*
   * Hard-edged gradient stops, one band per hour, in user space across the
   * plot so the colour change lands on the hour it describes rather than on
   * a proportion of the drawn path. Each band emits its colour at its own
   * hour and again at the next, which makes the transition a step instead of
   * a blend - a wave either breaks or it does not, and a smeared gradient
   * would invent intermediate states the data does not have.
   */
  const waveSteepnessStops = useMemo(() => {
    const banded = waveHourly.filter((entry) => entry.steepnessBand !== null)
    return banded.flatMap((entry, idx) => {
      const colour = WAVE_STEEPNESS_STROKE[entry.steepnessBand as string] ?? WAVE_STEEPNESS_STROKE.rolling
      const start = Math.min(1, Math.max(0, entry.hourOfDay / 23))
      const next = banded[idx + 1]
      const end = next === undefined ? 1 : Math.min(1, Math.max(0, next.hourOfDay / 23))
      return [
        { key: `${idx}-a`, offset: start, colour, band: entry.steepnessBand as string },
        { key: `${idx}-b`, offset: end, colour, band: entry.steepnessBand as string },
      ]
    })
  }, [waveHourly])
  const waveLabelByHour = useMemo(() => buildLabelByHour(waveHourly), [waveHourly])

  const wavePeakHeightM = useMemo(
    () => waveHourly.reduce((max, entry) => Math.max(max, entry.waveHeightM), 0),
    [waveHourly],
  )

  /*
   * The warning signs for the selected day, in the words a watchkeeper would
   * use rather than the field names.
   *
   * Three come from the backend, which sees the whole hourly series. The last
   * two are joined here because they need the weather forecast as well as the
   * wave one, and those arrive from separate endpoints already keyed by day.
   */
  const waveIndicatorMessages = useMemo(() => {
    const messages: string[] = []
    const flags = selectedWaveDay?.indicators

    if (flags?.waveFront) messages.push('Sea building fast, 3m or more inside three hours')
    if (flags?.rapidBuild) messages.push('Height and period both up by half in an hour, a danger signal')
    if (flags?.periodStep) messages.push('Period lengthening sharply, often ahead of a wave front')
    if (flags?.crossSea) messages.push('Crossing seas, swell and wind wave more than 60 degrees apart')

    const peakWindKts = selectedDay?.hourlyWind?.reduce((max, entry) => Math.max(max, entry.windSpeed), 0) ?? 0
    if (wavePeakHeightM > 0 && peakWindKts > 0) {
      if (wavePeakHeightM * METRES_TO_FEET > WIND_WAVE_CEILING * peakWindKts) {
        messages.push('Seas outrunning the wind that could have built them')
      }
    }

    // Cold air over warmer water is the direction that matters: the water is
    // the energy source. Warm air over cold water is not the same event.
    if (waveSeaTemperatureF !== null && typeof selectedDay?.low === 'number') {
      if (waveSeaTemperatureF - selectedDay.low >= AIR_SEA_DELTA_F) {
        messages.push('Cold air over warmer water, which feeds gusty, unsettled weather')
      }
    }

    return messages
  }, [selectedWaveDay, selectedDay, wavePeakHeightM, waveSeaTemperatureF])
  const waveChartMargin = hourlyChartMargin(265)

  const precipIntensities = precipHourly.map((entry) => Math.max(0, entry.precipIntensityMm))
  const precipMax = Math.max(1, ...precipIntensities)
  const precipBarWidth = precipHourly.length > 1 ? (hourlyChartWidth / (precipHourly.length - 1)) * 0.5 : 20

  const cloudTemps = cloudHourly.map((entry) => displayTemp(entry.temperatureF))
  const cloudTempMax = cloudTemps.length > 0 ? Math.max(...cloudTemps) : 0
  const cloudTempMin = cloudTemps.length > 0 ? Math.min(...cloudTemps) : 0
  /*
   * A minimum span, in the unit actually being displayed, so a day that
   * drifts a degree does not fill the frame and read as dramatic weather.
   * cloudTemps have already been through displayTemp, so the floor has to be
   * unit-aware or the imperial chart magnifies everything by 1.8.
   *
   * The day's real range is CENTRED inside that floor rather than anchored
   * to the bottom of it: a flat day then draws as a flat line through the
   * middle of the plot, which is the honest read. Bottom-anchoring would put
   * a still, mild day hard against the baseline and imply a cold snap.
   */
  const cloudMinSpan = unit === 'metric' ? 8 : 15
  const cloudRawSpan = cloudTempMax - cloudTempMin
  const cloudSpan = Math.max(cloudMinSpan, cloudRawSpan)
  const cloudScaleMin = cloudTempMin - (cloudSpan - cloudRawSpan) / 2
  const cloudScaleMax = cloudScaleMin + cloudSpan
  const cloudChartTop = HOURLY_CHART_TOP
  const cloudChartBottom = HOURLY_CHART_BOTTOM
  const cloudYFor = (value: number) =>
    cloudChartBottom - ((value - cloudScaleMin) / cloudSpan) * (cloudChartBottom - cloudChartTop)
  // The right-hand precipitation scale, in the same shape as every other
  // *YFor helper, so its two axis labels are positioned by the scale rather
  // than by the hardcoded pixels they used to sit at (axis rule 3 above).
  // Matches the <YAxis yAxisId="precip"> domain below exactly. precipMax has
  // a floor of 1, so there is no divide-by-zero to guard here.
  const precipYFor = (value: number) =>
    cloudChartTop + (1 - value / precipMax) * (cloudChartBottom - cloudChartTop)
  // Index of the lowest/highest temperature in the visible window, for the
  // L/H markers - matching indexOf's "first occurrence" tie-break is fine
  // here since a flat run of identical extreme values is rare in practice.
  const cloudMinIdx = cloudTemps.length > 0 ? cloudTemps.indexOf(cloudTempMin) : -1
  const cloudMaxIdx = cloudTemps.length > 0 ? cloudTemps.indexOf(cloudTempMax) : -1
  // A condition icon every 3 hours - dense enough to read at a glance like
  // the reference screenshot, without crowding 24 separate icons together.
  const cloudIconTicks = useMemo(
    () => cloudHourly.map((entry, idx) => ({ entry, idx })).filter(({ entry }) => entry.hourOfDay % 3 === 0),
    [cloudHourly],
  )

  // UV is plotted as a second series on the Cloud & Temperature chart rather
  // than its own standalone chart, sharing that chart's x-axis and index
  // alignment - both series come from the same WeatherKit hourly array for
  // the day, so indices line up 1:1 without needing to filter by sunrise/
  // sunset: at night UV is ~0 anyway, which already falls below the
  // "protection needed" threshold below without clipping the array.
  const uvValues = uvHourly.map((entry) => Math.max(0, entry.uvIndex))
  const uvIndex = Math.round(Math.max(0, ...uvValues))
  const uvMax = Math.max(11, ...uvValues)
  const uvProtectionIndices = useMemo(
    () =>
      uvHourly.reduce<number[]>((acc, entry, idx) => {
        if (Math.max(0, entry.uvIndex) >= 3) acc.push(idx)
        return acc
      }, []),
    [uvHourly],
  )
  const uvProtectionStart = uvProtectionIndices.length > 0 ? uvHourly[uvProtectionIndices[0]].label : null
  const uvProtectionEnd = uvProtectionIndices.length > 0 ? uvHourly[uvProtectionIndices[uvProtectionIndices.length - 1]].label : null

  // Recharts render data for the merged Cloud, Temperature & Precipitation chart.
  // Combines temperature (displayTemp-converted), UV index, and precipitation intensity/chance
  // on a shared hourOfDay axis.
  const cloudChartData = useMemo(() => {
    return Array.from({ length: 24 }, (_, hour) => {
      const cloudEntry = cloudHourly.find((c) => c.hourOfDay === hour) ?? cloudHourly[hour]
      const precipEntry = precipHourly.find((p) => p.hourOfDay === hour) ?? precipHourly[hour]
      const uvEntry = uvHourly[hour]

      const temperatureF = cloudEntry?.temperatureF ?? -1
      const displayTemperature =
        temperatureF >= 0 ? (unit === 'metric' ? fahrenheitToCelsius(temperatureF) : temperatureF) : null
      const precipIntensityMm = precipEntry ? Math.max(0, precipEntry.precipIntensityMm) : 0
      const precipChancePct =
        precipEntry && precipEntry.precipChancePct !== null
          ? Math.max(0, Math.min(100, precipEntry.precipChancePct))
          : null
      const uvVal = uvEntry ? Math.max(0, uvEntry.uvIndex) : 0

      return {
        hourOfDay: hour,
        displayTemperature,
        precipIntensityMm,
        precipChancePct,
        uvIndex: uvVal,
      }
    })
  }, [cloudHourly, precipHourly, uvHourly, unit])

  // Recharts' XAxis tickFormatter only receives the raw hourOfDay value, not
  // the hourly entry - look the real API-provided `.label` text up by hour
  // rather than re-deriving a 12-hour-clock string.
  const cloudLabelByHour = useMemo(() => {
    const labelMap = buildLabelByHour(cloudHourly)
    if (labelMap.size === 0) {
      return buildLabelByHour(precipHourly)
    }
    return labelMap
  }, [cloudHourly, precipHourly])

  const cloudChartMargin = hourlyChartMargin(265)
  const cloudChartConfig: ChartConfig = {
    displayTemperature: { label: `Temperature (${tempUnit})`, color: 'hsl(var(--chart-temp) / 0.9)' },
    precipIntensityMm: { label: 'Precipitation (mm/hr)', color: 'hsl(var(--chart-precip) / 0.85)' },
    uvIndex: { label: 'UV Index', color: 'hsl(var(--chart-uv))' },
  }

  const windTooltip = useChartTooltip(windHourly.length, selectedDayIndex, hourlyChartLeft, hourlyChartRight)
  const waveTooltip = useChartTooltip(waveHourly.length, selectedDayIndex, hourlyChartLeft, hourlyChartRight)
  const cloudTooltip = useChartTooltip(
    Math.max(cloudHourly.length, precipHourly.length),
    selectedDayIndex,
    hourlyChartLeft,
    hourlyChartRight,
  )

  const windTooltipEntry = windTooltip.activeIndex === null ? null : windHourly[windTooltip.activeIndex] ?? null
  const waveTooltipEntry = waveTooltip.activeIndex === null ? null : waveHourly[waveTooltip.activeIndex] ?? null
  const cloudTooltipEntry = cloudTooltip.activeIndex === null ? null : cloudHourly[cloudTooltip.activeIndex] ?? null
  const precipTooltipEntry = cloudTooltip.activeIndex === null ? null : precipHourly[cloudTooltip.activeIndex] ?? null
  const uvTooltipEntry = cloudTooltip.activeIndex === null ? null : uvHourly[cloudTooltip.activeIndex] ?? null

  if (loading && !hasForecast) {
    return (
      <div className="rounded-lg border bg-background/60 px-4 py-8 text-center">
        <p className="text-xs uppercase tracking-[0.16em] text-muted-foreground">Forecast</p>
        <p className="mt-2 font-medium text-foreground">Loading latest marine forecast...</p>
        <p className="mt-1 text-xs text-muted-foreground">Pulling weather and wind guidance for your position</p>
      </div>
    )
  }

  if (error && !hasForecast) {
    return (
      <div className="rounded-lg border border-gauge-primary/30 bg-gauge-primary/5 px-4 py-8 text-center">
        <p className="text-xs uppercase tracking-[0.16em] text-gauge-primary">Forecast Offline</p>
        <p className="mt-2 font-medium text-foreground">Unable to load forecast data right now</p>
        <p className="mt-1 text-xs text-muted-foreground">{error}</p>
        <div className="mt-4 flex justify-center">
          <Button type="button" size="sm" variant="outline" className="h-10 min-w-24" onClick={onRetry}>
            Retry
          </Button>
        </div>
      </div>
    )
  }

  if (!hasForecast) {
    return <div className="py-8 text-center text-muted-foreground">No forecast data available</div>
  }

  return (
    <div className="space-y-4 pb-4">
      {hourlyEntries.length > 0 && (
        <ForecastPanel
          testId="forecast-panel-today"
          title="Today"
          spanLabel="Next 24 hours"
          stale={weatherStale}
          staleLabel={weatherStaleLabel}
          intro={
            <div className="pr-2 text-base font-medium leading-relaxed text-foreground/90">
              <p>{summary ?? "Today's hourly forecast"}</p>
              <ForecastWarningNotice warnings={activeForecastWarning} />
            </div>
          }
        >
          <div className="px-2.5 py-2.5">
        <div className="flex gap-1.5 overflow-x-auto pb-1">
          {hourlyEntries.map((entry, idx) => {
            /*
             * Night mode reads the hour's own isDaylight flag (the same
             * per-hour signal the cloud chart's getCloudChartIcon already
             * uses) rather than "has a sunset marker appeared earlier in the
             * strip". That latch never turns back off, which was harmless at
             * the old 12-hour cap (rarely long enough to reach the following
             * sunrise) and wrong now the strip runs a full 24: every hour
             * from sunset to midnight the next day would render as night.
             * The synthetic sunset entry itself carries no real isDaylight
             * reading, so it is excluded rather than read as false-for-night.
             */
            const nightMode = entry.kind !== 'sunset' && !entry.isDaylight
            const isNowEntry = entry.label === 'Now' && entry.kind === 'forecast'
            const displayTemperature = entry.temperatureF >= 0 ? Math.round(displayTemp(entry.temperatureF)) : null
            /*
             * Wind owns the display slot. Twelve tiles of a calm day used to
             * render twelve big near-identical temperatures spanning about a
             * degree, while the one variable a passage decision turns on sat
             * in the smallest type in the tile. Size follows consequence.
             *
             * Wind only arrives when the provider gave one, so a forecast hour
             * without it falls back to the temperature rather than leaving the
             * biggest slot in the tile blank; the secondary line then has
             * nothing left to say and is dropped.
             */
            const hasWind = entry.kind === 'forecast' && entry.windSpeedKts >= 0
            const displaySlotClass = nightMode ? 'text-gauge-secondary' : 'text-foreground'

            return (
              <div
                key={`${entry.kind}-${entry.label}-${idx}`}
                data-testid="forecast-hour-tile"
                className={`relative flex min-w-[84px] flex-col items-center rounded-xl border px-2.5 py-3.5 text-center ${entry.kind === 'sunset' ? 'border-gauge-primary/20 bg-gauge-primary/10' : nightMode ? 'border-gauge-secondary/20 bg-gauge-secondary/10' : 'border-border/70 bg-card/80'} ${isNowEntry ? 'shadow-[0_0_0_1px_hsl(var(--gauge-primary)/0.22),0_10px_18px_hsl(var(--gauge-primary)/0.10)]' : ''}`}
              >
                {isNowEntry && (
                  <span className="absolute left-1/2 top-1.5 h-1.5 w-1.5 -translate-x-1/2 rounded-full bg-gauge-primary" />
                )}
                <p className={`text-base font-semibold tabular-nums ${isNowEntry ? 'text-gauge-primary-label' : nightMode ? 'text-gauge-secondary' : 'text-foreground/75'}`}>{entry.label}</p>
                <div className="mt-3.5 flex h-8 items-center justify-center">
                  {getHourlyWeatherIcon(entry, nightMode)}
                </div>
                {entry.kind === 'sunset' ? (
                  <p data-testid="forecast-hour-headline" className="mt-4 text-sm font-semibold uppercase tracking-[0.08em] text-gauge-primary-label">
                    Sunset
                  </p>
                ) : hasWind ? (
                  <p
                    data-testid="forecast-hour-headline"
                    className={`mt-4 flex items-baseline justify-center gap-0.5 whitespace-nowrap ${displaySlotClass}`}
                  >
                    <span className="font-display text-3xl leading-none tabular-nums">{Math.round(entry.windSpeedKts)}</span>
                    <span className="text-2xs">{windUnit} {entry.windDirection}</span>
                  </p>
                ) : (
                  <p data-testid="forecast-hour-headline" className={`mt-4 font-display text-3xl leading-none tabular-nums ${displaySlotClass}`}>
                    {displayTemperature !== null ? `${displayTemperature}°` : '—'}
                  </p>
                )}
                {hasWind && (
                  <p data-testid="forecast-hour-subline" className={`mt-1 whitespace-nowrap text-xs tabular-nums ${nightMode ? 'text-gauge-secondary/80' : 'text-muted-foreground'}`}>
                    {displayTemperature !== null ? `${displayTemperature}°` : '—'}
                  </p>
                )}
                {nightMode && entry.kind === 'forecast' && (
                  <span className="mt-1 text-2xs font-medium uppercase tracking-[0.12em] text-gauge-secondary">Night</span>
                )}
              </div>
            )
          })}
        </div>
          </div>
        </ForecastPanel>
      )}

      <ForecastPanel
        testId="forecast-panel-extended"
        title="10-Day Forecast"
        spanLabel={`${days.length} days`}
        stale={weatherStale}
        staleLabel={weatherStaleLabel}
        intro={extendedIntro}
      >
        <div className="flex flex-col gap-3 px-2.5 py-2.5">
          <div ref={dayTabsRowRef} className="sticky top-0 z-10 flex gap-1.5 overflow-x-auto bg-card/95 pb-2 pt-0.5">
            {days.map((day, idx) => {
              const isSelected = idx === selectedDayIndex
              const hasTrough = upperAirFlaggedDayKeys.has(day.dayKey)
              // Respect the null path rather than announcing a fabricated
              // number - precipitation is `number | null` when the provider
              // reported no chance-of-precipitation data at all.
              const precipLabel =
                day.precipitation === null
                  ? 'no precipitation data'
                  : `${Math.round(day.precipitation)}% chance of precipitation`
              // A sighted user reads all of this off the card itself; a
              // screen-reader user only gets what's in this string, so it
              // carries the same decision-relevant content: wind (the
              // display slot's own reasoning applies here too), condition,
              // high/low, precipitation chance, and the trough flag.
              const dayAriaLabel = `Select forecast day ${day.dayName} ${day.date}, wind ${Math.round(day.windSpeed)} ${windUnit} ${day.windDirection}, ${day.condition}, high ${Math.round(displayTemp(day.high))}${tempUnit} low ${Math.round(displayTemp(day.low))}${tempUnit}, ${precipLabel}${hasTrough ? ', upper air trough' : ''}`

              return (
                <button
                  key={day.dayKey}
                  type="button"
                  className={`min-w-[150px] shrink-0 rounded-lg border px-2.5 py-2 text-left transition-colors ${
                    isSelected
                      ? 'border-primary/40 bg-card'
                      : 'border-border/60 bg-background/40 hover:bg-muted/30'
                  }`}
                  aria-label={dayAriaLabel}
                  aria-pressed={isSelected}
                  onClick={() => selectDay(idx)}
                >
                  <div className="flex items-center justify-between gap-2">
                    <div>
                      {/* The selected day is carried by three cues, none of
                          them colour alone: the card lifts to the card-white
                          surface (the one tonal step this theme has), its
                          border takes the chrome token, and the day name drops
                          its muting. Type weight is the cue that survives
                          direct sun, which a tint does not. */}
                      <p className={`text-xs font-semibold uppercase ${isSelected ? 'text-foreground' : 'text-muted-foreground'}`}>
                        {idx === 0 ? 'Today' : day.dayName.slice(0, 3)}
                      </p>
                      <p className="text-2xs text-muted-foreground">{day.date}</p>
                    </div>
                    <div className="flex items-center gap-1">
                      {hasTrough && (
                        <span
                          data-testid="forecast-upper-air-marker"
                          title="Upper air supports a surface low developing"
                          aria-label="Upper air supports a surface low developing"
                          role="img"
                          className="rounded-sm bg-gauge-secondary/20 px-1 py-0.5 text-2xs font-semibold uppercase tracking-wide text-gauge-secondary"
                        >
                          TROUGH
                        </span>
                      )}
                      <WeatherConditionIcon condition={day.condition} size={26} />
                    </div>
                  </div>

                  <p className="mt-1 truncate text-2xs font-medium text-foreground">
                    {day.condition}
                  </p>

                  {/*
                    * Same swap as the hourly tile: wind takes the display slot,
                    * the high/low drops to the line under it. "17kts SE" and
                    * "6kts SE" used to render identically in a 10px footer, so
                    * the card said nothing about whether the day was sailable
                    * until you read it. Size only - no threshold tinting, per
                    * ADR 0071 section 2.
                    */}
                  <div className="mt-1.5 flex items-center justify-between gap-2">
                    <p data-testid="forecast-day-headline" className="flex items-baseline gap-1 whitespace-nowrap text-gauge-secondary">
                      <span className="font-display text-lg leading-none tabular-nums">{Math.round(day.windSpeed)}</span>
                      <span className="text-2xs">{windUnit} {day.windDirection}</span>
                    </p>
                    <p className="text-2xs tabular-nums text-muted-foreground">{day.precipitation === null ? '— precip' : `${Math.round(day.precipitation)}% precip`}</p>
                  </div>

                  <p data-testid="forecast-day-subline" className="mt-1 flex items-center gap-1 text-2xs tabular-nums text-muted-foreground">
                    <span className="font-semibold text-foreground">{Math.round(displayTemp(day.high))}</span>
                    <span>{tempUnit}</span>
                    <span>/</span>
                    <span className="font-semibold">{Math.round(displayTemp(day.low))}</span>
                  </p>
                </button>
              )
            })}
          </div>

          <div ref={detailsCardRef} className="rounded-lg border bg-background/60 p-3">
            <div className="mb-3 flex flex-wrap items-center gap-3">
              <div className="flex items-end gap-1">
                <WeatherConditionIcon condition={selectedDay.condition} size={22} />
                <span className="font-display text-4xl leading-none tabular-nums text-gauge-primary">{Math.round(displayTemp(selectedDay.high))}</span>
                <span className="pb-1 text-lg text-muted-foreground">{tempUnit}</span>
              </div>
              <p className="text-sm font-semibold uppercase tracking-[0.08em] text-foreground">{selectedDay.condition}</p>
              {/*
                * Two tiers in one wrapping row, both visible. Ten identical
                * chips are ten peers, and working memory does not hold ten:
                * the eye has to scan the lot to find the one it came for.
                * Nothing is hidden here, it is ranked. Decisions (wind, gusts
                * and precip) keep the chip and get the weight; the reference
                * stats, the 500mb reading among them, drop the chip and sit
                * as plain muted text after them. No colour escalation on wind - see
                * ADR 0071 section 2, there is no sourced threshold to escalate
                * against and an invented one is worse than none.
                */}
              <div className="flex flex-wrap items-center gap-x-2 gap-y-1.5 text-xs">
                <span className="rounded-sm bg-muted/80 px-2 py-1 text-sm">Wind <span data-testid="forecast-selected-wind" className="font-semibold tabular-nums text-gauge-secondary">{selectedDay.windSpeed.toFixed(1)} {windUnit}</span></span>
                <span className="rounded-sm bg-muted/80 px-2 py-1 text-sm">Gusts <span data-testid="forecast-selected-gust" className="font-semibold tabular-nums text-chart-gust">{selectedDay.windGust.toFixed(1)} {windUnit}</span></span>
                <span className="rounded-sm bg-muted/80 px-2 py-1 text-sm">Precip <span data-testid="forecast-selected-precip" className={`tabular-nums${precipitationPct !== null && precipitationPct > 20 ? ' font-semibold' : ''}`}>{precipitationPct === null ? '—' : `${Math.round(precipitationPct)}%`}</span></span>
                <span className="text-2xs text-muted-foreground">Humidity <span data-testid="forecast-selected-humidity" className="font-semibold tabular-nums">{humidityPct === null ? '—' : `${Math.round(humidityPct)}%`}</span></span>
                <span className="text-2xs text-muted-foreground">Visibility <span data-testid="forecast-selected-visibility" className="font-semibold tabular-nums">{visibilityNm === null ? '—' : `${visibilityNm.toFixed(1)} nm`}</span></span>
                <span className="text-2xs text-muted-foreground">UV Index <span data-testid="forecast-selected-uv" className="font-semibold tabular-nums text-gauge-secondary">{uvIndex}</span></span>
                {selectedUpperAir?.present && (
                  <span data-testid="forecast-upper-air-detail" className="text-2xs text-muted-foreground">
                    500mb <span className="font-semibold">{Math.round(selectedUpperAir.height500M)} m</span>
                    {selectedUpperAir.troughSupport
                      ? ', lowest of the window after a sustained fall. Conditions aloft support a surface low developing.'
                      : ` \u00b7 jet ${Math.round(selectedUpperAir.peakWind500Kts)} kt`}
                  </span>
                )}
                {selectedDay.sunriseTime && (
                  <span className="flex items-center gap-1 text-2xs text-muted-foreground">
                    <Sunrise size={12} className="text-gauge-primary" />
                    Sunrise <span className="font-semibold tabular-nums">{selectedDay.sunriseTime}</span>
                  </span>
                )}
                {selectedDay.sunsetTime && (
                  <span className="flex items-center gap-1 text-2xs text-muted-foreground">
                    <Sunset size={12} className="text-gauge-primary" />
                    Sunset <span className="font-semibold tabular-nums">{selectedDay.sunsetTime}</span>
                  </span>
                )}
                {selectedDay.moonPhase && (
                  <span className="flex items-center gap-1 text-2xs text-muted-foreground">
                    <span aria-hidden>{moonPhaseEmoji(selectedDay.moonPhase)}</span>
                    Moon <span className="font-semibold">{moonPhaseLabel(selectedDay.moonPhase)}</span>
                  </span>
                )}
              </div>
            </div>

            <div ref={chartCardRef} className="rounded-md border bg-card/70 p-2">
              <div className="mb-2 flex flex-col gap-0.5 sm:flex-row sm:items-center sm:justify-between">
                <h4 className="flex items-center gap-1.5 text-xs font-semibold uppercase tracking-[0.12em] text-muted-foreground">
                  <Cloud size={13} className="text-gauge-secondary" /> Cloud, Temperature & Rain
                </h4>
                {(isCached || updatedAt) && (
                  <p data-testid="forecast-refresh-meta" className="text-xs text-muted-foreground">
                    {provider ? `Data: ${provider} · ` : ''}
                    {isCached ? 'cached' : 'live'} · updated {formatRefreshAge(updatedAt, Date.now())}
                    {ttlSeconds ? ` · refreshes every ${Math.round(ttlSeconds / 60)}m` : ''}
                  </p>
                )}
              </div>
              {cloudHourly.length > 0 ? (
                <>
                  <p className="mb-2 text-base text-foreground/80">
                    {uvProtectionStart && uvProtectionEnd
                      ? `Sun protection recommended from ${uvProtectionStart} to ${uvProtectionEnd}.`
                      : 'No sun protection needed.'}
                  </p>
                  {selectedDay.precipitationSummary && (
                    <p className="mb-2 text-base text-foreground/80">{selectedDay.precipitationSummary}</p>
                  )}
                  <p data-testid="forecast-cloud-key" className="mb-2 text-sm text-muted-foreground">
                    Rain bar height is the rate in mm/hr, and bar opacity is the chance of it falling.
                  </p>
                  <div className="relative">
                  {cloudTooltipEntry && (
                    <ChartTooltipBubble
                      pixelX={cloudTooltip.tooltipPixelX ?? 0}
                      time={cloudTooltipEntry.label}
                      primary={`${Math.round(displayTemp(cloudTooltipEntry.temperatureF))}${tempUnit}`}
                      secondary={`${cloudTooltipEntry.condition}${precipTooltipEntry && precipTooltipEntry.precipIntensityMm > 0 ? ` · ${precipTooltipEntry.precipIntensityMm.toFixed(1)} mm/hr (${precipTooltipEntry.precipChancePct !== null ? `${Math.round(precipTooltipEntry.precipChancePct)}%` : '—'} chance)` : ''}`}
                      tertiary={uvTooltipEntry ? `UV ${Math.round(uvTooltipEntry.uvIndex)} · ${uvRiskLabel(uvTooltipEntry.uvIndex)}` : undefined}
                    />
                  )}
                  <div
                    data-testid="forecast-cloud-chart"
                    className="relative h-[265px] touch-none overflow-hidden rounded-sm bg-muted/15"
                    style={{ width: forecastChartWidth }}
                  >
                    <ComposedChart width={forecastChartWidth} height={265} data={cloudChartData} margin={cloudChartMargin}>
                      <XAxis
                        dataKey="hourOfDay"
                        type="number"
                        domain={[0, 23]}
                        allowDataOverflow
                        ticks={[0, 6, 12, 18]}
                        tickFormatter={(value: number) => cloudLabelByHour.get(value) ?? ''}
                        axisLine={false}
                        tickLine={false}
                        tick={{ fontSize: Number(AXIS_LABEL_FONT_SIZE), fill: AXIS_LABEL_COLOR }}
                        allowDuplicatedCategory={false}
                        height={RECHARTS_XAXIS_HEIGHT}
                      />
                      <CartesianGrid horizontal vertical={false} stroke="hsl(var(--chart-grid) / 0.12)" />
                      {/* Must stay the same scale cloudYFor uses, or the
                          axis labels, L/H dots and scrub marker drift off
                          the plotted curve. */}
                      <YAxis domain={[cloudScaleMin, cloudScaleMax]} hide />
                      <YAxis yAxisId="precip" domain={[0, precipMax]} orientation="right" hide />
                      <YAxis yAxisId="uv" domain={[0, uvMax]} hide />
                      {/* tempAreaGradientId/uvAreaGradientId are defined once,
                          below, in the overlay <svg> - SVG ids are
                          document-scoped, so a url(#id) fill here resolves
                          there regardless of which <svg> declared it. Same
                          pattern as windAreaGradientId and waveAreaGradientId
                          further down this file. */}
                      <Area
                        dataKey="uvIndex"
                        yAxisId="uv"
                        type="monotone"
                        isAnimationActive={false}
                        dot={false}
                        stroke="none"
                        fill={`url(#${uvAreaGradientId})`}
                      />
                      <Bar dataKey="precipIntensityMm" yAxisId="precip" barSize={precipBarWidth} isAnimationActive={false}>
                        {cloudChartData.map((entry, idx) => (
                          <Cell
                            key={idx}
                            data-testid="forecast-precip-bar"
                            fill={precipBarColor(entry.precipChancePct)}
                          />
                        ))}
                      </Bar>
                      <Area
                        dataKey="displayTemperature"
                        type="monotone"
                        isAnimationActive={false}
                        dot={false}
                        stroke={cloudChartConfig.displayTemperature.color}
                        strokeWidth={2.4}
                        fill={`url(#${tempAreaGradientId})`}
                      />
                      {cloudMinIdx >= 0 && (
                        <ReferenceDot
                          x={cloudHourly[cloudMinIdx].hourOfDay}
                          y={cloudTempMin}
                          r={3}
                          fill="hsl(var(--chart-temp) / 0.95)"
                          stroke="none"
                          isFront
                          label={{ value: 'L', position: 'bottom', fill: AXIS_LABEL_COLOR, fontSize: Number(AXIS_LABEL_FONT_SIZE) }}
                        />
                      )}
                      {cloudMaxIdx >= 0 && (
                        <ReferenceDot
                          x={cloudHourly[cloudMaxIdx].hourOfDay}
                          y={cloudTempMax}
                          r={3}
                          fill="hsl(var(--chart-temp) / 0.95)"
                          stroke="none"
                          isFront
                          label={{ value: 'H', position: 'top', fill: AXIS_LABEL_COLOR, fontSize: Number(AXIS_LABEL_FONT_SIZE) }}
                        />
                      )}
                    </ComposedChart>

                    <svg
                      ref={cloudTooltip.svgRef}
                      viewBox={`0 0 ${forecastChartWidth} 265`}
                      preserveAspectRatio="none"
                      className="pointer-events-auto absolute inset-0 h-full w-full touch-none focus-visible:outline-hidden focus-visible:ring-2 focus-visible:ring-ring"
                      tabIndex={0}
                      role="img"
                      aria-label={`Cloud, temperature and rain for ${selectedDay.dayName}, hourly. Use arrow keys to read values.`}
                      onPointerDown={cloudTooltip.onPointerDown}
                      onPointerMove={cloudTooltip.onPointerMove}
                      onPointerLeave={cloudTooltip.onPointerLeave}
                      onKeyDown={cloudTooltip.onKeyDown}
                      onFocus={cloudTooltip.onFocus}
                    >
                      <defs>
                        <linearGradient id={tempAreaGradientId} x1="0" y1="0" x2="0" y2="1">
                          <stop offset="0%" stopColor="hsl(var(--chart-temp))" stopOpacity="0.28" />
                          <stop offset="100%" stopColor="hsl(var(--chart-temp))" stopOpacity="0.02" />
                        </linearGradient>
                        <linearGradient id={uvAreaGradientId} x1="0" y1="0" x2="0" y2="1">
                          <stop offset="0%" stopColor="hsl(var(--chart-uv))" stopOpacity="0.30" />
                          <stop offset="100%" stopColor="hsl(var(--chart-uv))" stopOpacity="0.02" />
                        </linearGradient>
                      </defs>

                      {/* Two axes on one frame, so each carries its own colour
                          and its own unit (axis rules 1 and 2 above).

                          On the left, the DATA extremes rather than the scale
                          extremes - the reader wants the day's real high and
                          low - positioned by cloudYFor so they track the curve
                          instead of sitting at the fixed y=40/y=123 the old
                          fill-the-frame scale could assume. On the right, the
                          precipitation scale's own bounds through precipYFor,
                          which is what those two labels' hardcoded pixels used
                          to only accidentally agree with. */}
                      <text data-testid="forecast-cloud-temp-tick" x={6} y={axisTickLabelY(cloudYFor, cloudTempMax, cloudChartTop, cloudChartBottom)} fontSize={AXIS_LABEL_FONT_SIZE} fill="hsl(var(--chart-temp-label))">{Math.round(cloudTempMax)}{tempUnit}</text>
                      <text data-testid="forecast-cloud-temp-tick" x={6} y={axisTickLabelY(cloudYFor, cloudTempMin, cloudChartTop, cloudChartBottom)} fontSize={AXIS_LABEL_FONT_SIZE} fill="hsl(var(--chart-temp-label))">{Math.round(cloudTempMin)}</text>
                      <text data-testid="forecast-cloud-precip-tick" x={forecastChartWidth - 6} y={axisTickLabelY(precipYFor, precipMax, cloudChartTop, cloudChartBottom)} textAnchor="end" fontSize={AXIS_LABEL_FONT_SIZE} fill="hsl(var(--chart-precip-label))">{precipMax.toFixed(1)}mm</text>
                      <text data-testid="forecast-cloud-precip-tick" x={forecastChartWidth - 6} y={axisTickLabelY(precipYFor, 0, cloudChartTop, cloudChartBottom)} textAnchor="end" fontSize={AXIS_LABEL_FONT_SIZE} fill="hsl(var(--chart-precip-label))">0</text>
                      <line x1={hourlyChartLeft} y1={cloudChartBottom} x2={hourlyChartRight} y2={cloudChartBottom} stroke="hsl(var(--chart-grid) / 0.25)" strokeWidth="1" />

                      {cloudTooltipEntry && cloudTooltipEntry.hourOfDay >= 0 && (
                        <ChartTooltipMarker
                          x={hourlyXForHour(cloudTooltipEntry.hourOfDay)}
                          y={cloudYFor(displayTemp(cloudTooltipEntry.temperatureF))}
                          color="hsl(var(--chart-temp) / 0.95)"
                        />
                      )}
                    </svg>
                  </div>

                  <div className="pointer-events-none absolute inset-x-0 top-0 flex h-[50px] items-center" style={{ left: `${(hourlyChartLeft / forecastChartWidth) * 100}%`, right: `${100 - (hourlyChartRight / forecastChartWidth) * 100}%` }}>
                    {cloudIconTicks.map(({ entry, idx }) => (
                      <div
                        key={idx}
                        className="absolute -translate-x-1/2"
                        style={{ left: `${cloudHourly.length <= 1 ? 50 : (idx / (cloudHourly.length - 1)) * 100}%` }}
                        title={entry.condition}
                      >
                        {getCloudChartIcon(entry.condition, entry.isDaylight, 16)}
                      </div>
                    ))}
                  </div>
                  </div>
                  <p data-testid="forecast-cloud-legend" className="mt-1 text-2xs text-muted-foreground">
                    <span className="inline-flex items-center gap-1 align-middle"><LegendSwatch color={cloudChartConfig.displayTemperature.color ?? 'currentColor'} strokeWidth={2.4} /> Temp ({tempUnit})</span> · <span className="inline-flex items-center gap-1 align-middle"><LegendSwatch kind="bar" color={cloudChartConfig.precipIntensityMm.color ?? 'currentColor'} /> Rain (mm/hr)</span> · <span className="inline-flex items-center gap-1 align-middle"><LegendSwatch kind="bar" color="hsl(var(--chart-uv) / 0.5)" /> UV background</span>
                  </p>
                </>
              ) : (
                <ChartUnavailableMessage testId="forecast-cloud-unavailable" message="Cloud & temperature forecast unavailable for this day" />
              )}
            </div>

            <div className="mt-3 rounded-md border bg-card/70 p-2">
              <h4 className="mb-2 flex items-center gap-1.5 text-xs font-semibold uppercase tracking-[0.12em] text-muted-foreground">
                <Wind size={13} className="text-gauge-secondary" /> Wind ({windUnit})
              </h4>
              {windHourly.length > 0 ? (
                <>
                  {selectedDay.windSummary && (
                    <p className="mb-2 text-base text-foreground/80">{selectedDay.windSummary}</p>
                  )}
                  <p data-testid="forecast-wind-key" className="mb-2 text-sm text-muted-foreground">
                    Barbs show the direction the wind is coming from. A full feather is 10kt, a half feather 5kt.
                  </p>
                  <div className="relative">
                    {windTooltipEntry && (
                      <ChartTooltipBubble
                        pixelX={windTooltip.tooltipPixelX ?? 0}
                        time={windTooltipEntry.label}
                        primary={`${Math.round(windTooltipEntry.windSpeed)} ${windUnit} ${windTooltipEntry.windDirection}`}
                        secondary={`Gusts: ${Math.round(windTooltipEntry.windGust)} ${windUnit}`}
                      />
                    )}
                    <div
                      data-testid="forecast-wind-chart"
                      className="relative h-[265px] touch-none overflow-hidden rounded-sm bg-muted/15"
                      style={{ width: forecastChartWidth }}
                    >
                      <ComposedChart width={forecastChartWidth} height={265} margin={windChartMargin}>
                        <XAxis
                          dataKey="hourOfDay"
                          type="number"
                          domain={[0, 23]}
                          allowDataOverflow
                          ticks={[0, 6, 12, 18]}
                          tickFormatter={(value: number) => windLabelByHour.get(value) ?? ''}
                          axisLine={false}
                          tickLine={false}
                          tick={{ fontSize: Number(AXIS_LABEL_FONT_SIZE), fill: AXIS_LABEL_COLOR }}
                          height={RECHARTS_XAXIS_HEIGHT}
                        />
                        <YAxis domain={[0, windMax]} hide />
                        <CartesianGrid horizontal vertical={false} stroke="hsl(var(--chart-grid) / 0.12)" />
                        <Area
                          data={windChartData}
                          dataKey="windSpeed"
                          type="monotone"
                          isAnimationActive={false}
                          dot={false}
                          stroke="none"
                          fill={`url(#${windAreaGradientId})`}
                        />
                        <Line
                          data={windChartData}
                          dataKey="windGust"
                          type="monotone"
                          isAnimationActive={false}
                          dot={false}
                          stroke="hsl(var(--chart-gust) / 0.75)"
                          strokeWidth={1.5}
                          strokeDasharray="4 3"
                        />
                        <Line
                          data={windChartData}
                          dataKey="windSpeed"
                          type="monotone"
                          isAnimationActive={false}
                          dot={false}
                          stroke="hsl(var(--chart-wind) / 0.95)"
                          strokeWidth={2.4}
                        />
                        <Customized
                          component={() => (
                            <>
                              {windHourTicks.map(({ entry, idx }) => (
                                <WindBarb
                                  key={idx}
                                  cx={hourlyXForHour(entry.hourOfDay)}
                                  cy={25}
                                  speedKts={entry.windSpeed}
                                  directionDeg={entry.windDirectionDeg}
                                />
                              ))}
                            </>
                          )}
                        />
                      </ComposedChart>

                      <svg
                        ref={windTooltip.svgRef}
                        viewBox={`0 0 ${forecastChartWidth} 265`}
                        preserveAspectRatio="none"
                        className="pointer-events-auto absolute inset-0 h-full w-full touch-none focus-visible:outline-hidden focus-visible:ring-2 focus-visible:ring-ring"
                        tabIndex={0}
                        role="img"
                        aria-label={`Wind and gusts for ${selectedDay.dayName}, hourly. Use arrow keys to read values.`}
                        onPointerDown={windTooltip.onPointerDown}
                        onPointerMove={windTooltip.onPointerMove}
                        onPointerLeave={windTooltip.onPointerLeave}
                        onKeyDown={windTooltip.onKeyDown}
                        onFocus={windTooltip.onFocus}
                      >
                        <defs>
                          <linearGradient id={windAreaGradientId} x1="0" y1="0" x2="0" y2="1">
                            <stop offset="0%" stopColor="hsl(var(--chart-wind))" stopOpacity="0.28" />
                            <stop offset="100%" stopColor="hsl(var(--chart-wind))" stopOpacity="0.02" />
                          </linearGradient>
                        </defs>
                        {windAxisTicks.map((tick) => (
                          <text key={tick} x={6} y={axisTickLabelY(windYFor, tick, windChartTop, windChartBottom)} fontSize={AXIS_LABEL_FONT_SIZE} fill={AXIS_LABEL_COLOR}>
                            {tick}
                          </text>
                        ))}
                        <line x1={hourlyChartLeft} y1={windChartBottom} x2={hourlyChartRight} y2={windChartBottom} stroke="hsl(var(--chart-grid) / 0.25)" strokeWidth="1" />

                        {windTooltipEntry && windTooltipEntry.hourOfDay >= 0 && (
                          <ChartTooltipMarker
                            x={hourlyXForHour(windTooltipEntry.hourOfDay)}
                            y={windYFor(Math.max(0, windTooltipEntry.windSpeed))}
                            color="hsl(var(--chart-wind) / 0.95)"
                          />
                        )}
                      </svg>
                    </div>
                  </div>
                  <p data-testid="forecast-wind-legend" className="mt-1 text-2xs text-muted-foreground">
                    <span className="inline-flex items-center gap-1 align-middle"><LegendSwatch color="hsl(var(--chart-wind) / 0.95)" strokeWidth={2.4} /> Wind</span> · <span className="inline-flex items-center gap-1 align-middle"><LegendSwatch color="hsl(var(--chart-gust) / 0.75)" strokeWidth={1.5} dasharray="4 3" /> Gusts</span> ({windUnit})
                  </p>
                </>
              ) : (
                <ChartUnavailableMessage testId="forecast-wind-unavailable" message="Wind forecast unavailable for this day" />
              )}
            </div>

            <div className="mt-3 rounded-md border bg-card/70 p-2">
              <div className="mb-2 flex flex-col gap-0.5 sm:flex-row sm:items-center sm:justify-between">
                <h4 className="flex items-center gap-1.5 text-xs font-semibold uppercase tracking-[0.12em] text-muted-foreground">
                  <Waves size={13} className="text-gauge-secondary" /> Wave (m)
                  {/* The wave feed carries its own TTL and can go stale on a
                      different schedule than the weather feed the panel-level
                      badge above tracks, so it gets its own independent
                      amber marker here rather than inheriting that state. */}
                  {waveStale && <ForecastStaleBadge testId="forecast-wave-stale-badge" staleLabel={waveStaleLabel} />}
                </h4>
                {(waveIsCached || waveUpdatedAt) && (
                  <p data-testid="forecast-wave-refresh-meta" className="text-xs text-muted-foreground">
                    {waveProvider ? `Data: ${waveProvider} · ` : ''}
                    {waveIsCached ? 'cached' : 'live'} · updated {formatRefreshAge(waveUpdatedAt, Date.now())}
                    {waveTtlSeconds ? ` · refreshes every ${Math.round(waveTtlSeconds / 3600)}h` : ''}
                  </p>
                )}
              </div>
              <div data-testid="forecast-wave-body" className={waveStale ? 'grayscale' : undefined}>
                {waveLoading ? (
                  <p className="py-6 text-center text-xs text-muted-foreground" data-testid="forecast-wave-loading">
                    Loading wave forecast...
                  </p>
                ) : waveUnavailableDueToError ? (
                  <ChartUnavailableMessage testId="forecast-wave-error" message="Wave data unavailable" onRetry={onWaveRetry} />
                ) : waveHourly.length > 0 ? (
                  <>
                    {(() => {
                      const baseSummary = selectedWaveDay?.waveSummary
                      if (!baseSummary) return null
                      if (waveSeaTemperatureF === null) {
                        return <p className="mb-2 text-base text-foreground/80">{baseSummary}</p>
                      }
                      const seaTempDisplay = `${Math.round(displayTemp(waveSeaTemperatureF))}${tempUnit}`
                      const trimmed = baseSummary.replace(/\.$/, '')
                      const summaryWithTemp = `${trimmed} and sea surface temperature of ${seaTempDisplay}.`
                      return <p className="mb-2 text-base text-foreground/80">{summaryWithTemp}</p>
                    })()}
                    {wavePeakHeightM > 0 && (
                      <p data-testid="forecast-wave-largest" className="mb-2 text-sm text-muted-foreground">
                        Largest wave you are likely to meet: {(wavePeakHeightM * HIGHEST_WAVE_MULTIPLE).toFixed(1)} m.
                        Roughly one wave in seven reaches the significant height.
                      </p>
                    )}
                    {waveIndicatorMessages.length > 0 ? (
                      <ul
                        data-testid="forecast-wave-indicators"
                        className="mb-2 space-y-1 rounded-sm border border-gauge-secondary/40 bg-muted/25 p-2 text-sm text-foreground/90"
                      >
                        {waveIndicatorMessages.map((message) => (
                          <li key={message} className="flex items-start gap-1.5">
                            <Waves size={12} className="mt-0.5 shrink-0 text-gauge-secondary" aria-hidden />
                            <span>{message}</span>
                          </li>
                        ))}
                      </ul>
                    ) : (
                      /*
                       * Mirrors extendedIntro's epistemic structure: this
                       * branch only runs once waveHourly.length > 0, i.e.
                       * once the indicators actually ran for this day, so
                       * silence here would read as "never checked" when the
                       * true state is "checked, and clear" - indistinguishable
                       * from a dead feed on a product whose first principle is
                       * that a missing feed must never look like a calm one.
                       */
                      <p data-testid="forecast-wave-indicators-clear" className="mb-2 text-sm text-muted-foreground">
                        None of the leading indicators tripped for this day.
                      </p>
                    )}
                    <p data-testid="forecast-wave-key" className="mb-2 text-sm text-muted-foreground">
                      Arrows show the direction the swell is heading, with its period in seconds below each. The
                      number after it is height over wavelength: under 1:25 rolls, past 1:10 breaks.
                    </p>
                    <div className="relative">
                      {waveTooltipEntry && (
                        <ChartTooltipBubble
                          pixelX={waveTooltip.tooltipPixelX ?? 0}
                          time={waveTooltipEntry.label}
                          primary={`${waveTooltipEntry.waveHeightM.toFixed(1)} m`}
                          secondary={`Swell ${waveTooltipEntry.swellWaveHeightM.toFixed(1)}m from ${compassLabel(waveTooltipEntry.waveDirectionDeg)} · Chop ${waveTooltipEntry.windWaveHeightM.toFixed(1)}m${
                            waveTooltipEntry.steepnessBand ? ` · ${formatSteepnessRatio(waveTooltipEntry.steepnessRatio)} ${waveTooltipEntry.steepnessBand}` : ''
                          }`}
                        />
                      )}
                      <div
                        data-testid="forecast-wave-chart"
                        className="relative h-[265px] touch-none overflow-hidden rounded-sm bg-muted/15"
                        style={{ width: forecastChartWidth }}
                      >
                        <ComposedChart width={forecastChartWidth} height={265} margin={waveChartMargin}>
                          <XAxis
                            dataKey="hourOfDay"
                            type="number"
                            domain={[0, 23]}
                            allowDataOverflow
                            ticks={[0, 6, 12, 18]}
                            tickFormatter={(value: number) => waveLabelByHour.get(value) ?? ''}
                            axisLine={false}
                            tickLine={false}
                            tick={{ fontSize: Number(AXIS_LABEL_FONT_SIZE), fill: AXIS_LABEL_COLOR }}
                            height={RECHARTS_XAXIS_HEIGHT}
                          />
                          <YAxis domain={[0, waveMax]} hide />
                          <CartesianGrid horizontal vertical={false} stroke="hsl(var(--chart-grid) / 0.12)" />
                          <Area
                            data={waveChartData}
                            dataKey="waveHeightM"
                            type="monotone"
                            isAnimationActive={false}
                            dot={false}
                            stroke="none"
                            fill={`url(#${waveAreaGradientId})`}
                          />
                          <Line
                            data={waveChartData}
                            dataKey="swellWaveHeightM"
                            type="monotone"
                            isAnimationActive={false}
                            dot={false}
                            stroke="hsl(var(--chart-swell) / 0.85)"
                            strokeWidth={1.5}
                            strokeDasharray="2 3"
                          />
                          <Line
                            data={waveChartData}
                            dataKey="windWaveHeightM"
                            type="monotone"
                            isAnimationActive={false}
                            dot={false}
                            stroke="hsl(var(--chart-gust) / 0.85)"
                            strokeWidth={1.5}
                            strokeDasharray="4 3"
                          />
                          <Line
                            data={waveChartData}
                            dataKey="waveHeightM"
                            type="monotone"
                            isAnimationActive={false}
                            dot={false}
                            stroke={waveSteepnessStops.length > 0 ? `url(#${waveSteepnessGradientId})` : 'hsl(var(--chart-wave) / 0.9)'}
                            strokeWidth={2.4}
                          />
                          <Customized
                            component={() => (
                              <>
                                {waveHourTicks.map(({ entry, idx }) => {
                                  const x = hourlyXForHour(entry.hourOfDay)
                                  return (
                                    <g key={idx}>
                                      <WaveDirectionArrow cx={x} cy={18} directionDeg={entry.waveDirectionDeg} />
                                      {/* Period and steepness share one baseline, dropped clear of the
                                          arrow above it (which spans cy +/- len, i.e. 0..36 at cy=18):
                                          the plot starts at HOURLY_CHART_TOP (50) and a second row
                                          would land inside it. */}
                                      <text x={x} y={46} textAnchor="middle" fontSize={AXIS_LABEL_FONT_SIZE} fill={AXIS_LABEL_COLOR}>
                                        {entry.wavePeriodS.toFixed(1)}s
                                        {formatSteepnessRatio(entry.steepnessRatio) && (
                                          <tspan dx={5}>{formatSteepnessRatio(entry.steepnessRatio)}</tspan>
                                        )}
                                      </text>
                                    </g>
                                  )
                                })}
                              </>
                            )}
                          />
                        </ComposedChart>

                        <svg
                          ref={waveTooltip.svgRef}
                          viewBox={`0 0 ${forecastChartWidth} 265`}
                          preserveAspectRatio="none"
                          className="pointer-events-auto absolute inset-0 h-full w-full touch-none focus-visible:outline-hidden focus-visible:ring-2 focus-visible:ring-ring"
                          tabIndex={0}
                          role="img"
                          aria-label={`Wave height for ${selectedDay.dayName}, hourly. Use arrow keys to read values.`}
                          onPointerDown={waveTooltip.onPointerDown}
                          onPointerMove={waveTooltip.onPointerMove}
                          onPointerLeave={waveTooltip.onPointerLeave}
                          onKeyDown={waveTooltip.onKeyDown}
                          onFocus={waveTooltip.onFocus}
                        >
                          <defs>
                            <linearGradient id={waveAreaGradientId} x1="0" y1="0" x2="0" y2="1">
                              <stop offset="0%" stopColor="hsl(var(--chart-wave))" stopOpacity="0.28" />
                              <stop offset="100%" stopColor="hsl(var(--chart-wave))" stopOpacity="0.02" />
                            </linearGradient>
                            <linearGradient
                              id={waveSteepnessGradientId}
                              gradientUnits="userSpaceOnUse"
                              x1={hourlyChartLeft}
                              y1="0"
                              x2={hourlyChartRight}
                              y2="0"
                            >
                              {waveSteepnessStops.map((stop) => (
                                <stop
                                  key={stop.key}
                                  data-testid="forecast-wave-steepness-stop"
                                  data-band={stop.band}
                                  offset={`${stop.offset * 100}%`}
                                  stopColor={stop.colour}
                                />
                              ))}
                            </linearGradient>
                          </defs>
                          {/*
                            * Every built tick gets a label. The old
                            * Number.isInteger filter was right only for the
                            * fixed 0-3m frame it was written against; on a
                            * small frame it left [0, 1] and nothing else.
                            * waveTickStep already picks 1m ticks once the
                            * window goes past 2m, so integers-only falls out
                            * of the step rather than out of a filter.
                            */}
                          {waveAxisTicks.map((tick) => (
                            <text key={tick} x={6} y={axisTickLabelY(waveYFor, tick, waveChartTop, waveChartBottom)} fontSize={AXIS_LABEL_FONT_SIZE} fill={AXIS_LABEL_COLOR}>
                              {tick}
                            </text>
                          ))}
                          <line x1={hourlyChartLeft} y1={waveChartBottom} x2={hourlyChartRight} y2={waveChartBottom} stroke="hsl(var(--chart-grid) / 0.25)" strokeWidth="1" />

                          {waveTooltipEntry && waveTooltipEntry.hourOfDay >= 0 && (
                            <ChartTooltipMarker
                              x={hourlyXForHour(waveTooltipEntry.hourOfDay)}
                              y={waveYFor(Math.max(0, waveTooltipEntry.waveHeightM))}
                              color="hsl(var(--chart-wave) / 0.9)"
                            />
                          )}
                        </svg>
                      </div>
                    </div>
                    <p data-testid="forecast-wave-legend" className="mt-1 text-2xs text-muted-foreground">
                      <span className="inline-flex items-center gap-1 align-middle"><LegendSwatch color="hsl(var(--chart-wave) / 0.9)" strokeWidth={2.4} /> Total wave height (m)</span> · <span className="inline-flex items-center gap-1 align-middle"><LegendSwatch color="hsl(var(--chart-gust) / 0.85)" strokeWidth={1.5} dasharray="4 3" /> Wind wave (chop)</span> · <span className="inline-flex items-center gap-1 align-middle"><LegendSwatch color="hsl(var(--chart-swell) / 0.85)" strokeWidth={1.5} dasharray="2 3" /> Swell</span>
                    </p>
                  </>
                ) : (
                  <ChartUnavailableMessage testId="forecast-wave-unavailable" message="Wave forecast unavailable for this day" />
                )}
              </div>
            </div>


            <ForecastTideSection isImperial={unit === 'imperial'} dayOffset={selectedDayIndex} />

          </div>
        </div>
      </ForecastPanel>

      {hasUpperAirTrace && (
        <ForecastPanel
          testId="forecast-panel-upper-air"
          title={'Upper Air \u00b7 500mb'}
          spanLabel={`${upperAirDayStarts.length} days`}
          // The backend has sent updated_at/ttl_seconds on this feed all
          // along (backend/upper_air_providers.go); this panel was simply
          // the one place the hook and the props chain never carried them
          // through, so a dead upper-air feed rendered pixel-identical to a
          // live one.
          stale={upperAirStale}
          staleLabel={upperAirStaleLabel}
          intro={upperAirIntro}
        >
          <div className="px-2.5 py-2.5">
          {/* Both scales are readable off the two axes now, so the key is down
              to what the axes cannot say: the mechanism itself, what the grey
              shading means, what the red means, and that the line has been
              smoothed. */}
          <p data-testid="forecast-upper-air-key" className="mb-2 text-sm text-muted-foreground">
            Upper-level troughs feed surface lows. Expect stronger surface winds 24 to 48 hours later when heights drop into the shaded zone.{' '}
            {upperAirWindow?.present && (
              <>Grey band marks heights below {Math.round(upperAirWindow.lowQuintileM)} m. </>
            )}
            {upperAirTroughSpans.length > 0 && 'Red marks days with upper support for a surface low. '}
            Trace smoothed over 18 hours.
          </p>
          <div ref={upperAirCardRef} className="relative">
            {upperAirTooltipEntry && (
              <ChartTooltipBubble
                pixelX={upperAirTooltip.tooltipPixelX ?? 0}
                time={`${upperAirTickLabels.get(upperAirDayStarts.find((d) => d.dayKey === upperAirTooltipEntry.dayKey)?.idx ?? 0) ?? upperAirTooltipEntry.dayKey} ${String(upperAirTooltipEntry.localHour).padStart(2, '0')}:00`}
                primary={`${Math.round(upperAirTooltipEntry.height500M)} m`}
                secondary={`Jet ${Math.round(upperAirTooltipEntry.wind500Kts)} kt${
                  upperAirTooltipEntry.thicknessM > 0 ? ` \u00b7 Thickness ${Math.round(upperAirTooltipEntry.thicknessM)} m` : ''
                }${
                  upperAirTooltipEntry.surfaceGustKts === null
                    ? ''
                    : ` \u00b7 Surface gust ${Math.round(upperAirTooltipEntry.surfaceGustKts)} kts`
                }`}
              />
            )}
            <div
              data-testid="forecast-upper-air-chart"
              className="relative h-[265px] touch-none overflow-hidden rounded-sm bg-muted/15"
              style={{ width: upperAirChartWidth }}
            >
              {/* Bands sit behind the traces rather than on the pointer overlay,
                  so a wash never dims the line it is meant to explain. */}
              <svg
                viewBox={`0 0 ${upperAirChartWidth} 265`}
                preserveAspectRatio="none"
                className="pointer-events-none absolute inset-0 h-full w-full"
                style={{ zIndex: 0 }}
                aria-hidden
              >
                {upperAirWindow?.present && (
                  <rect
                    data-testid="forecast-upper-air-quintile-band"
                    x={hourlyChartLeft}
                    y={upperAirHeightYFor(upperAirWindow.lowQuintileM)}
                    width={Math.max(0, upperAirPlotWidth)}
                    height={Math.max(0, upperAirChartBottom - upperAirHeightYFor(upperAirWindow.lowQuintileM))}
                    fill="hsl(var(--chart-grid) / 0.14)"
                  />
                )}
              </svg>

              <div className="relative" style={{ zIndex: 1 }}>
                <ComposedChart width={upperAirChartWidth} height={265} data={upperAirChartData} margin={upperAirChartMargin}>
                  <XAxis
                    dataKey="idx"
                    type="number"
                    domain={[0, Math.max(1, upperAirChartData.length - 1)]}
                    allowDataOverflow
                    ticks={upperAirTicks}
                    tickFormatter={(value: number) => upperAirTickLabels.get(value) ?? ''}
                    axisLine={false}
                    tickLine={false}
                    tick={{ fontSize: Number(AXIS_LABEL_FONT_SIZE), fill: AXIS_LABEL_COLOR }}
                    height={RECHARTS_XAXIS_HEIGHT}
                  />
                  <YAxis yAxisId="height" domain={[upperAirMin, upperAirMax]} hide />
                  <YAxis yAxisId="gust" orientation="right" domain={[0, upperAirGustMax]} hide />
                  <CartesianGrid horizontal vertical={false} stroke="hsl(var(--chart-grid) / 0.12)" />
                  <Area
                    yAxisId="gust"
                    dataKey="surfaceGustKts"
                    type="monotone"
                    isAnimationActive={false}
                    connectNulls={false}
                    dot={false}
                    stroke="hsl(var(--chart-gust) / 0.5)"
                    strokeWidth={1.2}
                    fill={`url(#${upperAirGustGradientId})`}
                  />
                  <Line
                    yAxisId="height"
                    dataKey="height500Smoothed"
                    type="monotone"
                    isAnimationActive={false}
                    dot={false}
                    stroke={
                      upperAirTroughStops.length > 0
                        ? `url(#${upperAirTroughGradientId})`
                        : UPPER_AIR_TRACE_STROKE
                    }
                    strokeWidth={2.4}
                  />
                </ComposedChart>
              </div>

              <svg
                ref={upperAirTooltip.svgRef}
                viewBox={`0 0 ${upperAirChartWidth} 265`}
                preserveAspectRatio="none"
                className="pointer-events-auto absolute inset-0 h-full w-full touch-none focus-visible:outline-hidden focus-visible:ring-2 focus-visible:ring-ring"
                style={{ zIndex: 2 }}
                tabIndex={0}
                role="img"
                aria-label={`500mb height and jet wind over ${upperAirDayStarts.length} days. Use arrow keys to read values.`}
                onPointerDown={upperAirTooltip.onPointerDown}
                onPointerMove={upperAirTooltip.onPointerMove}
                onPointerLeave={upperAirTooltip.onPointerLeave}
                onKeyDown={upperAirTooltip.onKeyDown}
                onFocus={upperAirTooltip.onFocus}
              >
                <defs>
                  <linearGradient id={upperAirGustGradientId} x1="0" y1="0" x2="0" y2="1">
                    <stop offset="0%" stopColor="hsl(var(--chart-gust))" stopOpacity="0.22" />
                    <stop offset="100%" stopColor="hsl(var(--chart-gust))" stopOpacity="0.02" />
                  </linearGradient>
                  <linearGradient
                    id={upperAirTroughGradientId}
                    gradientUnits="userSpaceOnUse"
                    x1={hourlyChartLeft}
                    y1="0"
                    x2={upperAirChartRight}
                    y2="0"
                  >
                    {upperAirTroughStops.map((stop) => (
                      <stop
                        key={stop.key}
                        data-testid="forecast-upper-air-trough-stop"
                        data-band={stop.band}
                        offset={`${stop.offset * 100}%`}
                        stopColor={stop.colour}
                      />
                    ))}
                  </linearGradient>
                </defs>
                {/* Two series, two scales, one frame. Each axis is drawn in the
                    colour of the trace it belongs to, which is what says which
                    number goes with which line - and is what made the swatch
                    legend redundant. Unit on the top tick only, as the wind and
                    wave charts read. */}
                <text
                  data-testid="forecast-upper-air-height-tick"
                  x={6}
                  y={axisTickLabelY(upperAirHeightYFor, upperAirAxisHighM, upperAirChartTop, upperAirChartBottom)}
                  fontSize={AXIS_LABEL_FONT_SIZE}
                  fill="hsl(var(--chart-wave-label))"
                >
                  {Math.round(upperAirAxisHighM)} m
                </text>
                <text
                  data-testid="forecast-upper-air-height-tick"
                  x={6}
                  y={axisTickLabelY(upperAirHeightYFor, upperAirAxisLowM, upperAirChartTop, upperAirChartBottom)}
                  fontSize={AXIS_LABEL_FONT_SIZE}
                  fill="hsl(var(--chart-wave-label))"
                >
                  {Math.round(upperAirAxisLowM)}
                </text>
                {upperAirGustTicks.map((tick, i) => (
                  <text
                    key={tick}
                    data-testid="forecast-upper-air-gust-tick"
                    x={upperAirChartWidth - 6}
                    y={axisTickLabelY(upperAirGustYFor, tick, upperAirChartTop, upperAirChartBottom)}
                    textAnchor="end"
                    fontSize={AXIS_LABEL_FONT_SIZE}
                    fill="hsl(var(--chart-gust-label))"
                  >
                    {tick}{i === upperAirGustTicks.length - 1 ? ' kt' : ''}
                  </text>
                ))}
                <line
                  x1={hourlyChartLeft}
                  y1={upperAirChartBottom}
                  x2={upperAirChartRight}
                  y2={upperAirChartBottom}
                  stroke="hsl(var(--chart-grid) / 0.25)"
                  strokeWidth="1"
                />
                {upperAirTooltipEntry && (
                  <ChartTooltipMarker
                    x={upperAirXFor(upperAirTooltipEntry.idx)}
                    y={upperAirHeightYFor(upperAirTooltipEntry.height500M)}
                    color={UPPER_AIR_TRACE_STROKE}
                  />
                )}
              </svg>
            </div>
          </div>
          </div>
        </ForecastPanel>
      )}
    </div>
  )
}
