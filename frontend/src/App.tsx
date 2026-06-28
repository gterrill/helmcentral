import { Anchor, ArrowDown, ArrowUp, CloudSun, Compass, BatteryCharging, BatteryFull, BatteryMedium, BatteryLow, BatteryWarning } from 'lucide-react'
import { useEffect, useRef, useState, type CSSProperties, type ReactNode } from 'react'

import { AnchorWatchTile } from '@/components/anchor-watch-tile'
import { AnchorWatchDrawer } from '@/components/anchor-watch-drawer'
import { AlternatorTile } from '@/components/alternator-tile'
import { DepthSparkline } from '@/components/depth-sparkline'
import { MarineHeader } from '@/components/marine-header'
import { NearbyVesselsTile } from '@/components/nearby-vessels-tile'
import { RodeScopeTile } from '@/components/rode-scope-tile'
import { RadarDrawer } from '@/components/radar-drawer'
import { SignalKSettingsPanel } from '@/components/signalk-settings-panel'
import { CZoneSwitchesTile } from '@/components/czone-switches-tile'
import { GeneratorTile } from '@/components/generator-tile'
import { TanksTile } from '@/components/tanks-tile'
import { WindCompass } from '@/components/wind-compass'
import { BottomDrawer } from '@/components/ui/bottom-drawer'
import { ForecastDrawer } from '@/components/forecast-drawer'
import { RoutePlannerDrawer } from '@/components/route-planner-drawer'
import { SatChartsDrawer } from '@/components/sat-charts-drawer'
import { RouteTile } from '@/components/route-tile'
import { useRoutes } from '@/hooks/use-routes'
import { useSatCharts } from '@/hooks/use-sat-charts'
import { useDashboardRouteId } from '@/hooks/use-dashboard-route'
import { useRouteActivation } from '@/hooks/use-route-activation'
import { TideDrawer } from '@/components/tide-drawer'
import { useElectricalState } from '@/hooks/use-electrical-state'
import { useNearbyVessels } from '@/hooks/use-nearby-vessels'
import { useAnchorWatch } from '@/hooks/use-anchor-watch'
import { useAnchorWatchAutoClose } from '@/hooks/use-anchor-watch-auto-close'
import { usePlaceName } from '@/hooks/use-place-name'
import { useTanksState } from '@/hooks/use-tanks-state'
import { useTideToday } from '@/hooks/use-tide-today'
import { useVesselState } from '@/hooks/use-vessel-state'
import { useServerTrails } from '@/hooks/use-server-trails'
import { useWeatherForecast } from '@/hooks/use-weather-forecast'
import { useWeatherToday } from '@/hooks/use-weather-today'
import { useCZoneSwitches } from '@/hooks/use-czone-switches'
import { useDepthTrend } from '@/hooks/use-depth-trend'
import { useTheme, type Theme } from '@/hooks/use-theme'
import { useDarkMode } from '@/hooks/use-dark-mode'
import { anchorConfig, uiConfig } from '@/config/app-config'
import { Button } from '@/components/ui/button'
import { Tile } from '@/components/ui/tile'

const ANCHOR_IMAGERY_ENABLED_KEY = 'anchorWatch.imagery.enabled'
const AUTO_CLOSE_ANCHOR_WATCH_KEY = 'anchorWatch.autoClose.enabled'

function formatCoordinate(value: number | null, latitude: boolean) {
  if (value === null) {
    return '—'
  }

  const absolute = Math.abs(value)
  const degrees = Math.floor(absolute)
  const minutesFloat = (absolute - degrees) * 60
  const minutes = Math.floor(minutesFloat)
  const seconds = (minutesFloat - minutes) * 60
  const hemisphere = latitude ? (value >= 0 ? 'N' : 'S') : value >= 0 ? 'E' : 'W'

  return `${degrees}° ${String(minutes).padStart(2, '0')}' ${seconds.toFixed(1).padStart(4, '0')}" ${hemisphere}`
}

function formatHeading(headingTrue: number | null) {
  if (headingTrue === null) {
    return '—'
  }

  const normalized = ((headingTrue % 360) + 360) % 360
  const directions = ['N', 'NNE', 'NE', 'ENE', 'E', 'ESE', 'SE', 'SSE', 'S', 'SSW', 'SW', 'WSW', 'W', 'WNW', 'NW', 'NNW']
  const direction = directions[Math.round(normalized / 22.5) % directions.length]

  return `${Math.round(normalized)}° ${direction}`
}

function fahrenheitToCelsius(temp: number) {
  return (temp - 32) * (5 / 9)
}

