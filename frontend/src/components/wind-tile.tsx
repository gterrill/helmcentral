import { ArrowUp } from 'lucide-react'
import { memo, useEffect, useState, type CSSProperties, type ReactNode } from 'react'

import { computeCornerMasks, useFitScale, type ClusterCanvasConfig } from '@/lib/cluster-canvas'
import { Tile } from '@/components/ui/tile'
import { WindCompass } from '@/components/wind-compass'
import { formatHeading } from '@/lib/format'
import { GUST_WINDOW_LABELS, GUST_WINDOW_SPOKEN, nextGustWindow, parseGustWindow, type GustWindow } from '@/lib/gust-windows'
import { cn } from '@/lib/utils'

type WindMetricCardProps = {
  title: string
  value: ReactNode
  align?: 'left' | 'right'
  className?: string
  valueClassName?: string
  style?: CSSProperties
  valueFirst?: boolean
  onClick?: () => void
  ariaLabel?: string
}

function WindMetricCard({ title, value, align = 'left', className = '', valueClassName, style, valueFirst = false, onClick, ariaLabel }: WindMetricCardProps) {
  const alignmentClass = align === 'right' ? 'items-end text-right' : 'items-start text-left'
  const label = <p key="label" className="text-[10px] leading-none uppercase tracking-[0.16em] text-muted-foreground">{title}</p>
  const reading = <p key="value" className={cn('font-display text-2xl leading-[0.86] text-gauge-primary md:text-3xl', valueClassName)}>{value}</p>
  const sharedClassName = `relative flex flex-col justify-start gap-0.5 rounded-2xl border bg-background/80 px-4 py-2 shadow-[0_10px_24px_rgba(15,23,42,0.08)] ${alignmentClass} ${className}`.trim()

  if (onClick) {
    return (
      <button
        type="button"
        onClick={onClick}
        aria-label={ariaLabel}
        className={cn(sharedClassName, 'cursor-pointer ring-offset-background focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2')}
        style={style}
      >
        {valueFirst ? [reading, label] : [label, reading]}
      </button>
    )
  }

  return (
    <div
      className={sharedClassName}
      style={style}
    >
      {valueFirst ? [reading, label] : [label, reading]}
    </div>
  )
}

// Canvas sizes: mobile has no competing grid columns so it can afford a bigger
// compass than desktop's narrow lg column. Each scales down via useFitScale if
// its column ends up narrower than its design width.
const WIND_MOBILE_CFG: ClusterCanvasConfig = { width: 475, height: 363, ringBox: 295, topCardW: 195, bottomCardW: 224, cardH: 80, gap: 20 }
const WIND_DESKTOP_CFG: ClusterCanvasConfig = { width: 440, height: 225, ringBox: 198, topCardW: 170, bottomCardW: 203, cardH: 72, gap: 20 }

const WIND_MOBILE_MASKS = computeCornerMasks(WIND_MOBILE_CFG)
const WIND_DESKTOP_MASKS = computeCornerMasks(WIND_DESKTOP_CFG)

// Threshold above which the current is judged strong enough to visually call out.
const HIGH_DRIFT_IMPACT_KTS = 1.5

type WindGaugeClusterProps = {
  cfg: ClusterCanvasConfig
  masks: ReturnType<typeof computeCornerMasks>
  visibilityClassName: string
  setValue: ReactNode
  driftLabel: ReactNode
  gustLeftTitle: string
  gustLeftValue: ReactNode
  gustLeftAriaLabel: string
  onGustLeftClick: () => void
  gustRightTitle: string
  gustRightValue: ReactNode
  gustRightAriaLabel: string
  onGustRightClick: () => void
  currentColorClass: string
  headingTrue: number | null
  windAngleApparentDeg: number | null
  windSide: 'port' | 'starboard' | null
  windAngleRelativeDeg: number | null
  windSpeedApparentKts: number | null
}

