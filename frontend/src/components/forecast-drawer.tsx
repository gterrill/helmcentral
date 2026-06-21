import { useEffect, useId, useRef, useState } from 'react'
import type { PointerEvent as ReactPointerEvent, ReactNode } from 'react'

import { Cloud, CloudRain, Moon, Sun, Sunrise, Sunset, Wind, Waves } from 'lucide-react'

import { Button } from '@/components/ui/button'
import type { WeatherHourlyEntry, WeatherHourlyPrecipPoint, WeatherHourlyUVPoint, WeatherHourlyWavePoint, WeatherHourlyWindPoint } from '@/hooks/use-weather-forecast'

interface ForecastDay {
  date: string
  dayName: string
  condition: string
  high: number
  low: number
  windSpeed: number
  windGust: number
  windDirection: string
  windSummary: string | null
  waveSummary: string | null
  precipitationSummary: string | null
  precipitation: number
  sunriseTime: string | null
  sunsetTime: string | null
  moonPhase: string | null
  hourlyWind: WeatherHourlyWindPoint[]
  hourlyWave: WeatherHourlyWavePoint[]
  hourlyPrecip: WeatherHourlyPrecipPoint[]
  hourlyUV: WeatherHourlyUVPoint[]
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
}

function fahrenheitToCelsius(tempF: number) {
  return (tempF - 32) * (5 / 9)
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
		return <Sunset size={24} className="text-primary" />
  }

  const normalized = entry.condition.toLowerCase()
  if (isNight && (normalized.includes('clear') || normalized.includes('sunny'))) {
		return <Moon size={24} className="text-secondary" />
  }

  return getWeatherIcon(entry.condition, 24)
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
const AXIS_LABEL_COLOR = 'rgba(71,85,105,0.95)'

// Computes a y position for a left-axis tick label, nudging the top and
// bottom ticks inward so the text isn't clipped by the chart edges.
function axisTickLabelY(yFor: (value: number) => number, value: number, top: number, bottom: number) {
  const y = yFor(value)
  if (Math.abs(y - top) < 0.5) return top + 8
  if (Math.abs(y - bottom) < 0.5) return bottom - 2
  return y + 3
}

