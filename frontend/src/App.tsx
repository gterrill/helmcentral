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
import { RadarTargetsTile } from '@/components/radar-targets-tile'
import { RadarDrawer } from '@/components/radar-drawer'
import { SettingsPage, type SettingsPageHandle } from '@/components/settings/settings-page'
import type { SettingsSectionId } from '@/components/settings/settings-nav'
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
import { PageSkinSelect } from '@/components/page-skin-select'
import { PageHeroSelect } from '@/components/page-hero-select'
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
import { useRadarTargets } from '@/hooks/use-radar-targets'
import { useAnchorWatch } from '@/hooks/use-anchor-watch'
import { useAnchorPlacemarks } from '@/hooks/use-anchor-placemarks'
import { useAnchorWatchAutoClose } from '@/hooks/use-anchor-watch-auto-close'
import { usePlaceName } from '@/hooks/use-place-name'
import { useTanksState } from '@/hooks/use-tanks-state'
import { useTideToday } from '@/hooks/use-tide-today'
import { tideHeightFtOrNull } from '@/lib/rode-plan'
import { findActiveWindBulletin, useForecastWarnings } from '@/hooks/use-forecast-warnings'
import { useVesselState } from '@/hooks/use-vessel-state'
import { useAlarms } from '@/hooks/use-alarms'
import { useSettingsForm } from '@/hooks/use-settings-form'
import { SignalKDiscoveryPrompt } from '@/components/signalk-discovery-prompt'
import { useServerTrails } from '@/hooks/use-server-trails'
import { useWeatherForecast } from '@/hooks/use-weather-forecast'
import { useWaveForecast } from '@/hooks/use-wave-forecast'
import { useUpperAir } from '@/hooks/use-upper-air'
import { useWeatherToday } from '@/hooks/use-weather-today'
import { useAuth } from '@/hooks/use-auth'
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
  duplicateWidget,
  isEmbedWidgetId,
  isGaugeGroupWidgetId,
  isGaugeWidgetId,
  isClusterWidgetId,
  isLampStripWidgetId,
  newEmbedWidgetId,
  newGaugeGroupWidgetId,
  newGaugeWidgetId,
  newClusterWidgetId,
  newLampStripWidgetId,
  type DashboardLayoutItem,
  type DashboardWidgetId,
  type EmbedWidgetConfig,
  type GaugeGroupWidgetConfig,
  type EngineClusterConfig,
  type LampStripWidgetConfig,
  type GaugeWidgetConfig,
} from '@/lib/dashboard-widgets'
import { EmbedTile } from '@/components/embed-tile'
import { EmbedConfigDialog } from '@/components/embed-config-dialog'
import { GaugeConfigDialog } from '@/components/gauge-config-dialog'
import { GaugeTile } from '@/components/gauge-tile'
import { GaugeGroupConfigDialog } from '@/components/gauge-group-config-dialog'
import { GaugeGroupTile } from '@/components/gauge-group-tile'
import { EngineClusterConfigDialog } from '@/components/engine-cluster-config-dialog'
import { EngineClusterTile } from '@/components/engine-cluster-tile'
import { EngineProfileDialog } from '@/components/engine-profile-dialog'
import { LampStripConfigDialog } from '@/components/lamp-strip-config-dialog'
import { LampStripTile } from '@/components/lamp-strip-tile'
import { useGaugeValues } from '@/hooks/use-gauge-values'
import { LoginScreen } from '@/components/login-screen'
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
  SidebarFooter,
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
import { SidebarVersion } from '@/components/sidebar-version'
import {
  parseAppLocation,
  formatAppLocation,
  isCanonicalAppPath,
  type AppLocation,
  type PanelId,
} from '@/lib/app-location'

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
const ANCHOR_RADAR_ECHO_ENABLED_KEY = 'anchorWatch.radarEcho.enabled'
const AUTO_CLOSE_ANCHOR_WATCH_KEY = 'anchorWatch.autoClose.enabled'

