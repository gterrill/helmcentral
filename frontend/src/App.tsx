import {
  Anchor,
  CloudSun,
  LayoutDashboard,
  Map,
  Plus,
  Radar as RadarIcon,
  Route,
  Settings,
  Waves,
} from 'lucide-react'
import { useEffect, useState, type ReactNode } from 'react'

import { AnchorWatchTile } from '@/components/anchor-watch-tile'
import { AnchorWatchDrawer } from '@/components/anchor-watch-drawer'
import { AlternatorTile } from '@/components/alternator-tile'
import { BatteryPowerTile } from '@/components/battery-power-tile'
import { DepthTideTile } from '@/components/depth-tide-tile'
import { PositionTile } from '@/components/position-tile'
import { TodayNowTile } from '@/components/today-now-tile'
import { WindTile } from '@/components/wind-tile'
import { MarineHeader } from '@/components/marine-header'
import { VesselStatusBar } from '@/components/vessel-status-bar'
import { MarineWarningBanner } from '@/components/marine-warning-banner'
import { NearbyVesselsTile } from '@/components/nearby-vessels-tile'
import { RodeScopeTile } from '@/components/rode-scope-tile'
import { RadarDrawer } from '@/components/radar-drawer'
import { SignalKSettingsPanel } from '@/components/signalk-settings-panel'
import { CZoneSwitchesTile } from '@/components/czone-switches-tile'
import { GeneratorTile } from '@/components/generator-tile'
import { TanksTile } from '@/components/tanks-tile'
import { ForecastDrawer } from '@/components/forecast-drawer'
import { RoutePlannerDrawer } from '@/components/route-planner-drawer'
import { SatChartsDrawer } from '@/components/sat-charts-drawer'
import { RouteTile } from '@/components/route-tile'
import { DashboardBentoGrid } from '@/components/dashboard-bento-grid'
import { LayoutModeToggle } from '@/components/layout-mode-toggle'
import { useRoutes } from '@/hooks/use-routes'
import { useSatCharts } from '@/hooks/use-sat-charts'
import { useDashboardRouteId } from '@/hooks/use-dashboard-route'
import { useDashboardLayout } from '@/hooks/use-dashboard-layout'
import { useRouteActivation } from '@/hooks/use-route-activation'
import { TideDrawer } from '@/components/tide-drawer'
import { useElectricalState } from '@/hooks/use-electrical-state'
import { useNearbyVessels } from '@/hooks/use-nearby-vessels'
import { useAnchorWatch } from '@/hooks/use-anchor-watch'
import { useAnchorWatchAutoClose } from '@/hooks/use-anchor-watch-auto-close'
import { usePlaceName } from '@/hooks/use-place-name'
import { useTanksState } from '@/hooks/use-tanks-state'
import { useTideToday } from '@/hooks/use-tide-today'
import { findActiveWindBulletin, useMarineWarnings } from '@/hooks/use-marine-warnings'
import { useVesselState } from '@/hooks/use-vessel-state'
import { useServerTrails } from '@/hooks/use-server-trails'
import { useWeatherForecast } from '@/hooks/use-weather-forecast'
import { useWeatherToday } from '@/hooks/use-weather-today'
import { useCZoneSwitches } from '@/hooks/use-czone-switches'
import { useDepthTrend } from '@/hooks/use-depth-trend'
import { useDarkMode } from '@/hooks/use-dark-mode'
import { anchorConfig, uiConfig } from '@/config/app-config'
import { DASHBOARD_WIDGET_IDS, DASHBOARD_WIDGET_LABELS, type DashboardLayoutItem, type DashboardWidgetId } from '@/lib/dashboard-widgets'
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
  SidebarProvider,
  SidebarRail,
  SidebarTrigger,
} from '@/components/ui/sidebar'

type PanelId = 'forecast' | 'tides' | 'routes' | 'charts' | 'radar' | 'anchor-watch' | 'settings'