// Converts a 0-360 bearing to a 16-point compass label, for the wave/swell
// direction shown in the scrub tooltip (wind already gets this as a string
// straight from the API).
const COMPASS_POINTS = ['N', 'NNE', 'NE', 'ENE', 'E', 'ESE', 'SE', 'SSE', 'S', 'SSW', 'SW', 'WSW', 'W', 'WNW', 'NW', 'NNW']
function compassLabel(directionDeg: number): string {
  if (directionDeg < 0) return ''
  return COMPASS_POINTS[Math.round((directionDeg % 360) / 22.5) % 16]
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

// Shows a tooltip for the nearest hourly entry on mouse hover (desktop/
// tablet) or touch (mobile) - both go through the same pointer events, since
// `pointermove` only fires for a mouse on hover (no button needed) and only
// fires for touch while a finger is actually down and moving. Positioned at
// the pointer's own X so it never requires looking elsewhere.
//
// Clearing is pointer-type-aware: a mouse hides the tooltip as soon as it
// leaves the chart (normal hover behavior), but touch does NOT hide on
// pointerup/pointerleave - those fire the instant a finger lifts, which
// would otherwise make a quick tap flash the tooltip for a single frame
// instead of actually showing it. A touch tooltip stays until the next tap
// (anywhere) replaces or clears it.
function useChartTooltip(count: number, resetKey: number, chartLeft: number, chartRight: number) {
  const [point, setPoint] = useState<{ index: number; pixelX: number } | null>(null)
  const svgRef = useRef<SVGSVGElement>(null)

  // Switching days swaps in a different hourly array (different length,
  // different times) - drop any tooltip from the previous day's chart.
  useEffect(() => {
    setPoint(null)
  }, [resetKey])

  const updateFromClientX = (clientX: number) => {
    const svg = svgRef.current
    if (!svg || count <= 0) return
    const rect = svg.getBoundingClientRect()
    if (rect.width <= 0) return
    const pixelX = Math.max(0, Math.min(rect.width, clientX - rect.left))
    const viewBoxWidth = svg.viewBox.baseVal.width || chartRight
    const xInViewBox = (pixelX / rect.width) * viewBoxWidth
    const fraction = count <= 1 ? 0 : (xInViewBox - chartLeft) / (chartRight - chartLeft)
    const idx = Math.max(0, Math.min(count - 1, Math.round(fraction * (count - 1))))
    setPoint({ index: idx, pixelX })
  }

  // Only a mouse leaving the chart should hide the tooltip - for touch,
  // "leave" fires the moment a finger lifts, which isn't a meaningful signal
  // that the user is done looking at the value.
  const clearForMouse = (event: ReactPointerEvent<SVGSVGElement>) => {
    if (event.pointerType === 'mouse') setPoint(null)
  }

  return {
    svgRef,
    activeIndex: point === null ? null : Math.min(point.index, Math.max(0, count - 1)),
    tooltipPixelX: point?.pixelX ?? null,
    onPointerDown: (event: ReactPointerEvent<SVGSVGElement>) => updateFromClientX(event.clientX),
    onPointerMove: (event: ReactPointerEvent<SVGSVGElement>) => updateFromClientX(event.clientX),
    onPointerLeave: clearForMouse,
  }
}

// A small marker dot on the chart's primary series at the hovered/touched index.
function ChartTooltipMarker({ x, y, color }: { x: number; y: number; color: string }) {
  return <circle pointerEvents="none" cx={x} cy={y} r="5" fill={color} stroke="white" strokeWidth="2" />
}

// The floating time / prominent-value / secondary-value bubble, positioned
// at the pointer's X (clamped so it can't run off either edge of the chart)
// right above the chart - never behind a touch point, never elsewhere on
// the page.
function ChartTooltipBubble({ pixelX, time, primary, secondary }: { pixelX: number; time: string; primary: string; secondary: string }) {
  return (
    <div
      className="pointer-events-none absolute top-1 z-10 -translate-x-1/2 whitespace-nowrap rounded-md border border-border/60 bg-card px-2.5 py-1.5 shadow-md"
      style={{ left: `clamp(58px, ${pixelX}px, calc(100% - 58px))` }}
    >
      <p className="text-[9px] font-medium uppercase tracking-wide text-muted-foreground">{time}</p>
      <p className="font-display text-base leading-tight text-foreground">{primary}</p>
      <p className="text-[10px] text-muted-foreground">{secondary}</p>
    </div>
  )
}

// Left-axis band labels for the UV chart, positioned at each band's center value.
const UV_BAND_LABELS = [
  { value: 10, label: 'Extreme' },
  { value: 8.5, label: 'Very High' },
  { value: 6.5, label: 'High' },
  { value: 4, label: 'Moderate' },
  { value: 1, label: 'Low' },
]

// Parses a "6:32AM"/"5:09PM"-style local time label into an hour-of-day
// (0-23, possibly fractional), or null if unparseable.
function parseHourFromTimeLabel(label: string | null): number | null {
  const match = label?.match(/^(\d{1,2}):(\d{2})(AM|PM)$/)
  if (!match) return null

  let hour = parseInt(match[1], 10) % 12
  if (match[3] === 'PM') hour += 12
  return hour + parseInt(match[2], 10) / 60
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

export function ForecastDrawer({
  forecast,
  hourlyToday = [],
  summary = null,
  loading = false,
  error = null,
  onRetry,
  unit,
}: ForecastDrawerProps) {
  const hasForecast = Boolean(forecast && forecast.length > 0)
  const [selectedDayIndex, setSelectedDayIndex] = useState(0)
  const uvAreaGradientId = useId()
  const uvLineGradientId = useId()
  const detailsCardRef = useRef<HTMLDivElement>(null)
  const dayTabsRowRef = useRef<HTMLDivElement>(null)

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

  const selectedDay = days[selectedDayIndex] ?? days[0]

  const humidityPct = Math.max(35, Math.min(95, Math.round(45 + (selectedDay.precipitation * 0.4))))
  const visibilityNm = Math.max(1, 12 - (selectedDay.precipitation * 0.06))

  const windHourly = selectedDay.hourlyWind ?? []
  const waveHourly = selectedDay.hourlyWave ?? []
  const precipHourly = selectedDay.hourlyPrecip ?? []
  const uvHourlyFull = selectedDay.hourlyUV ?? []
  const sunriseHour = parseHourFromTimeLabel(selectedDay.sunriseTime)
  const sunsetHour = parseHourFromTimeLabel(selectedDay.sunsetTime)
  const uvHourly = sunriseHour !== null && sunsetHour !== null
    ? uvHourlyFull.filter((_entry, idx) => idx >= Math.floor(sunriseHour) && idx <= Math.ceil(sunsetHour))
    : uvHourlyFull

  const hourlyChartLeft = 30
  const hourlyChartRight = 980
  const hourlyChartWidth = hourlyChartRight - hourlyChartLeft

  const hourlyXFor = (idx: number, count: number) =>
    count <= 1 ? hourlyChartLeft + hourlyChartWidth / 2 : hourlyChartLeft + (idx * hourlyChartWidth) / (count - 1)

  const windSpeeds = windHourly.map((entry) => Math.max(0, entry.windSpeed))
  const windGusts = windHourly.map((entry) => Math.max(0, entry.windGust))
  const windDataMax = Math.max(0, ...windSpeeds, ...windGusts)
  const windMax = windDataMax <= 30 ? 30 : Math.ceil(windDataMax / 10) * 10
  const windChartTop = 35
  const windChartBottom = 125
  const windYFor = (value: number) => windChartTop + (1 - value / windMax) * (windChartBottom - windChartTop)
  const windAxisTicks = Array.from({ length: windMax / 10 + 1 }, (_, idx) => idx * 10)
  const windTickEvery = Math.max(1, Math.round(windHourly.length / 8))
  const windAreaPath = windHourly.length > 0
    ? `M ${hourlyXFor(0, windHourly.length)} ${windChartBottom} L ${windSpeeds.map((value, idx) => `${hourlyXFor(idx, windHourly.length)} ${windYFor(value)}`).join(' L ')} L ${hourlyXFor(windHourly.length - 1, windHourly.length)} ${windChartBottom} Z`
    : ''
  const windSpeedPoints = windSpeeds.map((value, idx) => `${hourlyXFor(idx, windHourly.length)},${windYFor(value)}`).join(' ')
  const windGustPoints = windGusts.map((value, idx) => `${hourlyXFor(idx, windHourly.length)},${windYFor(value)}`).join(' ')

  const waveHeights = waveHourly.map((entry) => Math.max(0, entry.waveHeightM))
  const windWaveHeights = waveHourly.map((entry) => Math.max(0, entry.windWaveHeightM))
  const swellWaveHeights = waveHourly.map((entry) => Math.max(0, entry.swellWaveHeightM))
  const waveDataMax = Math.max(0, ...waveHeights, ...windWaveHeights, ...swellWaveHeights)
  const waveMax = waveDataMax <= 3 ? 3 : Math.ceil(waveDataMax)
  const waveChartTop = 35
  const waveChartBottom = 125
  const waveYFor = (value: number) => waveChartTop + (1 - value / waveMax) * (waveChartBottom - waveChartTop)
  const waveAxisTicks = Array.from({ length: waveMax / 0.5 + 1 }, (_, idx) => idx * 0.5)
  const waveTickEvery = Math.max(1, Math.round(waveHourly.length / 8))
  const waveAreaPath = waveHourly.length > 0
    ? `M ${hourlyXFor(0, waveHourly.length)} ${waveChartBottom} L ${waveHeights.map((value, idx) => `${hourlyXFor(idx, waveHourly.length)} ${waveYFor(value)}`).join(' L ')} L ${hourlyXFor(waveHourly.length - 1, waveHourly.length)} ${waveChartBottom} Z`
    : ''
  const wavePoints = waveHeights.map((value, idx) => `${hourlyXFor(idx, waveHourly.length)},${waveYFor(value)}`).join(' ')
  const windWavePoints = windWaveHeights.map((value, idx) => `${hourlyXFor(idx, waveHourly.length)},${waveYFor(value)}`).join(' ')
  const swellWavePoints = swellWaveHeights.map((value, idx) => `${hourlyXFor(idx, waveHourly.length)},${waveYFor(value)}`).join(' ')

  const precipIntensities = precipHourly.map((entry) => Math.max(0, entry.precipIntensityMm))
  const precipChances = precipHourly.map((entry) => Math.max(0, Math.min(100, entry.precipChancePct)))
  const precipMax = Math.max(1, ...precipIntensities)
  const precipChartTop = 35
  const precipChartBottom = 125
  const precipBarYFor = (value: number) => precipChartTop + (1 - value / precipMax) * (precipChartBottom - precipChartTop)
  const precipChanceYFor = (value: number) => precipChartTop + (1 - value / 100) * (precipChartBottom - precipChartTop)
  const precipTickEvery = Math.max(1, Math.round(precipHourly.length / 8))
  const precipBarWidth = precipHourly.length > 1 ? (hourlyChartWidth / (precipHourly.length - 1)) * 0.5 : 20
  const precipChancePoints = precipChances.map((value, idx) => `${hourlyXFor(idx, precipHourly.length)},${precipChanceYFor(value)}`).join(' ')

  const uvValues = uvHourly.map((entry) => Math.max(0, entry.uvIndex))
  const uvIndex = Math.round(Math.max(0, ...uvValues))
  const uvMax = Math.max(11, ...uvValues)
  const uvChartTop = 35
  const uvChartBottom = 125
  const uvYFor = (value: number) => uvChartTop + (1 - value / uvMax) * (uvChartBottom - uvChartTop)
  const uvTickEvery = Math.max(1, Math.round(uvHourly.length / 8))
  const uvAreaPath = uvHourly.length > 0
    ? `M ${hourlyXFor(0, uvHourly.length)} ${uvChartBottom} L ${uvValues.map((value, idx) => `${hourlyXFor(idx, uvHourly.length)} ${uvYFor(value)}`).join(' L ')} L ${hourlyXFor(uvHourly.length - 1, uvHourly.length)} ${uvChartBottom} Z`
    : ''
  const uvPoints = uvValues.map((value, idx) => `${hourlyXFor(idx, uvHourly.length)},${uvYFor(value)}`).join(' ')
  const uvProtectionIndices = uvValues.reduce<number[]>((acc, value, idx) => {
    if (value >= 3) acc.push(idx)
    return acc
  }, [])
  const uvProtectionStart = uvProtectionIndices.length > 0 ? uvHourly[uvProtectionIndices[0]].label : null
  const uvProtectionEnd = uvProtectionIndices.length > 0 ? uvHourly[uvProtectionIndices[uvProtectionIndices.length - 1]].label : null

  const windTooltip = useChartTooltip(windHourly.length, selectedDayIndex, hourlyChartLeft, hourlyChartRight)
  const waveTooltip = useChartTooltip(waveHourly.length, selectedDayIndex, hourlyChartLeft, hourlyChartRight)
  const precipTooltip = useChartTooltip(precipHourly.length, selectedDayIndex, hourlyChartLeft, hourlyChartRight)
  const uvTooltip = useChartTooltip(uvHourly.length, selectedDayIndex, hourlyChartLeft, hourlyChartRight)

  const windTooltipEntry = windTooltip.activeIndex === null ? null : windHourly[windTooltip.activeIndex] ?? null
  const waveTooltipEntry = waveTooltip.activeIndex === null ? null : waveHourly[waveTooltip.activeIndex] ?? null
  const precipTooltipEntry = precipTooltip.activeIndex === null ? null : precipHourly[precipTooltip.activeIndex] ?? null
  const uvTooltipEntry = uvTooltip.activeIndex === null ? null : uvHourly[uvTooltip.activeIndex] ?? null

  return (
    <div className="space-y-4 pb-4">
      {hourlyEntries.length > 0 && (
    <div className="overflow-hidden rounded-[26px] border border-secondary/15 bg-[linear-gradient(180deg,rgba(255,249,239,0.96),rgba(238,245,243,0.92))] shadow-[0_14px_32px_rgba(38,84,79,0.08)]">
      <div className="border-b border-secondary/14 bg-[linear-gradient(90deg,rgba(199,137,0,0.10),rgba(52,116,109,0.08))] px-4 py-3.5">
        <p className="pr-2 text-[15px] font-medium leading-relaxed text-foreground/90">
          {summary ?? "Today's hourly forecast"}
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
                className={`relative flex min-w-[84px] flex-col items-center rounded-[20px] border px-2.5 py-3.5 text-center ${entry.kind === 'sunset' ? 'border-primary/20 bg-primary/10' : nightMode ? 'border-secondary/20 bg-secondary/10' : 'border-border/70 bg-card/80'} ${isNowEntry ? 'shadow-[0_0_0_1px_rgba(199,137,0,0.22),0_10px_18px_rgba(199,137,0,0.10)]' : ''}`}
              >
                {isNowEntry && (
                  <span className="absolute left-1/2 top-1.5 h-1.5 w-1.5 -translate-x-1/2 rounded-full bg-primary" />
                )}
                <p className={`text-[15px] font-semibold tabular-nums ${isNowEntry ? 'text-primary' : nightMode ? 'text-secondary' : 'text-foreground/75'}`}>{entry.label}</p>
                <div className="mt-3.5 flex h-8 items-center justify-center">
                  {getHourlyWeatherIcon(entry, nightMode)}
                </div>
                <p className={`mt-4 ${entry.kind === 'sunset' ? 'text-[13px] font-semibold uppercase tracking-[0.08em] text-primary' : nightMode ? 'font-display text-[2rem] leading-none text-secondary' : 'font-display text-[2rem] leading-none text-foreground'}`}>
                  {entry.kind === 'sunset' ? 'Sunset' : displayTemperature !== null ? `${displayTemperature}°` : '—'}
                </p>
                {entry.kind === 'forecast' && entry.windSpeedKts >= 0 && (
                  <p className={`mt-1 whitespace-nowrap text-[11px] font-semibold ${nightMode ? 'text-secondary/80' : 'text-secondary'}`}>
                    {Math.round(entry.windSpeedKts)}kts {entry.windDirection}
                  </p>
                )}
                {nightMode && entry.kind === 'forecast' && (
                  <span className="mt-1 text-[10px] font-medium uppercase tracking-[0.12em] text-secondary/80">Night</span>
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
                    <span className="font-display text-lg leading-none text-primary">
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

                <p className="mt-1 text-[10px] font-semibold text-secondary">{Math.round(day.windSpeed)}{windUnit} {day.windDirection}</p>
              </button>
            ))}
          </div>

          <div ref={detailsCardRef} className="rounded-lg border bg-background/60 p-3">
            <div className="mb-3 flex flex-wrap items-center gap-3">
              <div className="flex items-end gap-1">
                {getWeatherIcon(selectedDay.condition, 22)}
                <span className="font-display text-4xl leading-none text-primary">{Math.round(displayTemp(selectedDay.high))}</span>
                <span className="pb-1 text-lg text-muted-foreground">{tempUnit}</span>
              </div>
              <p className="text-sm font-semibold uppercase tracking-[0.08em] text-foreground">{selectedDay.condition}</p>
              <div className="flex flex-wrap gap-2 text-[11px]">
                <span className="rounded bg-muted/50 px-2 py-1">Wind <span data-testid="forecast-selected-wind" className="font-semibold text-secondary">{selectedDay.windSpeed.toFixed(1)} {windUnit}</span></span>
                <span className="rounded bg-muted/50 px-2 py-1">Gusts <span data-testid="forecast-selected-gust" className="font-semibold text-amber-600">{selectedDay.windGust.toFixed(1)} {windUnit}</span></span>
                <span className="rounded bg-muted/50 px-2 py-1">Precip <span className="font-semibold">{Math.round(selectedDay.precipitation)}%</span></span>
                <span className="rounded bg-muted/50 px-2 py-1">Humidity <span className="font-semibold">{humidityPct}%</span></span>
                <span className="rounded bg-muted/50 px-2 py-1">Visibility <span className="font-semibold">{visibilityNm.toFixed(1)} nm</span></span>
                <span className="rounded bg-muted/50 px-2 py-1">UV Index <span className="font-semibold text-secondary">{uvIndex}</span></span>
                {selectedDay.sunriseTime && (
                  <span className="flex items-center gap-1.5 rounded bg-muted/50 px-2 py-1">
                    <Sunrise size={13} className="text-amber-500" />
                    Sunrise <span className="font-semibold">{selectedDay.sunriseTime}</span>
                  </span>
                )}
                {selectedDay.sunsetTime && (
                  <span className="flex items-center gap-1.5 rounded bg-muted/50 px-2 py-1">
                    <Sunset size={13} className="text-primary" />
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

            <div className="rounded-md border bg-card/70 p-2">
              <h4 className="mb-2 flex items-center gap-1.5 text-[11px] font-semibold uppercase tracking-[0.12em] text-muted-foreground">
                <Wind size={13} className="text-secondary" /> Wind
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
                    <svg
                      ref={windTooltip.svgRef}
                      viewBox="0 0 1000 175"
                      data-testid="forecast-wind-chart"
                      className="h-[175px] w-full touch-none rounded bg-muted/15"
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

                      <path d={windAreaPath} fill="rgba(37,99,235,0.12)" />
                      <polyline points={windGustPoints} fill="none" stroke="rgba(245,158,11,0.75)" strokeWidth="1.5" strokeDasharray="4 3" strokeLinejoin="round" strokeLinecap="round" />
                      <polyline points={windSpeedPoints} fill="none" stroke="rgba(37,99,235,0.95)" strokeWidth="2.4" strokeLinejoin="round" strokeLinecap="round" />

                      {windHourly.map((entry, idx) => {
                        if (idx % windTickEvery !== 0 && idx !== windHourly.length - 1) return null
                        const x = hourlyXFor(idx, windHourly.length)
                        return (
                          <g key={idx}>
                            <WindBarb cx={x} cy={16} speedKts={entry.windSpeed} directionDeg={entry.windDirectionDeg} />
                            <text x={x} y={142} textAnchor="middle" fontSize={AXIS_LABEL_FONT_SIZE} fill={AXIS_LABEL_COLOR}>{entry.label}</text>
                          </g>
                        )
                      })}

                      {windTooltipEntry && (
                        <ChartTooltipMarker
                          x={hourlyXFor(windTooltip.activeIndex ?? 0, windHourly.length)}
                          y={windYFor(Math.max(0, windTooltipEntry.windSpeed))}
                          color="rgba(37,99,235,0.95)"
                        />
                      )}
                    </svg>
                  </div>
                  <p className="mt-1 text-[10px] text-muted-foreground">
                    <span className="text-blue-600">— Wind</span> · <span className="text-amber-600">- - Gusts</span> ({windUnit}) · barbs show direction the wind is coming from (full feather = 10kt, half = 5kt)
                  </p>
                </>
              ) : (
                <p className="py-6 text-center text-xs text-muted-foreground" data-testid="forecast-wind-unavailable">
                  Wind forecast unavailable for this day
                </p>
              )}
            </div>

            <div className="mt-3 rounded-md border bg-card/70 p-2">
              <h4 className="mb-2 flex items-center gap-1.5 text-[11px] font-semibold uppercase tracking-[0.12em] text-muted-foreground">
                <Waves size={13} className="text-secondary" /> Wave
              </h4>
              {waveHourly.length > 0 ? (
                <>
                  {selectedDay.waveSummary && (
                    <p className="mb-2 text-base text-foreground/80">{selectedDay.waveSummary}</p>
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
                    <svg
                      ref={waveTooltip.svgRef}
                      viewBox="0 0 1000 170"
                      data-testid="forecast-wave-chart"
                      className="h-[170px] w-full touch-none rounded bg-muted/15"
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

                      <path d={waveAreaPath} fill="rgba(20,184,166,0.12)" />
                      <polyline points={swellWavePoints} fill="none" stroke="rgba(139,92,246,0.85)" strokeWidth="1.5" strokeDasharray="2 3" strokeLinejoin="round" strokeLinecap="round" />
                      <polyline points={windWavePoints} fill="none" stroke="rgba(245,158,11,0.85)" strokeWidth="1.5" strokeDasharray="4 3" strokeLinejoin="round" strokeLinecap="round" />
                      <polyline points={wavePoints} fill="none" stroke="rgba(20,184,166,0.9)" strokeWidth="2.4" strokeLinejoin="round" strokeLinecap="round" />

                      {waveHourly.map((entry, idx) => {
                        if (idx % waveTickEvery !== 0 && idx !== waveHourly.length - 1) return null
                        const x = hourlyXFor(idx, waveHourly.length)
                        return (
                          <g key={idx}>
                            <WaveDirectionArrow cx={x} cy={16} directionDeg={entry.waveDirectionDeg} />
                            <text x={x} y={31} textAnchor="middle" fontSize={AXIS_LABEL_FONT_SIZE} fill={AXIS_LABEL_COLOR}>{entry.wavePeriodS.toFixed(1)}s</text>
                            <text x={x} y={140} textAnchor="middle" fontSize={AXIS_LABEL_FONT_SIZE} fill={AXIS_LABEL_COLOR}>{entry.label}</text>
                          </g>
                        )
                      })}

                      {waveTooltipEntry && (
                        <ChartTooltipMarker
                          x={hourlyXFor(waveTooltip.activeIndex ?? 0, waveHourly.length)}
                          y={waveYFor(Math.max(0, waveTooltipEntry.waveHeightM))}
                          color="rgba(20,184,166,0.9)"
                        />
                      )}
                    </svg>
                  </div>
                  <p className="mt-1 text-[10px] text-muted-foreground">
                    <span className="text-secondary">— Total wave height (m)</span> · <span className="text-amber-600">- - Wind wave (chop)</span> · <span className="text-violet-600">·· Swell</span> · arrows show direction the swell is heading, with period (sec) below each
                  </p>
                </>
              ) : (
                <p className="py-6 text-center text-xs text-muted-foreground" data-testid="forecast-wave-unavailable">
                  Wave forecast unavailable for this day
                </p>
              )}
            </div>

            <div className="mt-3 rounded-md border bg-card/70 p-2">
              <h4 className="mb-2 flex items-center gap-1.5 text-[11px] font-semibold uppercase tracking-[0.12em] text-muted-foreground">
                <CloudRain size={13} className="text-secondary" /> Precipitation
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
                    <svg
                      ref={precipTooltip.svgRef}
                      viewBox="0 0 1000 175"
                      data-testid="forecast-precip-chart"
                      className="h-[175px] w-full touch-none rounded bg-muted/15"
                      onPointerDown={precipTooltip.onPointerDown}
                      onPointerMove={precipTooltip.onPointerMove}
                      onPointerLeave={precipTooltip.onPointerLeave}
                    >
                      <text x={6} y={40} fontSize={AXIS_LABEL_FONT_SIZE} fill={AXIS_LABEL_COLOR}>{precipMax.toFixed(1)} mm/hr</text>
                      <text x={6} y={123} fontSize={AXIS_LABEL_FONT_SIZE} fill={AXIS_LABEL_COLOR}>0</text>
                      <text x={994} y={40} textAnchor="end" fontSize={AXIS_LABEL_FONT_SIZE} fill={AXIS_LABEL_COLOR}>100%</text>
                      <text x={994} y={123} textAnchor="end" fontSize={AXIS_LABEL_FONT_SIZE} fill={AXIS_LABEL_COLOR}>0%</text>
                      <line x1={hourlyChartLeft} y1={precipChartBottom} x2={hourlyChartRight} y2={precipChartBottom} stroke="rgba(80,98,118,0.25)" strokeWidth="1" />

                      {precipHourly.map((_entry, idx) => {
                        const intensity = precipIntensities[idx]
                        if (intensity <= 0) return null
                        const x = hourlyXFor(idx, precipHourly.length)
                        const y = precipBarYFor(intensity)
                        return (
                          <rect
                            key={idx}
                            data-testid="forecast-precip-bar"
                            x={x - precipBarWidth / 2}
                            y={y}
                            width={precipBarWidth}
                            height={precipChartBottom - y}
                            fill={precipBarColor(intensity)}
                          />
                        )
                      })}

                      <polyline points={precipChancePoints} fill="none" stroke="rgba(245,158,11,0.85)" strokeWidth="2.4" strokeLinejoin="round" strokeLinecap="round" />

                      {precipHourly.map((entry, idx) => {
                        if (idx % precipTickEvery !== 0 && idx !== precipHourly.length - 1) return null
                        const x = hourlyXFor(idx, precipHourly.length)
                        return (
                          <text key={idx} x={x} y={142} textAnchor="middle" fontSize={AXIS_LABEL_FONT_SIZE} fill={AXIS_LABEL_COLOR}>{entry.label}</text>
                        )
                      })}

                      {precipTooltipEntry && (
                        <ChartTooltipMarker
                          x={hourlyXFor(precipTooltip.activeIndex ?? 0, precipHourly.length)}
                          y={precipChanceYFor(Math.max(0, Math.min(100, precipTooltipEntry.precipChancePct)))}
                          color="rgba(245,158,11,0.9)"
                        />
                      )}
                    </svg>
                  </div>
                  <p className="mt-1 text-[10px] text-muted-foreground">
                    <span className="text-blue-500">▮ Intensity (mm/hr)</span> · <span className="text-amber-600">— Chance of precip (%)</span>
                  </p>
                </>
              ) : (
                <p className="py-6 text-center text-xs text-muted-foreground" data-testid="forecast-precip-unavailable">
                  Precipitation forecast unavailable for this day
                </p>
              )}
            </div>

            <div className="mt-3 rounded-md border bg-card/70 p-2">
              <h4 className="mb-2 flex items-center gap-1.5 text-[11px] font-semibold uppercase tracking-[0.12em] text-muted-foreground">
                <Sun size={13} className="text-secondary" /> UV Index
              </h4>
              {uvHourly.length > 0 ? (
                <>
                  <p className="mb-2 text-base text-foreground/80">
                    {uvProtectionStart && uvProtectionEnd
                      ? `Sun protection recommended from ${uvProtectionStart} to ${uvProtectionEnd}.`
                      : 'No sun protection needed.'}
                  </p>
                  <div className="relative">
                    {uvTooltipEntry && (
                      <ChartTooltipBubble
                        pixelX={uvTooltip.tooltipPixelX ?? 0}
                        time={uvTooltipEntry.label}
                        primary={`UV ${Math.round(uvTooltipEntry.uvIndex)}`}
                        secondary={uvRiskLabel(uvTooltipEntry.uvIndex)}
                      />
                    )}
                    <svg
                      ref={uvTooltip.svgRef}
                      viewBox="0 0 1000 175"
                      data-testid="forecast-uv-chart"
                      className="h-[175px] w-full touch-none rounded bg-muted/15"
                      onPointerDown={uvTooltip.onPointerDown}
                      onPointerMove={uvTooltip.onPointerMove}
                      onPointerLeave={uvTooltip.onPointerLeave}
                    >
                      <defs>
                        <linearGradient id={uvAreaGradientId} gradientUnits="userSpaceOnUse" x1={0} y1={uvYFor(0)} x2={0} y2={uvYFor(uvMax)}>
                          {UV_GRADIENT_STOPS.map((stop) => (
                            <stop key={stop.value} offset={stop.value / uvMax} stopColor={stop.color} stopOpacity="0.3" />
                          ))}
                        </linearGradient>
                        <linearGradient id={uvLineGradientId} gradientUnits="userSpaceOnUse" x1={0} y1={uvYFor(0)} x2={0} y2={uvYFor(uvMax)}>
                          {UV_GRADIENT_STOPS.map((stop) => (
                            <stop key={stop.value} offset={stop.value / uvMax} stopColor={stop.color} stopOpacity="0.9" />
                          ))}
                        </linearGradient>
                      </defs>

                      <text x={994} y={40} textAnchor="end" fontSize={AXIS_LABEL_FONT_SIZE} fill={AXIS_LABEL_COLOR}>{Math.round(uvMax)}</text>
                      <text x={994} y={123} textAnchor="end" fontSize={AXIS_LABEL_FONT_SIZE} fill={AXIS_LABEL_COLOR}>0</text>
                      <line x1={hourlyChartLeft} y1={uvChartBottom} x2={hourlyChartRight} y2={uvChartBottom} stroke="rgba(80,98,118,0.25)" strokeWidth="1" />

                      {UV_BAND_LABELS.map((band) => (
                        <text key={band.label} x={6} y={uvYFor(band.value)} fontSize={AXIS_LABEL_FONT_SIZE} fill={AXIS_LABEL_COLOR}>{band.label}</text>
                      ))}

                      <path d={uvAreaPath} fill={`url(#${uvAreaGradientId})`} />
                      <polyline points={uvPoints} fill="none" stroke={`url(#${uvLineGradientId})`} strokeWidth="2.4" strokeLinejoin="round" strokeLinecap="round" />

                      {uvHourly.map((entry, idx) => {
                        if (idx % uvTickEvery !== 0 && idx !== uvHourly.length - 1) return null
                        const x = hourlyXFor(idx, uvHourly.length)
                        return (
                          <text key={idx} x={x} y={142} textAnchor="middle" fontSize={AXIS_LABEL_FONT_SIZE} fill={AXIS_LABEL_COLOR}>{entry.label}</text>
                        )
                      })}

                      {uvTooltipEntry && (
                        <ChartTooltipMarker
                          x={hourlyXFor(uvTooltip.activeIndex ?? 0, uvHourly.length)}
                          y={uvYFor(Math.max(0, uvTooltipEntry.uvIndex))}
                          color="rgb(249,115,22)"
                        />
                      )}
                    </svg>
                  </div>
                </>
              ) : (
                <p className="py-6 text-center text-xs text-muted-foreground" data-testid="forecast-uv-unavailable">
                  UV index forecast unavailable for this day
                </p>
              )}
            </div>
          </div>
        </div>
      </div>
    </div>
  )
}