function formatTimeToGo(hours: number | null) {
  if (hours === null || !Number.isFinite(hours)) {
    return '—'
  }

  const absHours = Math.abs(hours)
  const totalMinutes = Math.max(0, Math.round(absHours * 60))
  if (totalMinutes === 0) {
    return '—'
  }

  if (absHours >= 24 * 7) {
    return `${Math.round(absHours / (24 * 7))}w`
  }

  if (absHours >= 24) {
    const days = Math.floor(absHours / 24)
    let remainingHours = Math.round(absHours - days * 24)
    if (remainingHours === 24) {
      remainingHours = 0
    }
    if (remainingHours === 0) {
      return `${days}d`
    }
    return `${days}d ${remainingHours}h`
  }

  if (absHours >= 10) {
    return `${Math.round(absHours)}h`
  }

  const hh = Math.floor(totalMinutes / 60)
  const mm = totalMinutes % 60

  return `${hh}h ${mm.toString().padStart(2, '0')}m`
}

type WindMetricCardProps = {
  title: string
  value: ReactNode
  align?: 'left' | 'right'
  className?: string
  style?: CSSProperties
  valueFirst?: boolean
}

function WindMetricCard({ title, value, align = 'left', className = '', style, valueFirst = false }: WindMetricCardProps) {
  const alignmentClass = align === 'right' ? 'items-end text-right' : 'items-start text-left'
  const label = <p key="label" className="text-[10px] leading-none uppercase tracking-[0.16em] text-muted-foreground">{title}</p>
  const reading = <p key="value" className="font-display text-2xl leading-[0.86] text-primary md:text-3xl">{value}</p>

  return (
    <div
      className={`relative flex flex-col justify-start gap-0.5 rounded-2xl border bg-background/80 px-4 py-2 shadow-[0_10px_24px_rgba(15,23,42,0.08)] ${alignmentClass} ${className}`.trim()}
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
  gust10mLabel: ReactNode
  gust1hLabel: ReactNode
  headingTrue: number | null
  windAngleApparentDeg: number | null
  windSide: 'port' | 'starboard' | null
  windAngleRelativeDeg: number | null
  windSpeedApparentKts: number | null
}

function WindGaugeCluster({
  cfg, masks, visibilityClassName,
  setValue, driftLabel, gust10mLabel, gust1hLabel,
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
            <WindMetricCard title="SET" value={setValue} style={{ width: cfg.topCardW, height: cfg.cardH, ...masks.tl }} />
          </div>

          <div className="self-start justify-self-end">
            <WindMetricCard title="DRIFT" value={driftLabel} align="right" style={{ width: cfg.topCardW, height: cfg.cardH, ...masks.tr }} />
          </div>

          <div className="self-end justify-self-start">
            <WindMetricCard title="MAX GUST 10M" value={gust10mLabel} valueFirst style={{ width: cfg.bottomCardW, height: cfg.cardH, ...masks.bl }} />
          </div>

          <div className="self-end justify-self-end">
            <WindMetricCard title="MAX GUST 1HR" value={gust1hLabel} align="right" valueFirst style={{ width: cfg.bottomCardW, height: cfg.cardH, ...masks.br }} />
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

export function App() {
  const [isDrawerOpen, setIsDrawerOpen] = useState(false)
  const [activeDrawerTab, setActiveDrawerTab] = useState('forecast')
  const [showAnchorImagery, setShowAnchorImagery] = useState(() => {
    const raw = globalThis.localStorage?.getItem(ANCHOR_IMAGERY_ENABLED_KEY)
    return raw === 'true'
  })
  const [autoCloseAnchorWatchEnabled, setAutoCloseAnchorWatchEnabled] = useState(() => {
    const raw = globalThis.localStorage?.getItem(AUTO_CLOSE_ANCHOR_WATCH_KEY)
    // Default to true if not set
    return raw !== 'false'
  })

  useEffect(() => {
    globalThis.localStorage?.setItem(ANCHOR_IMAGERY_ENABLED_KEY, String(showAnchorImagery))
  }, [showAnchorImagery])

  useEffect(() => {
    globalThis.localStorage?.setItem(AUTO_CLOSE_ANCHOR_WATCH_KEY, String(autoCloseAnchorWatchEnabled))
  }, [autoCloseAnchorWatchEnabled])

  const [theme, setTheme] = useTheme()
  const [isDarkTheme, toggleDarkMode] = useDarkMode()
  const { routes, loading: routesLoading, error: routesError, createRoute, updateRoute, deleteRoute } = useRoutes()
  const {
    charts: satCharts,
    loading: satChartsLoading,
    error: satChartsError,
    uploadChart,
    deleteChart: deleteSatChart,
  } = useSatCharts()
  const [dashboardRouteId, setDashboardRouteId] = useDashboardRouteId()
  const {
    status: routeActivationStatus,
    activating: routeActivating,
    deactivating: routeDeactivating,
    activateError: routeActivateError,
    activate: activateRoute,
    deactivate: deactivateRoute,
  } = useRouteActivation()

  // Handle anchor watch auto-close notifications
  const [toastMessage, setToastMessage] = useState<string | null>(null)
  useEffect(() => {
    const handleAutoClose = () => {
      setToastMessage('Anchor watch cleared — engines running, position outside zone')
      // Auto-dismiss after 5 seconds
      const timer = setTimeout(() => setToastMessage(null), 5000)
      return () => clearTimeout(timer)
    }

    window.addEventListener('anchor-watch-auto-closed', handleAutoClose)
    return () => window.removeEventListener('anchor-watch-auto-closed', handleAutoClose)
  }, [])

  const {
    depth,
    currentDriftKts,
    currentSetDeg,
    navigationState,
    latitude,
    longitude,
    gnssQualityIndicator,
    gnssHdop,
    gnssSatellites,
    gnssValidationState,
    gnssValidationReason,
    gnssCriticalAlert,
    headingTrue,
    windSpeedApparentKts,
    windAngleApparentDeg,
    windSide,
    windAngleRelativeDeg,
    maxGust10mKts,
    maxGust1hKts,
    generatorState,
    generatorManualStart,
    generatorManualStartTimer,
    generatorRunningByCondition,
    generatorRuntime,
    engine0Rpm,
    engine1Rpm,
    speedOverGroundKts,
  } = useVesselState(uiConfig.vesselStateRefreshSeconds)
  const { vessels: nearbyVessels, loading: nearbyVesselsLoading } = useNearbyVessels(uiConfig.vesselStateRefreshSeconds)
  const { tanks, loading: tanksLoading } = useTanksState(uiConfig.vesselStateRefreshSeconds)
  const {
    batterySocPercent,
    chargingCurrentA,
    chargingPowerW,
    solarOutputW,
    acOutputW,
    dc12vPowerW,
    dc24vVoltageV,
    generatorRealPowerW,
    alternator0,
    alternator1,
    batteryRatePercentPerHour,
    timeToGoHours,
  } = useElectricalState(5)
  const { weather } = useWeatherToday(uiConfig.vesselStateRefreshSeconds)
  const { tide } = useTideToday(uiConfig.vesselStateRefreshSeconds)
  const {
    forecast,
    hourlyToday: forecastHourlyToday,
    summary: forecastSummary,
    loading: forecastLoading,
    error: forecastError,
    isCached: forecastIsCached,
    updatedAt: forecastUpdatedAt,
    ttlSeconds: forecastTtlSeconds,
    refetch: refetchForecast,
  } = useWeatherForecast(uiConfig.forecastRefreshSeconds)
  const anchorWatch = useAnchorWatch(
    latitude,
    longitude,
    navigationState,
    uiConfig.vesselStateRefreshSeconds,
    gnssCriticalAlert,
  )
  const { isAutoCloseArmed, motoringSecondsElapsed } = useAnchorWatchAutoClose(
    navigationState,
    anchorWatch.distanceMeters,
    anchorWatch.radiusMeters,
    anchorWatch.anchorState !== 'none',
    autoCloseAnchorWatchEnabled,
  )
  const { getSelfTrail, getAisTrails } = useServerTrails(5000)
  const placeName = usePlaceName(latitude, longitude, uiConfig.vesselStateRefreshSeconds)
  const depthTrend = useDepthTrend('3h', 60)
  const { switches: czoneSwitches, loading: czoneLoading, pending: czonePending, toggleSwitch: toggleCZone } = useCZoneSwitches(5)
  const isImperialDistance = uiConfig.distanceUnits === 'imperial'
  const tideUnit = isImperialDistance ? 'ft' : 'm'
  const tideFtToDisplay = (ft: number) => (isImperialDistance ? ft : ft / 3.28084)
  const tideExtremes = [
    { isHigh: true, time: tide.high_tide_time, heightFt: tide.high_tide_height_ft },
    { isHigh: false, time: tide.low_tide_time, heightFt: tide.low_tide_height_ft },
  ].sort((a, b) => new Date(a.time).getTime() - new Date(b.time).getTime())
  const depthValue =
    depth !== null
      ? isImperialDistance
        ? (depth * 3.28084).toFixed(1)
        : depth.toFixed(1)
      : '—'
  const depthUnitLabel = isImperialDistance ? 'feet' : 'm'
  const awaLabel = windAngleApparentDeg !== null ? `${Math.round(windAngleApparentDeg).toString().padStart(3, '0')}°` : '---°'
  const headingLabel = formatHeading(headingTrue)
  const satellitesValueClass = gnssValidationState === 'critical'
    ? 'text-red-600'
    : gnssValidationState === 'degraded'
      ? 'text-amber-600'
      : 'text-secondary'
  const gnssDiagnosticLabel = [
    `Q${gnssQualityIndicator !== null ? gnssQualityIndicator : '—'}`,
    `HDOP ${gnssHdop !== null ? gnssHdop.toFixed(1) : '—'}`,
    gnssValidationState ?? '—',
    gnssValidationReason ?? '',
  ].filter(Boolean).join(' · ')
  const setDirectionLabel = currentSetDeg !== null ? formatHeading(currentSetDeg).split(' ').slice(1).join(' ') : '—'
  const setDegreesLabel = currentSetDeg !== null ? `${Math.round(((currentSetDeg % 360) + 360) % 360)}°` : '—'
  const setArrowRotation = currentSetDeg !== null ? ((currentSetDeg % 360) + 360) % 360 : 0
  const driftLabel = currentDriftKts !== null ? (
    <>
      {currentDriftKts.toFixed(1)}
      <span className="ml-1 text-xl text-muted-foreground">kts</span>
    </>
  ) : '—'
  const gust10mLabel = maxGust10mKts !== null ? (
    <>
      {maxGust10mKts.toFixed(1)}
      <span className="ml-1 text-xl text-muted-foreground">kts</span>
    </>
  ) : '—'
  const gust1hLabel = maxGust1hKts !== null ? (
    <>
      {maxGust1hKts.toFixed(1)}
      <span className="ml-1 text-xl text-muted-foreground">kts</span>
    </>
  ) : '—'
  const socLabel = batterySocPercent !== null ? Math.round(batterySocPercent).toString() : '—'
  const socBarWidth = `${Math.max(0, Math.min(100, batterySocPercent ?? 0))}%`
  const chargingCurrentLabel = chargingCurrentA !== null
    ? `${chargingCurrentA >= 0 ? '+' : '-'}${Math.abs(chargingCurrentA).toFixed(1)}`
    : '—'
  const chargingPowerLabel = chargingPowerW !== null
    ? `${chargingPowerW >= 0 ? '+' : '-'}${Math.abs(Math.round(chargingPowerW))}`
    : '—'
  const isDischarging = (chargingCurrentA !== null && chargingCurrentA < 0) || (chargingPowerW !== null && chargingPowerW < 0)
  const chargingValueClass = isDischarging ? 'text-amber-600' : 'text-secondary'
  const solarOutputLabel = solarOutputW !== null ? Math.round(solarOutputW).toString() : '—'
  const acOutputLabel = acOutputW !== null ? Math.round(acOutputW).toString() : '—'
  const dc12vPowerLabel = dc12vPowerW !== null ? Math.round(dc12vPowerW).toString() : '—'
  const dc24vVoltageLabel = dc24vVoltageV !== null ? dc24vVoltageV.toFixed(2) : '—'
  const chargeRateLabel = batteryRatePercentPerHour !== null
    ? `${batteryRatePercentPerHour >= 0 ? '+' : ''}${batteryRatePercentPerHour.toFixed(1)}`
    : '—'
  const chargeRateClass = batteryRatePercentPerHour !== null && batteryRatePercentPerHour < 0 ? 'text-amber-600' : 'text-secondary'
  const timeToGoLabel = formatTimeToGo(timeToGoHours)
  const timeToGoClass = timeToGoHours !== null && timeToGoHours < 0 ? 'text-amber-600' : 'text-secondary'

  let TimeToGoIcon = BatteryFull
  if (!isDischarging && batteryRatePercentPerHour !== null && batteryRatePercentPerHour > 0) {
    TimeToGoIcon = BatteryCharging
  } else {
    const soc = batterySocPercent ?? 0
    if (soc <= 20) TimeToGoIcon = BatteryWarning
    else if (soc <= 33) TimeToGoIcon = BatteryLow
    else if (soc <= 66) TimeToGoIcon = BatteryMedium
    else TimeToGoIcon = BatteryFull
  }

  const setValue = currentSetDeg !== null && currentDriftKts !== 0
    ? (
      <span className="inline-flex items-center gap-2">
        <span>{setDegreesLabel}</span>
        <span
          className="inline-flex h-7 w-7 items-center justify-center rounded-full border border-border/70 text-muted-foreground"
          title={`Set direction ${setDirectionLabel}`}
        >
          <ArrowUp className="h-4 w-4" style={{ transform: `rotate(${setArrowRotation}deg)` }} />
        </span>
      </span>
    )
    : <span className="font-display text-4xl tabular-nums leading-none text-primary">—</span>
  const isAlternatorTileVisible = (engine0Rpm !== null && engine0Rpm > 0) || (engine1Rpm !== null && engine1Rpm > 0)

  const handleDropAnchorHere = () => {
    if (latitude === null || longitude === null) return
    void anchorWatch.setAnchorHere(latitude, longitude)
  }

  return (
    <div className="min-h-screen p-4 pb-20 md:p-6 md:pb-24">
      {toastMessage && (
        <div className="fixed left-4 right-4 top-4 z-50 mx-auto max-w-md rounded-lg border border-amber-500/40 bg-amber-500/10 px-4 py-3 text-sm text-amber-600 shadow-lg md:left-auto md:right-4">
          {toastMessage}
        </div>
      )}
      <div className="mx-auto flex max-w-[1800px] flex-col gap-4">
        <MarineHeader isDark={isDarkTheme} onToggleDarkMode={toggleDarkMode} />

        <div className="grid grid-cols-1 gap-4 lg:grid-cols-3">

          <aside className="space-y-4">
            <section className="rounded-lg border bg-card p-4">
              <div className="mb-3 flex items-center justify-between">
                <h1 className="font-display text-sm tracking-[0.24em] text-muted-foreground">Apparent Wind - Course Up</h1>
                <Button variant="outline" size="sm">
                  <Compass className="h-4 w-4" />
                  AWA {awaLabel}
                </Button>
              </div>
              <WindGaugeCluster
                cfg={WIND_MOBILE_CFG}
                masks={WIND_MOBILE_MASKS}
                visibilityClassName="md:hidden"
                setValue={setValue}
                driftLabel={driftLabel}
                gust10mLabel={gust10mLabel}
                gust1hLabel={gust1hLabel}
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
                gust10mLabel={gust10mLabel}
                gust1hLabel={gust1hLabel}
                headingTrue={headingTrue}
                windAngleApparentDeg={windAngleApparentDeg}
                windSide={windSide}
                windAngleRelativeDeg={windAngleRelativeDeg}
                windSpeedApparentKts={windSpeedApparentKts}
              />
            </section>

            <div
              onClick={() => { setActiveDrawerTab('tides'); setIsDrawerOpen(true) }}
              className="cursor-pointer transition-opacity hover:opacity-80"
            >
              <Tile title="Depth & Tide">
                <div className="mt-1 rounded-md border bg-background/60 px-3 py-3">
                  <p className="text-[11px] uppercase tracking-[0.16em] text-muted-foreground">Depth</p>
                  <div className="mt-1 flex items-center gap-4">
                    <p className="shrink-0 font-display text-4xl text-secondary">
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
                    <span className="font-display text-4xl leading-none text-secondary">
                      {tide.current_tide_height_ft >= 0 ? tideFtToDisplay(tide.current_tide_height_ft).toFixed(isImperialDistance ? 1 : 2) : '—'}
                    </span>
                    <span className="text-lg text-muted-foreground">{tideUnit}</span>
                    <span className="ml-1 inline-flex items-center gap-1 text-xs font-semibold text-secondary">
                      {tide.tide_direction === 'Falling' ? <ArrowDown className="h-3 w-3" /> : <ArrowUp className="h-3 w-3" />}
                      {tide.tide_direction}
                    </span>
                  </div>
                  <div className="mt-2 flex flex-row flex-wrap items-center gap-x-4 gap-y-1">
                    {tideExtremes.map((extreme) => (
                      <p key={extreme.isHigh ? 'high' : 'low'} className="inline-flex items-center gap-1.5 text-xs text-foreground">
                        {extreme.isHigh ? (
                          <ArrowUp className="h-3.5 w-3.5 text-secondary" />
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
            <Tile title="Position">
              <div className="mt-2 flex items-start justify-between gap-4">
                <div>
                  <p className="font-mono text-sm">{formatCoordinate(latitude, true)}</p>
                  <p className="font-mono text-sm">{formatCoordinate(longitude, false)}</p>
                </div>
                <div className="shrink-0 text-right">
                  <p className="font-mono text-sm text-muted-foreground">HDG {headingLabel}</p>
                  {gnssValidationState === 'degraded' || gnssValidationState === 'critical' ? (
                    <p
                      className={`mt-1 max-w-[170px] truncate font-mono text-[10px] ${satellitesValueClass}`}
                      title={gnssDiagnosticLabel}
                    >
                      {gnssDiagnosticLabel}
                    </p>
                  ) : (
                    <p className="mt-1 font-mono text-sm text-muted-foreground">
                      SATS <span className={satellitesValueClass}>{gnssSatellites !== null ? gnssSatellites : '—'}</span>
                    </p>
                  )}
                </div>
              </div>
              <div className="mt-3 truncate rounded-md bg-secondary/10 px-3 py-2 font-display text-2xl text-secondary">
                {placeName ?? '—'}
              </div>
            </Tile>
            <div onClick={() => setIsDrawerOpen(true)} className="cursor-pointer transition-opacity hover:opacity-80">
              <Tile title="Today & Now">
                <div className="mt-2 grid grid-cols-[auto_1fr_auto] items-center gap-3">
                  <div className="grid h-12 w-12 place-items-center rounded-full bg-amber-100 text-amber-600">
                    <CloudSun className="h-7 w-7" />
                  </div>
                  <div>
                    <div className="flex items-end gap-1">
                      <p className="font-display text-5xl leading-none text-amber-700">
                        {weather.temperature_f >= 0 ? Math.round(uiConfig.distanceUnits === 'metric' ? fahrenheitToCelsius(weather.temperature_f) : weather.temperature_f) : '—'}
                      </p>
                      <p className="pb-1 text-xl font-semibold text-amber-700">{uiConfig.distanceUnits === 'metric' ? '°C' : '°F'}</p>
                    </div>
                    <p className="text-sm font-semibold uppercase tracking-[0.1em] text-foreground">
                      {weather.condition}
                    </p>
                    <p className="text-xs text-muted-foreground">
                      {weather.high_temp_f >= 0 ? `↑${Math.round(uiConfig.distanceUnits === 'metric' ? fahrenheitToCelsius(weather.high_temp_f) : weather.high_temp_f)}°` : '↑—°'} {weather.low_temp_f >= 0 ? `↓${Math.round(uiConfig.distanceUnits === 'metric' ? fahrenheitToCelsius(weather.low_temp_f) : weather.low_temp_f)}°` : '↓—°'}
                    </p>
                  </div>
                  <div className="text-right">
                    <p className="font-display text-xl leading-none text-secondary">
                      {weather.wind_speed_kts >= 0 ? `${Math.round(weather.wind_speed_kts)} kts` : '— kts'} {weather.wind_direction}
                    </p>
                    <p className="mt-1 text-xs text-muted-foreground">
                      Gust {weather.wind_gust_kts >= 0 ? `${Math.round(weather.wind_gust_kts)} kts` : '— kts'}
                    </p>
                    <p className="mt-1 text-xs text-muted-foreground">
                      {weather.precipitation_pct >= 0 ? `${Math.round(weather.precipitation_pct)}% precip` : '—% precip'}
                    </p>
                    <p className="mt-1 text-xs text-muted-foreground">
                      Sea {weather.sea_temperature_f >= 0 ? `${Math.round(uiConfig.distanceUnits === 'metric' ? fahrenheitToCelsius(weather.sea_temperature_f) : weather.sea_temperature_f)}°` : '—°'}
                    </p>
                  </div>
                </div>

              </Tile>
            </div>
          </aside>

          <aside className="space-y-4">
            {!isAlternatorTileVisible && (
              <>
                <AnchorWatchTile
                  watch={anchorWatch}
                  lat={latitude}
                  lon={longitude}
                  depthMeters={depth}
                  currentDriftKts={currentDriftKts}
                  currentSetDeg={currentSetDeg}
                  isImperial={isImperialDistance}
                  vesselHeadingDeg={headingTrue}
                  vesselTrail={getSelfTrail}
                  aisVessels={nearbyVessels}
                  aisTrails={getAisTrails}
                  isDarkTheme={isDarkTheme}
                  showImageryLayer={showAnchorImagery}
                  onImageryToggle={setShowAnchorImagery}
                  onFullscreen={() => { setActiveDrawerTab('anchor-watch'); setIsDrawerOpen(true) }}
                />

                <RodeScopeTile
                  anchorState={anchorWatch.anchorState}
                  rodeDeployedM={anchorWatch.rodeDeployedM}
                  seaState={anchorWatch.seaState}
                  seabedType={anchorWatch.seabedType}
                  depthM={depth}
                  windKts={windSpeedApparentKts}
                  isImperial={isImperialDistance}
                  anchorConfig={anchorConfig}
                  onUpdate={anchorWatch.updateRodeAndConditions}
                />
              </>
            )}

            <TanksTile tanks={tanks} loading={tanksLoading} />
            <RouteTile
              speedKts={speedOverGroundKts ?? 0}
              routes={routes}
              dashboardRouteId={dashboardRouteId}
              onOpen={() => { setActiveDrawerTab('routes'); setIsDrawerOpen(true) }}
            />
            <NearbyVesselsTile vessels={nearbyVessels} loading={nearbyVesselsLoading} distanceUnits={uiConfig.distanceUnits} />
          </aside>

          <aside className="space-y-4">
            <Tile title="Battery & Power">
              <div className="mt-1 grid grid-cols-[minmax(0,1.2fr)_minmax(0,1fr)] gap-2">
                <div className="min-w-0 rounded-md border bg-background/60 px-3 py-3">
                  <div className="flex items-baseline gap-2">
                    <span className="font-display text-6xl leading-none tabular-nums text-primary md:text-7xl">{socLabel}</span>
                    <span className="shrink-0 text-2xl leading-none text-foreground md:text-3xl">%</span>
                  </div>
                  <div className="mt-3 flex items-center gap-2">
                    <div className="h-1.5 flex-1 rounded-full bg-muted/60">
                      <div className="h-full rounded-full bg-primary" style={{ width: socBarWidth }} />
                    </div>
                    <span className="shrink-0 font-display text-sm tabular-nums leading-none text-secondary">
                      {dc24vVoltageLabel}<span className="text-xs text-muted-foreground">V</span>
                    </span>
                  </div>
                </div>
                <div className="min-w-0 rounded-md border bg-background/60 px-3 py-3">
                  <p className="text-[11px] uppercase tracking-[0.16em] text-muted-foreground">Battery</p>
                  <p className={`font-display text-3xl leading-none md:text-3xl ${chargingValueClass}`}>
                    {chargingCurrentLabel}
                    <span className="ml-1 text-xl text-muted-foreground">A</span>
                  </p>
                  <p className={`mt-1 font-display text-3xl leading-none ${chargingValueClass}`}>
                    {chargingPowerLabel}
                    <span className="ml-1 text-xl text-muted-foreground">W</span>
                  </p>
                </div>
              </div>

              <div className="mt-2 grid grid-cols-2 gap-2 text-sm">
                <div className="rounded-md border bg-background/60 px-3 py-2">
                  <p className="text-[11px] uppercase tracking-[0.16em] text-muted-foreground">AC Draw</p>
                  <p className="font-display text-4xl leading-none text-primary">
                    {acOutputLabel}
                    <span className="ml-1 text-xl text-muted-foreground">W</span>
                  </p>
                </div>
                <div className="rounded-md border bg-background/60 px-3 py-2">
                  <p className="text-[11px] uppercase tracking-[0.16em] text-muted-foreground">DC Draw</p>
                  <p className="font-display text-4xl leading-none text-primary">
                    {dc12vPowerLabel}
                    <span className="ml-1 text-xl text-muted-foreground">W</span>
                  </p>
                </div>
              </div>

              <div className="mt-2 rounded-md border bg-background/60 px-3 py-2">
                <p className="text-[11px] uppercase tracking-[0.16em] text-muted-foreground">Solar</p>
                <p className="font-display text-4xl leading-none text-secondary">
                  {solarOutputLabel}
                  <span className="ml-1 text-xl text-muted-foreground">W</span>
                </p>
              </div>

              <div className="mt-2 grid grid-cols-2 gap-2">
                <div className="rounded-md border bg-background/60 px-3 py-2">
                  <p className="flex items-center gap-1.5 text-[11px] uppercase tracking-[0.16em] text-muted-foreground">
                    Time Remaining
                    {timeToGoLabel !== '—' && <TimeToGoIcon className="h-3 w-3" />}
                  </p>
                  <p className={`mt-1 font-display text-3xl leading-none ${timeToGoClass}`}>
                    {timeToGoLabel}
                  </p>
                </div>
                <div className="rounded-md border bg-background/60 px-3 py-2">
                  <p className="text-[11px] uppercase tracking-[0.16em] text-muted-foreground">Charge Rate</p>
                  <p className={`mt-1 font-display text-3xl leading-none ${chargeRateClass}`}>
                    {chargeRateLabel}
                    <span className="ml-1 text-xl text-muted-foreground">%/hr</span>
                  </p>
                </div>
              </div>

              <div className="mt-2 rounded-md border bg-background/60 px-3 py-2">
                <div className="flex items-center justify-between text-sm uppercase tracking-[0.16em] text-muted-foreground">
                  <div className="inline-flex items-center gap-2">
                    <span className="h-2.5 w-2.5 rounded-full bg-muted-foreground/40" />
                    Hot Water
                  </div>
                  <span className="font-semibold">Off</span>
                </div>
                <div className="mt-2 grid grid-cols-4 gap-2">
                  <Button variant="outline" size="sm" className="h-9 text-xs">
                    1 HR
                  </Button>
                  <Button variant="outline" size="sm" className="h-9 text-xs">
                    1.5 HR
                  </Button>
                  <Button variant="outline" size="sm" className="h-9 text-xs">
                    2 HR
                  </Button>
                  <Button variant="outline" size="sm" className="h-9 text-xs">
                    ON
                  </Button>
                </div>
              </div>
            </Tile>

            <AlternatorTile
              port={alternator0}
              starboard={alternator1}
              enginesRunning={isAlternatorTileVisible}
            />
            <GeneratorTile
              generatorState={generatorState}
              generatorManualStart={generatorManualStart}
              generatorManualStartTimer={generatorManualStartTimer}
              generatorRunningByCondition={generatorRunningByCondition}
              generatorRuntime={generatorRuntime}
              generatorRealPowerW={generatorRealPowerW}
              batterySocPercent={batterySocPercent}
              batteryRatePercentPerHour={batteryRatePercentPerHour}
            />
            <CZoneSwitchesTile switches={czoneSwitches} loading={czoneLoading} pending={czonePending} onToggle={toggleCZone} />
          </aside>
        </div>

        {/* Bottom Drawers */}
        <BottomDrawer
          isOpen={isDrawerOpen}
          onOpen={() => setIsDrawerOpen(true)}
          onClose={() => setIsDrawerOpen(false)}
          tabs={[
            { id: 'forecast', label: 'Forecast' },
            { id: 'tides', label: 'Tides' },
            { id: 'routes', label: 'Routes' },
            { id: 'charts', label: 'Charts' },
            { id: 'radar', label: 'Radar' },
            {
              id: 'anchor-watch',
              label: 'Anchor Watch',
              indicator: anchorWatch.anchorState !== 'none',
            },
            { id: 'settings', label: 'Settings' },
          ]}
          activeTab={activeDrawerTab}
          onTabChange={setActiveDrawerTab}
        >
          {activeDrawerTab === 'forecast' && (
            <div className="px-6 py-4">
              <ForecastDrawer
              forecast={forecast}
              hourlyToday={forecastHourlyToday}
              summary={forecastSummary}
              loading={forecastLoading}
              error={forecastError}
              isCached={forecastIsCached}
              updatedAt={forecastUpdatedAt}
              ttlSeconds={forecastTtlSeconds}
              onRetry={refetchForecast}
              unit={uiConfig.distanceUnits as 'imperial' | 'metric'}
            />
            </div>
          )}
          {activeDrawerTab === 'tides' && (
            <div className="px-6 py-4">
              <TideDrawer isImperial={isImperialDistance} />
            </div>
          )}
          {activeDrawerTab === 'routes' && (
            <div className="px-6 py-4">
              <RoutePlannerDrawer
                isDarkTheme={isDarkTheme}
                currentSpeedKts={speedOverGroundKts}
                vesselLat={latitude}
                vesselLon={longitude}
                routes={routes}
                loading={routesLoading}
                error={routesError}
                createRoute={createRoute}
                updateRoute={updateRoute}
                deleteRoute={deleteRoute}
                dashboardRouteId={dashboardRouteId}
                onSetDashboardRouteId={setDashboardRouteId}
                activationStatus={routeActivationStatus}
                activating={routeActivating}
                deactivating={routeDeactivating}
                activateError={routeActivateError}
                onActivate={activateRoute}
                onDeactivate={deactivateRoute}
                satCharts={satCharts}
              />
            </div>
          )}
          {activeDrawerTab === 'charts' && (
            <div className="px-6 py-4">
              <SatChartsDrawer
                charts={satCharts}
                loading={satChartsLoading}
                error={satChartsError}
                uploadChart={uploadChart}
                deleteChart={deleteSatChart}
              />
            </div>
          )}
          {activeDrawerTab === 'radar' && (
            <div className="px-6 py-4">
              <RadarDrawer latitude={latitude} longitude={longitude} />
            </div>
          )}
          {activeDrawerTab === 'settings' && (
            <div className="px-6 py-4">
              <SignalKSettingsPanel
                autoCloseAnchorWatchEnabled={autoCloseAnchorWatchEnabled}
                onAutoCloseAnchorWatchToggle={setAutoCloseAnchorWatchEnabled}
                theme={theme}
                onThemeChange={(v) => setTheme(v as Theme)}
              />
            </div>
          )}
          {activeDrawerTab === 'anchor-watch' && (
            anchorWatch.anchorLat !== null && anchorWatch.anchorLon !== null
              ? (
                <div className="h-full">
                  <AnchorWatchDrawer
                    vesselLat={latitude ?? anchorWatch.anchorLat}
                    vesselLon={longitude ?? anchorWatch.anchorLon}
                    vesselHeadingDeg={headingTrue}
                    anchorLat={anchorWatch.anchorLat}
                    anchorLon={anchorWatch.anchorLon}
                    radiusMeters={anchorWatch.radiusMeters}
                    depthMeters={depth}
                    currentDriftKts={currentDriftKts}
                    currentSetDeg={currentSetDeg}
                    distanceMeters={anchorWatch.distanceMeters}
                    bearingDeg={anchorWatch.bearingDeg}
                    vesselTrail={getSelfTrail}
                    aisVessels={nearbyVessels}
                    aisTrails={getAisTrails}
                    isDarkTheme={isDarkTheme}
                    showImageryLayer={showAnchorImagery}
                    onImageryToggle={setShowAnchorImagery}
                    onAnchorReposition={anchorWatch.updatePosition}
                    onRadiusChange={anchorWatch.updateRadius}
                    onClearAnchor={anchorWatch.clearAnchor}
                    isImperial={isImperialDistance}
                    isAutoCloseArmed={isAutoCloseArmed}
                    motoringSecondsElapsed={motoringSecondsElapsed}
                  />
                </div>
              )
              : (
                <div className="px-6 py-8 text-center text-muted-foreground">
                  <div className="mb-4">Not monitoring</div>
                  <Button
                    className="h-11 bg-teal-600 text-teal-50 hover:bg-teal-700"
                    disabled={latitude === null || longitude === null}
                    onClick={handleDropAnchorHere}
                  >
                    <Anchor className="h-4 w-4" />
                    Drop
                  </Button>
                </div>
              )
          )}
        </BottomDrawer>
      </div>
    </div>
  )
}
