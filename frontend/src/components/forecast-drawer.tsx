import { useEffect, useState } from 'react'

import { Cloud, CloudRain, Sun } from 'lucide-react'

import { Button } from '@/components/ui/button'

interface ForecastDay {
  date: string
  dayName: string
  condition: string
  high: number
  low: number
  windSpeed: number
  windGust: number
  windDirection: string
  precipitation: number
}

interface ForecastDrawerProps {
  forecast: ForecastDay[]
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
  const days = forecast.slice(0, 6)

  useEffect(() => {
    setSelectedDayIndex((prev) => (prev < days.length ? prev : 0))
  }, [days.length])

  const selectedDay = days[selectedDayIndex] ?? days[0]

  const humidityPct = Math.max(35, Math.min(95, Math.round(45 + (selectedDay.precipitation * 0.4))))
  const visibilityNm = Math.max(1, 12 - (selectedDay.precipitation * 0.06))
  const uvIndex = Math.max(1, Math.min(11, Math.round((selectedDay.high - 32) / 8)))

  const tempSeries = days.map(day => displayTemp((day.high + day.low) / 2))
  const windSeries = days.map(day => day.windSpeed)
  const precipSeries = days.map(day => Math.max(0, day.precipitation))

  const chartLeft = 18
  const chartRight = 982
  const chartWidth = chartRight - chartLeft

  const xForIndex = (idx: number) => {
    if (days.length <= 1) {
      return chartLeft + chartWidth / 2
    }

    return chartLeft + (idx * chartWidth) / (days.length - 1)
  }

  const tempMin = Math.min(...tempSeries)
  const tempMax = Math.max(...tempSeries)
  const tempRange = Math.max(1, tempMax - tempMin)
  const tempYFor = (value: number) => 20 + (1 - (value - tempMin) / tempRange) * 90

  const windMin = Math.min(...windSeries)
  const windMax = Math.max(...windSeries)
  const windRange = Math.max(1, windMax - windMin)
  const windYFor = (value: number) => 150 + (1 - (value - windMin) / windRange) * 62
  const precipMax = Math.max(1, ...precipSeries)
  const precipHeightFor = (value: number) => (value / precipMax) * 62

  const tempPoints = tempSeries.map((value, idx) => `${xForIndex(idx)},${tempYFor(value)}`).join(' ')
  const windPoints = windSeries.map((value, idx) => `${xForIndex(idx)},${windYFor(value)}`).join(' ')
  const tempAreaPath = `M ${xForIndex(0)} 130 L ${tempSeries.map((value, idx) => `${xForIndex(idx)} ${tempYFor(value)}`).join(' L ')} L ${xForIndex(days.length - 1)} 130 Z`
  const windAreaPath = `M ${xForIndex(0)} 212 L ${windSeries.map((value, idx) => `${xForIndex(idx)} ${windYFor(value)}`).join(' L ')} L ${xForIndex(days.length - 1)} 212 Z`

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

      <div>
        <h3 className="mb-3 text-xs font-semibold uppercase tracking-[0.16em] text-muted-foreground">
          6-Day Forecast
        </h3>
        <div className="grid grid-cols-2 gap-1.5 md:grid-cols-3 lg:grid-cols-6">
          {days.map((day, idx) => (
            <button
              key={idx}
              type="button"
              className={`min-w-0 rounded-lg border px-1.5 py-1.5 text-center transition-colors ${idx === selectedDayIndex ? 'border-primary/50 bg-primary/5' : 'bg-background/60 hover:bg-muted/30'}`}
              aria-label={`Select forecast day ${day.dayName} ${day.date}`}
              onClick={() => setSelectedDayIndex(idx)}
            >
              <p className="text-[11px] font-semibold uppercase text-muted-foreground">
                {idx === 0 ? 'Today' : day.dayName.slice(0, 3)}
              </p>
              <p className="text-[10px] text-muted-foreground">{day.date}</p>

              <div className="my-1 flex justify-center">
                {getWeatherIcon(day.condition, 28)}
              </div>

              <p className="truncate text-[10px] font-medium text-foreground">
                {day.condition}
              </p>

              <div className="mt-1.5 flex items-center justify-center gap-1">
                <span className="font-display text-lg leading-none text-primary">
                  {Math.round(displayTemp(day.high))}
                </span>
                <span className="text-[9px] text-muted-foreground">
                  {tempUnit}
                </span>
                <span className="text-sm text-muted-foreground">/</span>
                <span className="font-display text-sm text-muted-foreground">{Math.round(displayTemp(day.low))}</span>
              </div>

              <div className="mt-1.5 text-[10px] leading-tight">
                <p className="font-semibold text-secondary">{Math.round(day.windSpeed)}{windUnit} {day.windDirection}</p>
                <p className="text-muted-foreground">{Math.round(day.precipitation)}% precip</p>
              </div>
            </button>
          ))}
        </div>
      </div>

