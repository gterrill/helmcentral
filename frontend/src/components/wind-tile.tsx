import { ArrowUp } from 'lucide-react'
import { memo, useEffect, useRef, useState, type CSSProperties, type ReactNode } from 'react'

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

// Wind-tile geometry: the 4 gauge cards + compass sit in a fixed-size canvas
// (rather than stretching with their column) so each card's inner corner can
// be cut with a radial-gradient mask that lines up with the compass ring.
// Mobile and desktop each get their own canvas size (mobile has no competing
// grid columns so it can afford a bigger compass than desktop's narrow lg
// column). Each canvas scales down (via useFitScale) if its column ends up
// narrower than its design width, e.g. a small phone, or the squeeze right
// after the lg breakpoint's 3-column split kicks in.
type WindCanvasConfig = {
  width: number
  height: number
  compassBox: number
  topCardW: number
  bottomCardW: number
  cardH: number
  gap: number
}

const WIND_MOBILE_CFG: WindCanvasConfig = { width: 475, height: 363, compassBox: 295, topCardW: 195, bottomCardW: 224, cardH: 80, gap: 20 }
const WIND_DESKTOP_CFG: WindCanvasConfig = { width: 440, height: 225, compassBox: 198, topCardW: 170, bottomCardW: 203, cardH: 72, gap: 20 }

// Width of the card's straight-edge border (Tailwind's bare `border` utility), so the
// drawn ring along the mask's cut edge reads as a continuous border, not just 3 sides.
const WIND_BORDER_WIDTH = 1

function computeWindMasks({ width, height, compassBox, topCardW, bottomCardW, cardH, gap }: WindCanvasConfig) {
  // WindCompass draws its outer ring at radius 130 inside a 280-wide viewBox.
  const compassRadius = (compassBox / 2) * (130 / 140)
  const inner = compassRadius + gap - 1
  const outer = compassRadius + gap + 1
  const ringOuter = outer + WIND_BORDER_WIDTH
  const centerX = width / 2
  const centerY = height / 2

  const mask = (cardLeft: number, cardTop: number): CSSProperties => {
    const x = centerX - cardLeft
    const y = centerY - cardTop
    const maskImg = `radial-gradient(circle at ${x}px ${y}px, transparent ${inner}px, #000 ${outer}px)`
    // A thin ring drawn just outside the mask's cut line, in the card's border color,
    // so the curved edge gets a stroke to match the card's straight-edge border.
    const ringImg = `radial-gradient(circle at ${x}px ${y}px, transparent ${outer}px, hsl(var(--border)) ${outer}px, hsl(var(--border)) ${ringOuter}px, transparent ${ringOuter}px)`
    return { maskImage: maskImg, WebkitMaskImage: maskImg, backgroundImage: ringImg }
  }

  return {
    tl: mask(0, 0),
    tr: mask(width - topCardW, 0),
    bl: mask(0, height - cardH),
    br: mask(width - bottomCardW, height - cardH),
  }
}

const WIND_MOBILE_MASKS = computeWindMasks(WIND_MOBILE_CFG)
const WIND_DESKTOP_MASKS = computeWindMasks(WIND_DESKTOP_CFG)

// Threshold above which the current is judged strong enough to visually call out.
const HIGH_DRIFT_IMPACT_KTS = 1.5

/** Scales a fixed-size design down (never up) to fit whatever width its column ends up with. */
function useFitScale(designWidth: number) {
  const ref = useRef<HTMLDivElement>(null)
  const [scale, setScale] = useState(1)

  useEffect(() => {
    const el = ref.current
    if (!el) return
    const observer = new ResizeObserver(([entry]) => {
      setScale(Math.min(1, entry.contentRect.width / designWidth))
    })
    observer.observe(el)
    return () => observer.disconnect()
  }, [designWidth])

  return [ref, scale] as const
}

type WindGaugeClusterProps = {
  cfg: WindCanvasConfig
  masks: ReturnType<typeof computeWindMasks>
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

        <div className="pointer-events-none absolute left-1/2 top-1/2 z-20 -translate-x-1/2 -translate-y-1/2" style={{ width: cfg.compassBox }}>
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
