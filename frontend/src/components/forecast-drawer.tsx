import { useEffect, useState } from 'react'
import type { ReactNode } from 'react'

import { Cloud, CloudRain, Moon, Sun, Sunset, Wind, Waves } from 'lucide-react'

import { Button } from '@/components/ui/button'
import type { WeatherHourlyEntry, WeatherHourlyWavePoint, WeatherHourlyWindPoint } from '@/hooks/use-weather-forecast'

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
  precipitation: number
  hourlyWind: WeatherHourlyWindPoint[]
  hourlyWave: WeatherHourlyWavePoint[]
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

  const color = 'rgba(20,184,166,0.85)'

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

function formatUpdatedAt(value: string | null | undefined) {
  if (!value) {
    return 'Unknown'
  }

  const parsed = new Date(value)
  if (Number.isNaN(parsed.getTime())) {
    return 'Unknown'
  }

  return parsed.toLocaleString('en-US', {
    month: 'short',
    day: 'numeric',
    hour: 'numeric',
    minute: '2-digit',
  })
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
  isCached = false,
  updatedAt = null,
  ttlSeconds = null,
  onRetry,
  unit,
}: ForecastDrawerProps) {
  const hasForecast = Boolean(forecast && forecast.length > 0)
  const showingStaleForecast = Boolean(error && hasForecast)
  const cacheMinutes = ttlSeconds && ttlSeconds > 0 ? Math.round(ttlSeconds / 60) : null
  const [nowMs, setNowMs] = useState(() => Date.now())
  const [selectedDayIndex, setSelectedDayIndex] = useState(0)

  useEffect(() => {
    if (!updatedAt) {
      return
    }

    const interval = setInterval(() => {
      setNowMs(Date.now())
    }, 30000)

    return () => clearInterval(interval)
  }, [updatedAt])

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

  const selectedDay = days[selectedDayIndex] ?? days[0]

  const humidityPct = Math.max(35, Math.min(95, Math.round(45 + (selectedDay.precipitation * 0.4))))
  const visibilityNm = Math.max(1, 12 - (selectedDay.precipitation * 0.06))
  const uvIndex = Math.max(1, Math.min(11, Math.round((selectedDay.high - 32) / 8)))

  const windHourly = selectedDay.hourlyWind ?? []
  const waveHourly = selectedDay.hourlyWave ?? []

  const hourlyChartLeft = 30
  const hourlyChartRight = 980
  const hourlyChartWidth = hourlyChartRight - hourlyChartLeft

  const hourlyXFor = (idx: number, count: number) =>
    count <= 1 ? hourlyChartLeft + hourlyChartWidth / 2 : hourlyChartLeft + (idx * hourlyChartWidth) / (count - 1)

  const windSpeeds = windHourly.map((entry) => Math.max(0, entry.windSpeed))
  const windGusts = windHourly.map((entry) => Math.max(0, entry.windGust))
  const windMax = Math.max(5, ...windSpeeds, ...windGusts)
  const windChartTop = 35
  const windChartBottom = 125
  const windYFor = (value: number) => windChartTop + (1 - value / windMax) * (windChartBottom - windChartTop)
  const windTickEvery = Math.max(1, Math.round(windHourly.length / 8))
  const windAreaPath = windHourly.length > 0
    ? `M ${hourlyXFor(0, windHourly.length)} ${windChartBottom} L ${windSpeeds.map((value, idx) => `${hourlyXFor(idx, windHourly.length)} ${windYFor(value)}`).join(' L ')} L ${hourlyXFor(windHourly.length - 1, windHourly.length)} ${windChartBottom} Z`
    : ''
  const windSpeedPoints = windSpeeds.map((value, idx) => `${hourlyXFor(idx, windHourly.length)},${windYFor(value)}`).join(' ')
  const windGustPoints = windGusts.map((value, idx) => `${hourlyXFor(idx, windHourly.length)},${windYFor(value)}`).join(' ')

  const waveHeights = waveHourly.map((entry) => Math.max(0, entry.waveHeightM))
  const waveMax = Math.max(0.5, ...waveHeights)
  const waveYFor = (value: number) => 12 + (1 - value / waveMax) * 108
  const waveTickEvery = Math.max(1, Math.round(waveHourly.length / 8))
  const waveAreaPath = waveHourly.length > 0
    ? `M ${hourlyXFor(0, waveHourly.length)} 120 L ${waveHeights.map((value, idx) => `${hourlyXFor(idx, waveHourly.length)} ${waveYFor(value)}`).join(' L ')} L ${hourlyXFor(waveHourly.length - 1, waveHourly.length)} 120 Z`
    : ''
  const wavePoints = waveHeights.map((value, idx) => `${hourlyXFor(idx, waveHourly.length)},${waveYFor(value)}`).join(' ')

  return (
    <div className="space-y-4 pb-4">
      {(isCached || showingStaleForecast) && (
        <div className="rounded-lg border border-amber-500/30 bg-amber-500/10 px-3 py-2 text-[11px] text-amber-900">
          <div className="flex flex-wrap items-center justify-between gap-2">
            <p className="font-semibold uppercase tracking-[0.08em]">
              {showingStaleForecast ? 'Showing last cached forecast' : 'Forecast served from cache'}
            </p>
            <p className="text-amber-800">Updated {formatUpdatedAt(updatedAt)} (Age: {formatRefreshAge(updatedAt, nowMs)})</p>
          </div>
          {showingStaleForecast && (
            <p className="mt-1 text-amber-800">Live refresh failed: {error}</p>
          )}
          {cacheMinutes !== null && (
            <p className="mt-1 text-amber-800">Cache window: {cacheMinutes} minutes</p>
          )}
        </div>
      )}

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
        <div className="flex flex-col gap-3 md:flex-row md:items-start">
          <div className="flex gap-1.5 overflow-x-auto pb-1 md:w-[200px] md:flex-none md:flex-col md:overflow-visible md:pb-0">
            {days.map((day, idx) => (
              <button
                key={idx}
                type="button"
                className={`min-w-[150px] shrink-0 rounded-lg border px-2.5 py-2 text-left transition-colors md:min-w-0 ${idx === selectedDayIndex ? 'border-primary/50 bg-primary/5' : 'border-border/60 bg-background/40 hover:bg-muted/30'}`}
                aria-label={`Select forecast day ${day.dayName} ${day.date}`}
                onClick={() => setSelectedDayIndex(idx)}
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

          <div className="flex-1 rounded-lg border bg-background/60 p-3">
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
              </div>
            </div>

            <div className="rounded-md border bg-card/70 p-2">
              <h4 className="mb-2 flex items-center gap-1.5 text-[11px] font-semibold uppercase tracking-[0.12em] text-muted-foreground">
                <Wind size={13} className="text-secondary" /> Wind
              </h4>
              {windHourly.length > 0 ? (
                <>
                  {selectedDay.windSummary && (
                    <p className="mb-2 text-[12px] text-foreground/80">{selectedDay.windSummary}</p>
                  )}
                  <svg viewBox="0 0 1000 175" data-testid="forecast-wind-chart" className="h-[175px] w-full rounded bg-muted/15">
                    <text x={6} y={40} fontSize="10" fill="rgba(100,116,139,0.9)">{Math.round(windMax)} {windUnit}</text>
                    <text x={6} y={123} fontSize="10" fill="rgba(100,116,139,0.9)">0</text>
                    <line x1={hourlyChartLeft} y1={windChartBottom} x2={hourlyChartRight} y2={windChartBottom} stroke="rgba(80,98,118,0.25)" strokeWidth="1" />

                    <path d={windAreaPath} fill="rgba(20,184,166,0.12)" />
                    <polyline points={windGustPoints} fill="none" stroke="rgba(245,158,11,0.75)" strokeWidth="1.5" strokeDasharray="4 3" strokeLinejoin="round" strokeLinecap="round" />
                    <polyline points={windSpeedPoints} fill="none" stroke="rgba(20,184,166,0.95)" strokeWidth="2.4" strokeLinejoin="round" strokeLinecap="round" />

                    {windHourly.map((entry, idx) => {
                      if (idx % windTickEvery !== 0 && idx !== windHourly.length - 1) return null
                      const x = hourlyXFor(idx, windHourly.length)
                      return (
                        <g key={idx}>
                          <WindBarb cx={x} cy={16} speedKts={entry.windSpeed} directionDeg={entry.windDirectionDeg} />
                          <text x={x} y={142} textAnchor="middle" fontSize="9" fill="rgba(100,116,139,0.9)">{entry.label}</text>
                        </g>
                      )
                    })}
                  </svg>
                  <p className="mt-1 text-[10px] text-muted-foreground">
                    <span className="text-secondary">— Wind</span> · <span className="text-amber-600">- - Gusts</span> ({windUnit}) · barbs show direction the wind is coming from (full feather = 10kt, half = 5kt)
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
                  <svg viewBox="0 0 1000 160" data-testid="forecast-wave-chart" className="h-[160px] w-full rounded bg-muted/15">
                    <text x={6} y={16} fontSize="10" fill="rgba(100,116,139,0.9)">{waveMax.toFixed(1)} m</text>
                    <text x={6} y={124} fontSize="10" fill="rgba(100,116,139,0.9)">0</text>
                    <line x1={hourlyChartLeft} y1={120} x2={hourlyChartRight} y2={120} stroke="rgba(80,98,118,0.25)" strokeWidth="1" />

                    <path d={waveAreaPath} fill="rgba(37,99,235,0.12)" />
                    <polyline points={wavePoints} fill="none" stroke="rgba(37,99,235,0.9)" strokeWidth="2.4" strokeLinejoin="round" strokeLinecap="round" />

                    {waveHourly.map((entry, idx) => {
                      if (idx % waveTickEvery !== 0 && idx !== waveHourly.length - 1) return null
                      const x = hourlyXFor(idx, waveHourly.length)
                      return (
                        <g key={idx}>
                          <text x={x} y={138} textAnchor="middle" fontSize="9" fill="rgba(100,116,139,0.9)">{entry.label}</text>
                          <text x={x} y={152} textAnchor="middle" fontSize="9" fill="rgba(105,114,128,0.9)">{entry.wavePeriodS.toFixed(1)}s</text>
                        </g>
                      )
                    })}
                  </svg>
                  <p className="mt-1 text-[10px] text-muted-foreground">
                    <span className="text-secondary">— Wave height (m)</span>, period shown below each tick
                  </p>
                </>
              ) : (
                <p className="py-6 text-center text-xs text-muted-foreground" data-testid="forecast-wave-unavailable">
                  Wave forecast unavailable for this day
                </p>
              )}
            </div>
          </div>
        </div>
      </div>
    </div>
  )
}
