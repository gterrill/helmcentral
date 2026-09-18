import {
  Anchor,
  BellRing,
  BookOpen,
  CircleHelp,
  CloudSun,
  FileText,
  LayoutDashboard,
  Map,
  Mic,
  MicOff,
  MonitorPlay,
  Radar as RadarIcon,
  Route,
  Settings,
  Sparkles,
} from 'lucide-react'
import { Suspense, lazy, useCallback, useEffect, useMemo, useRef, useState, type ReactNode } from 'react'
import { toast } from 'sonner'

import { AnchorWatchTile } from '@/components/anchor-watch-tile'
import { AlternatorTile } from '@/components/alternator-tile'
import { BatteryPowerTile } from '@/components/battery-power-tile'
import { HotWaterTile } from '@/components/hot-water-tile'
import { DepthTideTile } from '@/components/depth-tide-tile'
import { PositionTile } from '@/components/position-tile'
import { TodayNowTile } from '@/components/today-now-tile'
import { ClockTile } from '@/components/clock-tile'
import { CurrentConditionsTile } from '@/components/current-conditions-tile'
import { ForecastDaysTile } from '@/components/forecast-days-tile'
import { SeaStateTile } from '@/components/sea-state-tile'
import { WindTile } from '@/components/wind-tile'
import { MarineHeader } from '@/components/marine-header'
import { VesselStatusBar } from '@/components/vessel-status-bar'
import { AlarmBanner } from '@/components/alarm-banner'
import { NearbyVesselsTile } from '@/components/nearby-vessels-tile'
import { RadarTargetsTile } from '@/components/radar-targets-tile'
import type { SettingsPageHandle } from '@/components/settings/settings-page'
import type { SettingsSectionId } from '@/components/settings/settings-nav'

const AlarmsDrawer = lazy(() => import('@/components/alarms-drawer').then((mod) => ({ default: mod.AlarmsDrawer })))
const AnchorWatchDrawer = lazy(() => import('@/components/anchor-watch-drawer').then((mod) => ({ default: mod.AnchorWatchDrawer })))
const AssistantDrawer = lazy(() => import('@/components/assistant-drawer').then((mod) => ({ default: mod.AssistantDrawer })))
const DocumentsPanel = lazy(() => import('@/components/documents-panel').then((mod) => ({ default: mod.DocumentsPanel })))
const ForecastDrawer = lazy(() => import('@/components/forecast-drawer').then((mod) => ({ default: mod.ForecastDrawer })))
const RadarDrawer = lazy(() => import('@/components/radar-drawer').then((mod) => ({ default: mod.RadarDrawer })))
const RoutePlannerDrawer = lazy(() => import('@/components/route-planner-drawer').then((mod) => ({ default: mod.RoutePlannerDrawer })))
const SatChartsDrawer = lazy(() => import('@/components/sat-charts-drawer').then((mod) => ({ default: mod.SatChartsDrawer })))
const SettingsPage = lazy(() => import('@/components/settings/settings-page').then((mod) => ({ default: mod.SettingsPage })))
// MateSheet and ManualSheet (unlike the panels above) fetch nothing and run
// no effects until they've actually been opened - see the `hasOpened` latches
// below, next to where each is rendered, for why that makes them safe to
// lazy-load and mount only on first open rather than always up front.
const MateSheet = lazy(() => import('@/components/mate-sheet').then((mod) => ({ default: mod.MateSheet })))
const ManualSheet = lazy(() => import('@/components/manual-sheet').then((mod) => ({ default: mod.ManualSheet })))
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
import { RouteTile } from '@/components/route-tile'
import { DashboardBentoGrid, WALL_ROW_MARGIN } from '@/components/dashboard-bento-grid'
import { LayoutModeToggle } from '@/components/layout-mode-toggle'
import { LayoutToolbar } from '@/components/layout-toolbar'
import { EmptyPagePrompt } from '@/components/empty-page-prompt'
import type { AddTileMultiInstanceEntry } from '@/components/add-tile-picker'
import { Toaster } from '@/components/ui/sonner'
import { useRoutes } from '@/hooks/use-routes'
import { useSatCharts } from '@/hooks/use-sat-charts'
import { useDashboardRouteId } from '@/hooks/use-dashboard-route'
import { useDashboardPages } from '@/hooks/use-dashboard-pages'
import { useDashboardRibbon } from '@/hooks/use-dashboard-ribbon'
import { useActiveDashboardPageId } from '@/hooks/use-active-dashboard-page'
import { useKioskRotation } from '@/hooks/use-kiosk-rotation'
import { parseKioskOptions, KIOSK_FOLD_PX } from '@/lib/kiosk'
import { nextWaypoint, etaToWaypoint } from '@/lib/next-waypoint'
import { KioskShell } from '@/components/kiosk-shell'
import { KioskFoldGuide } from '@/components/kiosk-fold-guide'
import { DashboardPageSwitcher, KioskPageGlyph } from '@/components/dashboard-page-switcher'
import { useRouteActivation } from '@/hooks/use-route-activation'
import { useElectricalState } from '@/hooks/use-electrical-state'
import { useSolarState } from '@/hooks/use-solar-state'
import { useNearbyVessels } from '@/hooks/use-nearby-vessels'
import { useRadarTargets } from '@/hooks/use-radar-targets'
import { useAnchorWatch } from '@/hooks/use-anchor-watch'
import { useAnchorPlacemarks } from '@/hooks/use-anchor-placemarks'
import { ConnectionBanner } from '@/components/connection-banner'
import { usePlaceName } from '@/hooks/use-place-name'
import { useTanksState } from '@/hooks/use-tanks-state'
import { useTideToday } from '@/hooks/use-tide-today'
import { tideHeightFtOrNull } from '@/lib/rode-plan'
import { findActiveWindBulletin, useForecastWarnings } from '@/hooks/use-forecast-warnings'
import { useVesselState } from '@/hooks/use-vessel-state'
import { collisionAlarmStatesByVessel, useAlarms } from '@/hooks/use-alarms'
import { useAlarmRules } from '@/hooks/use-alarm-rules'
import { useSocBands } from '@/hooks/use-soc-bands'
import { useOvernightProjection } from '@/hooks/use-overnight-projection'
import { DEFAULT_SOC_PATH } from '@/lib/soc-bands'
import { useSettingsForm } from '@/hooks/use-settings-form'
import { collisionTuningUrl as buildCollisionTuningUrl } from '@/lib/collision-tuning'
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
import { FORECAST_REFRESH_SECONDS, PLACE_NAME_REFRESH_SECONDS, fallbackAssistantVoiceConfig } from '@/config/app-config'
import { useAppConfig } from '@/hooks/use-app-config'
import { useMateVoice } from '@/hooks/use-mate-voice'
import { useMateAnswerWatcher } from '@/hooks/use-mate-answer-watcher'
import { useSpeechOutput } from '@/hooks/use-speech-output'
import { BREAKPOINTS, useMinWidth } from '@/lib/breakpoints'
import {
  DASHBOARD_WIDGET_DEFAULT_SIZE,
  duplicateWidget,
  isEmbedWidgetId,
  isGaugeGroupWidgetId,
  isGaugeWidgetId,
  isClusterWidgetId,
  isLampStripWidgetId,
  isPoiMapWidgetId,
  newEmbedWidgetId,
  newGaugeGroupWidgetId,
  newGaugeWidgetId,
  newClusterWidgetId,
  newLampStripWidgetId,
  newPoiMapWidgetId,
  type BuiltinWidgetId,
  type DashboardLayoutItem,
  type DashboardWidgetId,
  type EmbedWidgetConfig,
  type GaugeGroupWidgetConfig,
  type EngineClusterConfig,
  type LampStripWidgetConfig,
  type GaugeWidgetConfig,
  type PoiMapWidgetConfig,
} from '@/lib/dashboard-widgets'
import { POI_CATEGORY_IDS } from '@/lib/poi'
import { EmbedTile } from '@/components/embed-tile'
import { EmbedConfigDialog } from '@/components/embed-config-dialog'
import { PoiMapTile } from '@/components/poi-map-tile'
import { PoiMapConfigDialog } from '@/components/poi-map-config-dialog'
import { GaugeConfigDialog } from '@/components/gauge-config-dialog'
import { GaugeTile } from '@/components/gauge-tile'
import { GaugeGroupConfigDialog } from '@/components/gauge-group-config-dialog'
import { GaugeGroupTile } from '@/components/gauge-group-tile'
import { EngineClusterConfigDialog } from '@/components/engine-cluster-config-dialog'
import { EngineClusterTile } from '@/components/engine-cluster-tile'
import { EngineProfileDialog } from '@/components/engine-profile-dialog'
import { LampStripConfigDialog } from '@/components/lamp-strip-config-dialog'
import { LampStripTile } from '@/components/lamp-strip-tile'
import { useGaugeAges, useGaugeValues } from '@/hooks/use-gauge-values'
import { LoginScreen } from '@/components/login-screen'
import { Button } from '@/components/ui/button'
import { Separator } from '@/components/ui/separator'
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
import { screenContextFor } from '@/lib/mate-screen'
import { manualTargetFor, type ManualTarget } from '@/lib/manual-links'
import { cn } from '@/lib/utils'

