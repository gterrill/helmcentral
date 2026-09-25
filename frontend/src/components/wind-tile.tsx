import { ArrowLeftRight, ArrowUp } from 'lucide-react'
import { memo, useEffect, useState, type CSSProperties, type ReactNode } from 'react'

import { computeCornerMasks, useFitScale, type ClusterCanvasConfig } from '@/lib/cluster-canvas'
import { Tile } from '@/components/ui/tile'
import { WindCompass } from '@/components/wind-compass'
import { formatHeading } from '@/lib/format'
import { GUST_WINDOW_LABELS, GUST_WINDOW_SPOKEN, nextGustWindow, parseGustWindow, type GustWindow } from '@/lib/gust-windows'
import { formatDataAge, isStale } from '@/lib/staleness'
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
  const reading = <p key="value" className={cn('font-display text-2xl leading-[0.86] text-gauge-primary md:text-3xl md:leading-9', valueClassName)}>{value}</p>
  // bg-card, not a translucent bg-background/NN: a browser contrast check
  // measured --gauge-primary against this card's ground at 2.6:1 (below the
  // 3:1 large-text bar) because a translucent ground composites onto
  // whatever ends up behind it rather than a known colour. bg-card is opaque
  // and gives --gauge-primary a fixed backdrop to clear: 3.20:1 in :root,
  // 8.69:1 in .dark, 8.94:1 in the instrument skin (see wind-tile.test.tsx).
  const sharedClassName = `relative flex flex-col justify-start gap-0.5 rounded-2xl border bg-card px-4 py-2 shadow-[0_10px_24px_rgba(15,23,42,0.08)] ${alignmentClass} ${className}`.trim()

  if (onClick) {
    return (
      <button
        type="button"
        onClick={onClick}
        aria-label={ariaLabel}
        className={cn(sharedClassName, 'cursor-pointer ring-offset-background focus-visible:outline-hidden focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2')}
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
  ringRotationDeg: number
  bowRotationDeg: number | null
  arrowAngleDeg: number | null
  windSide: 'port' | 'starboard' | null
  windAngleRelativeDeg: number | null
  windSpeedKts: number | null
  windKind: 'apparent' | 'true'
}

function WindGaugeCluster({
  cfg, masks, visibilityClassName,
  setValue, driftLabel,
  gustLeftTitle, gustLeftValue, gustLeftAriaLabel, onGustLeftClick,
  gustRightTitle, gustRightValue, gustRightAriaLabel, onGustRightClick,
  currentColorClass,
  ringRotationDeg, bowRotationDeg, arrowAngleDeg, windSide, windAngleRelativeDeg, windSpeedKts, windKind,
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
              ringRotationDeg={ringRotationDeg}
              bowRotationDeg={bowRotationDeg}
              arrowAngleDeg={arrowAngleDeg}
              windSide={windSide}
              windAngleRelativeDeg={windAngleRelativeDeg}
              windSpeedKts={windSpeedKts}
              kind={windKind}
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
  /**
   * True wind (ADR 0130), parsed by the backend the same way as apparent but
   * never substituted for it: a boat with no true-wind source shows '—' in
   * True mode rather than silently falling back to the apparent reading.
   */
  windSpeedTrueKts: number | null
  windAngleTrueDeg: number | null
  windSideTrue: 'port' | 'starboard' | null
  windAngleTrueRelativeDeg: number | null
  /** Compass bearing (0-360, true north) the true wind is blowing FROM. */
  windDirectionTrueDeg: number | null
  currentSetDeg: number | null
  currentDriftKts: number | null
  currentDriftImpactKts: number | null
  maxGustKts: Record<GustWindow, number | null>
  /** True wind's own MAX GUST ladder (ADR 0130); read instead of maxGustKts in True mode. */
  maxGustTrueKts: Record<GustWindow, number | null>
  /**
   * Seconds since the wind feed last reported, or null if the upstream
   * source publishes no age for it. `VesselState` in use-vessel-state.ts
   * carries no per-field timestamp for wind today, so this is always null
   * until a source actually publishes one — see isStale for why null reads
   * as "unknown," not "stale."
   */
  lastUpdateAgeS: number | null
}

const GUST_WINDOW_STORAGE_KEY_LEFT = 'windTile.gustWindow.left'
const GUST_WINDOW_STORAGE_KEY_RIGHT = 'windTile.gustWindow.right'
const GUST_WINDOW_DEFAULT_LEFT: GustWindow = '10m'
const GUST_WINDOW_DEFAULT_RIGHT: GustWindow = '1h'

type WindMode = 'apparent' | 'true'
type WindOrientation = 'course-up' | 'north-up'

const WIND_MODE_STORAGE_KEY = 'windTile.windMode'
const ORIENTATION_STORAGE_KEY = 'windTile.orientation'
const WIND_MODE_DEFAULT: WindMode = 'apparent'
const ORIENTATION_DEFAULT: WindOrientation = 'course-up'

function parseWindMode(value: string | null): WindMode | null {
  return value === 'apparent' || value === 'true' ? value : null
}

function parseOrientation(value: string | null): WindOrientation | null {
  return value === 'course-up' || value === 'north-up' ? value : null
}

function normalizeDeg(value: number): number {
  return ((value % 360) + 360) % 360
}

type WindToggleChipProps = {
  /** Prefix for each label span's data-testid, e.g. "wind-mode-chip". */
  testId: string
  ariaLabel: string
  onClick: () => void
  /** Which of the two options is current. */
  active: 'a' | 'b'
  labelA: string
  labelB: string
  /** Shown instead of labelA/labelB once the tile's own column drops under ~20rem (ADR 0130). */
  shortLabelA: string
  shortLabelB: string
}

/**
 * The shared chip shape for both title-bar toggles (ADR 0130): a compact
 * flip button showing the current mode as text, uppercased via CSS rather
 * than in the string itself, matching the autopilot mode badge's
 * micro-label.
 *
 * Both label options render at once, stacked in the same grid cell
 * (`col-start-1 row-start-1`), rather than swapping one string for another.
 * A grid cell sizes to the widest thing in it regardless of which one is
 * visible, so the chip's width is pinned to its longer option and flipping
 * state can never resize it — no min-width guess needed, and no guess to
 * get wrong. The option not currently active stays `invisible` (keeps its
 * layout box, so it can still be measured) and `aria-hidden` (never read by
 * assistive tech) rather than being removed from the DOM.
 *
 * The same trick runs twice: once for the full labels ("Apparent"/"True",
 * "Course Up"/"North Up"), and once for a short pair used only once the
 * tile's own container drops under `@max-[20rem]` — an iPad-portrait column
 * (~240px) has no room for the icon or the long labels without pushing the
 * tile's "Wind" title out of the header entirely. Both sets exist in the
 * DOM at all times; `hidden`/`@max-[20rem]:inline-grid` picks which one the
 * container's width actually renders, so there is nothing for JS to
 * recompute on resize.
 */
function WindToggleChip({ testId, ariaLabel, onClick, active, labelA, labelB, shortLabelA, shortLabelB }: WindToggleChipProps) {
  return (
    <button
      type="button"
      onClick={onClick}
      aria-label={ariaLabel}
      className="inline-flex h-6 shrink-0 items-center gap-1 rounded-sm border border-border bg-card px-1.5 text-[10px] font-medium uppercase leading-none tracking-[0.16em] text-muted-foreground transition-colors hover:border-foreground/30 hover:text-foreground ring-offset-background focus-visible:outline-hidden focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 @max-[20rem]:tracking-[0.08em]"
    >
      <ArrowLeftRight className="size-3 shrink-0 @max-[20rem]:hidden" aria-hidden="true" />
      <span className="inline-grid justify-items-start @max-[20rem]:hidden">
        <span data-testid={`${testId}-full-a`} className={cn('col-start-1 row-start-1', active !== 'a' && 'invisible')} aria-hidden={active === 'a' ? undefined : true}>{labelA}</span>
        <span data-testid={`${testId}-full-b`} className={cn('col-start-1 row-start-1', active !== 'b' && 'invisible')} aria-hidden={active === 'b' ? undefined : true}>{labelB}</span>
      </span>
      <span className="hidden justify-items-start @max-[20rem]:inline-grid">
        <span data-testid={`${testId}-short-a`} className={cn('col-start-1 row-start-1', active !== 'a' && 'invisible')} aria-hidden={active === 'a' ? undefined : true}>{shortLabelA}</span>
        <span data-testid={`${testId}-short-b`} className={cn('col-start-1 row-start-1', active !== 'b' && 'invisible')} aria-hidden={active === 'b' ? undefined : true}>{shortLabelB}</span>
      </span>
    </button>
  )
}

export const WindTile = memo(function WindTile({
  headingTrue,
  windAngleApparentDeg,
  windSide,
  windAngleRelativeDeg,
  windSpeedApparentKts,
  windSpeedTrueKts,
  windAngleTrueDeg,
  windSideTrue,
  windAngleTrueRelativeDeg,
  windDirectionTrueDeg,
  currentSetDeg,
  currentDriftKts,
  currentDriftImpactKts,
  maxGustKts,
  maxGustTrueKts,
  lastUpdateAgeS,
}: WindTileProps) {
  const feedStale = isStale(lastUpdateAgeS)
  const [gustWindowLeft, setGustWindowLeft] = useState<GustWindow>(() => {
    const raw = globalThis.localStorage?.getItem(GUST_WINDOW_STORAGE_KEY_LEFT) ?? null
    return parseGustWindow(raw) ?? GUST_WINDOW_DEFAULT_LEFT
  })
  const [gustWindowRight, setGustWindowRight] = useState<GustWindow>(() => {
    const raw = globalThis.localStorage?.getItem(GUST_WINDOW_STORAGE_KEY_RIGHT) ?? null
    return parseGustWindow(raw) ?? GUST_WINDOW_DEFAULT_RIGHT
  })
  const [windMode, setWindMode] = useState<WindMode>(() => {
    const raw = globalThis.localStorage?.getItem(WIND_MODE_STORAGE_KEY) ?? null
    return parseWindMode(raw) ?? WIND_MODE_DEFAULT
  })
  const [orientation, setOrientation] = useState<WindOrientation>(() => {
    const raw = globalThis.localStorage?.getItem(ORIENTATION_STORAGE_KEY) ?? null
    return parseOrientation(raw) ?? ORIENTATION_DEFAULT
  })

  useEffect(() => {
    globalThis.localStorage?.setItem(GUST_WINDOW_STORAGE_KEY_LEFT, gustWindowLeft)
  }, [gustWindowLeft])

  useEffect(() => {
    globalThis.localStorage?.setItem(GUST_WINDOW_STORAGE_KEY_RIGHT, gustWindowRight)
  }, [gustWindowRight])

  useEffect(() => {
    globalThis.localStorage?.setItem(WIND_MODE_STORAGE_KEY, windMode)
  }, [windMode])

  useEffect(() => {
    globalThis.localStorage?.setItem(ORIENTATION_STORAGE_KEY, orientation)
  }, [orientation])

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

  // Everything the compass centre and the MAX GUST cards read follows the
  // active mode — apparent stays exactly today's behaviour; true reads its
  // own separate fields/ladder rather than a converted copy of apparent's.
  const activeSpeedKts = windMode === 'true' ? windSpeedTrueKts : windSpeedApparentKts
  const activeSide = windMode === 'true' ? windSideTrue : windSide
  const activeRelativeDeg = windMode === 'true' ? windAngleTrueRelativeDeg : windAngleRelativeDeg
  const activeGustKts = windMode === 'true' ? maxGustTrueKts : maxGustKts

  // Course Up: ring rotates with heading (bow fixed at 12 o'clock, arrow at
  // the bow-relative angle) - unchanged from before this feature existed.
  // North Up: ring never rotates (true north always at 12 o'clock); the bow
  // triangle instead sweeps to headingTrue, and the arrow points at the
  // wind's absolute compass bearing rather than a bow-relative one. Neither
  // the bow nor an apparent-wind arrow can be placed without a heading, so
  // both are hidden (not guessed at 0) when headingTrue is null; a true-wind
  // arrow needs no heading (windDirectionTrueDeg is already absolute) so it
  // can still show.
  const ringRotationDeg = orientation === 'north-up' ? 0 : (headingTrue ?? 0)
  let bowRotationDeg: number | null
  let arrowAngleDeg: number | null
  if (orientation === 'north-up') {
    bowRotationDeg = headingTrue
    arrowAngleDeg = windMode === 'true'
      ? windDirectionTrueDeg
      : (headingTrue !== null && windAngleApparentDeg !== null ? normalizeDeg(headingTrue + windAngleApparentDeg) : null)
  } else {
    bowRotationDeg = 0
    arrowAngleDeg = windMode === 'true' ? windAngleTrueDeg : windAngleApparentDeg
  }

  const gustLeftTitle = `MAX GUST ${GUST_WINDOW_LABELS[gustWindowLeft]}`
  const gustLeftValue = formatGustValue(activeGustKts[gustWindowLeft])
  const gustLeftAriaLabel = `Max gust over ${GUST_WINDOW_SPOKEN[gustWindowLeft]} — click to change window`
  const onGustLeftClick = () => setGustWindowLeft(nextGustWindow(gustWindowLeft))

  const gustRightTitle = `MAX GUST ${GUST_WINDOW_LABELS[gustWindowRight]}`
  const gustRightValue = formatGustValue(activeGustKts[gustWindowRight])
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

  const titleExtra = (
    <div className="flex shrink-0 items-center gap-1.5">
      <WindToggleChip
        testId="wind-mode-chip"
        ariaLabel={windMode === 'true' ? 'Showing true wind — switch to apparent wind' : 'Showing apparent wind — switch to true wind'}
        onClick={() => setWindMode((mode) => (mode === 'true' ? 'apparent' : 'true'))}
        active={windMode === 'true' ? 'b' : 'a'}
        labelA="Apparent"
        labelB="True"
        shortLabelA="App"
        shortLabelB="True"
      />
      <WindToggleChip
        testId="orientation-chip"
        ariaLabel={orientation === 'north-up' ? 'North up — switch to course up' : 'Course up — switch to north up'}
        onClick={() => setOrientation((mode) => (mode === 'north-up' ? 'course-up' : 'north-up'))}
        active={orientation === 'north-up' ? 'b' : 'a'}
        labelA="Course Up"
        labelB="North Up"
        shortLabelA="C Up"
        shortLabelB="N Up"
      />
    </div>
  )

  return (
    <Tile title="Wind" className="@container" titleExtra={titleExtra} stale={feedStale} staleLabel={formatDataAge(lastUpdateAgeS)}>
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
        ringRotationDeg={ringRotationDeg}
        bowRotationDeg={bowRotationDeg}
        arrowAngleDeg={arrowAngleDeg}
        windSide={activeSide}
        windAngleRelativeDeg={activeRelativeDeg}
        windSpeedKts={activeSpeedKts}
        windKind={windMode}
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
        ringRotationDeg={ringRotationDeg}
        bowRotationDeg={bowRotationDeg}
        arrowAngleDeg={arrowAngleDeg}
        windSide={activeSide}
        windAngleRelativeDeg={activeRelativeDeg}
        windSpeedKts={activeSpeedKts}
        windKind={windMode}
      />
    </Tile>
  )
})