const PANEL_NAV_ITEMS: Array<{ id: PanelId; label: string; icon: typeof CloudSun }> = [
  { id: 'forecast', label: 'Forecast', icon: CloudSun },
  { id: 'tides', label: 'Tides', icon: Waves },
  { id: 'routes', label: 'Routes', icon: Route },
  { id: 'charts', label: 'Charts', icon: Map },
  { id: 'radar', label: 'Radar', icon: RadarIcon },
  { id: 'anchor-watch', label: 'Anchor Watch', icon: Anchor },
  { id: 'settings', label: 'Settings', icon: Settings },
]

const ANCHOR_IMAGERY_ENABLED_KEY = 'anchorWatch.imagery.enabled'
const AUTO_CLOSE_ANCHOR_WATCH_KEY = 'anchorWatch.autoClose.enabled'

// Recreates today's 3-column arrangement (cols=12, each column w=4) so a fresh
// install with no saved layout looks identical to the pre-bento dashboard.
const DEFAULT_DASHBOARD_LAYOUT: DashboardLayoutItem[] = [
  { id: 'vessel', x: 0, y: 0, w: 12, h: 3 },
  { id: 'wind', x: 0, y: 3, w: 4, h: 8 },
  { id: 'depth-tide', x: 0, y: 11, w: 4, h: 7 },
  { id: 'position', x: 0, y: 18, w: 4, h: 5 },
  { id: 'today-now', x: 0, y: 23, w: 4, h: 5 },
  { id: 'anchor-watch', x: 4, y: 3, w: 4, h: 8 },
  { id: 'rode-scope', x: 4, y: 11, w: 4, h: 6 },
  { id: 'tanks', x: 4, y: 17, w: 4, h: 4 },
  { id: 'route', x: 4, y: 21, w: 4, h: 4 },
  { id: 'nearby-vessels', x: 4, y: 25, w: 4, h: 5 },
  { id: 'battery-power', x: 8, y: 3, w: 4, h: 14 },
  { id: 'alternator', x: 8, y: 17, w: 4, h: 6 },
  { id: 'generator', x: 8, y: 23, w: 4, h: 5 },
  { id: 'czone-switches', x: 8, y: 28, w: 4, h: 6 },
]