const PANEL_NAV_ITEMS: Array<{ id: PanelId; label: string; icon: typeof CloudSun }> = [
  { id: 'forecast', label: 'Forecast', icon: CloudSun },
  { id: 'routes', label: 'Routes', icon: Route },
  { id: 'charts', label: 'Charts', icon: Map },
  { id: 'radar', label: 'Radar', icon: RadarIcon },
  { id: 'anchor-watch', label: 'Anchor Watch', icon: Anchor },
  { id: 'alarms', label: 'Alarms', icon: BellRing },
  { id: 'assistant', label: 'Mate', icon: Sparkles },
  { id: 'documents', label: 'Documents', icon: FileText },
  { id: 'settings', label: 'Settings', icon: Settings },
]

const ANCHOR_IMAGERY_ENABLED_KEY = 'anchorWatch.imagery.enabled'
const ANCHOR_RADAR_ECHO_ENABLED_KEY = 'anchorWatch.radarEcho.enabled'

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

  // `assistant` defaults defensively: several existing test suites mock
  // useAppConfig with only the ui/anchor blocks they exercise, and this
  // keeps them passing without every one of them growing an unrelated
  // voice-config fixture.
  const { ui: uiConfig, anchor: anchorConfig, assistant: assistantVoiceConfig = fallbackAssistantVoiceConfig } = useAppConfig()
  // ADR 0074: seeds the shell's initial panel/section/page from the URL the
  // app was loaded with. Computed once via a lazy initializer — this only
  // matters for the very first render, and re-parsing it on every render
  // would be wasted work (and wrong besides, once the URL sync effect below
  // starts rewriting the bar to match in-app navigation).
  const [initialLocation] = useState<AppLocation>(() =>
    parseAppLocation((globalThis.location?.pathname ?? '/') + (globalThis.location?.search ?? '')))
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
  const [layoutEditingRequested, setLayoutEditing] = useState(false)
  const canEditLayout = useMinWidth(BREAKPOINTS.lg)
  // Derived, not stored: narrowing the window past `lg` removes both the grid and
  // the toggle that would exit edit mode, so a stored flag would strand the
  // dashboard in a non-interactive state with no way back out.
  const layoutEditing = layoutEditingRequested && canEditLayout
  // The page a "New Page" click just created (ADR 0107): its name field
  // starts empty and focused instead of showing "Untitled page", and is the
  // only page whose field behaves that way. Cleared once that field settles
  // (Enter/blur that saves, or Escape) — see components/page-title-field.tsx.
  const [namingPageId, setNamingPageId] = useState<string | null>(null)
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
  // Same pattern as embedDraft: a freshly added Nearby map (ADR 0091 phase
  // 3b) exists only here until it holds a valid config.
  const [poiMapDraft, setPoiMapDraft] = useState<DashboardLayoutItem | null>(null)
  const gaugeValues = useGaugeValues()
  // Age behind each bound path (ADR 0083), riding the same gauge-values event
  // rather than a stream of its own -- see hooks/use-gauge-values.ts.
  const gaugeAges = useGaugeAges()
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

  // Hoisted ahead of useDarkMode (rather than left beside kioskOptions below,
  // where it used to live) so the kiosk dark-theme override just below has
  // it in scope. isKiosk depends only on activePanel, which is already set
  // by this point, so nothing about moving it changes what it means.
  const isKiosk = activePanel === 'kiosk'
  const [storedIsDarkTheme, toggleDarkMode] = useDarkMode()
  // The wall display always renders dark (operator decision, ADR 0089
  // phase 2), regardless of what this browser has stored — a fresh kiosk
  // profile otherwise defaults to light, which is how a light basemap ended
  // up inside dark instrument-skin tiles on the 1920x360 strip. The override
  // lives here, at the one place isDarkTheme is established, so every
  // consumer (the root `dark` class effect right below, and every tile/map
  // isDarkTheme prop threaded from this same variable) agrees without
  // special-casing any one of them. toggleDarkMode is left untouched: it
  // still reads and writes the real stored preference, so leaving /kiosk
  // resumes whatever the operator last chose on this browser rather than
  // whatever the wall display happened to force.
  const isDarkTheme = isKiosk || storedIsDarkTheme
  useEffect(() => {
    document.documentElement.classList.toggle('dark', isDarkTheme)
  }, [isDarkTheme])
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
  const { pages, loading: pagesLoading, error: pagesError, refetch: refetchPages, createPage, updatePage, deletePage, reorderPages, reordering } = useDashboardPages()
  const [activePageId, setActivePageId] = useActiveDashboardPageId(pages, initialLocation.pageId)
  const activePage = pages.find((p) => p.id === activePageId) ?? null
  // Hoisted ahead of the polling hooks below (item B) that gate themselves on
  // which widgets the active page (or the kiosk's current page, which drives
  // activePageId exactly the same way — see useKioskRotation below) actually
  // holds. Otherwise identical to its previous declaration further down.
  const effectiveWidgets = useMemo(() => activePage?.widgets ?? [], [activePage])
  // namingPageId only means anything for the page it was set on, while
  // layout mode can actually show that page's name field — stale otherwise.
  // PageTitleField's own onDone (an Enter/blur that saved, or an Escape)
  // clears it for the ordinary case, but switching pages without ever
  // touching the field skips that entirely, and so does narrowing the
  // window (or toggling Edit off) out of layout mode. Left uncleared, either
  // one strands the flag on a page that isn't showing right now — returning
  // to it later re-opens a blank, focus-stealing name field for a page that
  // already has a real name.
  useEffect(() => {
    if (namingPageId !== null && namingPageId !== activePageId) {
      setNamingPageId(null)
    }
  }, [activePageId, namingPageId])
  useEffect(() => {
    if (!layoutEditing) {
      setNamingPageId(null)
    }
  }, [layoutEditing])
  // ADR 0089: the wall display at /kiosk. Its query string is its own
  // (rotate, a pinned page for authoring/screenshots) rather than app state,
  // so it's parsed once here the same way initialLocation is, and never
  // written back to the URL — see the early returns in the sync effect and
  // the popstate handler below. isKiosk itself is declared earlier, beside
  // useDarkMode, so the kiosk dark-theme override there can read it.
  const [kioskOptions] = useState(() => parseKioskOptions(globalThis.location?.search ?? ''))
  // The pinned indicator ribbon (ADR 0082): one vessel-level lamp strip, not
  // tied to any page, so it lives beside the page hooks rather than inside
  // effectiveWidgets below.
  const { ribbon, saveRibbon } = useDashboardRibbon()
  const [ribbonDialogOpen, setRibbonDialogOpen] = useState(false)

  // The Mate sheet (ADR 0093 voice phase): a quick channel over whatever
  // page is on screen, opened by the header's "Ask Mate" button (and later
  // by voice) rather than navigating away to the Assistant panel. Rendered
  // once here, not per-panel, so it keeps its own thread across opens/closes
  // the same way the panel's own conversation does. `mateSheetQuestion` is
  // cleared on close so reopening later with no question never re-sends a
  // stale one.
  const [mateSheetOpen, setMateSheetOpen] = useState(false)
  const [mateSheetQuestion, setMateSheetQuestion] = useState<string | undefined>(undefined)
  const [mateSheetNewConversation, setMateSheetNewConversation] = useState(false)
  // MateSheet is lazy-loaded and does not mount at all until the sheet is
  // opened for the first time - it fetches its conversation list and runs
  // every other effect only while `open`, so there is nothing for it to do
  // before then. This ref latches true the first time `mateSheetOpen` goes
  // true and never resets, so the component then stays mounted across later
  // closes - its thread, active conversation, and speech-output state
  // survive being closed and reopened the same way they always have. A ref
  // (mutated during render, not via a separate effect) rather than state:
  // this needs to be visible the same render `mateSheetOpen` first turns
  // true, not one render later.
  const mateSheetHasOpenedRef = useRef(false)
  if (mateSheetOpen) mateSheetHasOpenedRef.current = true
  // Which conversation the Mate PANEL should open (ADR 0094): set only by
  // the sheet's "Open the Mate page" button, which hands over whatever
  // thread was active there. Null means "whatever the panel already had",
  // not "start a fresh one" - the panel's own hook falls back to its usual
  // newest-thread behaviour when this is null.
  const [matePanelConversationId, setMatePanelConversationId] = useState<string | null>(initialLocation.conversationId ?? null)
  // ADR 0106 F1: the Documents panel's current folder, mirrored into the URL
  // (?folder=) the same way matePanelConversationId mirrors Mate's active
  // thread - see applyAppLocation and the URL sync effect below. Unlike
  // conversationId, initialLocation.documentId (which document to open in
  // the viewer, e.g. from a Mate attachment chip's link) is handed to the
  // panel once as an initial prop rather than tracked in App state at all:
  // nothing here needs to know which document is open, only which folder.
  const [documentsFolderId, setDocumentsFolderId] = useState<string | null>(initialLocation.documentFolderId ?? null)
  // Ditto latch pattern (mateSheetHasOpenedRef/manualSheetHasOpenedRef
  // above), but the opposite direction - tracking that the operator has
  // left Documents at least once, rather than that something has opened. A
  // Mate attachment chip's link should open its document once, for the
  // navigation it names, not every time the Documents panel remounts (it
  // unmounts completely on every panel switch - see the Suspense
  // key={activePanel} below). initialLocation.documentId itself never
  // changes for the whole session (initialLocation is captured once, at the
  // very first render), so without this the same document would reopen on
  // every return to Documents.
  //
  // Mutated directly off `activePanel` here (same as the sheets above),
  // NOT by a flag set only inside the 'documents' switch case below:
  // DocumentsPanel is lazy-loaded (`const DocumentsPanel = lazy(...)`), and
  // activePanelContent's switch is a plain per-render computation, not a
  // per-mount one - several renders of App can happen while the panel is
  // still suspended and before it actually commits. A flag flipped as soon
  // as the 'documents' case is first evaluated would already read
  // "consumed" by the render that follows, well before DocumentsPanel ever
  // received the id it was meant to be handed. Tying the latch to the
  // actual panel-switch signal instead means it only ever flips on a real
  // departure from Documents, independent of how many times React
  // re-renders while the operator stays on it.
  const documentsLeftOnceRef = useRef(false)
  if (activePanel !== 'documents') documentsLeftOnceRef.current = true
  // The sheet's own active conversation (mate-answer-toast plan): mirrors
  // matePanelConversationId above, but for the sheet rather than the panel -
  // MateSheet reports it the same way AssistantDrawer already reports
  // matePanelConversationId, via onActiveConversationChange below. Needed so
  // the answer watcher can tell a conversation the sheet is showing right
  // now apart from one it merely knows about.
  const [mateSheetConversationId, setMateSheetConversationId] = useState<string | null>(null)
  const openMate = useCallback((question?: string, options?: { newConversation?: boolean }) => {
    setMateSheetQuestion(question)
    setMateSheetNewConversation(Boolean(options?.newConversation))
    setMateSheetOpen(true)
  }, [])
  const mateScreen = useMemo(
    () => screenContextFor({ panel: activePanel, section: settingsSection }, activePage?.name ?? null),
    [activePanel, settingsSection, activePage],
  )

  // mate-answer-toast plan: which conversation(s), if any, are actually on
  // screen right now - the Mate panel's active thread only counts while the
  // panel itself is the active one, and likewise the sheet's only while it's
  // open. Both can in principle be different conversations at once (the
  // sheet is an overlay over whatever panel is behind it), so this is a set
  // of up to two ids, not a single one.
  const viewedMateConversationIds = useMemo(() => {
    const ids = new Set<string>()
    if (activePanel === 'assistant' && matePanelConversationId !== null) ids.add(matePanelConversationId)
    if (mateSheetOpen && mateSheetConversationId !== null) ids.add(mateSheetConversationId)
    return ids
  }, [activePanel, matePanelConversationId, mateSheetOpen, mateSheetConversationId])

  // The "Open" action on a Mate-answer toast (mate-answer-toast plan): the
  // same navigation the sheet's own "Open the Mate page" button already
  // does (onOpenPanel below) - hand the panel this conversation and
  // navigate to it, through requestNavigate so a dirty Settings page still
  // gets to veto it exactly as any other navigation would.
  const openMateConversationFromToast = useCallback((conversationId: string) => {
    setMatePanelConversationId(conversationId)
    requestNavigate('assistant', () => setActivePanel('assistant'))
  }, [requestNavigate])

  // Mounted unconditionally (rules of hooks) but a no-op on the kiosk route
  // (`enabled: !isKiosk`) - see the hook's own doc comment. Never on the
  // kiosk path: no Mate UI is reachable there at all (isKiosk's early
  // return below is well before MateSheet/the Mate panel), so nothing
  // could ever be watched from that tab regardless, but this keeps that
  // explicit rather than incidental.
  useMateAnswerWatcher(viewedMateConversationIds, openMateConversationFromToast, !isKiosk)

  // The in-app manual (ADR 0095): a right-hand sheet, rendered once here
  // beside the Mate sheet, opened by the header's contextual `?`, the
  // sidebar's Manual item, or Settings' own Manual button - each hands
  // openManual a ManualTarget (or null for the contents page).
  const [manualOpen, setManualOpen] = useState(false)
  const [manualTarget, setManualTarget] = useState<ManualTarget | null>(null)
  // Same lazy-mount-on-first-open latch as mateSheetHasOpenedRef above:
  // ManualSheet fetches nothing before it has ever been opened (see
  // use-manual.ts), so there is nothing lost by not mounting it until then,
  // and its back-stack history then survives later closes.
  const manualSheetHasOpenedRef = useRef(false)
  if (manualOpen) manualSheetHasOpenedRef.current = true
  const openManual = useCallback((target: ManualTarget | null) => {
    setManualTarget(target)
    setManualOpen(true)
  }, [])

  // App-wide voice (ADR 0093 voice phase, "App-wide voice"): mounted once
  // here, not in the Mate panel/sheet, so push-to-talk - and, once the
  // switch is on, "Hey Mate" - work from any page. `prime` only unlocks
  // speechSynthesis from push-to-talk's own tap (a user gesture); the actual
  // speaking of a reply happens in MateSheet, which owns its own
  // useSpeechOutput instance. Both voiceInput and wakeWord are anded with
  // `!isKiosk` here rather than in the hook itself - the wall display has no
  // microphone and isn't a control surface, and this is the one place that
  // already knows which shell is rendering.
  const mateSpeechOutput = useSpeechOutput()
  const mateVoice = useMateVoice({
    voiceInput: assistantVoiceConfig.voiceInput && !isKiosk,
    wakeWord: assistantVoiceConfig.wakeWord && !isKiosk,
    readAloud: assistantVoiceConfig.readAloud,
    canWrite,
    prime: mateSpeechOutput.prime,
    onQuestion: openMate,
  })
  const { pushToTalk: mateVoicePushToTalk, cancel: mateVoiceCancel, listening: mateVoiceListening, error: mateVoiceError } = mateVoice
  // Drives the header mic's small dot and its title while wake mode is
  // actually running - mirrors the same condition useMateVoice itself gates
  // wake mode on, so the dot never claims to be listening when it isn't.
  const mateWakeActive = assistantVoiceConfig.wakeWord && assistantVoiceConfig.voiceInput && mateVoice.supported && canWrite && !isKiosk

  // A recognition error (blocked mic, no speech, offline) surfaces once as a
  // toast rather than a persistent banner - voice is a convenience on top of
  // typing into Mate, not a primary control path that needs to stay visible.
  useEffect(() => {
    if (mateVoiceError) toast.error(mateVoiceError)
  }, [mateVoiceError])

  // Alt+M push-to-talk from anywhere in the shell, and Escape to cancel
  // while listening - both ignored while typing into a field, so they don't
  // fight ordinary text entry (a settings field, the Mate composer itself).
  useEffect(() => {
    if (!assistantVoiceConfig.voiceInput || isKiosk) return

    const handleKeyDown = (event: KeyboardEvent) => {
      const target = event.target as HTMLElement | null
      const isEditable = target !== null
        && (target.tagName === 'INPUT' || target.tagName === 'TEXTAREA' || target.isContentEditable)

      if (event.altKey && event.code === 'KeyM' && !isEditable) {
        event.preventDefault()
        mateVoicePushToTalk()
        return
      }
      if (event.key === 'Escape' && mateVoiceListening) {
        mateVoiceCancel()
      }
    }

    window.addEventListener('keydown', handleKeyDown)
    return () => window.removeEventListener('keydown', handleKeyDown)
  }, [assistantVoiceConfig.voiceInput, isKiosk, mateVoicePushToTalk, mateVoiceCancel, mateVoiceListening])

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
    if (loc.panel === 'assistant') {
      setMatePanelConversationId(loc.conversationId ?? null)
    }
    if (loc.panel === 'documents') {
      setDocumentsFolderId(loc.documentFolderId ?? null)
    }
  }, [pages, pagesLoading, setActivePageId])

  // The single writer of window.location (ADR 0074). Chosen over pushing at
  // each of the ~10 existing setActivePanel call sites (sidebar, page
  // sub-items, breadcrumb, tile onOpen, alarm banner, anchor-watch
  // auto-close) because it needs no call-site churn and covers programmatic
  // changes too (e.g. the active page disappearing out from under a user).
  useEffect(() => {
    if (!shellVisible) return
    // /kiosk owns its own query string (rotate, a pinned page) rather than
    // app state, and never navigates anywhere else - writing to history here
    // would fight the device's fixed URL for no benefit to anyone looking at
    // a screen with no back button.
    if (isKiosk) return
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
    const next = formatAppLocation({
      panel: activePanel,
      pageId: activePageId,
      section: settingsSection,
      conversationId: activePanel === 'assistant' ? matePanelConversationId : null,
      documentFolderId: activePanel === 'documents' ? documentsFolderId : null,
    }, ctx)
    // documents is the one panel whose canonical URL can carry a query
    // string (?folder=) - pathname alone is never enough to tell it apart
    // from a bare /documents, so this compares against pathname+search
    // (harmless everywhere else: no other panel/location ever has one).
    const path = window.location.pathname + window.location.search
    if (next === path) return // popstate, or a clean deep link, already put us here

    // Normalise (replace) rather than add a history entry for a path this
    // effect didn't itself write — that only ever happens on first load, or
    // when the page list/admin role resolve to something that makes the
    // current bar non-canonical.
    const replace = first || firstPageChanged || !isCanonicalAppPath(path, { firstPageId: ctx.firstPageId, knownPageIds: ctx.knownPageIds, canAdmin })
    window.history[replace ? 'replaceState' : 'pushState'](null, '', next)
  }, [shellVisible, isKiosk, activePanel, activePageId, settingsSection, matePanelConversationId, documentsFolderId, pages, pagesLoading, canAdmin])

  // Handles Back/Forward. Goes through requestNavigate so a dirty Settings
  // page still gets to veto the navigation exactly as a sidebar click
  // would — window.location has already moved to the previous entry by the
  // time this fires, so without the re-push below, a guarded Back would
  // leave the bar on the destination while the guard dialog (and Settings
  // itself) stay on screen.
  useEffect(() => {
    const handlePopState = () => {
      if (!shellVisible) return
      // /kiosk never pushes or replaces history (see the sync effect above),
      // so there is nothing here for it to react to; a bare `return` also
      // means the device's own back/forward gestures, if it has any, don't
      // fight the fixed URL it was launched with.
      if (isKiosk) return
      // Documents (?folder=) is the one location whose canonical form needs
      // the query string too - see the sync effect above's own comment.
      const path = window.location.pathname + window.location.search
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
  }, [shellVisible, isKiosk, requestNavigate, applyAppLocation, settingsSection, pages, pagesLoading, canAdmin])

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

  // Anchor watch auto-raise notifications (ADR 0099). The decision itself is
  // now server-side (backend/anchor_auto_raise.go) - this state just drives
  // the toast once a client's own poll of GET /api/anchor-watch notices a
  // new last_auto_raise. See the effect below useAnchorWatch, which is where
  // that value actually arrives.
  const [toastMessage, setToastMessage] = useState<string | null>(null)
  const toastRef = useRef<HTMLDivElement>(null)

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
  // Vessel id -> worst live collision state, for the anchor-watch map's AIS
  // markers (ADR 0088). Memoized so the map doesn't see a new Map identity
  // on every render that changes nothing about the alarm list.
  const aisCollisionAlarms = useMemo(() => collisionAlarmStatesByVessel(alarms), [alarms])
  // Lifted out of AlarmsDrawer so a rule saved there reaches the battery
  // tile's SoC bands (useSocBands below) without a reload.
  const {
    rules: alarmRules,
    loading: alarmRulesLoading,
    error: alarmRulesError,
    createRule: createAlarmRule,
    updateRule: updateAlarmRule,
    deleteRule: deleteAlarmRule,
  } = useAlarmRules()
  const { projection: overnight } = useOvernightProjection()
  const socBands = useSocBands(alarmRules, overnight?.socPath ?? DEFAULT_SOC_PATH)
  // Only for deciding whether to offer SignalK discovery. Gated on `loading`
  // below so an unconfigured-looking empty address during the initial fetch
  // can't trigger the prompt spuriously.
  const { settings: currentSettings, loading: currentSettingsLoading } = useSettingsForm()
  // The AIS Target Prioritizer plugin's webapp lives on the SignalK server
  // itself, so the collision alarm card's tuning link is only as good as
  // the configured address (ADR 0090); null when unconfigured, which
  // means no link renders.
  const collisionTuningHref = useMemo(
    () => buildCollisionTuningUrl(currentSettings.signalk?.address, currentSettings.signalk?.port),
    [currentSettings.signalk?.address, currentSettings.signalk?.port],
  )
  const { vessels: nearbyVessels, loading: nearbyVesselsLoading, lastUpdateAgeS: nearbyVesselsAgeS } = useNearbyVessels()
  const { targets: radarTargets, radars: radarInfos, source: radarSource, loading: radarTargetsLoading } = useRadarTargets()
  const {
    tanks,
    loading: tanksLoading,
    lastUpdateAgeS: tanksAgeS,
    fuelVolumeM3,
    fuelVolumeAgeS,
    fuelTimeToEmptyS,
    fuelRangeM,
    fuelDerivedAgeS,
  } = useTanksState()
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
  // Both poll on the forecast cadence, not the /api/vessel-state SSE stream's
  // own cadence: the backend caches weather for 900s and tide predictions
  // move on the order of hours, so FORECAST_REFRESH_SECONDS (600s) is
  // already well inside both — see config/app-config.ts.
  const { weather } = useWeatherToday(FORECAST_REFRESH_SECONDS)
  const { tide } = useTideToday(FORECAST_REFRESH_SECONDS)
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
  // The anchor-watch record's own poll cadence is chosen inside the hook
  // itself (config/app-config.ts's ANCHOR_WATCH_ACTIVE/IDLE_REFRESH_SECONDS),
  // since it depends on whether a watch is currently active — not on this
  // component's unrelated vessel-state refresh setting.
  const anchorWatch = useAnchorWatch(latitude, longitude, gnssCriticalAlert)
  // Surfaces the server's auto-raise decision (ADR 0099, anchor_auto_raise.go)
  // as the same toast the old browser-side auto-close used to fire directly.
  // lastSeenAutoRaiseAtRef starts uninitialized (undefined) so the first
  // observation this session — which may already carry a last_auto_raise
  // from before this page loaded — only establishes a baseline rather than
  // toasting for a raise that happened who-knows-how-long ago. Every open
  // browser runs this same effect independently off its own poll of GET
  // /api/anchor-watch (useAnchorWatch), which is what makes this "once per
  // event per client" without any of them having decided the raise itself.
  const lastSeenAutoRaiseAtRef = useRef<string | null | undefined>(undefined)
  useEffect(() => {
    const at = anchorWatch.lastAutoRaise?.at ?? null
    if (lastSeenAutoRaiseAtRef.current === undefined) {
      lastSeenAutoRaiseAtRef.current = at
      return
    }
    if (at !== null && at !== lastSeenAutoRaiseAtRef.current) {
      lastSeenAutoRaiseAtRef.current = at
      setToastMessage('Anchor watch raised automatically: engines running, under way outside the zone')
    }
  }, [anchorWatch.lastAutoRaise])
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
  // Item B: only the anchor-watch tile/drawer and the Nearby/POI map tile
  // read getSelfTrail/getAisTrails (AnchorWatchTile, AnchorWatchDrawer and
  // PoiMapTile below all take them as props) — every other page, including
  // most of the kiosk rotation, has nowhere for a trail to go. Gate the poll
  // on whichever of those is actually on screen, rather than running it
  // app-wide regardless.
  const activePageHasTrailConsumerWidget = effectiveWidgets.some(
    (w) => w.id === 'anchor-watch' || isPoiMapWidgetId(w.id),
  )
  const trailsEnabled = activePageHasTrailConsumerWidget || activePanel === 'anchor-watch'
  const { getSelfTrail, getAisTrails } = useServerTrails(5000, trailsEnabled)
  // A reverse geocode of a slowly changing position, cached server-side per
  // ~550m grid cell — see PLACE_NAME_REFRESH_SECONDS (config/app-config.ts).
  const placeName = usePlaceName(latitude, longitude, PLACE_NAME_REFRESH_SECONDS)
  // The clock wall-display tile's next-waypoint line (ADR 0092): the same
  // pieces any other consumer of routeActivationStatus already has in scope,
  // just combined once here rather than inside the tile itself, which has no
  // reason to know about routes or route activation at all.
  const clockNextWaypoint = useMemo(() => {
    if (!routeActivationStatus || routeActivationStatus.state !== 'active' || routeActivationStatus.routeId === null) return null
    const route = routes.find((r) => r.id === routeActivationStatus.routeId)
    if (!route) return null
    const waypoint = nextWaypoint(route, routeActivationStatus)
    if (!waypoint || latitude === null || longitude === null) return null
    const eta = etaToWaypoint(latitude, longitude, waypoint.waypoint, speedOverGroundKts, route.planning_speed_kts, new Date())
    return { label: waypoint.label, etaAt: eta.etaAt, basis: eta.basis }
  }, [routeActivationStatus, routes, latitude, longitude, speedOverGroundKts])
  const depthTrend = useDepthTrend('3h', 60)
  // Item B: only the czone-switches widget reads this; poll it only while
  // the active page (or the kiosk's current page) actually has one placed.
  const activePageHasCZoneWidget = effectiveWidgets.some((w) => w.id === 'czone-switches')
  const { switches: czoneSwitches, loading: czoneLoading, pending: czonePending, error: czoneError, toggleSwitch: toggleCZone } = useCZoneSwitches(5, activePageHasCZoneWidget)
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

  // Drives activePageId exactly the way a sidebar click or a deep link
  // does (ADR 0089), so activePage/effectiveWidgets/dashboardGrid/renderWidget
  // all keep working unchanged whether the page came from a click or from
  // the rotation timer. Called unconditionally (rules of hooks); `enabled`
  // is what actually turns it off outside kiosk mode.
  const { feedEmpty: kioskFeedEmpty } = useKioskRotation({
    enabled: isKiosk,
    pages,
    navigationState,
    pinnedPageId: kioskOptions.pageId,
    refetch: refetchPages,
    onShow: setActivePageId,
  })

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

  const handleLayoutSettle = useCallback((next: DashboardLayoutItem[]) => {
    if (!activePage) return
    void updatePage(activePage.id, { widgets: next })
  }, [activePage, updatePage])

  const handleRemoveWidget = useCallback((id: DashboardWidgetId) => {
    if (!activePage) return
    void updatePage(activePage.id, { widgets: effectiveWidgets.filter((w) => w.id !== id) })
  }, [activePage, effectiveWidgets, updatePage])

  // Each built-in widget's footprint comes from DASHBOARD_WIDGET_DEFAULT_SIZE
  // (ADR 0107) instead of one hard-coded 4x6 for all of them — that was what
  // let Battery & Power land cut off before an operator ever touched a
  // resize handle.
  const handleAddWidget = useCallback((id: BuiltinWidgetId) => {
    if (!activePage) return
    const maxY = effectiveWidgets.reduce((max, w) => Math.max(max, w.y + w.h), 0)
    const { w, h } = DASHBOARD_WIDGET_DEFAULT_SIZE[id]
    void updatePage(activePage.id, { widgets: [...effectiveWidgets, { id, x: 0, y: maxY, w, h }] })
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

  // Starts at the same footprint as the built-in Alternator tile so a pair
  // can sit side-by-side without a resize pass.
  const handleAddGaugeGroup = useCallback(() => {
    const maxY = effectiveWidgets.reduce((max, w) => Math.max(max, w.y + w.h), 0)
    setGaugeGroupDraft({
      id: newGaugeGroupWidgetId(effectiveWidgets),
      x: 0,
      y: maxY,
      w: 4,
      h: 7,
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
   * Duplicate button inside the gauge group dialog. This is Save As, not
   * Save And Copy: change the values, then click Duplicate instead of Save,
   * and the edits land on a new tile while the original is left exactly as
   * it was when the dialog opened. Same for a brand-new draft that has
   * never been saved: Duplicate produces the one tile carrying the edits,
   * not a saved draft plus a separate copy.
   *
   * gaugeGroupDraft is always set here, since the dialog that calls this
   * only renders when it is, so there is no stand-in widget to build for
   * the case where it isn't.
   */
  const handleDuplicateGaugeGroup = useCallback((gaugeGroup: GaugeGroupWidgetConfig) => {
    if (!activePage || !gaugeGroupDraft) return
    const edited = { ...gaugeGroupDraft, gaugeGroup }
    const copy = duplicateWidget(edited, effectiveWidgets)
    if (!copy) return

    // Same placement a fresh widget gets elsewhere in this file: below
    // everything else, so the copy never lands on top of its source.
    const maxY = effectiveWidgets.reduce((max, w) => Math.max(max, w.y + w.h), 0)
    const placedCopy = { ...copy, x: 0, y: maxY }

    void updatePage(activePage.id, { widgets: [...effectiveWidgets, placedCopy] })
    setGaugeGroupDraft(null)
  }, [activePage, effectiveWidgets, gaugeGroupDraft, updatePage])

  /**
   * An engine profile lands as an ordinary gauge group (ADR 0053) — already
   * configured, and saved straight away rather than held as a draft, because
   * unlike a blank tile it is valid the moment it is built.
   */
  const handleApplyEngineProfile = useCallback((title: string, gauges: GaugeWidgetConfig[], _suffixes: string[], hero?: number) => {
    if (!activePage) return
    const maxY = effectiveWidgets.reduce((max, w) => Math.max(max, w.y + w.h), 0)
    void updatePage(activePage.id, {
      widgets: [...effectiveWidgets, {
        id: newGaugeGroupWidgetId(effectiveWidgets),
        x: 0, y: maxY, w: 4, h: 7,
        gaugeGroup: { title, gauges, ...(hero === undefined ? {} : { hero }) },
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

  // The ribbon (ADR 0082) reuses LampStripConfigDialog under a synthetic
  // { id: 'ribbon' } rather than a real page widget — there is no draft state
  // to hold, since it is vessel-level and already lives in `ribbon` itself.
  const handleSaveRibbon = useCallback((lamps: LampStripWidgetConfig) => {
    void saveRibbon(lamps)
    setRibbonDialogOpen(false)
  }, [saveRibbon])

  const handleRemoveRibbon = useCallback(() => {
    void saveRibbon(null)
    setRibbonDialogOpen(false)
  }, [saveRibbon])

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
    else if (isPoiMapWidgetId(placed.id)) setPoiMapDraft(placed)
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

  // Wide by default (w:12, h:7): the split layout needs the room, and a
  // narrower map-only tile is a resize away rather than the starting point.
  const handleAddPoiMap = useCallback(() => {
    const maxY = effectiveWidgets.reduce((max, w) => Math.max(max, w.y + w.h), 0)
    setPoiMapDraft({
      id: newPoiMapWidgetId(effectiveWidgets),
      x: 0, y: maxY, w: 12, h: 7,
      poiMap: { title: '', rangeNm: 5, categories: [...POI_CATEGORY_IDS], layout: 'split', showAis: true, showTrail: false },
    })
  }, [effectiveWidgets])

  const handleSavePoiMap = useCallback((id: DashboardWidgetId, poiMap: PoiMapWidgetConfig) => {
    if (!activePage) return
    if (effectiveWidgets.some((w) => w.id === id)) {
      void updatePage(activePage.id, {
        widgets: effectiveWidgets.map((w) => (w.id === id ? { ...w, poiMap } : w)),
      })
      return
    }
    if (poiMapDraft?.id === id) {
      void updatePage(activePage.id, { widgets: [...effectiveWidgets, { ...poiMapDraft, poiMap }] })
    }
  }, [activePage, effectiveWidgets, poiMapDraft, updatePage])

  // Item D: every SSE tick re-renders App, and renderWidget below runs fresh
  // on every one of those renders (it is deliberately not memoized itself —
  // see its own comment), so an inline `onConfigure={() => setXDraft(widget)}`
  // handed a memoized tile (EngineClusterTile, LampStripTile, GaugeGroupTile,
  // GaugeTile) a new function identity every second even when nothing about
  // that widget changed, defeating the tile's own React.memo. One stable
  // handler per widget id, cached here and reused across renders, fixes that
  // without changing any tile's onConfigure signature. effectiveWidgetsRef
  // mirrors the current widget list (latest-ref idiom, matching
  // use-telemetry-stream.ts's useTelemetryEvent) so a handler built once
  // still resolves to the current widget when it's eventually called.
  const effectiveWidgetsRef = useRef(effectiveWidgets)
  useEffect(() => { effectiveWidgetsRef.current = effectiveWidgets }, [effectiveWidgets])
  // globalThis.Map, not the lucide-react `Map` icon this file imports above.
  const configureHandlersRef = useRef(new globalThis.Map<DashboardWidgetId, () => void>())
  const configureHandlerFor = useCallback((id: DashboardWidgetId, apply: (widget: DashboardLayoutItem) => void): () => void => {
    let handler = configureHandlersRef.current.get(id)
    if (!handler) {
      handler = () => {
        const widget = effectiveWidgetsRef.current.find((w) => w.id === id)
        if (widget) apply(widget)
      }
      configureHandlersRef.current.set(id, handler)
    }
    return handler
  }, [])

  // Same reasoning as configureHandlerFor above, for the tiles/banner whose
  // onOpen just navigates — these take no widget-specific argument, so a
  // single stable callback per destination covers every call site.
  const openForecastPanel = useCallback(() => setActivePanel('forecast'), [])
  const openRoutesPanel = useCallback(() => setActivePanel('routes'), [])
  const openAlarmsPanel = useCallback(() => requestNavigate('alarms', () => setActivePanel('alarms')), [requestNavigate])
  const openAnchorWatchPanel = useCallback(() => setActivePanel('anchor-watch'), [])
  const openRibbonDialog = useCallback(() => setRibbonDialogOpen(true), [])

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
          ages={gaugeAges}
          editing={layoutEditing}
          onConfigure={configureHandlerFor(id, setClusterDraft)}
        />
      )
    }

    if (isLampStripWidgetId(id)) {
      if (!widget.lamps) return null
      return (
        <LampStripTile
          config={widget.lamps}
          values={gaugeValues}
          ages={gaugeAges}
          worstAlarmState={worstAlarmState}
          editing={layoutEditing}
          onConfigure={configureHandlerFor(id, setLampStripDraft)}
          onOpenAlarms={openAlarmsPanel}
        />
      )
    }

    if (isGaugeGroupWidgetId(id)) {
      if (!widget.gaugeGroup) return null
      return (
        <GaugeGroupTile
          config={widget.gaugeGroup}
          values={gaugeValues}
          ages={gaugeAges}
          editing={layoutEditing}
          onConfigure={configureHandlerFor(id, setGaugeGroupDraft)}
        />
      )
    }

    if (isGaugeWidgetId(id)) {
      if (!widget.gauge) return null
      return (
        <GaugeTile
          config={widget.gauge}
          value={gaugeValues[widget.gauge.path] ?? null}
          ages={gaugeAges}
          editing={layoutEditing}
          onConfigure={configureHandlerFor(id, setGaugeDraft)}
        />
      )
    }

    if (isEmbedWidgetId(id)) {
      return (
        <EmbedTile
          config={widget.embed}
          editing={layoutEditing}
          onConfigure={configureHandlerFor(id, setEmbedDraft)}
          isDarkTheme={isDarkTheme}
        />
      )
    }

    if (isPoiMapWidgetId(id)) {
      if (!widget.poiMap) return null
      return (
        <PoiMapTile
          config={widget.poiMap}
          editing={layoutEditing}
          onConfigure={configureHandlerFor(id, setPoiMapDraft)}
          latitude={latitude}
          longitude={longitude}
          headingTrue={headingTrue}
          gnssCriticalAlert={gnssCriticalAlert}
          positionLastUpdateAgeS={positionLastUpdateAgeS}
          nearbyVessels={nearbyVessels}
          aisCollisionAlarms={aisCollisionAlarms}
          getSelfTrail={getSelfTrail}
          isDarkTheme={isDarkTheme}
          forceDark={activePage?.skin === 'instrument'}
          distanceUnits={uiConfig.distanceUnits}
          interactive={!isKiosk}
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
            onOpen={layoutEditing ? undefined : openForecastPanel}
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
            onOpen={layoutEditing ? undefined : openForecastPanel}
          />
        )
      case 'clock':
        return (
          <ClockTile
            sunriseTime={forecast[0]?.sunriseTime ?? null}
            sunsetTime={forecast[0]?.sunsetTime ?? null}
            moonPhase={forecast[0]?.moonPhase ?? null}
            placeName={placeName}
            nextWaypoint={clockNextWaypoint}
          />
        )
      case 'current-conditions':
        return (
          <CurrentConditionsTile
            depth={depth}
            depthLastUpdateAgeS={depthLastUpdateAgeS}
            windSpeedApparentKts={windSpeedApparentKts}
            maxGustKts={maxGustKts}
            weather={weather}
            forecast={forecast}
            distanceUnits={uiConfig.distanceUnits}
          />
        )
      case 'forecast-days':
        return <ForecastDaysTile days={forecast} units={uiConfig.distanceUnits} />
      case 'sea-state':
        return (
          <SeaStateTile
            forecast={forecast}
            waveForecastDays={waveForecastDays}
            waveLoading={waveForecastLoading}
            waveError={waveForecastError}
            units={uiConfig.distanceUnits}
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
            aisCollisionAlarms={aisCollisionAlarms}
            radarTargets={radarTargets}
            radars={radarInfos}
            radarSource={radarSource}
            isDarkTheme={isDarkTheme}
            showImageryLayer={showAnchorImagery}
            onImageryToggle={setShowAnchorImagery}
            showRadarEcho={showRadarEcho}
            onRadarEchoToggle={setShowRadarEcho}
            onFullscreen={openAnchorWatchPanel}
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
            interactive={!isKiosk}
          />
        )
      case 'tanks':
        return (
          <TanksTile
            tanks={tanks}
            loading={tanksLoading}
            lastUpdateAgeS={tanksAgeS}
            fuelVolumeM3={fuelVolumeM3}
            fuelVolumeAgeS={fuelVolumeAgeS}
            fuelTimeToEmptyS={fuelTimeToEmptyS}
            fuelRangeM={fuelRangeM}
            fuelDerivedAgeS={fuelDerivedAgeS}
          />
        )
      case 'route':
        return (
          <RouteTile
            speedKts={speedOverGroundKts ?? 0}
            routes={routes}
            dashboardRouteId={dashboardRouteId}
            onOpen={openRoutesPanel}
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
            socBands={socBands}
            overnight={overnight}
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

  // The Add Tile menu's multi-instance entries (ADR 0107): each opens the
  // same config dialog/draft flow it always has — App.tsx still owns every
  // one of those handlers — the picker just offers them grouped alongside
  // the built-in widgets instead of listed separately underneath them.
  const addWidgetMultiInstanceEntries: AddTileMultiInstanceEntry[] = [
    { label: 'Gauge…', category: 'custom', onSelect: handleAddGauge },
    { label: 'Engine Cluster…', category: 'engine', onSelect: handleAddCluster },
    { label: 'From equipment profile…', category: 'engine', onSelect: () => setEngineProfileOpen(true) },
    { label: 'Indicators…', category: 'custom', onSelect: handleAddLampStrip },
    { label: 'Gauge Group…', category: 'custom', onSelect: handleAddGaugeGroup },
    { label: 'Embed…', category: 'custom', onSelect: handleAddEmbed },
    { label: 'Nearby map…', category: 'navigation', onSelect: handleAddPoiMap },
  ]

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
      {/* The layout toolbar (ADR 0107): page name field, Add Tile, Ribbon,
          Skin, Hero, Kiosk, in that fixed order. Replaces both the old
          "Layout Mode — Drag to rearrange" pill that used to sit here and
          the separate control row that used to sit below the grid. */}
      {layoutEditing && (
        <LayoutToolbar
          page={activePage}
          namingPageId={namingPageId}
          onSaveName={(id, name) => updatePage(id, { name }).then((saved) => saved !== null)}
          onDoneNaming={() => setNamingPageId(null)}
          placedWidgetIds={effectiveWidgets.map((w) => w.id)}
          onAddWidget={handleAddWidget}
          multiInstanceEntries={addWidgetMultiInstanceEntries}
          onOpenRibbon={() => setRibbonDialogOpen(true)}
          onSetSkin={(id, skin) => { void updatePage(id, { skin }) }}
          onSetHero={(id, hero) => { void updatePage(id, { hero }) }}
          onKioskPatch={(id, patch) => { void updatePage(id, patch) }}
          onDeletePage={(id) => {
            void deletePage(id).then((ok) => {
              if (ok && id === activePageId) {
                setActivePageId(pages.find((p) => p.id !== id)?.id ?? null)
              }
            })
          }}
          pageCount={pages.length}
          canWrite={canWrite}
        />
      )}

      {/* The pinned indicator ribbon (ADR 0082): one vessel-level lamp strip
          above the grid on every dashboard page and every width, inside the
          page's own skin rather than among the app-theme banners — the same
          slot ADR 0072 gave the hero row. Not part of effectiveWidgets, so it
          never enters react-grid-layout's managed array.

          Never rendered at /kiosk (ADR 0089): on a 1920x360 strip it ran
          about a third of the height and pushed a seven-row page below the
          fold. The wall display gets nothing here for free; a page that
          wants lamps on the wall adds its own lamp-strip widget, sized and
          placed like any other tile, same as it would for any other
          page-specific status row. */}
      {!isKiosk && ribbon && (
        <div data-testid="dashboard-ribbon" className="w-full min-w-0">
          <LampStripTile
            config={ribbon}
            values={gaugeValues}
            ages={gaugeAges}
            worstAlarmState={worstAlarmState}
            editing={layoutEditing}
            onConfigure={openRibbonDialog}
            onOpenAlarms={openAlarmsPanel}
          />
        </div>
      )}

      {/* An empty page shows a prompt instead of an unexplained blank stretch
          (ADR 0107) — never at /kiosk, where there's nothing to click and no
          operator watching to click it. A page carrying a hero always shows
          the grid: the hero row itself is content, even when it's the only
          widget on the page. Gated on !pagesLoading too: before the initial
          GET /api/dashboard-pages resolves there is no active page yet
          either, which reads the same as "empty" — without this the prompt
          flashed on every load, not just on a genuinely empty page. */}
      {!pagesLoading && effectiveWidgets.length === 0 && !activePage?.hero ? (
        <EmptyPagePrompt editing={layoutEditing} canEditLayout={canEditLayout} onOpenManual={openManual} isKiosk={isKiosk} />
      ) : (
        // relative so KioskFoldGuide (ADR 0089) can position itself against
        // exactly the content the kiosk route shows: the grid alone. The
        // ribbon sits outside this container (above) precisely because it no
        // longer counts against the fold budget — it never reaches the wall
        // at all.
        <div className="relative">
          <DashboardBentoGrid
            widgets={effectiveWidgets}
            editing={layoutEditing}
            heroId={activePage?.hero}
            renderWidget={renderWidget}
            onRemoveWidget={handleRemoveWidget}
            onDuplicateWidget={handleDuplicateWidget}
            onLayoutSettle={handleLayoutSettle}
            // A wall page's height is fixed by the panel, not by a scrolling
            // viewport, so its rows sit closer together. Taken from the page's
            // own kiosk flag rather than from the route, so the helm browser
            // authoring the page lays it out at the same geometry the wall will
            // render it at and the fold guide stays honest.
            rowMargin={activePage?.kiosk ? WALL_ROW_MARGIN : undefined}
          />

          {/* Authoring aid, not a kiosk feature: only shown while editing a
              page that is itself flagged for the wall display, so laying it
              out on the ordinary desktop dashboard shows exactly where the
              360px strip cuts off before saving. */}
          {layoutEditing && activePage?.kiosk && <KioskFoldGuide topPx={KIOSK_FOLD_PX} />}
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
        onDuplicate={handleDuplicateGaugeGroup}
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

      <LampStripConfigDialog
        widget={ribbonDialogOpen ? { id: 'ribbon', lamps: ribbon ?? undefined } : null}
        onCancel={() => setRibbonDialogOpen(false)}
        onSave={handleSaveRibbon}
        onRemove={handleRemoveRibbon}
      />

      <EmbedConfigDialog
        widget={embedDraft}
        open={embedDraft !== null}
        onOpenChange={(open) => { if (!open) setEmbedDraft(null) }}
        onSave={handleSaveEmbed}
      />

      <PoiMapConfigDialog
        widget={poiMapDraft}
        open={poiMapDraft !== null}
        onOpenChange={(open) => { if (!open) setPoiMapDraft(null) }}
        onSave={handleSavePoiMap}
      />
    </div>
  )

  const renderPanelFallback = (label: string) => (
    <div className="flex h-full min-h-[240px] items-center justify-center text-sm text-muted-foreground">
      Loading {label}…
    </div>
  )

  // Labels for the single Suspense fallback wrapped around
  // activePanelContent below - kept as its own lookup, rather than folded
  // into the switch that builds the content itself, so the fallback text is
  // available synchronously (before the lazy panel it describes has
  // resolved) without duplicating each case's JSX.
  const panelFallbackLabel = (panel: PanelId): string => {
    switch (panel) {
      case 'forecast': return 'forecast'
      case 'alarms': return 'alarms'
      case 'routes': return 'routes'
      case 'charts': return 'charts'
      case 'radar': return 'radar'
      case 'assistant': return 'Mate'
      case 'settings': return 'settings'
      case 'anchor-watch': return 'anchor watch'
      case 'documents': return 'documents'
      default: return panel
    }
  }

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
        return (
          <AlarmsDrawer
            alarms={alarms}
            onAcknowledge={acknowledgeAlarm}
            onSilence={silenceAlarm}
            rules={alarmRules}
            loading={alarmRulesLoading}
            error={alarmRulesError}
            createRule={createAlarmRule}
            updateRule={updateAlarmRule}
            deleteRule={deleteAlarmRule}
            collisionTuningUrl={collisionTuningHref}
          />
        )
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
      case 'assistant':
        return (
          <AssistantDrawer
            canWrite={canWrite}
            onOpenSettings={() => requestNavigate('settings', () => {
              setSettingsSection('assistant')
              setActivePanel('settings')
            })}
            initialConversationId={matePanelConversationId}
            onActiveConversationChange={setMatePanelConversationId}
          />
        )
      case 'documents': {
        const documentDeepLinkId = documentsLeftOnceRef.current ? null : (initialLocation.documentId ?? null)
        return (
          <DocumentsPanel
            initialFolderId={documentsFolderId}
            onFolderChange={setDocumentsFolderId}
            initialDocumentId={documentDeepLinkId}
          />
        )
      }
      case 'settings':
        return (
          <SettingsPage
            ref={settingsPageRef}
            onDirtyChange={setSettingsDirty}
            activeSectionId={settingsSection}
            onSectionChange={setSettingsSection}
            onOpenManual={openManual}
            onAskMate={openMate}
          />
        )
      case 'anchor-watch':
        return (
          <>
            {/* vesselLat/vesselLon fall back from the live fix to the anchor
                point (e.g. GPS lost after the anchor was already set), and stay
                null only when neither is available: the drawer renders an
                explicit "No GPS fix" placeholder in the map slot for that case
                rather than being handed a fabricated 0,0. */}
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
              aisCollisionAlarms={aisCollisionAlarms}
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
          </>
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

  // The wall display (ADR 0089) reuses dashboardGrid directly rather than
  // the ordinary shell: no sidebar, no header, no SidebarProvider. toastRef
  // already null-checks everywhere it's read and no tile calls useSidebar,
  // so nothing downstream depends on SidebarProvider being mounted. This
  // also means the manual (ADR 0095) needs no separate kiosk gating - the
  // header `?`, the sidebar Manual item and <ManualSheet> itself are all
  // declared below this return and never reached on an unattended screen.
  if (isKiosk) {
    return (
      <KioskShell rotate={kioskOptions.rotate} height={kioskOptions.height} alarms={alarms}>
        {pagesError ? (
          <div className="flex h-full items-center justify-center text-sm text-muted-foreground">
            Could not load dashboard pages. Retrying…
          </div>
        ) : kioskFeedEmpty && !kioskOptions.pageId ? (
          <div className="flex h-full items-center justify-center text-sm text-muted-foreground">
            No pages are flagged for the wall display yet.
          </div>
        ) : (
          dashboardGrid
        )}
      </KioskShell>
    )
  }

  return (
    <SidebarProvider>
      <div
        ref={toastRef}
        role="status"
        aria-live="polite"
        className="anchor-watch-toast left-4 right-4 top-4 m-0 mx-auto max-w-md rounded-lg border border-amber-500/40 bg-amber-500/10 px-4 py-3 text-sm text-amber-600 shadow-lg md:left-auto md:right-4"
        popover="manual"
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
                      <span className="min-w-0 flex-1 truncate">{page.name}</span>
                      <KioskPageGlyph page={page} />
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
            {/* Opens in its own tab (ADR 0089): the wall display is a
                separate, unattended screen, not a place this operator's own
                session navigates to and back from. A plain top-level item,
                not a sub-item of Dashboard, so it survives collapsing the
                sidebar to its icon rail. */}
            <SidebarMenuItem>
              <SidebarMenuButton
                render={<a href="/kiosk" target="_blank" rel="noopener noreferrer" />}
                tooltip="Wall display"
              >
                <MonitorPlay />
                <span>Wall display</span>
              </SidebarMenuButton>
            </SidebarMenuItem>
            {/* ADR 0095: opens the contents page of the in-app manual. Never
                `isActive` (it's a sheet over whatever's on screen, not a
                panel of its own) and carries no PanelId or URL - ADR 0074
                keeps sheets out of the address bar. */}
            <SidebarMenuItem>
              <SidebarMenuButton tooltip="Manual" onClick={() => openManual(null)}>
                <BookOpen />
                <span>Manual</span>
              </SidebarMenuButton>
            </SidebarMenuItem>
          </SidebarMenu>
        </SidebarContent>
        <SidebarFooter>
          <SidebarVersion />
        </SidebarFooter>
        <SidebarRail />
      </Sidebar>

      {/* The shell is min-h-svh, so panels normally grow the page and the
          window scrolls. Mate's thread scrolls inside its own viewport
          instead (ADR 0105), which only works if something above it has a
          bounded height, so the Mate panel caps the inset at the viewport. */}
      <SidebarInset className={activePanel === 'assistant' ? 'h-svh' : undefined}>
        {/* `min-w-0` on both halves is load-bearing, not cosmetic: without it a flex
            item refuses to shrink below its content width and the right-hand cluster
            gets pushed off a phone screen (AGENTS.md — prevent viewport overflows).
            The breadcrumb is the designated slack absorber, so it truncates while the
            clock and controls keep their size. */}
        <header className="relative z-60 flex h-14 shrink-0 items-center gap-2 border-b px-2 sm:px-4 lg:h-16">
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
                    // Named in place, not behind a dialog (ADR 0107): the page
                    // exists immediately as "Untitled page", and namingPageId
                    // is what starts its name field empty and focused.
                    void createPage('Untitled page', []).then((p) => {
                      if (p) {
                        setActivePageId(p.id)
                        setNamingPageId(p.id)
                        setLayoutEditing(true)
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
            {/* ADR 0095: the contextual manual - lands on the current
                screen's page (and heading, for the three dashboard
                sub-panels that share features/dashboard). `title` is how
                neighbouring header buttons carry a tooltip, same as this
                one's neighbours below. */}
            <Button
              variant="ghost"
              size="icon"
              aria-label="Open the manual"
              title="Manual for this screen"
              onClick={() => openManual(manualTargetFor({ panel: activePanel, section: settingsSection }))}
            >
              <CircleHelp className="h-4 w-4" />
            </Button>
            {/* ADR 0093 voice phase: opens the Mate sheet over whatever page is
                behind it, without navigating away - the shell-wide "push to
                talk" entry point the plan calls for, though this button is
                the tap-only affordance; voice itself lands in a later phase. */}
            <Button variant="ghost" size="icon" aria-label="Ask Mate" onClick={() => openMate()}>
              <Sparkles className="h-4 w-4" />
            </Button>
            {/* ADR 0093 voice phase, "App-wide voice": push-to-talk from the
                header, on every panel and dashboard page - Settings → Mate →
                "Voice input" gates it, and it's hidden for a read-only
                session the same way write controls are elsewhere. */}
            {assistantVoiceConfig.voiceInput && canWrite && (
              <div className="flex items-center gap-2">
                <Button
                  variant="ghost"
                  size="icon"
                  aria-label="Talk to Mate"
                  aria-pressed={mateVoiceListening}
                  disabled={!mateVoice.supported}
                  title={
                    !mateVoice.supported
                      ? (mateVoice.unsupportedReason === 'insecure-context'
                        ? 'Voice input needs the app opened over https'
                        : 'This browser has no speech recognition.')
                      : mateWakeActive ? 'Listening for Hey Mate' : undefined
                  }
                  className={cn('relative', mateVoiceListening && 'text-primary')}
                  onClick={mateVoicePushToTalk}
                >
                  {mateVoice.supported ? <Mic className="h-4 w-4" /> : <MicOff className="h-4 w-4" />}
                  {mateWakeActive && (
                    <span className="absolute right-1 top-1 h-1.5 w-1.5 rounded-full bg-primary" aria-hidden="true" />
                  )}
                </Button>
                {mateVoiceListening && (
                  <span className="text-[11px] text-muted-foreground">
                    {mateVoice.armed ? 'Mate is listening…' : mateVoice.interim !== '' ? mateVoice.interim : 'Listening…'}
                  </span>
                )}
              </div>
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
            <ConnectionBanner />
            {/* z-55 keeps a live alarm above the Mate sheet's backdrop (z-50)
                while staying under the dashboard header, which sits at
                z-60 same as the page selector and every other top-level
                header control. (impeccable critique 2026-09-12, P1)

                Popovers and dropdown menus are z-60 too (components/ui/
                popover.tsx, components/ui/dropdown-menu.tsx), so they draw
                above this banner. That includes one opened inside the Mate
                sheet: PopoverContent portals out of the sheet's own
                stacking context rather than nesting inside it, so its z-60
                is compared against the banner directly and clears it
                regardless of the sheet (ADR 0107). The sheet's own panel
                (components/ui/sheet.tsx) is z-70, above the banner and
                ordinary popovers/menus alike — only its backdrop is z-50 —
                and dialogs (components/ui/dialog.tsx, alert-dialog.tsx) sit
                at z-70/z-80, that same layer again or higher. Tooltips
                (components/ui/tooltip.tsx) sit at z-90, the topmost layer
                above all dialogs, because a tooltip can be anchored to a
                trigger inside any of them and must remain readable. */}
            <div className="relative z-55" data-testid="alarm-banner-stack">
              <AlarmBanner alarms={alarms} onOpen={openAlarmsPanel} />
            </div>

            <div className="min-h-0 flex-1">
              {activePanel === null ? (
                dashboardGrid
              ) : (
                <div className="h-full min-h-0 overflow-y-auto rounded-lg border bg-card p-4">
                  {/* One boundary for every panel, not eight. `key={activePanel}`
                      forces a fresh Suspense instance on every panel switch, so
                      it never keeps a previous panel's boundary state - each
                      panel always gets its own fallback while its own chunk
                      loads, not whatever state the boundary was last left in. */}
                  <Suspense key={activePanel} fallback={renderPanelFallback(panelFallbackLabel(activePanel))}>
                    {activePanelContent}
                  </Suspense>
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

      {/* Lazy-loaded, and not mounted at all until first opened - see
          mateSheetHasOpenedRef above. `fallback={null}` is fine here: the
          sheet itself is a Sheet primitive that renders nothing (no overlay,
          no panel) until `open` is true, so there is nothing that should be
          showing while its chunk loads on this very first open. */}
      {mateSheetHasOpenedRef.current && (
        <Suspense fallback={null}>
          <MateSheet
            open={mateSheetOpen}
            onOpenChange={(open) => {
              setMateSheetOpen(open)
              if (!open) {
                setMateSheetQuestion(undefined)
                setMateSheetNewConversation(false)
              }
            }}
            initialQuestion={mateSheetQuestion}
            newConversation={mateSheetNewConversation}
            screen={mateScreen}
            canWrite={canWrite}
            readAloud={assistantVoiceConfig.readAloud}
            onOpenPanel={(id) => {
              setMatePanelConversationId(id)
              setMateSheetOpen(false)
              requestNavigate('assistant', () => setActivePanel('assistant'))
            }}
            onActiveConversationChange={setMateSheetConversationId}
          />
        </Suspense>
      )}

      {/* Same reasoning as the Mate sheet above - see manualSheetHasOpenedRef. */}
      {manualSheetHasOpenedRef.current && (
        <Suspense fallback={null}>
          <ManualSheet
            open={manualOpen}
            onOpenChange={setManualOpen}
            target={manualTarget}
            onAskMate={(question) => {
              setManualOpen(false)
              openMate(question)
            }}
          />
        </Suspense>
      )}

      <Toaster isDarkTheme={isDarkTheme} />
    </SidebarProvider>
  )
}
