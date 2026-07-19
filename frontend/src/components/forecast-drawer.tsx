import { useEffect, useId, useMemo, useRef, useState } from 'react'
import type { ReactNode } from 'react'

import { Area, Bar, CartesianGrid, Cell, ComposedChart, Customized, Line, ReferenceDot, XAxis, YAxis } from 'recharts'

import { Cloud, CloudRain, Moon, Sun, Sunrise, Sunset, Wind, Waves } from 'lucide-react'

import { Button } from '@/components/ui/button'
import { ChartTooltipBubble, ChartTooltipMarker, ChartUnavailableMessage } from '@/components/chart-tooltip'
import { ForecastTideSection } from '@/components/forecast-tide-section'
import { WindWarningNotice } from '@/components/wind-warning-notice'
import type { ChartConfig } from '@/components/ui/chart'
import { useChartTooltip } from '@/hooks/use-chart-tooltip'
import type { WeatherHourlyCloudPoint, WeatherHourlyEntry, WeatherHourlyPrecipPoint, WeatherHourlyUVPoint, WeatherHourlyWindPoint } from '@/hooks/use-weather-forecast'
import type { WaveForecastDay } from '@/hooks/use-wave-forecast'
import type { ForecastWarnings } from '@/hooks/use-forecast-warnings'
import { useMeasuredWidth } from '@/hooks/use-measured-width'
import { compassPointFor } from '@/lib/format'
import { fahrenheitToCelsius } from '@/lib/units'

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
  precipitation: number
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
  isCached?: boolean
  updatedAt?: string | null
  ttlSeconds?: number | null
  onRetry?: () => void
  unit: 'imperial' | 'metric'
  activeForecastWarning?: ForecastWarnings | null
  waveDays?: WaveForecastDay[]
  waveSeaTemperatureF?: number | null
  waveLoading?: boolean
  waveError?: string | null
}

// Simple weather icon selector
function getWeatherIcon(condition: string, size: number = 40) {
  const iconProps = { size, className: 'text-amber-500' }
  
  if (condition.toLowerCase().includes('clear') || condition.toLowerCase().includes('sunny')) {
    return <Sun {...iconProps} className="text-yellow-500" />
  }
  if (condition.toLowerCase().includes('cloud')) {
    return <Cloud {...iconProps} className="text-gray-400" />
  }
  if (condition.toLowerCase().includes('rain') || condition.toLowerCase().includes('drizzle')) {
    return <CloudRain {...iconProps} className="text-blue-400" />
  }
  
  return <Cloud {...iconProps} className="text-gray-400" />
}

const MOON_PHASE_LABELS: Record<string, string> = {
  new: 'New Moon',
  waxingCrescent: 'Waxing Crescent',
  firstQuarter: 'First Quarter',
  waxingGibbous: 'Waxing Gibbous',
  full: 'Full Moon',
  waningGibbous: 'Waning Gibbous',
  lastQuarter: 'Last Quarter',
  waningCrescent: 'Waning Crescent',
}

const MOON_PHASE_EMOJI: Record<string, string> = {
  new: '🌑',
  waxingCrescent: '🌒',
  firstQuarter: '🌓',
  waxingGibbous: '🌔',
  full: '🌕',
  waningGibbous: '🌖',
  lastQuarter: '🌗',
  waningCrescent: '🌘',
}

function moonPhaseLabel(phase: string) {
  return MOON_PHASE_LABELS[phase] ?? phase
}

function moonPhaseEmoji(phase: string) {
  return MOON_PHASE_EMOJI[phase] ?? '🌙'
}

function getHourlyWeatherIcon(entry: WeatherHourlyEntry, isNight: boolean) {
  if (entry.kind === 'sunset') {
		return <Sunset size={24} className="text-gauge-primary" />
  }

  const normalized = entry.condition.toLowerCase()
  if (isNight && (normalized.includes('clear') || normalized.includes('sunny'))) {
		return <Moon size={24} className="text-gauge-secondary" />
  }

  return getWeatherIcon(entry.condition, 24)
}

// Same clear-night-becomes-moon override as getHourlyWeatherIcon, but keyed
// off the cloud chart's own per-hour isDaylight flag (sourced directly from
// WeatherKit) rather than re-deriving day/night from sunrise/sunset.
function getCloudChartIcon(condition: string, isDaylight: boolean, size: number) {
  const normalized = condition.toLowerCase()
  if (!isDaylight && (normalized.includes('clear') || normalized.includes('sunny'))) {
    return <Moon size={size} className="text-gauge-secondary" />
  }
  return getWeatherIcon(condition, size)
}

// Renders a single wind barb: a staff pointing toward the direction the wind
// is coming from, with feathers indicating speed (full barb = 10kt, half
// barb = 5kt, pennant = 50kt).
function WindBarb({ cx, cy, speedKts, directionDeg }: { cx: number; cy: number; speedKts: number; directionDeg: number }) {
  if (speedKts < 0 || directionDeg < 0) return null

  const color = 'rgba(37,99,235,0.85)'

  if (speedKts < 3) {
    return <circle data-testid="forecast-wind-barb" cx={cx} cy={cy} r="2.5" fill="none" stroke={color} strokeWidth="1.2" />
  }

  const angleRad = (directionDeg * Math.PI) / 180
  const dirX = Math.sin(angleRad)
  const dirY = -Math.cos(angleRad)
  const perpX = -dirY
  const perpY = dirX

  const staffLen = 12
  const barbLen = 5
  const barbSpacing = 3.5

  let remaining = Math.round(speedKts / 5) * 5
  const pennants = Math.floor(remaining / 50)
  remaining -= pennants * 50
  const fullBarbs = Math.floor(remaining / 10)
  remaining -= fullBarbs * 10
  const hasHalfBarb = remaining >= 5

  const features: ReactNode[] = []
  let pos = staffLen

  for (let i = 0; i < pennants; i++) {
    const baseX = cx + dirX * pos
    const baseY = cy + dirY * pos
    const innerPos = pos - barbSpacing
    const innerX = cx + dirX * innerPos
    const innerY = cy + dirY * innerPos
    const outerX = baseX + perpX * barbLen
    const outerY = baseY + perpY * barbLen
    features.push(<polygon key={`pennant-${i}`} points={`${baseX},${baseY} ${innerX},${innerY} ${outerX},${outerY}`} fill={color} />)
    pos -= barbSpacing
  }

  for (let i = 0; i < fullBarbs; i++) {
    const baseX = cx + dirX * pos
    const baseY = cy + dirY * pos
    const outerX = baseX + perpX * barbLen
    const outerY = baseY + perpY * barbLen
    features.push(<line key={`full-${i}`} x1={baseX} y1={baseY} x2={outerX} y2={outerY} stroke={color} strokeWidth="1.4" strokeLinecap="round" />)
    pos -= barbSpacing
  }

  if (hasHalfBarb) {
    const baseX = cx + dirX * pos
    const baseY = cy + dirY * pos
    const outerX = baseX + perpX * (barbLen / 2)
    const outerY = baseY + perpY * (barbLen / 2)
    features.push(<line key="half" x1={baseX} y1={baseY} x2={outerX} y2={outerY} stroke={color} strokeWidth="1.4" strokeLinecap="round" />)
  }

  return (
    <g data-testid="forecast-wind-barb">
      <line x1={cx} y1={cy} x2={cx + dirX * staffLen} y2={cy + dirY * staffLen} stroke={color} strokeWidth="1.4" strokeLinecap="round" />
      <circle cx={cx} cy={cy} r="1.4" fill={color} />
      {features}
    </g>
  )
}

