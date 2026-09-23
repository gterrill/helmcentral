import { memo } from 'react'

import { Tile } from '@/components/ui/tile'
import { BulletGauge } from '@/components/ui/bullet-gauge'
import type { WeatherToday } from '@/hooks/use-weather-today'
import type { WeatherForecastDay, WeatherNextHour } from '@/hooks/use-weather-forecast'
import type { GustWindow } from '@/lib/gust-windows'
import type { DistanceUnits } from '@/config/app-config'
import { next24hWindBand, todayTempBand } from '@/lib/forecast-bands'
import { computeNowcastStatus, nowcastIntensityLabel, type NowcastBar } from '@/lib/nowcast'
import { fahrenheitToCelsius } from '@/lib/units'
import { formatDataAge, isStale } from '@/lib/staleness'

export interface CurrentConditionsTileProps {
  depth: number | null
  /** Seconds since the depth feed last reported; null reads as unknown, not stale (see isStale). */
  depthLastUpdateAgeS: number | null
  windSpeedApparentKts: number | null
  maxGustKts: Record<GustWindow, number | null>
  weather: WeatherToday
  forecast: WeatherForecastDay[]
  /** null/omitted means the configured provider supplied no next-hour nowcast for this position (ADR 0126) - the tile falls back to the hourly/daily rain line. */
  nextHour?: WeatherNextHour | null
  distanceUnits: DistanceUnits
}

/** Fraction (0-1) of the strip's plot height a bar is drawn at. Height comes
 * from intensity (mm/h) only, never fabricated from chance alone - a
 * chance-only point (no intensity data) still gets a faint fixed-height
 * wash so the strip isn't silently blank, mirroring HelmCast's
 * precipitation-chart.js (bars represent intensity; a chance-only reading
 * is conveyed through the status line and a low-opacity fill, not a
 * invented bar height). */
function nowcastBarHeightFraction(bar: NowcastBar, maxMmPerH: number): number {
  if (maxMmPerH > 0 && bar.mmPerH > 0) {
    return Math.max(bar.mmPerH / maxMmPerH, 0.12)
  }
  if (bar.chancePct !== null && bar.chancePct > 0) {
    return 0.15
  }
  return 0
}

/** Fill opacity by intensity band (light/moderate/heavy) - a chance-only
 * bar (0 mm/h but a positive chance) gets a faint wash rather than one of
 * the three real intensity tones. */
function nowcastBarFillOpacity(bar: NowcastBar): number {
  if (bar.mmPerH <= 0) return 0.25
  const intensity = nowcastIntensityLabel(bar.mmPerH)
  if (intensity === 'heavy') return 0.9
  if (intensity === 'moderate') return 0.65
  return 0.45
}

const NOWCAST_STRIP_WIDTH = 300
const NOWCAST_STRIP_PLOT_HEIGHT = 24
const NOWCAST_STRIP_TICKS = [0, 20, 40, 60]

/**
 * The tile's bottom-half nowcast strip: one bar per next_hour point
 * (1-minute bars for WeatherKit, 15-minute bars for Open-Meteo - see
 * ADR 0126), 0/20/40/60-minute ticks, hand-rolled SVG per ADR 0012. Only
 * rendered when computeNowcastStatus says there is real rain signal in the
 * window (design point 7) - callers never draw this for a dry/absent
 * nowcast, which would read as a false "definitely no rain" instead of
 * "nothing to show here".
 *
 * The plot's own SVG uses `preserveAspectRatio="none"` so its bars/gridlines
 * stretch to fill whatever width the tile is given rather than
 * letterboxing - correct for rects and lines, but an SVG `<text>` inside
 * that same non-uniformly-scaled viewport gets its glyphs squashed
 * horizontally whenever the rendered aspect ratio doesn't match the
 * viewBox's. The 0/20/40/60-minute tick labels are therefore drawn as plain
 * HTML below the plot instead, positioned by percentage (the ticks are
 * always evenly spaced, so a percentage-based left offset lands exactly
 * under each gridline at any tile width) rather than inside the SVG.
 */