      <div className="rounded-lg border bg-background/60 p-3">
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
          <div className="mb-1 grid grid-cols-6 text-center text-[10px] font-semibold uppercase text-muted-foreground">
            {days.map((day, idx) => (
              <div key={idx}>{idx === 0 ? 'Today' : day.dayName.slice(0, 3)}</div>
            ))}
          </div>
          <svg viewBox="0 0 1000 220" className="h-[240px] w-full rounded bg-muted/15">
            {days.map((_, idx) => {
              const stripeStart = chartLeft + (idx * chartWidth) / days.length
              const stripeWidth = chartWidth / days.length
              return (
                <rect
                  key={idx}
                  x={stripeStart}
                  y={0}
                  width={stripeWidth}
                  height={220}
                  fill={idx % 2 === 0 ? 'rgba(80,98,118,0.05)' : 'rgba(80,98,118,0.09)'}
                />
              )
            })}

            <text x={6} y={18} fontSize="10" fill="rgba(100,116,139,0.9)">100%</text>
            <text x={6} y={130} fontSize="10" fill="rgba(100,116,139,0.9)">0%</text>
            <text x={6} y={158} fontSize="10" fill="rgba(100,116,139,0.9)">WIND</text>

            <line x1={chartLeft} y1={130} x2={chartRight} y2={130} stroke="rgba(80,98,118,0.25)" strokeWidth="1" />
            <line x1={chartLeft} y1={212} x2={chartRight} y2={212} stroke="rgba(80,98,118,0.2)" strokeWidth="1" />

            {days.map((day, idx) => {
              const slotWidth = chartWidth / days.length
              const barWidth = slotWidth * 0.55
              const barHeight = precipHeightFor(day.precipitation)
              const x = xForIndex(idx) - barWidth / 2
              const y = 130 - barHeight

              return (
                <g key={`precip-${idx}`}>
                  <rect x={x} y={y} width={barWidth} height={barHeight} rx={2} fill="rgba(59,130,246,0.28)" />
                  <text x={xForIndex(idx)} y={y - 4} textAnchor="middle" fontSize="10" fill="rgba(59,130,246,0.9)">
                    {Math.round(day.precipitation)}%
                  </text>
                </g>
              )
            })}

            <path d={tempAreaPath} fill="rgba(59,130,246,0.12)" />
            <polyline points={tempPoints} fill="none" stroke="rgba(37,99,235,0.9)" strokeWidth="3" strokeLinejoin="round" strokeLinecap="round" />

            {tempSeries.map((value, idx) => (
              <g key={`temp-${idx}`}>
                <circle cx={xForIndex(idx)} cy={tempYFor(value)} r="3" fill="rgba(37,99,235,0.95)" />
                <text x={xForIndex(idx)} y={tempYFor(value) - 8} textAnchor="middle" fontSize="11" fill="rgba(37,99,235,0.9)">
                  {Math.round(value)}°
                </text>
              </g>
            ))}

            <path d={windAreaPath} fill="rgba(20,184,166,0.12)" />
            <polyline points={windPoints} fill="none" stroke="rgba(20,184,166,0.95)" strokeWidth="2.4" strokeLinejoin="round" strokeLinecap="round" />
            {windSeries.map((value, idx) => (
              <g key={`wind-${idx}`}>
                <circle cx={xForIndex(idx)} cy={windYFor(value)} r="3" fill="rgba(20,184,166,0.95)" />
                <text x={xForIndex(idx)} y={windYFor(value) - 8} textAnchor="middle" fontSize="10" fill="rgba(24,161,151,0.95)">
                  {Math.round(value)}
                </text>
                <text x={xForIndex(idx)} y={windYFor(value) + 14} textAnchor="middle" fontSize="9" fill="rgba(105,114,128,0.9)">
                  {days[idx].windDirection}
                </text>
              </g>
            ))}
          </svg>
        </div>
      </div>
    </div>
  )
}
