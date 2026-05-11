import { Cloud, CloudRain, Sun } from 'lucide-react'

interface ForecastDay {
  date: string
  dayName: string
  condition: string
  high: number
  low: number
  windSpeed: number
  windDirection: string
  precipitation: number
}

interface ForecastDrawerProps {
  forecast: ForecastDay[]
  unit: 'imperial' | 'metric'
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

export function ForecastDrawer({ forecast, unit }: ForecastDrawerProps) {
  if (!forecast || forecast.length === 0) {
    return <div className="py-8 text-center text-muted-foreground">No forecast data available</div>
  }

  const tempUnit = unit === 'metric' ? '°C' : '°F'
  const windUnit = 'kts'

  return (
    <div className="space-y-6 pb-4">
      {/* 6-Day Forecast Grid */}
      <div>
        <h3 className="mb-3 text-xs font-semibold uppercase tracking-[0.16em] text-muted-foreground">
          6-Day Forecast
        </h3>
        <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-6">
          {forecast.slice(0, 6).map((day, idx) => (
            <div
              key={idx}
              className="rounded-lg border bg-background/60 px-3 py-3 text-center"
            >
              <p className="text-xs font-semibold uppercase text-muted-foreground">
                {day.dayName}
              </p>
              <p className="text-[10px] text-muted-foreground">{day.date}</p>

              <div className="my-2 flex justify-center">
                {getWeatherIcon(day.condition, 32)}
              </div>

              <p className="line-clamp-2 text-[11px] font-medium text-foreground">
                {day.condition}
              </p>

              <div className="mt-2 flex items-center justify-center gap-1">
                <span className="font-display text-sm font-semibold text-primary">
                  {Math.round(day.high)}
                </span>
                <span className="text-[10px] text-muted-foreground">
                  {tempUnit}
                </span>
              </div>

              <div className="flex items-center justify-center gap-1 text-[10px] text-muted-foreground">
                <span>{Math.round(day.low)}{tempUnit}</span>
              </div>

              <div className="mt-2 border-t pt-2 text-[10px]">
                <p className="font-semibold text-secondary">
                  {Math.round(day.windSpeed)} {windUnit}
                </p>
                <p className="text-muted-foreground">{day.windDirection}</p>
              </div>

              <div className="mt-1 text-[10px] text-muted-foreground">
                {day.precipitation > 0 ? `${Math.round(day.precipitation)}% precip` : 'No rain'}
              </div>
            </div>
          ))}
        </div>
      </div>

      {/* Forecast Charts Section */}
      <div>
        <h3 className="mb-3 text-xs font-semibold uppercase tracking-[0.16em] text-muted-foreground">
          Forecast Charts
        </h3>

        {/* Temperature Chart Placeholder */}
        <div className="mb-4 rounded-lg border bg-background/60 p-4">
          <p className="mb-3 text-xs font-semibold uppercase text-muted-foreground">
            Temperature Trend
          </p>
          <div className="relative h-32 w-full rounded bg-muted/20">
            <div className="absolute inset-0 flex items-end justify-around px-2 pb-2">
              {forecast.slice(0, 6).map((day, idx) => {
                const maxTemp = Math.max(...forecast.map(d => d.high))
                const minTemp = Math.min(...forecast.map(d => d.low))
                const range = maxTemp - minTemp || 1
                const normalizedHigh = ((day.high - minTemp) / range) * 100
                const normalizedLow = ((day.low - minTemp) / range) * 100
                
                return (
                  <div key={idx} className="flex flex-col items-center gap-1">
                    <div className="relative h-20 w-6">
                      <div
                        className="absolute bottom-0 w-full rounded-t bg-gradient-to-t from-amber-500/40 to-amber-500/20"
                        style={{ height: `${normalizedHigh}%` }}
                      />
                      <div
                        className="absolute bottom-0 w-full rounded-t bg-gradient-to-t from-amber-600/60 to-amber-500/40"
                        style={{ height: `${normalizedLow}%` }}
                      />
                    </div>
                    <span className="text-[9px] text-muted-foreground">
                      {day.dayName.slice(0, 3)}
                    </span>
                  </div>
                )
              })}
            </div>
          </div>
          <div className="mt-2 flex gap-4 text-xs">
            <div className="flex items-center gap-2">
              <div className="h-2 w-4 rounded bg-amber-500/40" />
              <span className="text-muted-foreground">High</span>
            </div>
            <div className="flex items-center gap-2">
              <div className="h-2 w-4 rounded bg-amber-600/60" />
              <span className="text-muted-foreground">Low</span>
            </div>
          </div>
        </div>

        {/* Wind Chart Placeholder */}
        <div className="rounded-lg border bg-background/60 p-4">
          <p className="mb-3 text-xs font-semibold uppercase text-muted-foreground">
            Wind Speed Trend
          </p>
          <div className="relative h-24 w-full rounded bg-muted/20">
            <div className="absolute inset-0 flex items-end justify-around px-2 pb-2">
              {forecast.slice(0, 6).map((day, idx) => {
                const maxWind = Math.max(...forecast.map(d => d.windSpeed))
                const normalizedWind = (day.windSpeed / (maxWind || 1)) * 100
                
                return (
                  <div key={idx} className="flex flex-col items-center gap-1">
                    <div
                      className="rounded-t bg-gradient-to-t from-secondary/60 to-secondary/20"
                      style={{
                        width: '6px',
                        height: `${normalizedWind}%`,
                      }}
                    />
                    <span className="text-[9px] text-muted-foreground">
                      {day.dayName.slice(0, 3)}
                    </span>
                  </div>
                )
              })}
            </div>
          </div>
          <div className="mt-2 text-xs text-muted-foreground">
            Max: {Math.max(...forecast.map(d => d.windSpeed)).toFixed(1)} {windUnit}
          </div>
        </div>
      </div>

      {/* Detailed Table */}
      <div>
        <h3 className="mb-3 text-xs font-semibold uppercase tracking-[0.16em] text-muted-foreground">
          Detailed Forecast
        </h3>
        <div className="overflow-x-auto rounded-lg border bg-background/60">
          <table className="w-full text-xs">
            <thead>
              <tr className="border-b bg-muted/30">
                <th className="px-3 py-2 text-left font-semibold text-muted-foreground">
                  Day
                </th>
                <th className="px-3 py-2 text-center font-semibold text-muted-foreground">
                  High / Low
                </th>
                <th className="px-3 py-2 text-center font-semibold text-muted-foreground">
                  Condition
                </th>
                <th className="px-3 py-2 text-center font-semibold text-muted-foreground">
                  Wind
                </th>
                <th className="px-3 py-2 text-center font-semibold text-muted-foreground">
                  Precip
                </th>
              </tr>
            </thead>
            <tbody>
              {forecast.slice(0, 6).map((day, idx) => (
                <tr key={idx} className="border-b last:border-b-0">
                  <td className="px-3 py-2 font-medium text-foreground">
                    <div>{day.dayName}</div>
                    <div className="text-[9px] text-muted-foreground">{day.date}</div>
                  </td>
                  <td className="px-3 py-2 text-center font-display">
                    <span className="text-primary">{Math.round(day.high)}</span>
                    <span className="text-muted-foreground"> / </span>
                    <span className="text-amber-600">{Math.round(day.low)}</span>
                    <span className="text-[9px] text-muted-foreground">{tempUnit}</span>
                  </td>
                  <td className="px-3 py-2 text-center text-foreground">
                    {day.condition}
                  </td>
                  <td className="px-3 py-2 text-center font-semibold text-secondary">
                    {Math.round(day.windSpeed)} {windUnit}
                    <div className="text-[9px] text-muted-foreground">
                      {day.windDirection}
                    </div>
                  </td>
                  <td className="px-3 py-2 text-center text-muted-foreground">
                    {day.precipitation > 0
                      ? `${Math.round(day.precipitation)}%`
                      : '—'}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </div>
    </div>
  )
}