function NowcastStrip({ bars, isHourlySourced }: { bars: NowcastBar[]; isHourlySourced: boolean }) {
  const maxMmPerH = Math.max(0, ...bars.map((b) => b.mmPerH))

  return (
    <div className="relative">
      <svg
        viewBox={`0 0 ${NOWCAST_STRIP_WIDTH} ${NOWCAST_STRIP_PLOT_HEIGHT}`}
        preserveAspectRatio="none"
        className="h-6 w-full"
        data-testid="nowcast-strip"
        role="img"
        aria-label={isHourlySourced ? 'Rain expected in the next hour, from the hourly forecast' : 'Rain expected in the next hour'}
      >
      {NOWCAST_STRIP_TICKS.map((minute) => {
        const x = (minute / 60) * NOWCAST_STRIP_WIDTH
        return (
          <line
            key={minute}
            x1={x}
            y1={0}
            x2={x}
            y2={NOWCAST_STRIP_PLOT_HEIGHT}
            stroke="hsl(var(--border))"
            strokeWidth={1}
            strokeDasharray="2,2"
          />
        )
      })}
      {bars.map((bar, i) => {
        const heightFrac = nowcastBarHeightFraction(bar, maxMmPerH)
        if (heightFrac <= 0) return null
        const x = (bar.offsetMinutes / 60) * NOWCAST_STRIP_WIDTH
        const width = (bar.widthMinutes / 60) * NOWCAST_STRIP_WIDTH
        const height = heightFrac * NOWCAST_STRIP_PLOT_HEIGHT
        return (
          <rect
            key={i}
            x={x}
            y={NOWCAST_STRIP_PLOT_HEIGHT - height}
            width={Math.max(0, width - 0.5)}
            height={height}
            fill={`hsl(var(--chart-precip) / ${nowcastBarFillOpacity(bar)})`}
          />
        )
      })}
      <line
        x1={0}
        y1={NOWCAST_STRIP_PLOT_HEIGHT}
        x2={NOWCAST_STRIP_WIDTH}
        y2={NOWCAST_STRIP_PLOT_HEIGHT}
        stroke="hsl(var(--border))"
        strokeWidth={1}
      />
      </svg>
      <div className="relative mt-0.5 h-[10px] text-[10px] text-muted-foreground" aria-hidden="true">
        {NOWCAST_STRIP_TICKS.map((minute) => {
          const leftPct = (minute / 60) * 100
          // Mirrors the SVG text's old textAnchor behaviour (start/middle/end)
          // via translateX, so each label still lines up under its own
          // gridline at any tile width without the SVG's own non-uniform
          // scale ever touching a glyph.
          const translate = minute === 0 ? '0%' : minute === 60 ? '-100%' : '-50%'
          return (
            <span
              key={minute}
              className="absolute top-0 whitespace-nowrap"
              style={{ left: `${leftPct}%`, transform: `translateX(${translate})` }}
            >
              {minute === 0 ? 'Now' : `${minute}m`}
            </span>
          )
        })}
      </div>
      {/* Overlaid, not stacked in flow: the caption must not grow the
          tile's height past its h7/minH budget on the 1920x360 wall (ADR
          0126 addendum) - absolute positioning over the strip's own box
          costs zero extra vertical space. */}
      {isHourlySourced && (
        <span
          className="pointer-events-none absolute right-0 top-0 text-[10px] text-muted-foreground"
          data-testid="nowcast-hourly-caption"
        >
          Hourly forecast
        </span>
      )}
    </div>
  )
}

const METRES_TO_FEET = 3.28084

function niceMax(...values: (number | null | undefined)[]): number {
  const usable = values.filter((v): v is number => typeof v === 'number' && Number.isFinite(v))
  const max = usable.length > 0 ? Math.max(...usable) : 10
  return Math.max(10, Math.ceil((max + 5) / 5) * 5)
}

/**
 * Depth, wind and temperature at a glance for the wall display (ADR 0092):
 * three readouts, each with a bullet gauge showing today's forecast range
 * where one applies, plus a line naming the next rain the forecast expects.
 * Every field is a structural dash when its source has nothing to say - a
 * missing forecast hour is never read as calm, and a feed with no
 * precipitation data at all never gets coerced into "no rain expected".
 */