// Renders a small double-headed arrow pointing in the direction the swell
// is heading (the API reports the direction it's coming from, so this is
// rotated 180 degrees to match the convention used by most swell forecasts).
function WaveDirectionArrow({ cx, cy, directionDeg }: { cx: number; cy: number; directionDeg: number }) {
  if (directionDeg < 0) return null

  const color = 'rgba(20,184,166,0.85)'
  const angleRad = ((directionDeg + 180) * Math.PI) / 180
  const dirX = Math.sin(angleRad)
  const dirY = -Math.cos(angleRad)
  const perpX = -dirY
  const perpY = dirX

  const len = 9
  const headSize = 4
  const tipX = cx + dirX * len
  const tipY = cy + dirY * len
  const tailX = cx - dirX * len
  const tailY = cy - dirY * len
  const headBaseX = tipX - dirX * headSize
  const headBaseY = tipY - dirY * headSize
  const leftX = headBaseX + perpX * (headSize * 0.6)
  const leftY = headBaseY + perpY * (headSize * 0.6)
  const rightX = headBaseX - perpX * (headSize * 0.6)
  const rightY = headBaseY - perpY * (headSize * 0.6)

  return (
    <g data-testid="forecast-wave-arrow">
      <line x1={tailX} y1={tailY} x2={tipX} y2={tipY} stroke={color} strokeWidth="1.4" strokeLinecap="round" />
      <polygon points={`${tipX},${tipY} ${leftX},${leftY} ${rightX},${rightY}`} fill={color} />
    </g>
  )
}

// Colors a precipitation intensity bar by Apple Weather's Light/Moderate/Heavy
// bands (mm/hr), using progressively darker blue.
function precipBarColor(intensityMm: number) {
  if (intensityMm >= 7.6) return 'rgba(29,78,216,0.9)'
  if (intensityMm >= 2.5) return 'rgba(59,130,246,0.85)'
  return 'rgba(147,197,253,0.85)'
}

// WHO UV Index scale color stops (Low/Moderate/High/Very High/Extreme),
// used to build the UV chart's gradient fill and line.
const UV_GRADIENT_STOPS = [
  { value: 0, color: 'rgb(34,197,94)' },
  { value: 2, color: 'rgb(34,197,94)' },
  { value: 3, color: 'rgb(250,204,21)' },
  { value: 5, color: 'rgb(250,204,21)' },
  { value: 6, color: 'rgb(249,115,22)' },
  { value: 7, color: 'rgb(249,115,22)' },
  { value: 8, color: 'rgb(239,68,68)' },
  { value: 10, color: 'rgb(239,68,68)' },
  { value: 11, color: 'rgb(168,85,247)' },
]

// Shared axis label styling, used for every value/tick/band label across the
// Wind, Wave, Precipitation and UV charts so they read consistently.
const AXIS_LABEL_FONT_SIZE = '10'
const AXIS_LABEL_COLOR = 'hsl(var(--muted-foreground))'

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
// 35..125 band before each chart's own per-value Y scaling is applied.
const HOURLY_CHART_TOP = 35
const HOURLY_CHART_BOTTOM = 125

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