function WindGaugeCluster({
  cfg, masks, visibilityClassName,
  setValue, driftLabel,
  gustLeftTitle, gustLeftValue, gustLeftAriaLabel, onGustLeftClick,
  gustRightTitle, gustRightValue, gustRightAriaLabel, onGustRightClick,
  currentColorClass,
  headingTrue, windAngleApparentDeg, windSide, windAngleRelativeDeg, windSpeedApparentKts,
}: WindGaugeClusterProps) {
  const [fitRef, scale] = useFitScale(cfg.width)

  return (
    <div
      ref={fitRef}
      className={`relative mx-auto ${visibilityClassName}`}
      style={{ maxWidth: cfg.width, height: cfg.height * scale }}
    >
      <div
        className="absolute left-0 top-0 origin-top-left"
        style={{ width: cfg.width, height: cfg.height, transform: `scale(${scale})` }}
      >
        <div className="grid h-full w-full grid-cols-2 grid-rows-2">
          <div className="self-start justify-self-start">
            <WindMetricCard
              title={gustLeftTitle}
              value={gustLeftValue}
              onClick={onGustLeftClick}
              ariaLabel={gustLeftAriaLabel}
              style={{ width: cfg.topCardW, height: cfg.cardH, ...masks.tl }}
            />
          </div>

          <div className="self-start justify-self-end">
            <WindMetricCard
              title={gustRightTitle}
              value={gustRightValue}
              align="right"
              onClick={onGustRightClick}
              ariaLabel={gustRightAriaLabel}
              style={{ width: cfg.topCardW, height: cfg.cardH, ...masks.tr }}
            />
          </div>

          <div className="self-end justify-self-start">
            <WindMetricCard title="SET" value={setValue} valueClassName={currentColorClass} valueFirst style={{ width: cfg.bottomCardW, height: cfg.cardH, ...masks.bl }} />
          </div>

          <div className="self-end justify-self-end">
            <WindMetricCard title="DRIFT" value={driftLabel} valueClassName={currentColorClass} align="right" valueFirst style={{ width: cfg.bottomCardW, height: cfg.cardH, ...masks.br }} />
          </div>
        </div>

        <div className="pointer-events-none absolute left-1/2 top-1/2 z-20 -translate-x-1/2 -translate-y-1/2" style={{ width: cfg.ringBox }}>
          <div className="aspect-square w-full">
            <WindCompass
              headingTrue={headingTrue}
              windAngleApparentDeg={windAngleApparentDeg}
              windSide={windSide}
              windAngleRelativeDeg={windAngleRelativeDeg}
              windSpeedKts={windSpeedApparentKts}
            />
          </div>
        </div>
      </div>
    </div>
  )
}

export interface WindTileProps {
  headingTrue: number | null
  windAngleApparentDeg: number | null
  windSide: 'port' | 'starboard' | null
  windAngleRelativeDeg: number | null
  windSpeedApparentKts: number | null
  currentSetDeg: number | null
  currentDriftKts: number | null
  currentDriftImpactKts: number | null
  maxGustKts: Record<GustWindow, number | null>
}

const GUST_WINDOW_STORAGE_KEY_LEFT = 'windTile.gustWindow.left'
const GUST_WINDOW_STORAGE_KEY_RIGHT = 'windTile.gustWindow.right'
const GUST_WINDOW_DEFAULT_LEFT: GustWindow = '10m'
const GUST_WINDOW_DEFAULT_RIGHT: GustWindow = '1h'