export const CurrentConditionsTile = memo(function CurrentConditionsTile({
  depth,
  depthLastUpdateAgeS,
  windSpeedApparentKts,
  maxGustKts,
  weather,
  forecast,
  nextHour,
  distanceUnits,
}: CurrentConditionsTileProps) {
  const isImperial = distanceUnits === 'imperial'
  const depthStale = isStale(depthLastUpdateAgeS)
  const depthDisplay = depth !== null ? (isImperial ? depth * METRES_TO_FEET : depth).toFixed(1) : '—'
  const depthUnit = isImperial ? 'ft' : 'm'

  const now = new Date()
  const nowHour = now.getHours()
  const windBand = next24hWindBand(forecast, nowHour)
  const tempBandF = todayTempBand(forecast[0])
  const nowcast = computeNowcastStatus({
    now,
    // WeatherNextHour (use-weather-forecast.ts) already has exactly the
    // shape computeNowcastStatus's nextHour param wants - no need to repack
    // it field by field.
    nextHour: nextHour ?? null,
    forecastDays: forecast,
    nowHour,
  })

  const obsGust1h = maxGustKts['1h']
  const windMax = niceMax(windBand?.max, windBand?.gustMax, obsGust1h, windSpeedApparentKts)
  const windMarkers = [
    ...(windBand ? [{ value: windBand.gustMax, label: 'fcst gust' }] : []),
    ...(obsGust1h !== null ? [{ value: obsGust1h, label: 'obs' }] : []),
  ]

  const displayTemp = (tempF: number) => (isImperial ? tempF : fahrenheitToCelsius(tempF))
  const tempUnit = isImperial ? '°F' : '°C'
  const tempValue = weather.temperature_f >= 0 ? Math.round(displayTemp(weather.temperature_f)) : null
  const tempBand = tempBandF ? { low: displayTemp(tempBandF.low), high: displayTemp(tempBandF.high) } : null
  const tempMin = tempBand ? Math.floor(tempBand.low) - 5 : 0
  const tempMax = tempBand ? Math.ceil(tempBand.high) + 5 : 40

  return (
    <Tile title="Current Conditions" stale={depthStale} staleLabel={formatDataAge(depthLastUpdateAgeS)}>
      <div className="mt-2 grid grid-cols-3 gap-3">
        <div className="flex min-w-0 flex-col gap-1">
          <span className="text-[10px] uppercase tracking-[0.1em] text-muted-foreground">Depth</span>
          <span className="font-display text-4xl leading-none tabular-nums text-gauge-secondary">
            {depthDisplay}
            <span className="ml-1 text-[11px] text-muted-foreground">{depthUnit}</span>
          </span>
        </div>

        <div className="flex min-w-0 flex-col gap-1">
          <span className="text-[10px] uppercase tracking-[0.1em] text-muted-foreground">Wind</span>
          <span className="font-display text-4xl leading-none tabular-nums text-gauge-secondary">
            {windSpeedApparentKts !== null ? Math.round(windSpeedApparentKts) : '—'}
            <span className="ml-1 text-[11px] text-muted-foreground">kts</span>
          </span>
          <BulletGauge
            value={windSpeedApparentKts}
            min={0}
            max={windMax}
            bandLow={windBand?.min ?? null}
            bandHigh={windBand?.max ?? null}
            markers={windMarkers}
            unit="kt"
          />
        </div>

        <div className="flex min-w-0 flex-col gap-1">
          <span className="text-[10px] uppercase tracking-[0.1em] text-muted-foreground">Temp</span>
          <span className="font-display text-4xl leading-none tabular-nums text-gauge-secondary">
            {tempValue !== null ? tempValue : '—'}
            <span className="ml-1 text-[11px] text-muted-foreground">{tempUnit}</span>
          </span>
          <BulletGauge
            value={tempValue}
            min={tempMin}
            max={tempMax}
            bandLow={tempBand?.low ?? null}
            bandHigh={tempBand?.high ?? null}
            unit={tempUnit}
          />
        </div>
      </div>

      <div className="mt-3 min-w-0">
        {nowcast.showChart && <NowcastStrip bars={nowcast.bars} isHourlySourced={nowcast.isHourlySourced} />}
        <p className={`truncate text-[11px] text-muted-foreground ${nowcast.showChart ? 'mt-1' : ''}`}>{nowcast.line}</p>
      </div>
    </Tile>
  )
})
