import {
  Anchor,
  BellRing,
  CloudSun,
  LayoutDashboard,
  Map,
  Plus,
  Radar as RadarIcon,
  Route,
  Settings,
} from 'lucide-react'
import { useCallback, useEffect, useMemo, useRef, useState, type ReactNode } from 'react'

import { AnchorWatchTile } from '@/components/anchor-watch-tile'
import { AnchorWatchDrawer } from '@/components/anchor-watch-drawer'
import { AlternatorTile } from '@/components/alternator-tile'
import { BatteryPowerTile } from '@/components/battery-power-tile'
import { HotWaterTile } from '@/components/hot-water-tile'
import { DepthTideTile } from '@/components/depth-tide-tile'
import { PositionTile } from '@/components/position-tile'
import { TodayNowTile } from '@/components/today-now-tile'
import { WindTile } from '@/components/wind-tile'
import { MarineHeader } from '@/components/marine-header'
import { VesselStatusBar } from '@/components/vessel-status-bar'
import { ForecastWarningsBanner } from '@/components/forecast-warnings-banner'
import { AlarmBanner } from '@/components/alarm-banner'
import { AlarmsDrawer } from '@/components/alarms-drawer'
import { NearbyVesselsTile } from '@/components/nearby-vessels-tile'
import { RodeScopeTile } from '@/components/rode-scope-tile'
import { RadarDrawer } from '@/components/radar-drawer'
import { SettingsPage, type SettingsPageHandle } from '@/components/settings/settings-page'
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '@/components/ui/alert-dialog'
import { AutopilotTile } from '@/components/autopilot-tile'
import { CZoneSwitchesTile } from '@/components/czone-switches-tile'
import { GeneratorTile } from '@/components/generator-tile'
import { SolarTile } from '@/components/solar-tile'
import { TanksTile } from '@/components/tanks-tile'
import { ForecastDrawer } from '@/components/forecast-drawer'
import { RoutePlannerDrawer } from '@/components/route-planner-drawer'
import { SatChartsDrawer } from '@/components/sat-charts-drawer'
import { RouteTile } from '@/components/route-tile'
import { DashboardBentoGrid } from '@/components/dashboard-bento-grid'
import { LayoutModeToggle } from '@/components/layout-mode-toggle'
import { Toaster } from '@/components/ui/sonner'
import { useRoutes } from '@/hooks/use-routes'
import { useSatCharts } from '@/hooks/use-sat-charts'
import { useDashboardRouteId } from '@/hooks/use-dashboard-route'
import { useDashboardPages } from '@/hooks/use-dashboard-pages'
import { useActiveDashboardPageId } from '@/hooks/use-active-dashboard-page'
import { DashboardPageSwitcher } from '@/components/dashboard-page-switcher'
import { useRouteActivation } from '@/hooks/use-route-activation'
import { useElectricalState } from '@/hooks/use-electrical-state'
import { useSolarState } from '@/hooks/use-solar-state'
import { useNearbyVessels } from '@/hooks/use-nearby-vessels'
import { useAnchorWatch } from '@/hooks/use-anchor-watch'
import { useAnchorWatchAutoClose } from '@/hooks/use-anchor-watch-auto-close'
import { usePlaceName } from '@/hooks/use-place-name'
import { useTanksState } from '@/hooks/use-tanks-state'
import { useTideToday } from '@/hooks/use-tide-today'
import { findActiveWindBulletin, useForecastWarnings } from '@/hooks/use-forecast-warnings'
import { useVesselState } from '@/hooks/use-vessel-state'
import { useAlarms } from '@/hooks/use-alarms'
import { useSettingsForm } from '@/hooks/use-settings-form'
import { SignalKDiscoveryPrompt } from '@/components/signalk-discovery-prompt'
import { useServerTrails } from '@/hooks/use-server-trails'
import { useWeatherForecast } from '@/hooks/use-weather-forecast'
import { useWaveForecast } from '@/hooks/use-wave-forecast'
import { useWeatherToday } from '@/hooks/use-weather-today'
import { useAutopilot } from '@/hooks/use-autopilot'
import { useCZoneSwitches } from '@/hooks/use-czone-switches'
import { useDepthTrend } from '@/hooks/use-depth-trend'
import { useDarkMode } from '@/hooks/use-dark-mode'
import { FORECAST_REFRESH_SECONDS } from '@/config/app-config'
import { useAppConfig } from '@/hooks/use-app-config'
import { BREAKPOINTS, useMinWidth } from '@/lib/breakpoints'
import {
  DASHBOARD_WIDGET_IDS,
  DASHBOARD_WIDGET_LABELS,
  isEmbedWidgetId,
  isGaugeWidgetId,
  newEmbedWidgetId,
  newGaugeWidgetId,
  type DashboardLayoutItem,
  type DashboardWidgetId,
  type EmbedWidgetConfig,
  type GaugeWidgetConfig,
} from '@/lib/dashboard-widgets'
import { EmbedTile } from '@/components/embed-tile'
import { EmbedConfigDialog } from '@/components/embed-config-dialog'
import { GaugeConfigDialog } from '@/components/gauge-config-dialog'
import { GaugeTile } from '@/components/gauge-tile'
import { useGaugeValues } from '@/hooks/use-gauge-values'
import { Button } from '@/components/ui/button'
import { Separator } from '@/components/ui/separator'
import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover'
import {
  Breadcrumb,
  BreadcrumbItem,
  BreadcrumbLink,
  BreadcrumbList,
  BreadcrumbPage,
  BreadcrumbSeparator,
} from '@/components/ui/breadcrumb'
import {
  Sidebar,
  SidebarContent,
  SidebarInset,
  SidebarMenu,
  SidebarMenuItem,
  SidebarMenuButton,
  SidebarMenuSub,
  SidebarMenuSubButton,
  SidebarMenuSubItem,
  SidebarProvider,
  SidebarRail,
  SidebarTrigger,
} from '@/components/ui/sidebar'