export const WindTile = memo(function WindTile({
  headingTrue,
  windAngleApparentDeg,
  windSide,
  windAngleRelativeDeg,
  windSpeedApparentKts,
  currentSetDeg,
  currentDriftKts,
  currentDriftImpactKts,
  maxGustKts,
}: WindTileProps) {
  const [gustWindowLeft, setGustWindowLeft] = useState<GustWindow>(() => {
    const raw = globalThis.localStorage?.getItem(GUST_WINDOW_STORAGE_KEY_LEFT) ?? null
    return parseGustWindow(raw) ?? GUST_WINDOW_DEFAULT_LEFT
  })
  const [gustWindowRight, setGustWindowRight] = useState<GustWindow>(() => {
    const raw = globalThis.localStorage?.getItem(GUST_WINDOW_STORAGE_KEY_RIGHT) ?? null
    return parseGustWindow(raw) ?? GUST_WINDOW_DEFAULT_RIGHT
  })

  useEffect(() => {
    globalThis.localStorage?.setItem(GUST_WINDOW_STORAGE_KEY_LEFT, gustWindowLeft)
  }, [gustWindowLeft])

  useEffect(() => {
    globalThis.localStorage?.setItem(GUST_WINDOW_STORAGE_KEY_RIGHT, gustWindowRight)
  }, [gustWindowRight])
  const setDirectionLabel = currentSetDeg !== null ? formatHeading(currentSetDeg).split(' ').slice(1).join(' ') : '—'
  const setDegreesLabel = currentSetDeg !== null ? `${Math.round(((currentSetDeg % 360) + 360) % 360)}°` : '—'
  const setArrowRotation = currentSetDeg !== null ? ((currentSetDeg % 360) + 360) % 360 : 0
  const currentFavorable = currentDriftImpactKts !== null ? currentDriftImpactKts >= 0 : null
  const currentColorClass = currentFavorable === null ? '' : currentFavorable ? 'text-gauge-secondary' : 'text-amber-600'
  const highDriftImpact = currentDriftImpactKts !== null && Math.abs(currentDriftImpactKts) >= HIGH_DRIFT_IMPACT_KTS
  const driftLabel = currentDriftKts !== null ? (
    <>
      {currentDriftKts.toFixed(1)}
      <span className="ml-1 text-xl text-muted-foreground">kts</span>
    </>
  ) : '—'
  const formatGustValue = (kts: number | null): ReactNode => kts !== null ? (
    <>
      {kts.toFixed(1)}
      <span className="ml-1 text-xl text-muted-foreground">kts</span>
    </>
  ) : '—'

  const gustLeftTitle = `MAX GUST ${GUST_WINDOW_LABELS[gustWindowLeft]}`
  const gustLeftValue = formatGustValue(maxGustKts[gustWindowLeft])
  const gustLeftAriaLabel = `Max gust over ${GUST_WINDOW_SPOKEN[gustWindowLeft]} — click to change window`
  const onGustLeftClick = () => setGustWindowLeft(nextGustWindow(gustWindowLeft))

  const gustRightTitle = `MAX GUST ${GUST_WINDOW_LABELS[gustWindowRight]}`
  const gustRightValue = formatGustValue(maxGustKts[gustWindowRight])
  const gustRightAriaLabel = `Max gust over ${GUST_WINDOW_SPOKEN[gustWindowRight]} — click to change window`
  const onGustRightClick = () => setGustWindowRight(nextGustWindow(gustWindowRight))

  const setValue = currentSetDeg !== null && currentDriftKts !== 0
    ? (
      <span className="inline-flex items-center gap-2">
        <span>{setDegreesLabel}</span>
        <span
          className={cn('inline-flex h-7 w-7 items-center justify-center rounded-full border border-border/70', currentColorClass || 'text-muted-foreground')}
          title={`Set direction ${setDirectionLabel}`}
        >
          <ArrowUp
            className={highDriftImpact ? 'h-5 w-5' : 'h-4 w-4'}
            strokeWidth={highDriftImpact ? 2.75 : 2}
            style={{ transform: `rotate(${setArrowRotation}deg)` }}
          />
        </span>
      </span>
    )
    : <span className="font-display text-4xl tabular-nums leading-none text-gauge-primary">—</span>

  return (
    <Tile title="Apparent Wind - Course Up">
      <WindGaugeCluster
        cfg={WIND_MOBILE_CFG}
        masks={WIND_MOBILE_MASKS}
        visibilityClassName="md:hidden"
        setValue={setValue}
        driftLabel={driftLabel}
        gustLeftTitle={gustLeftTitle}
        gustLeftValue={gustLeftValue}
        gustLeftAriaLabel={gustLeftAriaLabel}
        onGustLeftClick={onGustLeftClick}
        gustRightTitle={gustRightTitle}
        gustRightValue={gustRightValue}
        gustRightAriaLabel={gustRightAriaLabel}
        onGustRightClick={onGustRightClick}
        currentColorClass={currentColorClass}
        headingTrue={headingTrue}
        windAngleApparentDeg={windAngleApparentDeg}
        windSide={windSide}
        windAngleRelativeDeg={windAngleRelativeDeg}
        windSpeedApparentKts={windSpeedApparentKts}
      />

      <WindGaugeCluster
        cfg={WIND_DESKTOP_CFG}
        masks={WIND_DESKTOP_MASKS}
        visibilityClassName="hidden md:block"
        setValue={setValue}
        driftLabel={driftLabel}
        gustLeftTitle={gustLeftTitle}
        gustLeftValue={gustLeftValue}
        gustLeftAriaLabel={gustLeftAriaLabel}
        onGustLeftClick={onGustLeftClick}
        gustRightTitle={gustRightTitle}
        gustRightValue={gustRightValue}
        gustRightAriaLabel={gustRightAriaLabel}
        onGustRightClick={onGustRightClick}
        currentColorClass={currentColorClass}
        headingTrue={headingTrue}
        windAngleApparentDeg={windAngleApparentDeg}
        windSide={windSide}
        windAngleRelativeDeg={windAngleRelativeDeg}
        windSpeedApparentKts={windSpeedApparentKts}
      />
    </Tile>
  )
})
