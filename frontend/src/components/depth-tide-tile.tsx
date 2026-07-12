import { ArrowDown, ArrowUp } from 'lucide-react'
import { memo } from 'react'
import type { DepthTrendData } from '@/hooks/use-depth-trend'
import type { TideToday } from '@/hooks/use-tide-today'
import { DepthSparkline } from '@/components/depth-sparkline'
import { Tile } from '@/components/ui/tile'

export interface DepthTideTileProps {
  depth: number | null
  isImperialDistance: boolean
  navigationState: string | null
  depthTrend: DepthTrendData
  tide: TideToday
  onOpen?: () => void
}

export const DepthTideTile = memo(function DepthTideTile({
  depth,
  isImperialDistance,
  navigationState,
  depthTrend,
  tide,
  onOpen,
}: DepthTideTileProps) {
  const depthValue =
    depth !== null
      ? isImperialDistance
        ? (depth * 3.28084).toFixed(1)
        : depth.toFixed(1)
      : '—'
  const depthUnitLabel = isImperialDistance ? 'feet' : 'm'
  const tideUnit = isImperialDistance ? 'ft' : 'm'
  const tideFtToDisplay = (ft: number) => (isImperialDistance ? ft : ft / 3.28084)
  const tideExtremes = [
    { isHigh: true, time: tide.high_tide_time, heightFt: tide.high_tide_height_ft },
    { isHigh: false, time: tide.low_tide_time, heightFt: tide.low_tide_height_ft },
  ].sort((a, b) => new Date(a.time).getTime() - new Date(b.time).getTime())

  return (
    <div onClick={onOpen} className={onOpen ? 'cursor-pointer transition-opacity hover:opacity-80' : undefined}>
      <Tile title="Depth & Tide">
        <div className="mt-1 rounded-md border bg-background/60 px-3 py-3">
          <p className="text-[11px] uppercase tracking-[0.16em] text-muted-foreground">Depth</p>
          <div className="mt-1 flex items-center gap-4">
            <p className="shrink-0 font-display text-4xl text-gauge-secondary">
              {depthValue}
              <span className="ml-2 align-baseline text-xl text-muted-foreground">{depth !== null ? depthUnitLabel : 'unavailable'}</span>
            </p>
            {(navigationState === 'anchored' || navigationState === 'moored') && (
              <DepthSparkline
                points={depthTrend.points}
                isImperial={isImperialDistance}
                since={depthTrend.since}
                tideType={depthTrend.tideType}
                tideDepthM={depthTrend.tideDepthM}
                className="min-w-0 flex-1"
              />
            )}
          </div>
        </div>
        <div className="mt-2 rounded-md border bg-background/60 px-3 py-2">
          <p className="text-[11px] uppercase tracking-[0.16em] text-muted-foreground">
            Tide{tide.station_name ? ` — ${tide.station_name}` : ''}
          </p>
          <div className="mt-1 flex items-baseline gap-2">
            <span className="font-display text-4xl leading-none text-gauge-secondary">
              {tide.current_tide_height_ft >= 0 ? tideFtToDisplay(tide.current_tide_height_ft).toFixed(isImperialDistance ? 1 : 2) : '—'}
            </span>
            <span className="text-lg text-muted-foreground">{tideUnit}</span>
            <span className="ml-1 inline-flex items-center gap-1 text-xs font-semibold text-gauge-secondary">
              {tide.tide_direction === 'Falling' ? <ArrowDown className="h-3 w-3" /> : <ArrowUp className="h-3 w-3" />}
              {tide.tide_direction}
            </span>
          </div>
          <div className="mt-2 flex flex-row flex-wrap items-center gap-x-4 gap-y-1">
            {tideExtremes.map((extreme) => (
              <p key={extreme.isHigh ? 'high' : 'low'} className="inline-flex items-center gap-1.5 text-xs text-foreground">
                {extreme.isHigh ? (
                  <ArrowUp className="h-3.5 w-3.5 text-gauge-secondary" />
                ) : (
                  <ArrowDown className="h-3.5 w-3.5 text-amber-600" />
                )}
                {extreme.isHigh ? 'High' : 'Low'}
                {extreme.heightFt >= 0 && (
                  <span className="text-muted-foreground">({tideFtToDisplay(extreme.heightFt).toFixed(isImperialDistance ? 1 : 2)} {tideUnit})</span>
                )}
                {' '}{new Date(extreme.time).toLocaleTimeString('en-US', { hour: 'numeric', minute: '2-digit' })}
              </p>
            ))}
          </div>
        </div>
      </Tile>
    </div>
  )
})