type PanelId = 'forecast' | 'routes' | 'charts' | 'radar' | 'anchor-watch' | 'alarms' | 'settings'

const PANEL_NAV_ITEMS: Array<{ id: PanelId; label: string; icon: typeof CloudSun }> = [
  { id: 'forecast', label: 'Forecast', icon: CloudSun },
  { id: 'routes', label: 'Routes', icon: Route },
  { id: 'charts', label: 'Charts', icon: Map },
  { id: 'radar', label: 'Radar', icon: RadarIcon },
  { id: 'anchor-watch', label: 'Anchor Watch', icon: Anchor },
  { id: 'alarms', label: 'Alarms', icon: BellRing },
  { id: 'settings', label: 'Settings', icon: Settings },
]

const ANCHOR_IMAGERY_ENABLED_KEY = 'anchorWatch.imagery.enabled'
const AUTO_CLOSE_ANCHOR_WATCH_KEY = 'anchorWatch.autoClose.enabled'

export function App() {
  const { ui: uiConfig, anchor: anchorConfig } = useAppConfig()
  const [activePanel, setActivePanel] = useState<PanelId | null>(null)
  const [showAnchorImagery, setShowAnchorImagery] = useState(() => {
    const raw = globalThis.localStorage?.getItem(ANCHOR_IMAGERY_ENABLED_KEY)
    return raw === 'true'
  })
  const [autoCloseAnchorWatchEnabled, setAutoCloseAnchorWatchEnabled] = useState(() => {
    const raw = globalThis.localStorage?.getItem(AUTO_CLOSE_ANCHOR_WATCH_KEY)
    // Default to true if not set
    return raw !== 'false'
  })
  const [layoutEditingRequested, setLayoutEditing] = useState(false)
  const canEditLayout = useMinWidth(BREAKPOINTS.lg)
  // Derived, not stored: narrowing the window past `lg` removes both the grid and
  // the toggle that would exit edit mode, so a stored flag would strand the
  // dashboard in a non-interactive state with no way back out.
  const layoutEditing = layoutEditingRequested && canEditLayout
  // The embed widget currently open in the config dialog. For a freshly added
  // embed this is the only place it exists until it is given a URL and saved.
  const [embedDraft, setEmbedDraft] = useState<DashboardLayoutItem | null>(null)
  // Same pattern as embedDraft: a freshly added gauge exists only here until it
  // is given a path, since the backend rejects one without.
  const [gaugeDraft, setGaugeDraft] = useState<DashboardLayoutItem | null>(null)
  const gaugeValues = useGaugeValues()
  const [settingsDirty, setSettingsDirty] = useState(false)
  const settingsPageRef = useRef<SettingsPageHandle>(null)
  const [pendingNavigation, setPendingNavigation] = useState<(() => void) | null>(null)
  const [isSavingBeforeNavigate, setIsSavingBeforeNavigate] = useState(false)
  const [saveAndContinueError, setSaveAndContinueError] = useState<string | null>(null)

  // Intercepts sidebar/breadcrumb navigation away from a dirty Settings
  // page: instead of navigating immediately, stashes the navigation as a
  // pending callback and lets the confirmation dialog decide (Cancel stays
  // put, Discard runs it as-is, Save and Continue runs it only after a
  // successful save). Navigating while NOT on a dirty Settings page (the
  // overwhelmingly common case) is unaffected — `navigate()` runs immediately.
  const requestNavigate = useCallback((targetPanel: PanelId | null, navigate: () => void) => {
    if (activePanel === 'settings' && targetPanel !== 'settings' && settingsDirty) {
      setPendingNavigation(() => navigate)
      return
    }
    navigate()
  }, [activePanel, settingsDirty])

  const handleSaveAndContinue = useCallback(async () => {
    setIsSavingBeforeNavigate(true)
    setSaveAndContinueError(null)
    try {
      await settingsPageRef.current?.save()
      pendingNavigation?.()
      setPendingNavigation(null)
    } catch (err) {
      // Stay on the page so the user can fix it and retry. The Settings
      // page renders its own error banner too, but this dialog is modal and
      // covers it — without repeating the reason here, a rejected save (e.g.
      // POST /api/settings refusing an unreachable SignalK address) looks
      // like the button simply did nothing.
      setSaveAndContinueError(err instanceof Error ? err.message : 'Unable to save settings')
    } finally {
      setIsSavingBeforeNavigate(false)
    }
  }, [pendingNavigation])

  // settingsDirty is only meaningful while the Settings page is actually
  // mounted and reporting it via onDirtyChange. Once the user has left
  // (Discard, a successful Save and Continue, or any other route away from
  // 'settings'), clear it explicitly rather than leaving the sidebar dot lit
  // on stale state — SettingsPageContent stops calling onDirtyChange the
  // moment it unmounts, so nothing else would ever reset this otherwise.
  useEffect(() => {
    if (activePanel !== 'settings') {
      setSettingsDirty(false)
    }
  }, [activePanel])

  useEffect(() => {
    globalThis.localStorage?.setItem(ANCHOR_IMAGERY_ENABLED_KEY, String(showAnchorImagery))
  }, [showAnchorImagery])

  useEffect(() => {
    globalThis.localStorage?.setItem(AUTO_CLOSE_ANCHOR_WATCH_KEY, String(autoCloseAnchorWatchEnabled))
  }, [autoCloseAnchorWatchEnabled])

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
  const { pages, error: pagesError, createPage, updatePage, deletePage } = useDashboardPages()
  const [activePageId, setActivePageId] = useActiveDashboardPageId(pages)
  const activePage = pages.find((p) => p.id === activePageId) ?? null

  // Handle anchor watch auto-close notifications
  const [toastMessage, setToastMessage] = useState<string | null>(null)
  const toastRef = useRef<HTMLDivElement>(null)
  useEffect(() => {
    const handleAutoClose = () => {
      setToastMessage('Anchor watch cleared — engines running, position outside zone')
    }

    window.addEventListener('anchor-watch-auto-closed', handleAutoClose)
    return () => window.removeEventListener('anchor-watch-auto-closed', handleAutoClose)
  }, [])

  // Drives the toast's top-layer visibility from state, and owns its
  // auto-dismiss timer so the timer is reliably cleared (previously this
  // cleanup was returned from the window event handler above, where
  // `addEventListener` silently discards it).
  useEffect(() => {
    const toast = toastRef.current
    if (!toast) return
    if (toastMessage === null) {
      toast.hidePopover()
      return
    }
    toast.showPopover()
    const timer = setTimeout(() => setToastMessage(null), 5000)
    return () => clearTimeout(timer)
  }, [toastMessage])

  const {
    depth,
    currentDriftKts,
    currentSetDeg,
    currentDriftImpactKts,
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
    maxGustKts,
    generatorState,
    generatorManualStart,
    generatorManualStartTimer,
    generatorRunningByCondition,
    generatorRuntime,
    engine0Rpm,
    engine1Rpm,
    speedOverGroundKts,
    source: vesselStateSource,
  } = useVesselState()

  const { alarms, acknowledge: acknowledgeAlarm } = useAlarms()
  // Only for deciding whether to offer SignalK discovery. Gated on `loading`
  // below so an unconfigured-looking empty address during the initial fetch
  // can't trigger the prompt spuriously.
  const { settings: currentSettings, loading: currentSettingsLoading } = useSettingsForm()
  const { vessels: nearbyVessels, loading: nearbyVesselsLoading } = useNearbyVessels()
  const { tanks, loading: tanksLoading } = useTanksState()
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
    charger0CurrentA,
    charger0AcIn1CurrentA,
    charger0ChargingMode,
    charger0Error,
    batteryRatePercentPerHour,
    timeToGoHours,
  } = useElectricalState()
  const {
    currentW: solarCurrentW,
    todayKWh: solarTodayKWh,
    yesterdayKWh: solarYesterdayKWh,
    peakTodayW: solarPeakTodayW,
    controllers: solarControllers,
  } = useSolarState()
  const { weather } = useWeatherToday(uiConfig.vesselStateRefreshSeconds)
  const { tide } = useTideToday(uiConfig.vesselStateRefreshSeconds)
  const { activeWarning: activeForecastWarning } = useForecastWarnings(FORECAST_REFRESH_SECONDS)
  const {
    forecast,
    hourlyToday: forecastHourlyToday,
    summary: forecastSummary,
    loading: forecastLoading,
    error: forecastError,
    provider: forecastProvider,
    isCached: forecastIsCached,
    updatedAt: forecastUpdatedAt,
    ttlSeconds: forecastTtlSeconds,
    refetch: refetchForecast,
  } = useWeatherForecast(FORECAST_REFRESH_SECONDS)
  const {
    days: waveForecastDays,
    seaTemperatureF: waveSeaTemperatureF,
    loading: waveForecastLoading,
    error: waveForecastError,
  } = useWaveForecast(FORECAST_REFRESH_SECONDS)
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
  const autopilot = useAutopilot()
  const isImperialDistance = uiConfig.distanceUnits === 'imperial'
  const isAlternatorTileVisible = (engine0Rpm !== null && engine0Rpm > 0) || (engine1Rpm !== null && engine1Rpm > 0)

  const handleDropAnchorHere = () => {
    if (latitude === null || longitude === null) return
    void anchorWatch.setAnchorHere(latitude, longitude)
  }

  const hasActiveWindBulletin = Boolean(findActiveWindBulletin(activeForecastWarning))
  const hasActiveAnchorWatch = anchorWatch.anchorState !== 'none'

  const effectiveWidgets = useMemo(() => activePage?.widgets ?? [], [activePage])
  const unplacedWidgetIds = DASHBOARD_WIDGET_IDS.filter((id) => !effectiveWidgets.some((w) => w.id === id))

  const handleLayoutSettle = useCallback((next: DashboardLayoutItem[]) => {
    if (!activePage) return
    void updatePage(activePage.id, { widgets: next })
  }, [activePage, updatePage])

  const handleRemoveWidget = useCallback((id: DashboardWidgetId) => {
    if (!activePage) return
    void updatePage(activePage.id, { widgets: effectiveWidgets.filter((w) => w.id !== id) })
  }, [activePage, effectiveWidgets, updatePage])

  const handleAddWidget = useCallback((id: DashboardWidgetId) => {
    if (!activePage) return
    const maxY = effectiveWidgets.reduce((max, w) => Math.max(max, w.y + w.h), 0)
    void updatePage(activePage.id, { widgets: [...effectiveWidgets, { id, x: 0, y: maxY, w: 4, h: 6 }] })
  }, [activePage, effectiveWidgets, updatePage])

  // A new embed is held as an unsaved draft until it has a URL — the backend
  // rejects a blank one, and rightly so, rather than persisting a broken widget.
  // Cancelling therefore just discards it. Wider and taller than the builtin
  // default above, since a chart needs the room.
  const handleAddEmbed = useCallback(() => {
    const maxY = effectiveWidgets.reduce((max, w) => Math.max(max, w.y + w.h), 0)
    setEmbedDraft({
      id: newEmbedWidgetId(effectiveWidgets),
      x: 0,
      y: maxY,
      w: 6,
      h: 8,
      embed: { title: '', url: '' },
    })
  }, [effectiveWidgets])

  const handleAddGauge = useCallback(() => {
    const maxY = effectiveWidgets.reduce((max, w) => Math.max(max, w.y + w.h), 0)
    setGaugeDraft({
      id: newGaugeWidgetId(effectiveWidgets),
      x: 0,
      y: maxY,
      w: 3,
      h: 6,
      gauge: { path: '', label: '', display: 'numeric', quantity: 'raw', unit: 'raw' },
    })
  }, [effectiveWidgets])

  const handleSaveGauge = useCallback((gauge: GaugeWidgetConfig) => {
    if (!activePage || !gaugeDraft) return
    const id = gaugeDraft.id
    if (effectiveWidgets.some((w) => w.id === id)) {
      void updatePage(activePage.id, {
        widgets: effectiveWidgets.map((w) => (w.id === id ? { ...w, gauge } : w)),
      })
    } else {
      void updatePage(activePage.id, { widgets: [...effectiveWidgets, { ...gaugeDraft, gauge }] })
    }
    setGaugeDraft(null)
  }, [activePage, effectiveWidgets, gaugeDraft, updatePage])

  const handleSaveEmbed = useCallback((id: DashboardWidgetId, embed: EmbedWidgetConfig) => {
    if (!activePage) return
    if (effectiveWidgets.some((w) => w.id === id)) {
      void updatePage(activePage.id, {
        widgets: effectiveWidgets.map((w) => (w.id === id ? { ...w, embed } : w)),
      })
      return
    }
    if (embedDraft?.id === id) {
      void updatePage(activePage.id, { widgets: [...effectiveWidgets, { ...embedDraft, embed }] })
    }
  }, [activePage, effectiveWidgets, embedDraft, updatePage])

  // Not wrapped in useCallback: exhaustive-deps reports ~58 dependencies here
  // (essentially the entire polled-data surface of the component — vessel,
  // electrical, tanks, nearby-vessels, anchor watch, wind, etc.), several of
  // which change every few seconds independently. Memoizing would just
  // recreate the reference on nearly every render anyway, so it buys no real
  // stabilization — left as a plain function per the task's own guidance for
  // this case.
  const renderWidget = (widget: DashboardLayoutItem): ReactNode => {
    const { id } = widget
    if (isGaugeWidgetId(id)) {
      if (!widget.gauge) return null
      return (
        <GaugeTile
          config={widget.gauge}
          value={gaugeValues[widget.gauge.path] ?? null}
          editing={layoutEditing}
          onConfigure={() => setGaugeDraft(widget)}
        />
      )
    }

    if (isEmbedWidgetId(id)) {
      return (
        <EmbedTile
          config={widget.embed}
          editing={layoutEditing}
          onConfigure={() => setEmbedDraft(widget)}
          isDarkTheme={isDarkTheme}
        />
      )
    }

    switch (id) {
      case 'vessel':
        return <MarineHeader />
      case 'wind':
        return (
          <WindTile
            headingTrue={headingTrue}
            windAngleApparentDeg={windAngleApparentDeg}
            windSide={windSide}
            windAngleRelativeDeg={windAngleRelativeDeg}
            windSpeedApparentKts={windSpeedApparentKts}
            currentSetDeg={currentSetDeg}
            currentDriftKts={currentDriftKts}
            currentDriftImpactKts={currentDriftImpactKts}
            maxGustKts={maxGustKts}
          />
        )
      case 'depth-tide':
        return (
          <DepthTideTile
            depth={depth}
            isImperialDistance={isImperialDistance}
            navigationState={navigationState}
            depthTrend={depthTrend}
            tide={tide}
            onOpen={layoutEditing ? undefined : () => setActivePanel('forecast')}
          />
        )
      case 'position':
        return (
          <PositionTile
            latitude={latitude}
            longitude={longitude}
            headingTrue={headingTrue}
            gnssValidationState={gnssValidationState}
            gnssQualityIndicator={gnssQualityIndicator}
            gnssHdop={gnssHdop}
            gnssValidationReason={gnssValidationReason}
            gnssSatellites={gnssSatellites}
            placeName={placeName}
          />
        )
      case 'today-now':
        return (
          <TodayNowTile
            weather={weather}
            highTempF={forecast[0]?.high ?? -1}
            lowTempF={forecast[0]?.low ?? -1}
            seaTemperatureF={waveSeaTemperatureF ?? null}
            distanceUnits={uiConfig.distanceUnits}
            onOpen={layoutEditing ? undefined : () => setActivePanel('forecast')}
          />
        )
      case 'anchor-watch':
        return (
          <AnchorWatchTile
            watch={anchorWatch}
            lat={latitude}
            lon={longitude}
            depthMeters={depth}
            currentDriftKts={currentDriftKts}
            currentSetDeg={currentSetDeg}
            currentDriftImpactKts={currentDriftImpactKts}
            isImperial={isImperialDistance}
            vesselHeadingDeg={headingTrue}
            vesselTrail={getSelfTrail}
            aisVessels={nearbyVessels}
            aisTrails={getAisTrails}
            isDarkTheme={isDarkTheme}
            showImageryLayer={showAnchorImagery}
            onImageryToggle={setShowAnchorImagery}
            onFullscreen={() => setActivePanel('anchor-watch')}
          />
        )
      case 'rode-scope':
        return (
          <RodeScopeTile
            anchorState={anchorWatch.anchorState}
            gnssCritical={anchorWatch.gnssCritical}
            rodeDeployedM={anchorWatch.rodeDeployedM}
            seaState={anchorWatch.seaState}
            seabedType={anchorWatch.seabedType}
            depthM={depth}
            windKts={windSpeedApparentKts}
            isImperial={isImperialDistance}
            anchorConfig={anchorConfig}
            onUpdate={anchorWatch.updateRodeAndConditions}
          />
        )
      case 'tanks':
        return <TanksTile tanks={tanks} loading={tanksLoading} />
      case 'route':
        return (
          <RouteTile
            speedKts={speedOverGroundKts ?? 0}
            routes={routes}
            dashboardRouteId={dashboardRouteId}
            onOpen={() => setActivePanel('routes')}
          />
        )
      case 'nearby-vessels':
        return <NearbyVesselsTile vessels={nearbyVessels} loading={nearbyVesselsLoading} distanceUnits={uiConfig.distanceUnits} />
      case 'battery-power':
        return (
          <BatteryPowerTile
            batterySocPercent={batterySocPercent}
            chargingCurrentA={chargingCurrentA}
            chargingPowerW={chargingPowerW}
            solarOutputW={solarOutputW}
            acOutputW={acOutputW}
            dc12vPowerW={dc12vPowerW}
            dc24vVoltageV={dc24vVoltageV}
            charger0CurrentA={charger0CurrentA}
            charger0AcIn1CurrentA={charger0AcIn1CurrentA}
            charger0ChargingMode={charger0ChargingMode}
            charger0Error={charger0Error}
            batteryRatePercentPerHour={batteryRatePercentPerHour}
            timeToGoHours={timeToGoHours}
          />
        )
      case 'solar':
        return (
          <SolarTile
            currentW={solarCurrentW}
            todayKWh={solarTodayKWh}
            yesterdayKWh={solarYesterdayKWh}
            peakTodayW={solarPeakTodayW}
            controllers={solarControllers}
          />
        )
      case 'alternator':
        return <AlternatorTile port={alternator0} starboard={alternator1} enginesRunning={isAlternatorTileVisible} />
      case 'generator':
        return (
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
        )
      case 'czone-switches':
        return <CZoneSwitchesTile switches={czoneSwitches} loading={czoneLoading} pending={czonePending} onToggle={toggleCZone} />
      case 'autopilot':
        return (
          <AutopilotTile
            state={autopilot.state}
            pending={autopilot.pending}
            error={autopilot.error}
            availableModes={autopilot.availableModes}
            capabilityError={autopilot.capabilityError}
            onEngage={autopilot.engage}
            onDisengage={autopilot.disengage}
            onTack={autopilot.tack}
            onGybe={autopilot.gybe}
            onAdjustHeading={autopilot.adjustHeading}
            onSetMode={autopilot.setMode}
            onDodge={autopilot.dodge}
            onClearDodge={autopilot.clearDodge}
            headingTrueDeg={headingTrue}
          />
        )
      case 'hot-water':
        return <HotWaterTile />
      default:
        return null
    }
  }

  const dashboardGrid = (
    <div className="flex flex-col gap-4">
      {layoutEditing && (
        <div className="inline-flex w-fit items-center gap-2 rounded-md border border-primary/30 bg-primary/10 px-3 py-1.5 text-[10px] font-semibold uppercase tracking-[0.16em] text-primary">
          Layout Mode — Drag to rearrange
        </div>
      )}

      <DashboardBentoGrid
        widgets={effectiveWidgets}
        editing={layoutEditing}
        renderWidget={renderWidget}
        onRemoveWidget={handleRemoveWidget}
        onLayoutSettle={handleLayoutSettle}
      />

      {/* Always available in layout mode: Embed is never "placed", so unlike the
          builtin widgets it can be added any number of times. */}
      {layoutEditing && (
        <Popover>
          <PopoverTrigger className="inline-flex w-fit items-center gap-1 rounded-md border border-border bg-background/70 px-3 py-1.5 text-xs font-semibold uppercase tracking-[0.1em] text-muted-foreground hover:border-primary/40 hover:text-primary">
            <Plus className="h-3.5 w-3.5" />
            Add Widget
          </PopoverTrigger>
          <PopoverContent className="w-56 p-1">
            <div className="flex flex-col">
              {unplacedWidgetIds.map((id) => (
                <button
                  key={id}
                  type="button"
                  onClick={() => handleAddWidget(id)}
                  className="rounded-sm px-2 py-1.5 text-left text-sm hover:bg-accent hover:text-accent-foreground"
                >
                  {DASHBOARD_WIDGET_LABELS[id]}
                </button>
              ))}
              {unplacedWidgetIds.length > 0 && <div className="my-1 h-px bg-border" />}
              <button
                type="button"
                onClick={handleAddGauge}
                className="rounded-sm px-2 py-1.5 text-left text-sm hover:bg-accent hover:text-accent-foreground"
              >
                Gauge…
              </button>
              <button
                type="button"
                onClick={handleAddEmbed}
                className="rounded-sm px-2 py-1.5 text-left text-sm hover:bg-accent hover:text-accent-foreground"
              >
                Embed…
              </button>
            </div>
          </PopoverContent>
        </Popover>
      )}

      <GaugeConfigDialog
        widget={gaugeDraft}
        onCancel={() => setGaugeDraft(null)}
        onSave={handleSaveGauge}
      />

      <EmbedConfigDialog
        widget={embedDraft}
        open={embedDraft !== null}
        onOpenChange={(open) => { if (!open) setEmbedDraft(null) }}
        onSave={handleSaveEmbed}
      />
    </div>
  )

  const activePanelContent = (() => {
    switch (activePanel) {
      case 'forecast':
        return (
          <ForecastDrawer
            forecast={forecast}
            hourlyToday={forecastHourlyToday}
            summary={forecastSummary}
            loading={forecastLoading}
            error={forecastError}
            provider={forecastProvider}
            isCached={forecastIsCached}
            updatedAt={forecastUpdatedAt}
            ttlSeconds={forecastTtlSeconds}
            onRetry={refetchForecast}
            unit={uiConfig.distanceUnits as 'imperial' | 'metric'}
            activeForecastWarning={activeForecastWarning}
            waveDays={waveForecastDays}
            waveSeaTemperatureF={waveSeaTemperatureF}
            waveLoading={waveForecastLoading}
            waveError={waveForecastError}
          />
        )
      case 'alarms':
        return <AlarmsDrawer alarms={alarms} onAcknowledge={acknowledgeAlarm} />
      case 'routes':
        return (
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
        )
      case 'charts':
        return (
          <SatChartsDrawer
            charts={satCharts}
            loading={satChartsLoading}
            error={satChartsError}
            uploadChart={uploadChart}
            deleteChart={deleteSatChart}
          />
        )
      case 'radar':
        return <RadarDrawer latitude={latitude} longitude={longitude} />
      case 'settings':
        return (
          <SettingsPage
            ref={settingsPageRef}
            autoCloseAnchorWatchEnabled={autoCloseAnchorWatchEnabled}
            onAutoCloseAnchorWatchToggle={setAutoCloseAnchorWatchEnabled}
            onDirtyChange={setSettingsDirty}
          />
        )
      case 'anchor-watch':
        return anchorWatch.anchorLat !== null && anchorWatch.anchorLon !== null
          ? (
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
              currentDriftImpactKts={currentDriftImpactKts}
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
      default:
        return null
    }
  })()

  return (
    <SidebarProvider>
      <div
        ref={toastRef}
        role="status"
        aria-live="polite"
        className="anchor-watch-toast left-4 right-4 top-4 m-0 mx-auto max-w-md rounded-lg border border-amber-500/40 bg-amber-500/10 px-4 py-3 text-sm text-amber-600 shadow-lg md:left-auto md:right-4"
        // @types/react 18 predates the Popover API attribute.
        // eslint-disable-next-line @typescript-eslint/no-explicit-any
        {...({ popover: 'manual' } as any)}
      >
        {toastMessage}
      </div>

      <Sidebar collapsible="icon">
        <SidebarContent>
          <SidebarMenu>
            <SidebarMenuItem>
              <SidebarMenuButton isActive={activePanel === null} onClick={() => requestNavigate(null, () => setActivePanel(null))} tooltip="Dashboard">
                <LayoutDashboard />
                <span>Dashboard</span>
              </SidebarMenuButton>
            </SidebarMenuItem>
            {pages.length > 0 && (
              <SidebarMenuSub>
                {pages.map((page) => (
                  <SidebarMenuSubItem key={page.id}>
                    <SidebarMenuSubButton
                      render={<button type="button" />}
                      isActive={activePanel === null && page.id === activePageId}
                      onClick={() => requestNavigate(null, () => {
                        setActivePanel(null)
                        setActivePageId(page.id)
                      })}
                    >
                      <span>{page.name}</span>
                    </SidebarMenuSubButton>
                  </SidebarMenuSubItem>
                ))}
              </SidebarMenuSub>
            )}
            {PANEL_NAV_ITEMS.map(({ id, label, icon: Icon }) => (
              <SidebarMenuItem key={id}>
                <SidebarMenuButton isActive={activePanel === id} onClick={() => requestNavigate(id, () => setActivePanel(id))} tooltip={label}>
                  <Icon />
                  <span>{label}</span>
                  {id === 'forecast' && hasActiveWindBulletin && (
                    <span className="h-1.5 w-1.5 rounded-full bg-amber-400" aria-hidden="true" />
                  )}
                  {id === 'anchor-watch' && hasActiveAnchorWatch && (
                    <span className="h-1.5 w-1.5 rounded-full bg-amber-400" aria-hidden="true" />
                  )}
                  {id === 'settings' && settingsDirty && (
                    <span className="h-1.5 w-1.5 rounded-full bg-amber-400" aria-hidden="true" />
                  )}
                </SidebarMenuButton>
              </SidebarMenuItem>
            ))}
          </SidebarMenu>
        </SidebarContent>
        <SidebarRail />
      </Sidebar>

      <SidebarInset>
        {/* `min-w-0` on both halves is load-bearing, not cosmetic: without it a flex
            item refuses to shrink below its content width and the right-hand cluster
            gets pushed off a phone screen (AGENTS.md — prevent viewport overflows).
            The breadcrumb is the designated slack absorber, so it truncates while the
            clock and controls keep their size. */}
        <header className="flex h-14 shrink-0 items-center gap-2 border-b px-2 sm:px-4 lg:h-16">
          <div className="flex min-w-0 flex-1 items-center gap-2">
            <SidebarTrigger className="-ml-1" />
            <Separator orientation="vertical" className="mr-2 hidden h-4 sm:block" />
            <Breadcrumb className="min-w-0">
              <BreadcrumbList className="flex-nowrap">
                {activePanel === null ? (
                  <BreadcrumbItem>
                    <BreadcrumbPage>Dashboard</BreadcrumbPage>
                  </BreadcrumbItem>
                ) : (
                  <>
                    {/* Below `sm` only the leaf crumb survives — the parent link is
                        redundant with the sidebar, which navigates to the same place. */}
                    <BreadcrumbItem className="hidden sm:inline-flex">
                      <BreadcrumbLink href="#" onClick={(e) => { e.preventDefault(); requestNavigate(null, () => setActivePanel(null)) }}>
                        Dashboard
                      </BreadcrumbLink>
                    </BreadcrumbItem>
                    <BreadcrumbSeparator className="hidden sm:block" />
                    <BreadcrumbItem className="min-w-0">
                      <BreadcrumbPage className="truncate">
                        {PANEL_NAV_ITEMS.find((item) => item.id === activePanel)?.label}
                      </BreadcrumbPage>
                    </BreadcrumbItem>
                  </>
                )}
              </BreadcrumbList>
            </Breadcrumb>
          </div>
          <div className="flex min-w-0 shrink-0 items-center gap-2">
            {activePanel === null && !pagesError && (
              <>
                <DashboardPageSwitcher
                  pages={pages}
                  activePageId={activePageId}
                  onSelect={setActivePageId}
                  onCreate={() => {
                    void createPage(`Page ${pages.length + 1}`, []).then((p) => {
                      if (p) {
                        setActivePageId(p.id)
                        setLayoutEditing(true)
                      }
                    })
                  }}
                  onRename={(id, name) => { void updatePage(id, { name }) }}
                  onDelete={(id) => {
                    void deletePage(id).then((ok) => {
                      if (ok && id === activePageId) {
                        setActivePageId(pages.find((p) => p.id !== id)?.id ?? null)
                      }
                    })
                  }}
                />
                {/* Gated on the same breakpoint the bento grid uses to decide whether
                    to mount at all. Below `lg` there is no grid to rearrange, so the
                    control is absent rather than present-but-inert. */}
                {canEditLayout && (
                  <LayoutModeToggle editing={layoutEditing} onToggle={() => setLayoutEditing((prev) => !prev)} />
                )}
              </>
            )}
            <VesselStatusBar isDark={isDarkTheme} onToggleDarkMode={toggleDarkMode} />
          </div>
        </header>
        <div className="flex min-h-0 flex-1 flex-col px-2 py-2">
          <div className="mx-auto flex w-full max-w-[1800px] flex-1 min-h-0 flex-col gap-4">
            <AlarmBanner alarms={alarms} onOpen={() => requestNavigate('alarms', () => setActivePanel('alarms'))} />
            <ForecastWarningsBanner warnings={activeForecastWarning} />

            <div className="min-h-0 flex-1">
              {activePanel === null ? (
                dashboardGrid
              ) : (
                <div className="h-full min-h-0 overflow-y-auto rounded-lg border bg-card p-4">
                  {activePanelContent}
                </div>
              )}
            </div>
          </div>
        </div>
      </SidebarInset>

      {!currentSettingsLoading && (
        <SignalKDiscoveryPrompt
          configuredAddress={currentSettings.signalk?.address ?? ''}
          vesselStateSource={vesselStateSource}
        />
      )}

      <AlertDialog
        open={pendingNavigation !== null}
        onOpenChange={(open) => {
          if (!open) {
            setPendingNavigation(null)
            setSaveAndContinueError(null)
          }
        }}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Unsaved changes</AlertDialogTitle>
            <AlertDialogDescription>
              You have unsaved changes on the Settings page. Save them before leaving, or discard them?
            </AlertDialogDescription>
          </AlertDialogHeader>
          {saveAndContinueError && (
            <div className="rounded-md border border-destructive/30 bg-destructive/5 px-3 py-2 text-xs uppercase tracking-[0.08em] text-destructive">
              {saveAndContinueError}
            </div>
          )}
          <AlertDialogFooter>
            <AlertDialogCancel
              onClick={() => {
                setPendingNavigation(null)
                setSaveAndContinueError(null)
              }}
            >
              Cancel
            </AlertDialogCancel>
            <AlertDialogAction
              onClick={() => {
                pendingNavigation?.()
                setPendingNavigation(null)
                setSaveAndContinueError(null)
              }}
            >
              Discard
            </AlertDialogAction>
            <Button onClick={() => void handleSaveAndContinue()} disabled={isSavingBeforeNavigate}>
              {isSavingBeforeNavigate ? 'Saving…' : 'Save and Continue'}
            </Button>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>

      <Toaster isDarkTheme={isDarkTheme} />
    </SidebarProvider>
  )
}