export function App() {
  // Called before every other hook, and unconditionally on every render
  // (rules of hooks) even while the login screen is what actually renders
  // below — the conditional return happens only at the very end of this
  // component, once every hook Helmcentral's dashboard needs has already run.
  const auth = useAuth()
  // Role-gating is cosmetic only (ADR 0040 §frontend): the server is the
  // enforcement point for every one of these tiers. In mode:none there is no
  // session and no role to gate on, so both stay permissive — "no behaviour
  // change at all" is the explicit requirement for that mode.
  const canWrite = auth.mode !== 'signalk' || auth.role === 'readwrite' || auth.role === 'admin'
  const canAdmin = auth.mode !== 'signalk' || auth.role === 'admin'

  const { ui: uiConfig, anchor: anchorConfig } = useAppConfig()
  // ADR 0074: seeds the shell's initial panel/section/page from the URL the
  // app was loaded with. Computed once via a lazy initializer — this only
  // matters for the very first render, and re-parsing it on every render
  // would be wasted work (and wrong besides, once the URL sync effect below
  // starts rewriting the bar to match in-app navigation).
  const [initialLocation] = useState<AppLocation>(() => parseAppLocation(globalThis.location?.pathname ?? '/'))
  const [activePanel, setActivePanel] = useState<PanelId | null>(initialLocation.panel)
  // Lives in App, not in SettingsPage/SettingsNav, because App is the one
  // place that also writes it to the URL (`/settings/<id>`) — and it is
  // deliberately NOT reset when leaving Settings, so returning to Settings
  // later (without a section-specific deep link) lands back where it was.
  const [settingsSection, setSettingsSection] = useState<SettingsSectionId>(initialLocation.section ?? 'general')
  const [showAnchorImagery, setShowAnchorImagery] = useState(() => {
    const raw = globalThis.localStorage?.getItem(ANCHOR_IMAGERY_ENABLED_KEY)
    return raw === 'true'
  })
  const [showRadarEcho, setShowRadarEcho] = useState(() => {
    const raw = globalThis.localStorage?.getItem(ANCHOR_RADAR_ECHO_ENABLED_KEY)
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
  // Same again for a gauge group (ADR 0049): the backend rejects an empty one,
  // so a new group has no business reaching it until it holds a bound gauge.
  const [gaugeGroupDraft, setGaugeGroupDraft] = useState<DashboardLayoutItem | null>(null)
  const [lampStripDraft, setLampStripDraft] = useState<DashboardLayoutItem | null>(null)
  const [engineProfileOpen, setEngineProfileOpen] = useState(false)
  const [clusterDraft, setClusterDraft] = useState<DashboardLayoutItem | null>(null)
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
  // Returns whether it ran `navigate()` (false when it stashed it instead) —
  // existing call sites ignore this; the popstate handler (ADR 0074) uses it
  // to know whether it needs to re-push the URL Back just moved away from.
  const requestNavigate = useCallback((targetPanel: PanelId | null, navigate: () => void): boolean => {
    if (activePanel === 'settings' && targetPanel !== 'settings' && settingsDirty) {
      setPendingNavigation(() => navigate)
      return false
    }
    navigate()
    return true
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
    globalThis.localStorage?.setItem(ANCHOR_RADAR_ECHO_ENABLED_KEY, String(showRadarEcho))
  }, [showRadarEcho])

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
  const { pages, loading: pagesLoading, error: pagesError, createPage, updatePage, deletePage, reorderPages, reordering } = useDashboardPages()
  const [activePageId, setActivePageId] = useActiveDashboardPageId(pages, initialLocation.pageId)
  const activePage = pages.find((p) => p.id === activePageId) ?? null

  // Gates the two URL-writing effects below on the shell actually being
  // shown (mirrors the render gate further down): while auth is still
  // resolving, or a signalk install is waiting on sign-in, `canAdmin` reads
  // permissive-false rather than a real answer, and without this gate the
  // sync effect would rewrite an admin's `/settings` deep link to `/`
  // before they ever get to the login form.
  const shellVisible = !auth.loading && auth.mode !== null && !(auth.mode === 'signalk' && auth.user === null)

  // Flips to true the first time the URL sync effect below runs (whether or
  // not it actually writes) — see that effect's own comment for why the
  // write path needs to know this.
  const locationInitialisedRef = useRef(false)
  const previousFirstPageIdRef = useRef<string | null>(null)

  // Applies a parsed location to the shell's own state. Separate from
  // requestNavigate's plain `() => setActivePanel(...)` callbacks (every
  // existing sidebar/tile/breadcrumb call site) because a location can also
  // carry a page id or a settings section, and the dashboard branch has to
  // reconcile an unknown/absent page id to the first page rather than just
  // setting it blind.
  const applyAppLocation = useCallback((loc: AppLocation) => {
    setActivePanel(loc.panel)
    if (loc.panel === null && !pagesLoading) {
      const knownPageId = loc.pageId != null && pages.some((p) => p.id === loc.pageId) ? loc.pageId : null
      setActivePageId(knownPageId ?? pages[0]?.id ?? null)
    }
    if (loc.panel === 'settings') {
      setSettingsSection(loc.section ?? 'general')
    }
  }, [pages, pagesLoading, setActivePageId])

  // The single writer of window.location (ADR 0074). Chosen over pushing at
  // each of the ~10 existing setActivePanel call sites (sidebar, page
  // sub-items, breadcrumb, tile onOpen, alarm banner, anchor-watch
  // auto-close) because it needs no call-site churn and covers programmatic
  // changes too (e.g. the active page disappearing out from under a user).
  useEffect(() => {
    if (!shellVisible) return
    // Page structure not yet known: leave whatever deep link brought us
    // here alone rather than guessing at a page id that might still turn
    // out valid once the list loads.
    if (activePanel === null && pagesLoading) return

    // Set BEFORE the equality check below, or the first user click right
    // after a clean deep link (where the write below is skipped because
    // next === path already) would still see `first` as true and wrongly
    // replace that click's own entry instead of pushing it.
    const first = !locationInitialisedRef.current
    locationInitialisedRef.current = true

    const ctx = { firstPageId: pages[0]?.id ?? null, knownPageIds: pagesLoading ? null : pages.map((p) => p.id), canAdmin }
    const firstPageChanged = previousFirstPageIdRef.current !== ctx.firstPageId
    previousFirstPageIdRef.current = ctx.firstPageId
    const next = formatAppLocation({ panel: activePanel, pageId: activePageId, section: settingsSection }, ctx)
    const path = window.location.pathname
    if (next === path) return // popstate, or a clean deep link, already put us here

    // Normalise (replace) rather than add a history entry for a path this
    // effect didn't itself write — that only ever happens on first load, or
    // when the page list/admin role resolve to something that makes the
    // current bar non-canonical.
    const replace = first || firstPageChanged || !isCanonicalAppPath(path, { firstPageId: ctx.firstPageId, knownPageIds: ctx.knownPageIds, canAdmin })
    window.history[replace ? 'replaceState' : 'pushState'](null, '', next)
  }, [shellVisible, activePanel, activePageId, settingsSection, pages, pagesLoading, canAdmin])

  // Handles Back/Forward. Goes through requestNavigate so a dirty Settings
  // page still gets to veto the navigation exactly as a sidebar click
  // would — window.location has already moved to the previous entry by the
  // time this fires, so without the re-push below, a guarded Back would
  // leave the bar on the destination while the guard dialog (and Settings
  // itself) stay on screen.
  useEffect(() => {
    const handlePopState = () => {
      if (!shellVisible) return
      const path = window.location.pathname
      const ctx = { firstPageId: pages[0]?.id ?? null, knownPageIds: pagesLoading ? null : pages.map((p) => p.id), canAdmin }
      const parsed = parseAppLocation(path)
      if (!isCanonicalAppPath(path, ctx)) {
        // So the sync effect's own equality check holds once it runs off
        // the state change requestNavigate is about to (maybe) apply.
        window.history.replaceState(null, '', formatAppLocation(parsed, ctx))
      }
      if (!requestNavigate(parsed.panel, () => applyAppLocation(parsed))) {
        // Guarded: the browser already moved off Settings, so push it back —
        // the bar has to agree with the panel still on screen while the
        // confirmation dialog is up.
        window.history.pushState(null, '', formatAppLocation({ panel: 'settings', section: settingsSection }, ctx))
      }
    }
    window.addEventListener('popstate', handlePopState)
    return () => window.removeEventListener('popstate', handlePopState)
  }, [shellVisible, requestNavigate, applyAppLocation, settingsSection, pages, pagesLoading, canAdmin])

  // If admin access ends (or was never established) while Settings happens
  // to be open, drop back to the dashboard. The sync effect above then sees
  // `/settings` as non-canonical (canAdmin false) and replaces it with `/`.
  useEffect(() => {
    if (shellVisible && !canAdmin && activePanel === 'settings') {
      setActivePanel(null)
    }
  }, [shellVisible, canAdmin, activePanel])

  useEffect(() => {
    const label = activePanel ? PANEL_NAV_ITEMS.find((item) => item.id === activePanel)?.label : null
    document.title = label ? `${label} · Helmcentral` : 'Helmcentral Dashboard'
  }, [activePanel])

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
    vesselLengthOverallM,
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
    depthLastUpdateAgeS,
    positionLastUpdateAgeS,
    windLastUpdateAgeS,
  } = useVesselState()

  const { alarms, worst: worstAlarmState, acknowledge: acknowledgeAlarm, silence: silenceAlarm } = useAlarms()
  // Only for deciding whether to offer SignalK discovery. Gated on `loading`
  // below so an unconfigured-looking empty address during the initial fetch
  // can't trigger the prompt spuriously.
  const { settings: currentSettings, loading: currentSettingsLoading } = useSettingsForm()
  const { vessels: nearbyVessels, loading: nearbyVesselsLoading, lastUpdateAgeS: nearbyVesselsAgeS } = useNearbyVessels()
  const { targets: radarTargets, radars: radarInfos, source: radarSource, loading: radarTargetsLoading } = useRadarTargets()
  const { tanks, loading: tanksLoading, lastUpdateAgeS: tanksAgeS } = useTanksState()
  const {
    lastUpdateAgeS: electricalLastUpdateAgeS,
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
    lastUpdateAgeS: solarLastUpdateAgeS,
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
    provider: waveForecastProvider,
    isCached: waveForecastIsCached,
    updatedAt: waveForecastUpdatedAt,
    ttlSeconds: waveForecastTtlSeconds,
    loading: waveForecastLoading,
    error: waveForecastError,
    refetch: refetchWaveForecast,
  } = useWaveForecast(FORECAST_REFRESH_SECONDS)
  // Upper air keeps its own refresh cadence: the global models behind it run
  // four times a day, so polling it on the surface-forecast interval would
  // re-fetch the same numbers.
  const {
    days: upperAirDays,
    series: upperAirSeries,
    windowBand: upperAirWindow,
    updatedAt: upperAirUpdatedAt,
    ttlSeconds: upperAirTtlSeconds,
  } = useUpperAir()
  const anchorWatch = useAnchorWatch(
    latitude,
    longitude,
    uiConfig.vesselStateRefreshSeconds,
    gnssCriticalAlert,
  )
  // The forecast wind band shared by the Rode Planner, the tile's Scope row,
  // and the drawer's Scope row (frontend/src/lib/rode-plan.ts's
  // resolvePlanningWindBand) — one operator choice, not three independent
  // seeds. Deliberately not persisted: a band chosen for last night's
  // forecast must not silently drive tonight's recommendation. A reload
  // returns to the live seed.
  const [windBandId, setWindBandId] = useState<string | null>(null)
  // The planning depth is seeded from the depth at drop and editable from
  // there (ADR 0063). Hoisted here for the same reason windBandId is (ADR
  // 0059 §3): three surfaces (tile, drawer's map Scope row, and the Rode
  // Planner) plan off this number, and a component-local copy is how the
  // tile and planner drifted apart before computeScopeRecommendation
  // existed. While anchored the planning depth lives on the watch record
  // (anchorWatch.planningDepthM, persisted server-side); this state only
  // covers the not-anchored "what-if" case, which has nowhere else to live
  // and is deliberately lost on reload. Cleared on every hasActiveAnchorWatch
  // transition below — without that, a pre-drop what-if would reappear once
  // the anchor comes up and is raised again.
  const [sessionPlanningDepth, setSessionPlanningDepth] = useState<{ depthM: number; tideHeightFt: number | null } | null>(null)
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
  const { switches: czoneSwitches, loading: czoneLoading, pending: czonePending, error: czoneError, toggleSwitch: toggleCZone } = useCZoneSwitches(5)
  const autopilot = useAutopilot()
  const isImperialDistance = uiConfig.distanceUnits === 'imperial'
  const isAlternatorTileVisible = (engine0Rpm !== null && engine0Rpm > 0) || (engine1Rpm !== null && engine1Rpm > 0)

  const handleDropAnchorHere = () => {
    if (latitude === null || longitude === null) return
    void anchorWatch.setAnchorHere(latitude, longitude, {
      planningDepthM: depth,
      planningTideHeightFt: tideHeightFtOrNull(tide),
    })
  }

  const hasActiveWindBulletin = Boolean(findActiveWindBulletin(activeForecastWarning))
  const hasActiveAnchorWatch = anchorWatch.anchorState !== 'none'

  // See sessionPlanningDepth's own comment above for why this has to be
  // cleared on every transition rather than left to go stale.
  useEffect(() => {
    setSessionPlanningDepth(null)
  }, [hasActiveAnchorWatch])

  // The single resolved planning depth every surface plans against (ADR
  // 0063): the persisted watch record while anchored, the session what-if
  // otherwise.
  const resolvedPlanningDepthM = hasActiveAnchorWatch ? anchorWatch.planningDepthM : sessionPlanningDepth?.depthM ?? null
  const resolvedPlanningTideHeightFt = hasActiveAnchorWatch ? anchorWatch.planningTideHeightFt : sessionPlanningDepth?.tideHeightFt ?? null

  const handlePlanningDepthChange = useCallback((depthM: number, tideHeightFt: number | null) => {
    if (hasActiveAnchorWatch) {
      void anchorWatch.updatePlanningDepth(depthM, tideHeightFt ?? -1)
    } else {
      setSessionPlanningDepth({ depthM, tideHeightFt })
    }
  }, [hasActiveAnchorWatch, anchorWatch])

  // One poller for the whole app: the tile and the fullscreen drawer both
  // draw the same session's pins, and each running its own would double the
  // request rate for identical data.
  const { placemarks, createPlacemark, removePlacemark } = useAnchorPlacemarks(hasActiveAnchorWatch)

  // Settings hosts Secrets as a subsection (settings-page.tsx), so hiding
  // this one nav item covers both per ADR 0040's tier table — there is no
  // separate top-level Secrets entry to hide independently.
  const visiblePanelNavItems = canAdmin ? PANEL_NAV_ITEMS : PANEL_NAV_ITEMS.filter((item) => item.id !== 'settings')

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

  // Wider and taller than a single gauge: a cluster needs the room.
  const handleAddGaugeGroup = useCallback(() => {
    const maxY = effectiveWidgets.reduce((max, w) => Math.max(max, w.y + w.h), 0)
    setGaugeGroupDraft({
      id: newGaugeGroupWidgetId(effectiveWidgets),
      x: 0,
      y: maxY,
      w: 6,
      h: 8,
      gaugeGroup: { title: '', gauges: [{ path: '', label: '', display: 'numeric', quantity: 'raw', unit: 'raw' }] },
    })
  }, [effectiveWidgets])

  const handleSaveGaugeGroup = useCallback((gaugeGroup: GaugeGroupWidgetConfig) => {
    if (!activePage || !gaugeGroupDraft) return
    const id = gaugeGroupDraft.id
    if (effectiveWidgets.some((w) => w.id === id)) {
      void updatePage(activePage.id, {
        widgets: effectiveWidgets.map((w) => (w.id === id ? { ...w, gaugeGroup } : w)),
      })
    } else {
      void updatePage(activePage.id, { widgets: [...effectiveWidgets, { ...gaugeGroupDraft, gaugeGroup }] })
    }
    setGaugeGroupDraft(null)
  }, [activePage, effectiveWidgets, gaugeGroupDraft, updatePage])

  /**
   * An engine profile lands as an ordinary gauge group (ADR 0053) — already
   * configured, and saved straight away rather than held as a draft, because
   * unlike a blank tile it is valid the moment it is built.
   */
  const handleApplyEngineProfile = useCallback((title: string, gauges: GaugeWidgetConfig[]) => {
    if (!activePage) return
    const maxY = effectiveWidgets.reduce((max, w) => Math.max(max, w.y + w.h), 0)
    void updatePage(activePage.id, {
      widgets: [...effectiveWidgets, {
        id: newGaugeGroupWidgetId(effectiveWidgets),
        x: 0, y: maxY, w: 6, h: 8,
        gaugeGroup: { title, gauges },
      }],
    })
    setEngineProfileOpen(false)
  }, [activePage, effectiveWidgets, updatePage])

  // Tall: the canvas is 460x300 plus the tile chrome.
  const handleAddCluster = useCallback(() => {
    const maxY = effectiveWidgets.reduce((max, w) => Math.max(max, w.y + w.h), 0)
    setClusterDraft({
      id: newClusterWidgetId(effectiveWidgets),
      x: 0, y: maxY, w: 6, h: 7,
      cluster: {
        title: '',
        ring: { path: '', label: 'RPM', display: 'radial', quantity: 'raw', unit: 'raw' },
        centre: { path: '', label: 'Hours', display: 'numeric', quantity: 'raw', unit: 'raw' },
        corners: [],
      },
    })
  }, [effectiveWidgets])

  const handleSaveCluster = useCallback((cluster: EngineClusterConfig) => {
    if (!activePage || !clusterDraft) return
    const id = clusterDraft.id
    if (effectiveWidgets.some((w) => w.id === id)) {
      void updatePage(activePage.id, {
        widgets: effectiveWidgets.map((w) => (w.id === id ? { ...w, cluster } : w)),
      })
    } else {
      void updatePage(activePage.id, { widgets: [...effectiveWidgets, { ...clusterDraft, cluster }] })
    }
    setClusterDraft(null)
  }, [activePage, effectiveWidgets, clusterDraft, updatePage])

  // Wide and short: a ribbon spans the page rather than occupying a cell.
  const handleAddLampStrip = useCallback(() => {
    const maxY = effectiveWidgets.reduce((max, w) => Math.max(max, w.y + w.h), 0)
    setLampStripDraft({
      id: newLampStripWidgetId(effectiveWidgets),
      x: 0,
      y: maxY,
      w: 12,
      h: 3,
      lamps: { title: 'Status', lamps: [{ path: '', label: '' }], showCheck: true },
    })
  }, [effectiveWidgets])

  const handleSaveLampStrip = useCallback((lamps: LampStripWidgetConfig) => {
    if (!activePage || !lampStripDraft) return
    const id = lampStripDraft.id
    if (effectiveWidgets.some((w) => w.id === id)) {
      void updatePage(activePage.id, {
        widgets: effectiveWidgets.map((w) => (w.id === id ? { ...w, lamps } : w)),
      })
    } else {
      void updatePage(activePage.id, { widgets: [...effectiveWidgets, { ...lampStripDraft, lamps }] })
    }
    setLampStripDraft(null)
  }, [activePage, effectiveWidgets, lampStripDraft, updatePage])

  /**
   * Copies a tile and opens the copy's config straight away — the copy exists
   * to be retargeted, so making that the immediate next step is the point.
   * Persisted first: unlike a fresh draft, a duplicate is already valid.
   */
  const handleDuplicateWidget = useCallback((id: DashboardWidgetId) => {
    if (!activePage) return
    const source = effectiveWidgets.find((w) => w.id === id)
    if (!source) return
    const copy = duplicateWidget(source, effectiveWidgets)
    if (!copy) return

    const maxY = effectiveWidgets.reduce((max, w) => Math.max(max, w.y + w.h), 0)
    const placed = { ...copy, x: 0, y: maxY }
    void updatePage(activePage.id, { widgets: [...effectiveWidgets, placed] })

    if (isClusterWidgetId(placed.id)) setClusterDraft(placed)
    else if (isLampStripWidgetId(placed.id)) setLampStripDraft(placed)
    else if (isGaugeGroupWidgetId(placed.id)) setGaugeGroupDraft(placed)
    else if (isGaugeWidgetId(placed.id)) setGaugeDraft(placed)
    else if (isEmbedWidgetId(placed.id)) setEmbedDraft(placed)
  }, [activePage, effectiveWidgets, updatePage])

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
    if (isClusterWidgetId(id)) {
      if (!widget.cluster) return null
      return (
        <EngineClusterTile
          config={widget.cluster}
          values={gaugeValues}
          editing={layoutEditing}
          onConfigure={() => setClusterDraft(widget)}
        />
      )
    }

    if (isLampStripWidgetId(id)) {
      if (!widget.lamps) return null
      return (
        <LampStripTile
          config={widget.lamps}
          values={gaugeValues}
          worstAlarmState={worstAlarmState}
          editing={layoutEditing}
          onConfigure={() => setLampStripDraft(widget)}
          onOpenAlarms={() => requestNavigate('alarms', () => setActivePanel('alarms'))}
        />
      )
    }

    if (isGaugeGroupWidgetId(id)) {
      if (!widget.gaugeGroup) return null
      return (
        <GaugeGroupTile
          config={widget.gaugeGroup}
          values={gaugeValues}
          editing={layoutEditing}
          onConfigure={() => setGaugeGroupDraft(widget)}
        />
      )
    }

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
            lastUpdateAgeS={windLastUpdateAgeS}
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
            lastUpdateAgeS={depthLastUpdateAgeS}
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
            lastUpdateAgeS={positionLastUpdateAgeS}
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
            lastUpdateAgeS={positionLastUpdateAgeS}
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
            radarTargets={radarTargets}
            radars={radarInfos}
            radarSource={radarSource}
            isDarkTheme={isDarkTheme}
            showImageryLayer={showAnchorImagery}
            onImageryToggle={setShowAnchorImagery}
            showRadarEcho={showRadarEcho}
            onRadarEchoToggle={setShowRadarEcho}
            onFullscreen={() => setActivePanel('anchor-watch')}
            placemarks={placemarks}
            onPlacemarkCreate={createPlacemark}
            onPlacemarkRemove={removePlacemark}
            tide={tide}
            windSpeedApparentKts={windSpeedApparentKts}
            maxGustKts={maxGustKts}
            anchorConfig={anchorConfig}
            selectedWindBandId={windBandId}
            planningDepthM={resolvedPlanningDepthM}
            planningTideHeightFt={resolvedPlanningTideHeightFt}
          />
        )
      case 'tanks':
        return <TanksTile tanks={tanks} loading={tanksLoading} lastUpdateAgeS={tanksAgeS} />
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
        return <NearbyVesselsTile vessels={nearbyVessels} loading={nearbyVesselsLoading} distanceUnits={uiConfig.distanceUnits} lastUpdateAgeS={nearbyVesselsAgeS} />
      case 'radar-targets':
        return (
          <RadarTargetsTile
            targets={radarTargets}
            radars={radarInfos}
            source={radarSource}
            loading={radarTargetsLoading}
            distanceUnits={uiConfig.distanceUnits}
          />
        )
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
            lastUpdateAgeS={electricalLastUpdateAgeS}
          />
        )
      case 'solar':
        return (
          <SolarTile
            currentW={solarCurrentW}
            todayKWh={solarTodayKWh}
            yesterdayKWh={solarYesterdayKWh}
            peakTodayW={solarPeakTodayW}
            lastUpdateAgeS={solarLastUpdateAgeS}
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
            readOnly={!canWrite}
          />
        )
      case 'czone-switches':
        return <CZoneSwitchesTile switches={czoneSwitches} loading={czoneLoading} pending={czonePending} onToggle={toggleCZone} error={czoneError} readOnly={!canWrite} />
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
            readOnly={!canWrite}
          />
        )
      case 'hot-water':
        return <HotWaterTile />
      default:
        return null
    }
  }

  const dashboardGrid = (
    // ADR 0060: the skin lives on the page, so the attribute sits on this
    // shared root and every tile beneath it re-skins with no component
    // changes. bg-background is a no-op while unskinned — it is exactly what
    // body already paints in both themes — and starts doing something only
    // once a skin redefines --background above it. The padding is a token
    // rather than a conditional class for the same reason: a component must
    // never branch on the skin value, only consume tokens the skin redefines.
    <div
      data-skin={activePage?.skin === 'instrument' ? 'instrument' : undefined}
      className="flex flex-col gap-4 bg-background"
      style={{ padding: 'var(--board-pad)', borderRadius: 'var(--board-radius)' }}
    >
      {layoutEditing && (
        <div className="inline-flex w-fit items-center gap-2 rounded-md border border-primary/30 bg-primary/10 px-3 py-1.5 text-[10px] font-semibold uppercase tracking-[0.16em] text-primary">
          Layout Mode — Drag to rearrange
        </div>
      )}

      <DashboardBentoGrid
        widgets={effectiveWidgets}
        editing={layoutEditing}
        heroId={activePage?.hero}
        renderWidget={renderWidget}
        onRemoveWidget={handleRemoveWidget}
        onDuplicateWidget={handleDuplicateWidget}
        onLayoutSettle={handleLayoutSettle}
      />

      {/* Always available in layout mode: Embed is never "placed", so unlike the
          builtin widgets it can be added any number of times. */}
      {layoutEditing && (
        <div className="flex w-fit flex-wrap items-center gap-2">
        <PageSkinSelect
          page={activePage ?? null}
          onSetSkin={(id, skin) => { void updatePage(id, { skin }) }}
        />
        <PageHeroSelect
          page={activePage ?? null}
          onSetHero={(id, hero) => { void updatePage(id, { hero }) }}
        />
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
                onClick={handleAddCluster}
                className="rounded-sm px-2 py-1.5 text-left text-sm hover:bg-accent hover:text-accent-foreground"
              >
                Engine Cluster…
              </button>
              <button
                type="button"
                onClick={() => setEngineProfileOpen(true)}
                className="rounded-sm px-2 py-1.5 text-left text-sm hover:bg-accent hover:text-accent-foreground"
              >
                From engine profile…
              </button>
              <button
                type="button"
                onClick={handleAddLampStrip}
                className="rounded-sm px-2 py-1.5 text-left text-sm hover:bg-accent hover:text-accent-foreground"
              >
                Indicators…
              </button>
              <button
                type="button"
                onClick={handleAddGaugeGroup}
                className="rounded-sm px-2 py-1.5 text-left text-sm hover:bg-accent hover:text-accent-foreground"
              >
                Gauge Group…
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
        </div>
      )}

      <GaugeConfigDialog
        widget={gaugeDraft}
        onCancel={() => setGaugeDraft(null)}
        onSave={handleSaveGauge}
      />

      <GaugeGroupConfigDialog
        widget={gaugeGroupDraft}
        onCancel={() => setGaugeGroupDraft(null)}
        onSave={handleSaveGaugeGroup}
      />

      <EngineClusterConfigDialog
        widget={clusterDraft}
        onCancel={() => setClusterDraft(null)}
        onSave={handleSaveCluster}
      />

      <EngineProfileDialog
        open={engineProfileOpen}
        onCancel={() => setEngineProfileOpen(false)}
        onApply={handleApplyEngineProfile}
      />

      <LampStripConfigDialog
        widget={lampStripDraft}
        onCancel={() => setLampStripDraft(null)}
        onSave={handleSaveLampStrip}
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
            upperAirDays={upperAirDays}
            upperAirSeries={upperAirSeries}
            upperAirWindow={upperAirWindow}
            upperAirUpdatedAt={upperAirUpdatedAt}
            upperAirTtlSeconds={upperAirTtlSeconds}
            waveSeaTemperatureF={waveSeaTemperatureF}
            waveLoading={waveForecastLoading}
            waveError={waveForecastError}
            waveProvider={waveForecastProvider}
            waveIsCached={waveForecastIsCached}
            waveUpdatedAt={waveForecastUpdatedAt}
            waveTtlSeconds={waveForecastTtlSeconds}
            onWaveRetry={refetchWaveForecast}
          />
        )
      case 'alarms':
        return <AlarmsDrawer alarms={alarms} onAcknowledge={acknowledgeAlarm} onSilence={silenceAlarm} />
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
            activeSectionId={settingsSection}
            onSectionChange={setSettingsSection}
          />
        )
      case 'anchor-watch':
        return (
          // vesselLat/vesselLon fall back from the live fix to the anchor
          // point (e.g. GPS lost after the anchor was already set), and stay
          // null only when neither is available — the drawer renders an
          // explicit "No GPS fix" placeholder in the map slot for that case
          // rather than being handed a fabricated 0,0.
          <AnchorWatchDrawer
            placemarks={placemarks}
            onPlacemarkCreate={createPlacemark}
            onPlacemarkRemove={removePlacemark}
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
            bowOffsetM={anchorWatch.bowOffsetM}
            bowOffsetApplied={anchorWatch.bowOffsetApplied}
            bowOffsetReason={anchorWatch.bowOffsetReason}
            anchorSetAt={anchorWatch.setAt}
            vesselTrail={getSelfTrail}
            aisVessels={nearbyVessels}
            aisTrails={getAisTrails}
            radarTargets={radarTargets}
            radars={radarInfos}
            radarSource={radarSource}
            isDarkTheme={isDarkTheme}
            showImageryLayer={showAnchorImagery}
            onImageryToggle={setShowAnchorImagery}
            showRadarEcho={showRadarEcho}
            onRadarEchoToggle={setShowRadarEcho}
            onAnchorReposition={anchorWatch.updatePosition}
            onRadiusChange={anchorWatch.updateRadius}
            onClearAnchor={anchorWatch.clearAnchor}
            isImperial={isImperialDistance}
            isAutoCloseArmed={isAutoCloseArmed}
            motoringSecondsElapsed={motoringSecondsElapsed}
            onDropAnchor={handleDropAnchorHere}
            canDrop={latitude !== null && longitude !== null}
            anchorState={anchorWatch.anchorState}
            rodeDeployedM={anchorWatch.rodeDeployedM}
            seaState={anchorWatch.seaState}
            seabedType={anchorWatch.seabedType}
            windSpeedApparentKts={windSpeedApparentKts}
            maxGustKts={maxGustKts}
            tide={tide}
            anchorConfig={anchorConfig}
            vesselLengthOverallM={vesselLengthOverallM}
            windBandId={windBandId}
            onWindBandChange={setWindBandId}
            onUpdateRodeAndConditions={anchorWatch.updateRodeAndConditions}
            planningDepthM={resolvedPlanningDepthM}
            planningTideHeightFt={resolvedPlanningTideHeightFt}
            onPlanningDepthChange={handlePlanningDepthChange}
          />
        )
      default:
        return null
    }
  })()

  // Gate the entire dashboard shell, not just its content area — an
  // unauthenticated visitor under mode:signalk gets no sidebar navigation,
  // no widget data, nothing, only the login form. mode:none renders exactly
  // as it did before this change.
  //
  // The order below matters, and all three non-dashboard branches come first
  // deliberately. `canWrite`/`canAdmin` above are derived from
  // `auth.mode !== 'signalk'`, so an unknown mode reads as permissive — which
  // means "render the dashboard whenever mode isn't literally 'signalk'"
  // would hand full admin affordances to a visitor whose mode probe merely
  // failed. Server enforcement still holds either way, but the UI does not
  // get to guess: it says what it doesn't know.
  if (auth.loading) {
    return (
      <div className="flex min-h-screen items-center justify-center bg-background text-sm text-muted-foreground">
        Checking sign-in…
      </div>
    )
  }

  if (auth.mode === null) {
    return (
      <div className="flex min-h-screen flex-col items-center justify-center gap-3 bg-background px-6 text-center">
        <p className="text-sm font-semibold text-foreground">
          Could not determine whether this Helmcentral requires a sign-in.
        </p>
        {auth.error && (
          <p className="max-w-md text-xs text-muted-foreground" role="alert">
            {auth.error}
          </p>
        )}
        <Button variant="outline" size="sm" onClick={() => globalThis.location.reload()}>
          Retry
        </Button>
      </div>
    )
  }

  if (auth.mode === 'signalk' && auth.user === null) {
    return <LoginScreen onLogin={auth.login} error={auth.error} />
  }

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
            {visiblePanelNavItems.map(({ id, label, icon: Icon }) => (
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
        <SidebarFooter>
          <SidebarVersion />
        </SidebarFooter>
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
                  canWrite={canWrite}
                  onReorder={reorderPages}
                  reordering={reordering}
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
            <VesselStatusBar
              isDark={isDarkTheme}
              onToggleDarkMode={toggleDarkMode}
              username={auth.mode === 'signalk' ? auth.user?.username ?? null : null}
              onLogout={auth.mode === 'signalk' ? () => { void auth.logout() } : undefined}
            />
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