export function App() {
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
  const [layoutEditing, setLayoutEditing] = useState(false)

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
  const {
    widgets: savedWidgets,
    error: dashboardLayoutError,
    saveLayout,
  } = useDashboardLayout()

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
  const { activeWarning: activeMarineWarning } = useMarineWarnings(uiConfig.forecastRefreshSeconds)
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
  const isAlternatorTileVisible = (engine0Rpm !== null && engine0Rpm > 0) || (engine1Rpm !== null && engine1Rpm > 0)

  const handleDropAnchorHere = () => {
    if (latitude === null || longitude === null) return
    void anchorWatch.setAnchorHere(latitude, longitude)
  }

  const hasActiveWindBulletin = Boolean(findActiveWindBulletin(activeMarineWarning))
  const hasActiveAnchorWatch = anchorWatch.anchorState !== 'none'

  const effectiveWidgets = savedWidgets !== null && savedWidgets.length > 0 ? savedWidgets : DEFAULT_DASHBOARD_LAYOUT
  const unplacedWidgetIds = DASHBOARD_WIDGET_IDS.filter((id) => !effectiveWidgets.some((w) => w.id === id))

  const handleLayoutSettle = (next: DashboardLayoutItem[]) => {
    void saveLayout(next)
  }

  const handleRemoveWidget = (id: DashboardWidgetId) => {
    void saveLayout(effectiveWidgets.filter((w) => w.id !== id))
  }

  const handleAddWidget = (id: DashboardWidgetId) => {
    const maxY = effectiveWidgets.reduce((max, w) => Math.max(max, w.y + w.h), 0)
    void saveLayout([...effectiveWidgets, { id, x: 0, y: maxY, w: 4, h: 6 }])
  }

  const renderWidget = (id: DashboardWidgetId): ReactNode => {
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
            maxGust10mKts={maxGust10mKts}
            maxGust1hKts={maxGust1hKts}
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
            onOpen={layoutEditing ? undefined : () => setActivePanel('tides')}
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
            batteryRatePercentPerHour={batteryRatePercentPerHour}
            timeToGoHours={timeToGoHours}
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

      {layoutEditing && unplacedWidgetIds.length > 0 && (
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
            </div>
          </PopoverContent>
        </Popover>
      )}
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
            isCached={forecastIsCached}
            updatedAt={forecastUpdatedAt}
            ttlSeconds={forecastTtlSeconds}
            onRetry={refetchForecast}
            unit={uiConfig.distanceUnits as 'imperial' | 'metric'}
            activeMarineWarning={activeMarineWarning}
          />
        )
      case 'tides':
        return <TideDrawer isImperial={isImperialDistance} />
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
          <SignalKSettingsPanel
            autoCloseAnchorWatchEnabled={autoCloseAnchorWatchEnabled}
            onAutoCloseAnchorWatchToggle={setAutoCloseAnchorWatchEnabled}
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
      {toastMessage && (
        <div className="fixed left-4 right-4 top-4 z-50 mx-auto max-w-md rounded-lg border border-amber-500/40 bg-amber-500/10 px-4 py-3 text-sm text-amber-600 shadow-lg md:left-auto md:right-4">
          {toastMessage}
        </div>
      )}

      <Sidebar collapsible="icon">
        <SidebarContent>
          <SidebarMenu>
            <SidebarMenuItem>
              <SidebarMenuButton isActive={activePanel === null} onClick={() => setActivePanel(null)} tooltip="Dashboard">
                <LayoutDashboard />
                <span>Dashboard</span>
              </SidebarMenuButton>
            </SidebarMenuItem>
            {PANEL_NAV_ITEMS.map(({ id, label, icon: Icon }) => (
              <SidebarMenuItem key={id}>
                <SidebarMenuButton isActive={activePanel === id} onClick={() => setActivePanel(id)} tooltip={label}>
                  <Icon />
                  <span>{label}</span>
                  {id === 'forecast' && hasActiveWindBulletin && (
                    <span className="h-1.5 w-1.5 rounded-full bg-amber-400" aria-hidden="true" />
                  )}
                  {id === 'anchor-watch' && hasActiveAnchorWatch && (
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
        <header className="flex h-16 shrink-0 items-center gap-2 border-b">
          <div className="flex items-center gap-2 px-4">
            <SidebarTrigger className="-ml-1" />
            <Separator orientation="vertical" className="mr-2 h-4" />
            <Breadcrumb>
              <BreadcrumbList>
                {activePanel === null ? (
                  <BreadcrumbItem>
                    <BreadcrumbPage>Dashboard</BreadcrumbPage>
                  </BreadcrumbItem>
                ) : (
                  <>
                    <BreadcrumbItem>
                      <BreadcrumbLink href="#" onClick={(e) => { e.preventDefault(); setActivePanel(null) }}>
                        Dashboard
                      </BreadcrumbLink>
                    </BreadcrumbItem>
                    <BreadcrumbSeparator />
                    <BreadcrumbItem>
                      <BreadcrumbPage>
                        {PANEL_NAV_ITEMS.find((item) => item.id === activePanel)?.label}
                      </BreadcrumbPage>
                    </BreadcrumbItem>
                  </>
                )}
              </BreadcrumbList>
            </Breadcrumb>
          </div>
          <div className="ml-auto flex items-center gap-2 px-4">
            {activePanel === null && !dashboardLayoutError && (
              <LayoutModeToggle editing={layoutEditing} onToggle={() => setLayoutEditing((prev) => !prev)} />
            )}
            <VesselStatusBar isDark={isDarkTheme} onToggleDarkMode={toggleDarkMode} />
          </div>
        </header>
        <div className="flex min-h-0 flex-1 flex-col px-2 py-2">
          <div className="mx-auto flex w-full max-w-[1800px] flex-1 min-h-0 flex-col gap-4">
            <MarineWarningBanner warnings={activeMarineWarning} />

            <div className="min-h-0 flex-1">
              {activePanel === null ? (
                dashboardGrid
              ) : (
                <div className="h-full min-h-0 overflow-y-auto rounded-lg border bg-card">
                  {activePanelContent}
                </div>
              )}
            </div>
          </div>
        </div>
      </SidebarInset>
    </SidebarProvider>
  )
}