// Left-axis band labels for the UV chart, positioned at each band's center value.
const UV_BAND_LABELS = [
  { value: 10, label: 'Extreme' },
  { value: 8.5, label: 'Very High' },
  { value: 6.5, label: 'High' },
  { value: 4, label: 'Moderate' },
  { value: 1, label: 'Low' },
]


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
  precipitation: 0,
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
  onRetry,
  unit,
  activeForecastWarning = null,
  waveDays = [],
  waveSeaTemperatureF = null,
  waveLoading = false,
  waveError = null,
}: ForecastDrawerProps) {
  const hasForecast = Boolean(forecast && forecast.length > 0)
  const [selectedDayIndex, setSelectedDayIndex] = useState(0)
  const uvLineGradientId = useId()
  const detailsCardRef = useRef<HTMLDivElement>(null)
  const dayTabsRowRef = useRef<HTMLDivElement>(null)

  const tempUnit = unit === 'metric' ? '°C' : '°F'
  const windUnit = 'kts'
  const displayTemp = (tempF: number) => (unit === 'metric' ? fahrenheitToCelsius(tempF) : tempF)
  const days = forecast.slice(0, 10)
  const hourlyEntries = hourlyToday.slice(0, 12)

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
  // A wave-provider outage must read as visibly different from "this
  // location just has no wave data" - if the whole wave fetch failed AND we
  // have no matching day for the selected forecast day, that's the outage
  // case (rendered via ChartUnavailableMessage further below); an empty
  // hourlyWave array with no error is the legitimate "no data" case.
  const waveUnavailableDueToError = Boolean(waveError) && selectedWaveDay === null

  const humidityPct = Math.max(35, Math.min(95, Math.round(45 + (selectedDay.precipitation * 0.4))))
  const visibilityNm = Math.max(1, 12 - (selectedDay.precipitation * 0.06))

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
  // and a bottom derived from the chart's own SVG viewBox height (170 for
  // Wave, 175 for the rest) so recharts' plot rectangle lands exactly where
  // the yFor-family pixel math and tooltip overlay already expect it.
  function hourlyChartMargin(viewboxHeight: number) {
    return {
      left: hourlyChartLeft,
      right: forecastChartWidth - hourlyChartRight,
      top: HOURLY_CHART_TOP,
      bottom: viewboxHeight - HOURLY_CHART_BOTTOM - RECHARTS_XAXIS_HEIGHT,
    }
  }

  const windSpeeds = windHourly.map((entry) => Math.max(0, entry.windSpeed))
  const windGusts = windHourly.map((entry) => Math.max(0, entry.windGust))
  const windDataMax = Math.max(0, ...windSpeeds, ...windGusts)
  const windMax = windDataMax <= 30 ? 30 : Math.ceil(windDataMax / 10) * 10
  const windChartTop = HOURLY_CHART_TOP
  const windChartBottom = HOURLY_CHART_BOTTOM
  const windYFor = (value: number) => windChartTop + (1 - value / windMax) * (windChartBottom - windChartTop)
  const windAxisTicks = Array.from({ length: windMax / 10 + 1 }, (_, idx) => idx * 10)
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
  const windChartMargin = hourlyChartMargin(175)

  const waveHeights = waveHourly.map((entry) => Math.max(0, entry.waveHeightM))
  const windWaveHeights = waveHourly.map((entry) => Math.max(0, entry.windWaveHeightM))
  const swellWaveHeights = waveHourly.map((entry) => Math.max(0, entry.swellWaveHeightM))
  const waveDataMax = Math.max(0, ...waveHeights, ...windWaveHeights, ...swellWaveHeights)
  const waveMax = waveDataMax <= 3 ? 3 : Math.ceil(waveDataMax)
  const waveChartTop = HOURLY_CHART_TOP
  const waveChartBottom = HOURLY_CHART_BOTTOM
  const waveYFor = (value: number) => waveChartTop + (1 - value / waveMax) * (waveChartBottom - waveChartTop)
  const waveAxisTicks = Array.from({ length: waveMax / 0.5 + 1 }, (_, idx) => idx * 0.5)
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
  const waveLabelByHour = useMemo(() => buildLabelByHour(waveHourly), [waveHourly])
  // Wave's viewBox is 170 tall (not the 175 the other hourly charts use), so
  // its bottom margin is derived from that height, not a hardcoded 175.
  const waveChartMargin = hourlyChartMargin(170)

  const precipIntensities = precipHourly.map((entry) => Math.max(0, entry.precipIntensityMm))
  const precipMax = Math.max(1, ...precipIntensities)
  const precipChartTop = HOURLY_CHART_TOP
  const precipChartBottom = HOURLY_CHART_BOTTOM
  const precipChanceYFor = (value: number) => precipChartTop + (1 - value / 100) * (precipChartBottom - precipChartTop)
  // Recharts render data: hourOfDay drives the shared numeric x-axis, and the
  // intensity/chance values reuse the same clamped arrays the tooltip/corner
  // labels already use, so the Bar/Line series and the manual overlay stay
  // in perfect sync.
  const precipChartData = useMemo(
    () =>
      precipHourly.map((entry) => ({
        hourOfDay: entry.hourOfDay,
        precipIntensityMm: Math.max(0, entry.precipIntensityMm),
        precipChancePct: Math.max(0, Math.min(100, entry.precipChancePct)),
      })),
    [precipHourly],
  )
  // Recharts' XAxis tickFormatter only receives the raw hourOfDay value, not
  // the hourly entry - look the real API-provided `.label` text up by hour
  // rather than re-deriving a 12-hour-clock string, so the tick text is
  // guaranteed to match whatever the backend sends.
  const precipLabelByHour = useMemo(() => buildLabelByHour(precipHourly), [precipHourly])
  // Margin numbers are derived from the exact same constants the tooltip
  // hook and manual overlay already use, so recharts' internal plot
  // rectangle lands on precisely (hourlyChartLeft, precipChartTop) -
  // (hourlyChartRight, precipChartBottom) in the shared 0..forecastChartWidth
  // x 0..175 coordinate space.
  const precipChartMargin = hourlyChartMargin(175)
  const precipBarWidth = precipHourly.length > 1 ? (hourlyChartWidth / (precipHourly.length - 1)) * 0.5 : 20
  const precipChartConfig: ChartConfig = {
    precipIntensityMm: { label: 'Intensity (mm/hr)', color: 'rgba(59,130,246,0.85)' },
    precipChancePct: { label: 'Chance of precip (%)', color: 'rgba(245,158,11,0.85)' },
  }

  const cloudTemps = cloudHourly.map((entry) => displayTemp(entry.temperatureF))
  const cloudTempMax = cloudTemps.length > 0 ? Math.max(...cloudTemps) : 0
  const cloudTempMin = cloudTemps.length > 0 ? Math.min(...cloudTemps) : 0
  const cloudTempRange = Math.max(1, cloudTempMax - cloudTempMin)
  const cloudChartTop = HOURLY_CHART_TOP
  const cloudChartBottom = HOURLY_CHART_BOTTOM
  const cloudYFor = (value: number) =>
    cloudChartBottom - ((value - cloudTempMin) / cloudTempRange) * (cloudChartBottom - cloudChartTop)
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
  const uvYFor = (value: number) => cloudChartTop + (1 - value / uvMax) * (cloudChartBottom - cloudChartTop)
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

  // Recharts render data for the temperature Area - displayTemp-converted
  // (metric/imperial) so the plotted values match cloudTemps/cloudYFor
  // exactly, since recharts can't perform that unit conversion itself.
  const cloudChartData = useMemo(
    () =>
      cloudHourly.map((entry) => ({
        hourOfDay: entry.hourOfDay,
        displayTemperature: unit === 'metric' ? fahrenheitToCelsius(entry.temperatureF) : entry.temperatureF,
      })),
    [cloudHourly, unit],
  )
  // UV points have no hourOfDay of their own (WeatherHourlyUVPoint only
  // carries label/uvIndex) - they're aligned to the cloud chart's hourly
  // array purely by array position, so idx doubles as hourOfDay here (see
  // the UV tooltip marker below, which is the one hourlyXForHour call site
  // that deliberately keeps using activeIndex instead of a real hourOfDay).
  const uvChartData = useMemo(
    () => uvHourly.map((entry, idx) => ({ hourOfDay: idx, uvIndex: Math.max(0, entry.uvIndex) })),
    [uvHourly],
  )
  // Recharts' XAxis tickFormatter only receives the raw hourOfDay value, not
  // the hourly entry - look the real API-provided `.label` text up by hour
  // rather than re-deriving a 12-hour-clock string.
  const cloudLabelByHour = useMemo(() => buildLabelByHour(cloudHourly), [cloudHourly])
  const cloudChartMargin = hourlyChartMargin(175)
  const cloudChartConfig: ChartConfig = {
    displayTemperature: { label: `Temperature (${tempUnit})`, color: 'rgba(217,119,6,0.9)' },
    uvIndex: { label: 'UV Index', color: 'rgb(249,115,22)' },
  }

  const windTooltip = useChartTooltip(windHourly.length, selectedDayIndex, hourlyChartLeft, hourlyChartRight)
  const waveTooltip = useChartTooltip(waveHourly.length, selectedDayIndex, hourlyChartLeft, hourlyChartRight)
  const precipTooltip = useChartTooltip(precipHourly.length, selectedDayIndex, hourlyChartLeft, hourlyChartRight)
  const cloudTooltip = useChartTooltip(cloudHourly.length, selectedDayIndex, hourlyChartLeft, hourlyChartRight)

  const windTooltipEntry = windTooltip.activeIndex === null ? null : windHourly[windTooltip.activeIndex] ?? null
  const waveTooltipEntry = waveTooltip.activeIndex === null ? null : waveHourly[waveTooltip.activeIndex] ?? null
  const precipTooltipEntry = precipTooltip.activeIndex === null ? null : precipHourly[precipTooltip.activeIndex] ?? null
  const cloudTooltipEntry = cloudTooltip.activeIndex === null ? null : cloudHourly[cloudTooltip.activeIndex] ?? null
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
      <div className="rounded-lg border border-amber-500/30 bg-amber-500/5 px-4 py-8 text-center">
        <p className="text-xs uppercase tracking-[0.16em] text-amber-700">Forecast Offline</p>
        <p className="mt-2 font-medium text-foreground">Unable to load forecast data right now</p>
        <p className="mt-1 text-xs text-muted-foreground">{error}</p>
        <div className="mt-4 flex justify-center">
          <Button type="button" size="sm" variant="outline" className="h-9 min-w-24" onClick={onRetry}>
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
    <div className="overflow-hidden rounded-[26px] border border-gauge-secondary/15 bg-[linear-gradient(180deg,rgba(255,249,239,0.96),rgba(238,245,243,0.92))] shadow-[0_14px_32px_rgba(38,84,79,0.08)]">
      <div className="border-b border-gauge-secondary/14 bg-[linear-gradient(90deg,rgba(199,137,0,0.10),rgba(52,116,109,0.08))] px-4 py-3.5">
        <p className="pr-2 text-[15px] font-medium leading-relaxed text-foreground/90">
          {summary ?? "Today's hourly forecast"}
          <WindWarningNotice warnings={activeForecastWarning} />
        </p>
      </div>
      <div className="px-2.5 py-2.5">
        <div className="flex gap-1.5 overflow-x-auto pb-1">
          {hourlyEntries.map((entry, idx) => {
            const nightMode = hourlyEntries.slice(0, idx).some((item) => item.kind === 'sunset')
            const isNowEntry = entry.label === 'Now' && entry.kind === 'forecast'
            const displayTemperature = entry.temperatureF >= 0 ? Math.round(displayTemp(entry.temperatureF)) : null

            return (
              <div
                key={`${entry.kind}-${entry.label}-${idx}`}
                className={`relative flex min-w-[84px] flex-col items-center rounded-[20px] border px-2.5 py-3.5 text-center ${entry.kind === 'sunset' ? 'border-gauge-primary/20 bg-gauge-primary/10' : nightMode ? 'border-gauge-secondary/20 bg-gauge-secondary/10' : 'border-border/70 bg-card/80'} ${isNowEntry ? 'shadow-[0_0_0_1px_rgba(199,137,0,0.22),0_10px_18px_rgba(199,137,0,0.10)]' : ''}`}
              >
                {isNowEntry && (
                  <span className="absolute left-1/2 top-1.5 h-1.5 w-1.5 -translate-x-1/2 rounded-full bg-gauge-primary" />
                )}
                <p className={`text-[15px] font-semibold tabular-nums ${isNowEntry ? 'text-gauge-primary' : nightMode ? 'text-gauge-secondary' : 'text-foreground/75'}`}>{entry.label}</p>
                <div className="mt-3.5 flex h-8 items-center justify-center">
                  {getHourlyWeatherIcon(entry, nightMode)}
                </div>
                <p className={`mt-4 ${entry.kind === 'sunset' ? 'text-[13px] font-semibold uppercase tracking-[0.08em] text-gauge-primary' : nightMode ? 'font-display text-[2rem] leading-none text-gauge-secondary' : 'font-display text-[2rem] leading-none text-foreground'}`}>
                  {entry.kind === 'sunset' ? 'Sunset' : displayTemperature !== null ? `${displayTemperature}°` : '—'}
                </p>
                {entry.kind === 'forecast' && entry.windSpeedKts >= 0 && (
                  <p className={`mt-1 whitespace-nowrap text-[11px] font-semibold ${nightMode ? 'text-gauge-secondary/80' : 'text-gauge-secondary'}`}>
                    {Math.round(entry.windSpeedKts)}kts {entry.windDirection}
                  </p>
                )}
                {nightMode && entry.kind === 'forecast' && (
                  <span className="mt-1 text-[10px] font-medium uppercase tracking-[0.12em] text-gauge-secondary/80">Night</span>
                )}
              </div>
            )
          })}
        </div>
      </div>
    </div>
      )}

      <div>
        <h3 className="mb-3 text-xs font-semibold uppercase tracking-[0.16em] text-muted-foreground">
          10-Day Forecast
        </h3>
        <div className="flex flex-col gap-3">
          <div ref={dayTabsRowRef} className="sticky top-0 z-10 flex gap-1.5 overflow-x-auto bg-card pb-2 pt-0.5">
            {days.map((day, idx) => (
              <button
                key={idx}
                type="button"
                className={`min-w-[150px] shrink-0 rounded-lg border px-2.5 py-2 text-left transition-colors ${idx === selectedDayIndex ? 'border-primary/50 bg-primary/5' : 'border-border/60 bg-background/40 hover:bg-muted/30'}`}
                aria-label={`Select forecast day ${day.dayName} ${day.date}`}
                onClick={() => selectDay(idx)}
              >
                <div className="flex items-center justify-between gap-2">
                  <div>
                    <p className="text-[11px] font-semibold uppercase text-muted-foreground">
                      {idx === 0 ? 'Today' : day.dayName.slice(0, 3)}
                    </p>
                    <p className="text-[10px] text-muted-foreground">{day.date}</p>
                  </div>
                  {getWeatherIcon(day.condition, 26)}
                </div>

                <p className="mt-1 truncate text-[10px] font-medium text-foreground">
                  {day.condition}
                </p>

                <div className="mt-1.5 flex items-center justify-between gap-2">
                  <div className="flex items-center gap-1">
                    <span className="font-display text-lg leading-none text-gauge-primary">
                      {Math.round(displayTemp(day.high))}
                    </span>
                    <span className="text-[9px] text-muted-foreground">
                      {tempUnit}
                    </span>
                    <span className="text-sm text-muted-foreground">/</span>
                    <span className="font-display text-sm text-muted-foreground">{Math.round(displayTemp(day.low))}</span>
                  </div>
                  <p className="text-[10px] text-muted-foreground">{Math.round(day.precipitation)}% precip</p>
                </div>

                <p className="mt-1 text-[10px] font-semibold text-gauge-secondary">{Math.round(day.windSpeed)}{windUnit} {day.windDirection}</p>
              </button>
            ))}
          </div>

          <div ref={detailsCardRef} className="rounded-lg border bg-background/60 p-3">
            <div className="mb-3 flex flex-wrap items-center gap-3">
              <div className="flex items-end gap-1">
                {getWeatherIcon(selectedDay.condition, 22)}
                <span className="font-display text-4xl leading-none text-gauge-primary">{Math.round(displayTemp(selectedDay.high))}</span>
                <span className="pb-1 text-lg text-muted-foreground">{tempUnit}</span>
              </div>
              <p className="text-sm font-semibold uppercase tracking-[0.08em] text-foreground">{selectedDay.condition}</p>
              <div className="flex flex-wrap gap-2 text-[11px]">
                <span className="rounded bg-muted/50 px-2 py-1">Wind <span data-testid="forecast-selected-wind" className="font-semibold text-gauge-secondary">{selectedDay.windSpeed.toFixed(1)} {windUnit}</span></span>
                <span className="rounded bg-muted/50 px-2 py-1">Gusts <span data-testid="forecast-selected-gust" className="font-semibold text-amber-600">{selectedDay.windGust.toFixed(1)} {windUnit}</span></span>
                <span className="rounded bg-muted/50 px-2 py-1">Precip <span className="font-semibold">{Math.round(selectedDay.precipitation)}%</span></span>
                <span className="rounded bg-muted/50 px-2 py-1">Humidity <span className="font-semibold">{humidityPct}%</span></span>
                <span className="rounded bg-muted/50 px-2 py-1">Visibility <span className="font-semibold">{visibilityNm.toFixed(1)} nm</span></span>
                <span className="rounded bg-muted/50 px-2 py-1">UV Index <span className="font-semibold text-gauge-secondary">{uvIndex}</span></span>
                {selectedDay.sunriseTime && (
                  <span className="flex items-center gap-1.5 rounded bg-muted/50 px-2 py-1">
                    <Sunrise size={13} className="text-amber-500" />
                    Sunrise <span className="font-semibold">{selectedDay.sunriseTime}</span>
                  </span>
                )}
                {selectedDay.sunsetTime && (
                  <span className="flex items-center gap-1.5 rounded bg-muted/50 px-2 py-1">
                    <Sunset size={13} className="text-gauge-primary" />
                    Sunset <span className="font-semibold">{selectedDay.sunsetTime}</span>
                  </span>
                )}
                {selectedDay.moonPhase && (
                  <span className="flex items-center gap-1.5 rounded bg-muted/50 px-2 py-1">
                    <span aria-hidden>{moonPhaseEmoji(selectedDay.moonPhase)}</span>
                    Moon <span className="font-semibold">{moonPhaseLabel(selectedDay.moonPhase)}</span>
                  </span>
                )}
              </div>
            </div>

            <div ref={chartCardRef} className="rounded-md border bg-card/70 p-2">
              <h4 className="mb-2 flex items-center gap-1.5 text-[11px] font-semibold uppercase tracking-[0.12em] text-muted-foreground">
                <Cloud size={13} className="text-gauge-secondary" /> Cloud & Temperature
              </h4>
              {cloudHourly.length > 0 ? (
                <>
                  <p className="mb-2 text-base text-foreground/80">
                    {uvProtectionStart && uvProtectionEnd
                      ? `Sun protection recommended from ${uvProtectionStart} to ${uvProtectionEnd}.`
                      : 'No sun protection needed.'}
                  </p>
                  <div className="relative">
                  {cloudTooltipEntry && (
                    <ChartTooltipBubble
                      pixelX={cloudTooltip.tooltipPixelX ?? 0}
                      time={cloudTooltipEntry.label}
                      primary={`${Math.round(displayTemp(cloudTooltipEntry.temperatureF))}${tempUnit}`}
                      secondary={cloudTooltipEntry.condition}
                      tertiary={uvTooltipEntry ? `UV ${Math.round(uvTooltipEntry.uvIndex)} · ${uvRiskLabel(uvTooltipEntry.uvIndex)}` : undefined}
                    />
                  )}
                  <div
                    data-testid="forecast-cloud-chart"
                    className="relative h-[175px] touch-none overflow-hidden rounded bg-muted/15"
                    style={{ width: forecastChartWidth }}
                  >
                    <ComposedChart width={forecastChartWidth} height={175} margin={cloudChartMargin}>
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
                      <CartesianGrid vertical horizontal={false} stroke="rgba(80,98,118,0.12)" />
                      <YAxis domain={[cloudTempMin, cloudTempMin + cloudTempRange]} hide />
                      <YAxis yAxisId="uv" domain={[0, uvMax]} hide />
                      <Area
                        data={cloudChartData}
                        dataKey="displayTemperature"
                        type="linear"
                        isAnimationActive={false}
                        dot={false}
                        stroke={cloudChartConfig.displayTemperature.color}
                        strokeWidth={2.4}
                        fill="rgba(217,119,6,0.12)"
                      />
                      <Line
                        data={uvChartData}
                        dataKey="uvIndex"
                        yAxisId="uv"
                        type="linear"
                        isAnimationActive={false}
                        dot={false}
                        stroke={`url(#${uvLineGradientId})`}
                        strokeWidth={2}
                      />
                      {cloudMinIdx >= 0 && (
                        <ReferenceDot
                          x={cloudHourly[cloudMinIdx].hourOfDay}
                          y={cloudTempMin}
                          r={3}
                          fill="rgba(217,119,6,0.95)"
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
                          fill="rgba(217,119,6,0.95)"
                          stroke="none"
                          isFront
                          label={{ value: 'H', position: 'top', fill: AXIS_LABEL_COLOR, fontSize: Number(AXIS_LABEL_FONT_SIZE) }}
                        />
                      )}
                    </ComposedChart>

                    <svg
                      ref={cloudTooltip.svgRef}
                      viewBox={`0 0 ${forecastChartWidth} 175`}
                      preserveAspectRatio="none"
                      className="pointer-events-auto absolute inset-0 h-full w-full touch-none"
                      onPointerDown={cloudTooltip.onPointerDown}
                      onPointerMove={cloudTooltip.onPointerMove}
                      onPointerLeave={cloudTooltip.onPointerLeave}
                    >
                      <defs>
                        <linearGradient id={uvLineGradientId} gradientUnits="userSpaceOnUse" x1={0} y1={uvYFor(0)} x2={0} y2={uvYFor(uvMax)}>
                          {UV_GRADIENT_STOPS.map((stop) => (
                            <stop key={stop.value} offset={stop.value / uvMax} stopColor={stop.color} stopOpacity="0.9" />
                          ))}
                        </linearGradient>
                      </defs>

                      <text x={6} y={40} fontSize={AXIS_LABEL_FONT_SIZE} fill={AXIS_LABEL_COLOR}>{Math.round(cloudTempMax)}{tempUnit}</text>
                      <text x={6} y={123} fontSize={AXIS_LABEL_FONT_SIZE} fill={AXIS_LABEL_COLOR}>{Math.round(cloudTempMin)}{tempUnit}</text>
                      {UV_BAND_LABELS.map((band) => (
                        <text key={band.label} x={forecastChartWidth - 6} y={uvYFor(band.value)} textAnchor="end" fontSize={AXIS_LABEL_FONT_SIZE} fill={AXIS_LABEL_COLOR}>{band.label}</text>
                      ))}
                      <line x1={hourlyChartLeft} y1={cloudChartBottom} x2={hourlyChartRight} y2={cloudChartBottom} stroke="rgba(80,98,118,0.25)" strokeWidth="1" />

                      {cloudTooltipEntry && cloudTooltipEntry.hourOfDay >= 0 && (
                        <ChartTooltipMarker
                          x={hourlyXForHour(cloudTooltipEntry.hourOfDay)}
                          y={cloudYFor(displayTemp(cloudTooltipEntry.temperatureF))}
                          color="rgba(217,119,6,0.95)"
                        />
                      )}
                      {uvTooltipEntry && (
                        <ChartTooltipMarker
                          // UV has no real per-entry hourOfDay (see uvChartData above) -
                          // its own series is keyed on array index, so activeIndex IS
                          // the real domain value here, unlike the other 4 marker call
                          // sites above which look up a genuine hourOfDay.
                          x={hourlyXForHour(cloudTooltip.activeIndex ?? 0)}
                          y={uvYFor(Math.max(0, uvTooltipEntry.uvIndex))}
                          color="rgb(249,115,22)"
                        />
                      )}
                    </svg>
                  </div>

                  <div className="pointer-events-none absolute inset-x-0 top-0 flex h-[35px] items-center" style={{ left: `${(hourlyChartLeft / forecastChartWidth) * 100}%`, right: `${100 - (hourlyChartRight / forecastChartWidth) * 100}%` }}>
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
                </>
              ) : (
                <ChartUnavailableMessage testId="forecast-cloud-unavailable" message="Cloud & temperature forecast unavailable for this day" />
              )}
            </div>

            <div className="rounded-md border bg-card/70 p-2">
              <h4 className="mb-2 flex items-center gap-1.5 text-[11px] font-semibold uppercase tracking-[0.12em] text-muted-foreground">
                <Wind size={13} className="text-gauge-secondary" /> Wind
              </h4>
              {windHourly.length > 0 ? (
                <>
                  {selectedDay.windSummary && (
                    <p className="mb-2 text-base text-foreground/80">{selectedDay.windSummary}</p>
                  )}
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
                      className="relative h-[175px] touch-none overflow-hidden rounded bg-muted/15"
                      style={{ width: forecastChartWidth }}
                    >
                      <ComposedChart width={forecastChartWidth} height={175} margin={windChartMargin}>
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
                        <Area
                          data={windChartData}
                          dataKey="windSpeed"
                          type="linear"
                          isAnimationActive={false}
                          dot={false}
                          stroke="none"
                          fill="rgba(37,99,235,0.12)"
                        />
                        <Line
                          data={windChartData}
                          dataKey="windGust"
                          type="linear"
                          isAnimationActive={false}
                          dot={false}
                          stroke="rgba(245,158,11,0.75)"
                          strokeWidth={1.5}
                          strokeDasharray="4 3"
                        />
                        <Line
                          data={windChartData}
                          dataKey="windSpeed"
                          type="linear"
                          isAnimationActive={false}
                          dot={false}
                          stroke="rgba(37,99,235,0.95)"
                          strokeWidth={2.4}
                        />
                        <Customized
                          component={() => (
                            <>
                              {windHourTicks.map(({ entry, idx }) => (
                                <WindBarb
                                  key={idx}
                                  cx={hourlyXForHour(entry.hourOfDay)}
                                  cy={16}
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
                        viewBox={`0 0 ${forecastChartWidth} 175`}
                        preserveAspectRatio="none"
                        className="pointer-events-auto absolute inset-0 h-full w-full touch-none"
                        onPointerDown={windTooltip.onPointerDown}
                        onPointerMove={windTooltip.onPointerMove}
                        onPointerLeave={windTooltip.onPointerLeave}
                      >
                        {windAxisTicks.map((tick) => (
                          <text key={tick} x={6} y={axisTickLabelY(windYFor, tick, windChartTop, windChartBottom)} fontSize={AXIS_LABEL_FONT_SIZE} fill={AXIS_LABEL_COLOR}>
                            {tick}{tick === windMax ? ` ${windUnit}` : ''}
                          </text>
                        ))}
                        <line x1={hourlyChartLeft} y1={windChartBottom} x2={hourlyChartRight} y2={windChartBottom} stroke="rgba(80,98,118,0.25)" strokeWidth="1" />

                        {windTooltipEntry && windTooltipEntry.hourOfDay >= 0 && (
                          <ChartTooltipMarker
                            x={hourlyXForHour(windTooltipEntry.hourOfDay)}
                            y={windYFor(Math.max(0, windTooltipEntry.windSpeed))}
                            color="rgba(37,99,235,0.95)"
                          />
                        )}
                      </svg>
                    </div>
                  </div>
                  <p className="mt-1 text-[10px] text-muted-foreground">
                    <span className="text-blue-600">— Wind</span> · <span className="text-amber-600">- - Gusts</span> ({windUnit}) · barbs show direction the wind is coming from (full feather = 10kt, half = 5kt)
                  </p>
                </>
              ) : (
                <ChartUnavailableMessage testId="forecast-wind-unavailable" message="Wind forecast unavailable for this day" />
              )}
            </div>

            <div className="mt-3 rounded-md border bg-card/70 p-2">
              <h4 className="mb-2 flex items-center justify-between gap-1.5">
                <span className="flex items-center gap-1.5 text-[11px] font-semibold uppercase tracking-[0.12em] text-muted-foreground">
                  <Waves size={13} className="text-gauge-secondary" /> Wave
                </span>
                {waveSeaTemperatureF !== null && (
                  <span className="text-[11px] text-muted-foreground">
                    Sea {Math.round(displayTemp(waveSeaTemperatureF))}{tempUnit}
                  </span>
                )}
              </h4>
              {waveLoading ? (
                <p className="py-6 text-center text-xs text-muted-foreground" data-testid="forecast-wave-loading">
                  Loading wave forecast...
                </p>
              ) : waveUnavailableDueToError ? (
                <ChartUnavailableMessage testId="forecast-wave-error" message="Wave data unavailable" />
              ) : waveHourly.length > 0 ? (
                <>
                  {selectedWaveDay?.waveSummary && (
                    <p className="mb-2 text-base text-foreground/80">{selectedWaveDay.waveSummary}</p>
                  )}
                  <div className="relative">
                    {waveTooltipEntry && (
                      <ChartTooltipBubble
                        pixelX={waveTooltip.tooltipPixelX ?? 0}
                        time={waveTooltipEntry.label}
                        primary={`${waveTooltipEntry.waveHeightM.toFixed(1)} m`}
                        secondary={`Swell ${waveTooltipEntry.swellWaveHeightM.toFixed(1)}m from ${compassLabel(waveTooltipEntry.waveDirectionDeg)} · Chop ${waveTooltipEntry.windWaveHeightM.toFixed(1)}m`}
                      />
                    )}
                    <div
                      data-testid="forecast-wave-chart"
                      className="relative h-[170px] touch-none overflow-hidden rounded bg-muted/15"
                      style={{ width: forecastChartWidth }}
                    >
                      <ComposedChart width={forecastChartWidth} height={170} margin={waveChartMargin}>
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
                        <Area
                          data={waveChartData}
                          dataKey="waveHeightM"
                          type="linear"
                          isAnimationActive={false}
                          dot={false}
                          stroke="none"
                          fill="rgba(20,184,166,0.12)"
                        />
                        <Line
                          data={waveChartData}
                          dataKey="swellWaveHeightM"
                          type="linear"
                          isAnimationActive={false}
                          dot={false}
                          stroke="rgba(139,92,246,0.85)"
                          strokeWidth={1.5}
                          strokeDasharray="2 3"
                        />
                        <Line
                          data={waveChartData}
                          dataKey="windWaveHeightM"
                          type="linear"
                          isAnimationActive={false}
                          dot={false}
                          stroke="rgba(245,158,11,0.85)"
                          strokeWidth={1.5}
                          strokeDasharray="4 3"
                        />
                        <Line
                          data={waveChartData}
                          dataKey="waveHeightM"
                          type="linear"
                          isAnimationActive={false}
                          dot={false}
                          stroke="rgba(20,184,166,0.9)"
                          strokeWidth={2.4}
                        />
                        <Customized
                          component={() => (
                            <>
                              {waveHourTicks.map(({ entry, idx }) => {
                                const x = hourlyXForHour(entry.hourOfDay)
                                return (
                                  <g key={idx}>
                                    <WaveDirectionArrow cx={x} cy={16} directionDeg={entry.waveDirectionDeg} />
                                    <text x={x} y={31} textAnchor="middle" fontSize={AXIS_LABEL_FONT_SIZE} fill={AXIS_LABEL_COLOR}>{entry.wavePeriodS.toFixed(1)}s</text>
                                  </g>
                                )
                              })}
                            </>
                          )}
                        />
                      </ComposedChart>

                      <svg
                        ref={waveTooltip.svgRef}
                        viewBox={`0 0 ${forecastChartWidth} 170`}
                        preserveAspectRatio="none"
                        className="pointer-events-auto absolute inset-0 h-full w-full touch-none"
                        onPointerDown={waveTooltip.onPointerDown}
                        onPointerMove={waveTooltip.onPointerMove}
                        onPointerLeave={waveTooltip.onPointerLeave}
                      >
                        {waveAxisTicks.filter((tick) => Number.isInteger(tick)).map((tick) => (
                          <text key={tick} x={6} y={axisTickLabelY(waveYFor, tick, waveChartTop, waveChartBottom)} fontSize={AXIS_LABEL_FONT_SIZE} fill={AXIS_LABEL_COLOR}>
                            {tick}{tick === waveMax ? ' m' : ''}
                          </text>
                        ))}
                        <line x1={hourlyChartLeft} y1={waveChartBottom} x2={hourlyChartRight} y2={waveChartBottom} stroke="rgba(80,98,118,0.25)" strokeWidth="1" />

                        {waveTooltipEntry && waveTooltipEntry.hourOfDay >= 0 && (
                          <ChartTooltipMarker
                            x={hourlyXForHour(waveTooltipEntry.hourOfDay)}
                            y={waveYFor(Math.max(0, waveTooltipEntry.waveHeightM))}
                            color="rgba(20,184,166,0.9)"
                          />
                        )}
                      </svg>
                    </div>
                  </div>
                  <p className="mt-1 text-[10px] text-muted-foreground">
                    <span className="text-gauge-secondary">— Total wave height (m)</span> · <span className="text-amber-600">- - Wind wave (chop)</span> · <span className="text-violet-600">·· Swell</span> · arrows show direction the swell is heading, with period (sec) below each
                  </p>
                </>
              ) : (
                <ChartUnavailableMessage testId="forecast-wave-unavailable" message="Wave forecast unavailable for this day" />
              )}
            </div>

            <div className="mt-3 rounded-md border bg-card/70 p-2">
              <h4 className="mb-2 flex items-center gap-1.5 text-[11px] font-semibold uppercase tracking-[0.12em] text-muted-foreground">
                <CloudRain size={13} className="text-gauge-secondary" /> Precipitation
              </h4>
              {precipHourly.length > 0 ? (
                <>
                  {selectedDay.precipitationSummary && (
                    <p className="mb-2 text-base text-foreground/80">{selectedDay.precipitationSummary}</p>
                  )}
                  <div className="relative">
                    {precipTooltipEntry && (
                      <ChartTooltipBubble
                        pixelX={precipTooltip.tooltipPixelX ?? 0}
                        time={precipTooltipEntry.label}
                        primary={`${Math.round(precipTooltipEntry.precipChancePct)}% chance`}
                        secondary={`${precipTooltipEntry.precipIntensityMm.toFixed(1)} mm/hr`}
                      />
                    )}
                    <div
                      data-testid="forecast-precip-chart"
                      className="relative h-[175px] touch-none overflow-hidden rounded bg-muted/15"
                      style={{ width: forecastChartWidth }}
                    >
                      <ComposedChart width={forecastChartWidth} height={175} data={precipChartData} margin={precipChartMargin}>
                        <XAxis
                          dataKey="hourOfDay"
                          type="number"
                          domain={[0, 23]}
                          allowDataOverflow
                          ticks={[0, 6, 12, 18]}
                          tickFormatter={(value: number) => precipLabelByHour.get(value) ?? ''}
                          axisLine={false}
                          tickLine={false}
                          tick={{ fontSize: Number(AXIS_LABEL_FONT_SIZE), fill: AXIS_LABEL_COLOR }}
                          height={RECHARTS_XAXIS_HEIGHT}
                        />
                        <YAxis domain={[0, precipMax]} hide />
                        <YAxis yAxisId="chance" domain={[0, 100]} orientation="right" hide />
                        <Bar dataKey="precipIntensityMm" barSize={precipBarWidth} isAnimationActive={false}>
                          {precipChartData.map((entry, idx) => (
                            <Cell key={idx} data-testid="forecast-precip-bar" fill={precipBarColor(entry.precipIntensityMm)} />
                          ))}
                        </Bar>
                        <Line
                          dataKey="precipChancePct"
                          yAxisId="chance"
                          type="linear"
                          dot={false}
                          isAnimationActive={false}
                          stroke={precipChartConfig.precipChancePct.color}
                          strokeWidth={2.4}
                        />
                      </ComposedChart>

                      <svg
                        ref={precipTooltip.svgRef}
                        viewBox={`0 0 ${forecastChartWidth} 175`}
                        preserveAspectRatio="none"
                        className="pointer-events-auto absolute inset-0 h-full w-full touch-none"
                        onPointerDown={precipTooltip.onPointerDown}
                        onPointerMove={precipTooltip.onPointerMove}
                        onPointerLeave={precipTooltip.onPointerLeave}
                      >
                        <text x={6} y={40} fontSize={AXIS_LABEL_FONT_SIZE} fill={AXIS_LABEL_COLOR}>{precipMax.toFixed(1)} mm/hr</text>
                        <text x={6} y={123} fontSize={AXIS_LABEL_FONT_SIZE} fill={AXIS_LABEL_COLOR}>0</text>
                        <text x={forecastChartWidth - 6} y={40} textAnchor="end" fontSize={AXIS_LABEL_FONT_SIZE} fill={AXIS_LABEL_COLOR}>100%</text>
                        <text x={forecastChartWidth - 6} y={123} textAnchor="end" fontSize={AXIS_LABEL_FONT_SIZE} fill={AXIS_LABEL_COLOR}>0%</text>
                        <line x1={hourlyChartLeft} y1={precipChartBottom} x2={hourlyChartRight} y2={precipChartBottom} stroke="rgba(80,98,118,0.25)" strokeWidth="1" />

                        {precipTooltipEntry && precipTooltipEntry.hourOfDay >= 0 && (
                          <ChartTooltipMarker
                            x={hourlyXForHour(precipTooltipEntry.hourOfDay)}
                            y={precipChanceYFor(Math.max(0, Math.min(100, precipTooltipEntry.precipChancePct)))}
                            color="rgba(245,158,11,0.9)"
                          />
                        )}
                      </svg>
                    </div>
                  </div>
                  <p className="mt-1 text-[10px] text-muted-foreground">
                    <span className="text-blue-500">▮ Intensity (mm/hr)</span> · <span className="text-amber-600">— Chance of precip (%)</span>
                  </p>
                </>
              ) : (
                <ChartUnavailableMessage testId="forecast-precip-unavailable" message="Precipitation forecast unavailable for this day" />
              )}
            </div>

            <ForecastTideSection isImperial={unit === 'imperial'} dayOffset={selectedDayIndex} />

          </div>
        </div>
      </div>
    </div>
  )
}
