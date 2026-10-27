import {
  Anchor,
  BellRing,
  BookOpen,
  CircleHelp,
  CloudSun,
  FileText,
  LayoutDashboard,
  Maximize,
  Mic,
  MicOff,
  Minimize,
  Radar as RadarIcon,
  Settings,
  Ship,
  Sparkles,
  MonitorPlay,
  Wrench,
} from 'lucide-react'
import { Suspense, lazy, useCallback, useEffect, useMemo, useRef, useState, useSyncExternalStore, type ReactNode } from 'react'
import { toast } from 'sonner'

import {
  LiveAlarmBanner,
  LiveAlarmsDrawer,
  LiveAlternatorTile,
  LiveAnchorWatchDrawer,
  LiveAnchorWatchTile,
  LiveAutopilotTile,
  LiveClockTile,
  LiveCurrentConditionsTile,
  LiveDepthTideTile,
  LiveDisplayShell,
  LiveForecastPage,
  LiveGeneratorTile,
  LiveNearbyVesselsTile,
  LivePoiMapTile,
  LivePositionTile,
  LiveRadarDrawer,
  LiveRouteTile,
  LiveSignalKDiscoveryPrompt,
  LiveWindTile,
} from '@/components/live-surfaces'
import { BatteryPowerTile } from '@/components/battery-power-tile'
import { HotWaterTile } from '@/components/hot-water-tile'
import { TodayNowTile } from '@/components/today-now-tile'
import { ForecastConditionsTile } from '@/components/forecast-conditions-tile'
import { MarineHeader } from '@/components/marine-header'
import { VesselStatusBar } from '@/components/vessel-status-bar'
import { SetupLine } from '@/components/setup-line'
import { requestSetupFocus } from '@/lib/setup-status'
import { RadarTargetsTile } from '@/components/radar-targets-tile'
// Not lazy: prefetchNoteEditor() is a plain function that only reaches into
// the editor-vendor chunk through a dynamic import() of its own (see that
// file's doc comment) — importing the function itself here costs nothing.
import { prefetchNoteEditor } from '@/components/note-editor'
import { SETTINGS_SECTIONS, type SettingsSectionId } from '@/components/settings/settings-nav'
import { INVENTORY_SECTIONS } from '@/components/inventory/inventory-nav'

const AssistantDrawer = lazy(() => import('@/components/assistant-drawer').then((mod) => ({ default: mod.AssistantDrawer })))
const DocumentsRoutePanel = lazy(() => import('@/components/documents-route-panel').then((mod) => ({ default: mod.DocumentsRoutePanel })))
const PassagesRoutePanel = lazy(() => import('@/components/passages/passages-route-panel').then((mod) => ({ default: mod.PassagesRoutePanel })))
const InventoryRoutePanel = lazy(() => import('@/components/inventory/inventory-route-panel').then((mod) => ({ default: mod.InventoryRoutePanel })))
const SettingsRoutePanel = lazy(() => import('@/components/settings/settings-route-panel').then((mod) => ({ default: mod.SettingsRoutePanel })))
// MateSheet and HelpSheet (unlike the panels above) fetch nothing and run
// no effects until they've actually been opened - see the `hasOpened`
// latches below, next to where each is rendered, for why that makes them
// safe to lazy-load and mount only on first open rather than always up
// front. NoteCaptureSheet (ADR 0119) used to live here too, as a global
// sheet the header action and Alt+N both opened; ADR 0121 moved it into
// documents-panel.tsx, which now owns it entirely - a note is created only
// from Documents' own New → Note menu.
const MateSheet = lazy(() => import('@/components/mate-sheet').then((mod) => ({ default: mod.MateSheet })))
const HelpSheet = lazy(() => import('@/components/help-sheet').then((mod) => ({ default: mod.HelpSheet })))
// ADR 0142: the CRUD pattern library's dev-only tour, reachable only at
// /patterns and only when import.meta.env.DEV (see patternsGalleryRequested
// below) - lazy like every panel above so its fixtures never reach the
// entry chunk, and additionally never even requested in a production build
// since the render path that names it is dead code once DEV is false.
const PatternsGallery = lazy(() => import('@/components/patterns/gallery').then((mod) => ({ default: mod.PatternsGallery })))
import { NavigationGuardDialog } from '@/components/navigation-guard-dialog'
import { CZoneSwitchesTile } from '@/components/czone-switches-tile'
import { SolarTile } from '@/components/solar-tile'
import { TanksTile } from '@/components/tanks-tile'
import { CLUSTER_DEFAULT_SIZE, DashboardBentoGrid, raiseGaugeHeight } from '@/components/dashboard-bento-grid'
import { LayoutModeToggle } from '@/components/layout-mode-toggle'
import { LayoutToolbar } from '@/components/layout-toolbar'
import { EmptyPagePrompt } from '@/components/empty-page-prompt'
import type { AddTileMultiInstanceEntry } from '@/components/add-tile-picker'
import { Toaster } from '@/components/ui/sonner'
import { useDashboardPages, type DashboardPage, type CreatePageInit } from '@/hooks/use-dashboard-pages'
import { useDashboardRibbon } from '@/hooks/use-dashboard-ribbon'
import { useActiveDashboardPageId } from '@/hooks/use-active-dashboard-page'
import { useDisplayRotation, type UseDisplayRotationResult } from '@/hooks/use-display-rotation'
import { useDisplayRemote } from '@/hooks/use-display-remote'
import { useDisplays } from '@/hooks/use-displays'
import { LoadError, RefreshFailedOverlay } from '@/components/patterns/refresh-failed-note'
import { NO_DISPLAY_OPTIONS, displayFoldPx, displayRowMargin, DEFAULT_DWELL_SECONDS, DISPLAY_RECOVERY_POLL_MS } from '@/lib/displays'
import { nextWaypoint, traversalOrder } from '@/lib/next-waypoint'
import { DisplayFoldGuide } from '@/components/display-fold-guide'
import { DisplayRemoteToast } from '@/components/display-remote-toast'
import { WallDisplaysPanel } from '@/components/wall-displays-panel'
import { DisplayEditorPanel } from '@/components/display-editor-panel'
import type { DisplayPatch } from '@/components/page-display-select'
import { DashboardPageSwitcher } from '@/components/dashboard-page-switcher'
import { useRouteActivation } from '@/hooks/use-route-activation'
import { useElectricalState } from '@/hooks/use-electrical-state'
import { useSolarState } from '@/hooks/use-solar-state'
import { useNearbyVessels } from '@/hooks/use-nearby-vessels'
import { useRadarTargets } from '@/hooks/use-radar-targets'
import { useAnchorWatch } from '@/hooks/use-anchor-watch'
import { useAnchorPlacemarks } from '@/hooks/use-anchor-placemarks'
import { ConnectionBanner } from '@/components/connection-banner'
import { useTanksState } from '@/hooks/use-tanks-state'
import { useTideToday } from '@/hooks/use-tide-today'
import { tideHeightFtOrNull } from '@/lib/rode-plan'
import { findActiveWindBulletin, useForecastWarnings } from '@/hooks/use-forecast-warnings'
import { getVesselStateSnapshot, useRetainVesselState, useVesselStateSelector, type VesselStateValue } from '@/hooks/use-vessel-state'
import { useRetainAlarms } from '@/hooks/use-alarms'
import { useAlarmRules } from '@/hooks/use-alarm-rules'
import { useSocBands } from '@/hooks/use-soc-bands'
import { useOvernightProjection } from '@/hooks/use-overnight-projection'
import { useSettingsForm } from '@/hooks/use-settings-form'
import { collisionTuningUrl as buildCollisionTuningUrl } from '@/lib/collision-tuning'
import { useAnchorTrail } from '@/hooks/use-anchor-trail'
import { useServerTrails } from '@/hooks/use-server-trails'
import { useWeatherForecast } from '@/hooks/use-weather-forecast'
import { buildForecastDrawerProps } from '@/lib/forecast-drawer-props'
import { useWaveForecast } from '@/hooks/use-wave-forecast'
import { useUpperAir } from '@/hooks/use-upper-air'
import { useWeatherToday } from '@/hooks/use-weather-today'
import { useAuth } from '@/hooks/use-auth'
import { useAutopilot } from '@/hooks/use-autopilot'
import { useCZoneSwitches } from '@/hooks/use-czone-switches'
import { useDepthTrend } from '@/hooks/use-depth-trend'
import { useDarkMode } from '@/hooks/use-dark-mode'
import { useVesselTimeZone } from '@/hooks/use-vessel-identity'
import { FORECAST_REFRESH_SECONDS, fallbackAssistantVoiceConfig } from '@/config/app-config'
import { useAppConfig } from '@/hooks/use-app-config'
import { useMateVoice } from '@/hooks/use-mate-voice'
import { useMateAnswerWatcher } from '@/hooks/use-mate-answer-watcher'
import { useSpeechOutput } from '@/hooks/use-speech-output'
import { BREAKPOINTS, useMinWidth } from '@/lib/breakpoints'
import { useFullscreen } from '@/hooks/use-fullscreen'
import { runningFromHomeScreen, servedOverHttps, useScreenWakeLock } from '@/hooks/use-screen-wake-lock'
import { useSwipePaging } from '@/hooks/use-swipe-paging'
import { useVersionReload } from '@/hooks/use-version-reload'
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
import { PoiMapConfigDialog } from '@/components/poi-map-config-dialog'
import { GaugeConfigDialog } from '@/components/gauge-config-dialog'
import { GaugeTile } from '@/components/gauge-tile'
import { GaugeGroupConfigDialog } from '@/components/gauge-group-config-dialog'
import { EngineClusterConfigDialog } from '@/components/engine-cluster-config-dialog'
import { EngineClusterHost, GaugeGroupHost, LampStripHost } from '@/components/equipment-tile-hosts'
import { useEquipmentTileContext } from '@/hooks/use-equipment-tile-context'
import { LampStripConfigDialog } from '@/components/lamp-strip-config-dialog'
import { useRetainGaugeValues } from '@/hooks/use-gauge-values'
import { LoginScreen } from '@/components/login-screen'
import { Button } from '@/components/ui/button'
import { Separator } from '@/components/ui/separator'
import {
  Breadcrumb,
  BreadcrumbItem,
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
import { LazyBoundary } from '@/components/lazy-boundary'
import { UpdatedToast } from '@/components/updated-toast'
import { ReloadConfirmDialog, UpdateNotices } from '@/components/update-notices'
import { DisplayUpdatedNotice } from '@/components/display-updated-notice'
import { inventorySectionOf, settingsSectionOf, type AppLocation, type PanelId } from '@/lib/app-location'
import { browserHistory, createAppRouter } from '@/lib/app-router'
import { AppRouterContext, useAppLocation, useAppRouter, useNavigate } from '@/hooks/use-app-location'
import { useHasNavigationGuard } from '@/hooks/use-navigation-guard'
import { screenContextFor } from '@/lib/mate-screen'
import { helpTargetFor, type HelpTarget } from '@/lib/help-links'
// Its own module, not the patterns barrel: the barrel would drag TanStack
// Table into the entry chunk for the sake of one empty div.
import { SaveBarSlot } from '@/components/patterns/save-bar-slot'
import { cn } from '@/lib/utils'

/**
 * Ordered the way the boat is actually run, not the order the panels were
 * built in: what you want at a glance underway first (Alarms, Anchor Watch,
 * Forecast, Radar), then the reference material you go and look
 * something up in (Documents, Inventory, Mate), then the rows you touch once
 * and leave alone (Wall displays, Settings). ADR 0123 put Inventory right
 * after Documents rather than after Passages - the equipment registry links
 * documents on every read, so the two panels sit next to each other in the
 * sidebar the way they already do in the code. Maintenance belongs after
 * Inventory once that panel exists. Dashboard is pinned above this list and
 * Help below it, both rendered separately in the sidebar.
 *
 * Revision "one panel, not three" (2026-09-20): Notes and Manuals no longer
 * have rows here. ADR 0116 already decided a note is a document and a
 * manual is a folder; this revision finishes that thought by having
 * Documents render both, rather than splitting the same underlying table
 * back into three sidebar destinations. Capture (what the Notes row used
 * to be the entry point for) is now the global header action just right of
 * this sidebar, reachable from every screen rather than only this one -
 * see ADR 0119.
 */
const PANEL_NAV_ITEMS: Array<{ id: Exclude<PanelId, 'display'>; label: string; icon: typeof CloudSun }> = [
  { id: 'alarms', label: 'Alarms', icon: BellRing },
  { id: 'anchor-watch', label: 'Anchor Watch', icon: Anchor },
  { id: 'forecast', label: 'Forecast', icon: CloudSun },
  { id: 'radar', label: 'Radar', icon: RadarIcon },
  { id: 'passages', label: 'Passages', icon: Ship },
  { id: 'documents', label: 'Documents', icon: FileText },
  { id: 'inventory', label: 'Inventory', icon: Wrench },
  { id: 'assistant', label: 'Mate', icon: Sparkles },
  { id: 'wall-displays', label: 'Wall displays', icon: MonitorPlay },
  { id: 'settings', label: 'Settings', icon: Settings },
]

// The header breadcrumb's second crumb, for the two panels that have their
// own in-page SectionNav (Settings, Inventory) - every other panel is a
// single crumb (see the breadcrumb JSX below). Panels are not children of
// Dashboard, so the root crumb is always the panel's own name, never
// "Dashboard"; this only ever supplies the section beneath it.
function activePanelSectionLabel(location: AppLocation): string | null {
  if (location.panel === 'settings') {
    const section = settingsSectionOf(location.route)
    return SETTINGS_SECTIONS.find((item) => item.id === section)?.label ?? null
  }
  if (location.panel === 'inventory') {
    const section = inventorySectionOf(location.route)
    return INVENTORY_SECTIONS.find((item) => item.id === section)?.label ?? null
  }
  return null
}

// The header's "settings for this page" gear: which panels have a matching
// Settings section, and which one. Only these four have anywhere for the
// gear to point - Dashboard, Settings itself and every other panel have no
// entry here, so the header hides the button rather than showing one that
// goes nowhere useful.
const PANEL_SETTINGS_SECTIONS: Partial<Record<PanelId, SettingsSectionId>> = {
  alarms: 'alarms',
  'anchor-watch': 'anchor-watch',
  assistant: 'assistant',
  radar: 'mayara',
}

const ANCHOR_IMAGERY_ENABLED_KEY = 'anchorWatch.imagery.enabled'
const ANCHOR_RADAR_ECHO_ENABLED_KEY = 'anchorWatch.radarEcho.enabled'

// ADR 0124: the max wait requestIdleCallback (or its setTimeout fallback)
// is given before firing the note editor's own chunk prefetch regardless of
// how busy the browser stays - generous, since nothing on screen is waiting
// on this the way embed-tile.tsx's own mount deferral is.
const NOTE_EDITOR_PREFETCH_IDLE_TIMEOUT_MS = 2000

// How long the full screen swipe-paging toast (the page name + "n / total"
// pill) stays up after a swipe changes the page, before it clears itself.
const SWIPE_PAGE_TOAST_MS = 1500

/**
 * ADR 0110: `/` (and every other collapse-to-first-page case — an unknown
 * deep-linked page id, a page that just got deleted) has to land on the
 * first ordinary dashboard page, never a wall page. A wall page sitting at
 * position 0 used to mean `/` opened it directly, defeating the entire
 * point of moving wall pages out of the Dashboard list. Pure, so
 * app-location.ts's isCanonicalAppPath check (fed the same value as
 * `firstPageId`) and every call site here agree on exactly one page.
 */
function firstDashboardPageId(pages: readonly DashboardPage[]): string | null {
  return pages.find((p) => !p.display_id)?.id ?? null
}

interface DisplayRemoteControllerProps {
  rotation: Pick<UseDisplayRotationResult, 'next' | 'previous' | 'pause' | 'resume' | 'paused'>
  onAction: () => void
}

/**
 * Mounted only while the wall route is actually showing a resolved display -
 * as one of DisplayShell's children below, never as a top-level App() hook
 * call. That's deliberate, not incidental: use-display-remote.ts's global
 * keydown listener has no `enabled` flag of its own (its own doc comment:
 * arrow keys/Space/Enter do nothing on a wall page, so an always-on listener
 * costs nothing THERE), and that assumption only holds while this component
 * is actually mounted. Calling the hook unconditionally from App() itself
 * would intercept Enter/Space/arrows everywhere else too — Space while
 * naming a display "Saloon TV", Enter inside any other dialog — which is
 * exactly what mounting/unmounting this small wrapper with the route avoids.
 */
function DisplayRemoteController({ rotation, onAction }: DisplayRemoteControllerProps) {
  useDisplayRemote({
    next: () => { rotation.next(); onAction() },
    previous: () => { rotation.previous(); onAction() },
    togglePause: () => {
      if (rotation.paused) rotation.resume()
      else rotation.pause()
      onAction()
    },
    resume: () => { rotation.resume(); onAction() },
  })
  return null
}

// ADR 0171: one router per mounted App, reading the address the app was
// loaded with. It is created here, not at module scope, so each mount (and
// each test) starts from the address it finds.
export function App() {
  const [router] = useState(() => createAppRouter(browserHistory()))
  useEffect(() => router.start(), [router])
  return (
    <AppRouterContext.Provider value={router}>
      <AppShell />
    </AppRouterContext.Provider>
  )
}

const selectNavigationState = (state: VesselStateValue) => state.navigationState

function AppShell() {
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
  // ADR 0171: the router owns what the app is showing. `appLocation` is the one
  // value for it (panel, and that panel's view), `navigate` the one way to
  // change it. `initialLocation` is what the app was loaded with, which
  // still carries a `?document=` deep link the live location does not.
  const router = useAppRouter()
  const appLocation = useAppLocation()
  const navigate = useNavigate()
  const initialLocation = router.initialLocation
  const activePanel: PanelId | null = appLocation.panel
  // ADR 0142: /patterns is deliberately NOT a PanelId - it never needs
  // the router's canonical-path or navigation-guard machinery, so it reads
  // the raw pathname directly rather than growing AppLocation a shape none
  // of that machinery would ever act on. import.meta.env.DEV is statically
  // replaced at build time, so this is `false` (and the PatternsGallery branch below dead code) in
  // a production build - see that branch's own comment for what this
  // guards, beyond the lazy-loading gallery.tsx already gets on its own.
  const [patternsGalleryRequested] = useState(() =>
    import.meta.env.DEV && (globalThis.location?.pathname ?? '').replace(/\/+$/, '') === '/patterns')
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
  const fullscreen = useFullscreen()
  // Chrome only hides for the dashboard grid itself, not a panel - if the
  // operator reaches a panel while fullscreen (an alarm banner tap, say),
  // the sidebar and header have to come back so it's reachable. Fullscreen
  // itself is never auto-exited on navigation; the operator leaves it with
  // Esc or the floating exit button below.
  const isFullscreenDashboard = fullscreen.isFullscreen && activePanel === null
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
  const [clusterDraft, setClusterDraft] = useState<DashboardLayoutItem | null>(null)
  // Same pattern as embedDraft: a freshly added Nearby map (ADR 0091 phase
  // 3b) exists only here until it holds a valid config.
  const [poiMapDraft, setPoiMapDraft] = useState<DashboardLayoutItem | null>(null)
  // Gauges, engine clusters, gauge groups and lamp strips read their own
  // readings from the gauge-values store (ADR 0215). The shell holds the store
  // open, so a page switch never starts a tile blank, but listens to none of
  // it: a tick does not re-render the shell.
  useRetainGaugeValues()
  // The equipment items, profiles and reporting sources that tiles showing a
  // piece of equipment read their paths and zones from (ADR 0187).
  const equipmentContext = useEquipmentTileContext()
  // The Details page's document name, for the Mate sheet's screen context.
  const [documentDetailsTitle, setDocumentDetailsTitle] = useState<string | null>(null)
  // Unsent Mate composer text or staged attachments, one flag per mounted
  // composer (the /mate panel and the sheet over any page).
  const [matePanelHasDraft, setMatePanelHasDraft] = useState(false)
  const [mateSheetHasDraft, setMateSheetHasDraft] = useState(false)
  // A newer build than this page loaded with is checked for every five
  // minutes and on return to the page. An attended screen is only told: the
  // operator reloads from the toast or the sidebar line, and is asked first
  // when something is unsaved. The wall display and a Home Screen dashboard
  // have nobody to ask and reload by themselves (never over unsaved work).
  // Settings, Document Details, the Inventory panel, note drafts and the
  // maintenance log dialogs register a navigation guard while they hold
  // unsaved work, so "any guard" is "unsaved work"; Mate drafts are
  // deliberately unguarded (leaving a half-typed message is fine) and only
  // count here. The sidebar's unsaved dot: Settings holds a guard exactly
  // while it is dirty.
  const settingsDirty = useHasNavigationGuard('settings')
  const mateDraftRef = useRef(false)
  mateDraftRef.current = matePanelHasDraft || mateSheetHasDraft
  const hasUnsavedWork = useCallback(() => router.hasGuards() || mateDraftRef.current, [router])
  // Set below, once every overlay's state has been declared.
  const unattendedRef = useRef(false)
  const [reloadConfirmOpen, setReloadConfirmOpen] = useState(false)
  const versionWatch = useVersionReload({ hasUnsavedWork, unattended: () => unattendedRef.current })
  const requestReload = useCallback(() => {
    if (hasUnsavedWork()) setReloadConfirmOpen(true)
    else window.location.reload()
  }, [hasUnsavedWork])
  useEffect(() => {
    globalThis.localStorage?.setItem(ANCHOR_IMAGERY_ENABLED_KEY, String(showAnchorImagery))
  }, [showAnchorImagery])

  useEffect(() => {
    globalThis.localStorage?.setItem(ANCHOR_RADAR_ECHO_ENABLED_KEY, String(showRadarEcho))
  }, [showRadarEcho])

  // Hoisted ahead of useDarkMode (rather than left beside displayOptions
  // below, where it used to live) so the wall dark-theme override just below
  // has it in scope. isDisplay depends only on activePanel, which is already
  // set by this point, so nothing about moving it changes what it means.
  const isDisplay = activePanel === 'display'
  // Full screen at the helm means the operator is watching, so the screen
  // shouldn't sleep. A Home Screen app on iPad or iPhone has no full screen
  // but is already free of browser bars, so its dashboard holds the lock
  // instead; one that does have full screen (Android, desktop) waits for it.
  // A wall display keeps its own wake-lock setting in DisplayShell. HTTPS
  // only: the browser offers no wake lock elsewhere.
  const homeScreenDashboard = runningFromHomeScreen() && !fullscreen.supported && activePanel === null
  useScreenWakeLock((fullscreen.isFullscreen || homeScreenDashboard) && !isDisplay && servedOverHttps())
  // The forecast hook is called here, ahead of the theme, because Auto mode
  // reads the same sunrise and sunset the Clock tile shows.
  const weatherFeed = useWeatherForecast(FORECAST_REFRESH_SECONDS)
  const { forecast, nextHour: forecastNextHour } = weatherFeed
  const vesselTimeZone = useVesselTimeZone()
  const [storedIsDarkTheme, toggleDarkMode, themeState] = useDarkMode({
    sunriseTime: forecast[0]?.sunriseTime ?? null,
    sunsetTime: forecast[0]?.sunsetTime ?? null,
    timeZone: vesselTimeZone,
  })
  // The wall display always renders dark (operator decision, ADR 0089
  // phase 2, carried into ADR 0110), regardless of what this browser has
  // stored — a fresh profile otherwise defaults to light, which is how a
  // light basemap ended up inside dark instrument-skin tiles on the
  // 1920x360 strip. The override lives here, at the one place isDarkTheme is
  // established, so every consumer (the root `dark` class effect right
  // below, and every tile/map isDarkTheme prop threaded from this same
  // variable) agrees without special-casing any one of them. toggleDarkMode
  // is left untouched: it still reads and writes the real stored preference,
  // so leaving /display resumes whatever the operator last chose on this
  // browser rather than whatever the wall display happened to force.
  const isDarkTheme = isDisplay || storedIsDarkTheme
  useEffect(() => {
    document.documentElement.classList.toggle('dark', isDarkTheme)
  }, [isDarkTheme])

  // ADR 0124: warms the note editor's own chunk (Slate plus Plate — the
  // largest dependency graph this project puts behind a React.lazy()
  // boundary, ADR 0117's editor-vendor chunk) once the shell has painted
  // and the browser has spare cycles, so the first New → Note the operator
  // picks doesn't pay a cold fetch behind the capture sheet's own Suspense
  // fallback. requestIdleCallback, where it exists, so this never steals a
  // frame from first paint — the same fallback-to-setTimeout shape
  // embed-tile.tsx already uses for its own idle-scheduled mount, since the
  // wall kiosk's WPE WebKit (Safari 16-era) has neither API. isDisplay
  // gates it off entirely there: the kiosk route never opens a note editor
  // (isDisplay's own screens never mount NoteEditor), so fetching the chunk
  // would only cost that browser a startup download it will never use.
  // prefetchNoteEditor() is memoised, so re-running this on every isDisplay
  // flip (leaving /display resumes the ordinary shell) costs nothing beyond
  // the first real call.
  useEffect(() => {
    if (isDisplay) return
    if (typeof window.requestIdleCallback === 'function') {
      const id = window.requestIdleCallback(() => { prefetchNoteEditor() }, {
        timeout: NOTE_EDITOR_PREFETCH_IDLE_TIMEOUT_MS,
      })
      return () => window.cancelIdleCallback(id)
    }
    const id = window.setTimeout(() => { prefetchNoteEditor() }, NOTE_EDITOR_PREFETCH_IDLE_TIMEOUT_MS)
    return () => window.clearTimeout(id)
  }, [isDisplay])
  const { status: routeActivationStatus } = useRouteActivation()
  const { pages, loading: pagesLoading, error: pagesError, refetch: refetchPages, createPage, updatePage, deletePage, reorderPages, reordering } = useDashboardPages()
  // ADR 0110: the wall display records themselves. Fetched here (not inside
  // the wall branch further down) because the ordinary shell needs the list
  // too — the sidebar's display group, the layout toolbar's display picker
  // and duplicate-to-display popover, and the displays-management dialog all
  // render regardless of which route is active.
  const { displays, loading: displaysLoading, error: displaysError, refetch: refetchDisplays, createDisplay, updateDisplay, deleteDisplay } = useDisplays()
  const [activePageId, setActivePageId] = useActiveDashboardPageId(pages, !isDisplay)
  const activePage = pages.find((p) => p.id === activePageId) ?? null
  // ADR 0110: a page assigned to a display leaves the Dashboard sub-list
  // (and the header's DashboardPageSwitcher, and full screen swipe paging
  // just below) for the sidebar's own "Wall displays" group, nested under
  // the display it's actually on. Hoisted up here (rather than declared
  // beside its other two uses, right before the non-display return) because
  // the swipe hook below needs it and every hook in this component has to
  // run unconditionally, before any of the early returns further down
  // (auth gate, wall display).
  const dashboardSubListPages = pages.filter((p) => !p.display_id)
  const [swipePageToast, setSwipePageToast] = useState<{ name: string; index: number; total: number } | null>(null)
  const swipePageToastTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null)
  // Left/right switches to the next/previous page in the same sub-list and
  // order the header's DashboardPageSwitcher shows, going through
  // setActivePageId - the same path its own onSelect uses - so the URL and
  // history behave exactly as a header pick would. No wrap at either end.
  const goToDashboardPageOffset = (offset: 1 | -1) => {
    const list = dashboardSubListPages
    if (list.length === 0) return
    // A wall-display page opened from the sidebar is not in this list, so
    // there is no neighbour to move to.
    const currentIndex = list.findIndex((p) => p.id === activePageId)
    if (currentIndex === -1) return
    const nextIndex = currentIndex + offset
    if (nextIndex < 0 || nextIndex >= list.length) return
    const nextPage = list[nextIndex]
    setActivePageId(nextPage.id)
    if (swipePageToastTimerRef.current !== null) clearTimeout(swipePageToastTimerRef.current)
    setSwipePageToast({ name: nextPage.name, index: nextIndex + 1, total: list.length })
    swipePageToastTimerRef.current = setTimeout(() => {
      swipePageToastTimerRef.current = null
      setSwipePageToast(null)
    }, SWIPE_PAGE_TOAST_MS)
  }
  // Full screen only, and never while a layout is being edited - a swipe
  // there could just as easily be a drag meant for the grid itself.
  const swipeContainerRef = useSwipePaging<HTMLDivElement>({
    enabled: isFullscreenDashboard && !layoutEditing,
    onNext: () => goToDashboardPageOffset(1),
    onPrevious: () => goToDashboardPageOffset(-1),
  })
  // Drops the toast (and whatever timer is still counting it down) the
  // instant full screen ends, rather than leaving it to finish counting down
  // over whatever the operator navigated to instead.
  useEffect(() => {
    if (!isFullscreenDashboard) setSwipePageToast(null)
    return () => {
      if (swipePageToastTimerRef.current !== null) {
        clearTimeout(swipePageToastTimerRef.current)
        swipePageToastTimerRef.current = null
      }
    }
  }, [isFullscreenDashboard])
  // Hoisted ahead of the polling hooks below (item B) that gate themselves on
  // which widgets the active page (or the wall's current page, which drives
  // activePageId exactly the same way — see useDisplayRotation below) actually
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
  // ADR 0110 (superseding ADR 0089): the wall display at /display/<slug>.
  // Its query string is its own (just `?page=`, for authoring/screenshots)
  // rather than app state, so the location carries it parsed and the router
  // never writes it back. isDisplay itself is declared earlier, beside
  // useDarkMode, so the wall dark-theme override there can read it.
  const displayOptions = appLocation.panel === 'display' ? appLocation.options : NO_DISPLAY_OPTIONS
  // Read off the location, which for the wall is the one it was loaded with:
  // the wall never navigates. Deliberately no fallback to "the first display"
  // when the slug is missing or unknown - that would silently put one
  // display's geometry on another screen, the masking fallback AGENTS.md
  // forbids. The wall branch below renders an explicit diagnostic instead.
  const wallDisplaySlug = appLocation.panel === 'display' ? appLocation.slug : null
  const wallDisplay = isDisplay ? displays.find((d) => d.slug === wallDisplaySlug) ?? null : null
  // The pinned indicator ribbon (ADR 0082): one vessel-level lamp strip, not
  // tied to any page, so it lives beside the page hooks rather than inside
  // effectiveWidgets below.
  const { ribbon, saveRibbon } = useDashboardRibbon()
  const [ribbonDialogOpen, setRibbonDialogOpen] = useState(false)
  // The displays-management dialog (ADR 0110 §6): opened from the sidebar's
  // "Wall displays" group and from PageDisplaySelect's "no displays yet"
  // dead-end, not a route - same precedent as the ribbon dialog above
  // (ADR 0074: dialogs carry no URL).

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
  // Whether `mateSheetQuestion` came in by voice: only then does Mate write
  // an answer meant for speaking, and only then is it read aloud.
  const [mateSheetSpoken, setMateSheetSpoken] = useState(false)
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
  // thread was active there, and by the panel's own list/search selecting
  // one (assistant-drawer.tsx's onActiveConversationChange). Null means a
  // fresh empty chat (Mate UI cycle: "Mate opens on an empty chat") - the
  // panel's own hook no longer falls back to the newest existing thread when
  // this is null.
  const matePanelConversationId = appLocation.panel === 'assistant' ? appLocation.conversationId : null
  const openMatePanel = useCallback((conversationId: string | null) => {
    return navigate({ panel: 'assistant', conversationId })
  }, [navigate])
  // The panel reports its own active thread. That only means something while
  // the panel is the thing on screen: a report that lands as it unmounts must
  // not pull the operator back to Mate.
  const reportMatePanelConversation = useCallback((conversationId: string | null) => {
    if (router.getSnapshot().location.panel === 'assistant') navigate({ panel: 'assistant', conversationId })
  }, [router, navigate])
  // The Documents panel's `?document=` deep link (which document to open in
  // the viewer, e.g. from a Mate attachment chip's link) is handed to the
  // panel once as an initial prop rather than tracked in the location at
  // all: nothing here needs to know which document is open.
  // Ditto latch pattern (mateSheetHasOpenedRef/helpSheetHasOpenedRef
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
  //
  // Also trips while the Details page is open (ADR 0115 §2):
  // activePanel stays 'documents' the whole time an operator goes from the
  // listing to a document's Details page and back, so without this
  // Details-view check, DocumentsPanel unmounting for the Details page
  // and remounting on the way back (activePanelContent switches between two
  // different component types under the one Suspense boundary - see the
  // comment on that Suspense below) would still see the latch un-tripped
  // and hand the fresh mount initialLocation.documentId again, reopening a
  // Mate attachment chip's ?document= viewer the operator had already seen
  // and dismissed before ever visiting Details.
  const documentsLeftOnceRef = useRef(false)
  if (appLocation.panel !== 'documents' || appLocation.route.view === 'details') documentsLeftOnceRef.current = true
  // The sheet's own active conversation (mate-answer-toast plan): mirrors
  // matePanelConversationId above, but for the sheet rather than the panel -
  // MateSheet reports it the same way AssistantDrawer already reports
  // matePanelConversationId, via onActiveConversationChange below. Needed so
  // the answer watcher can tell a conversation the sheet is showing right
  // now apart from one it merely knows about.
  const [mateSheetConversationId, setMateSheetConversationId] = useState<string | null>(null)
  const openMate = useCallback((question?: string, options?: { newConversation?: boolean; spoken?: boolean }) => {
    setMateSheetQuestion(question)
    setMateSheetNewConversation(Boolean(options?.newConversation))
    setMateSheetSpoken(Boolean(options?.spoken))
    setMateSheetOpen(true)
  }, [])
  // Push-to-talk and "Hey Mate": the one way a question arrives by voice.
  const openMateBySpeech = useCallback((question: string) => {
    openMate(question, { spoken: true })
  }, [openMate])
  const mateScreen = useMemo(
    () => screenContextFor(appLocation, activePage?.name ?? null, documentDetailsTitle),
    [appLocation, activePage, documentDetailsTitle],
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
  // navigate to it, through the router so a dirty Settings page still gets to
  // veto it exactly as any other navigation would.
  const openMateConversationFromToast = useCallback((conversationId: string) => {
    openMatePanel(conversationId)
  }, [openMatePanel])

  // Jumps to a specific Settings section through the router, so a dirty
  // Details/Inventory page still gets to veto it exactly as any other
  // navigation would. One helper for every "take me to my settings" link:
  // AssistantDrawer's own settings button, DocumentsPanel's Mate-failure
  // line, and the header's per-panel settings gear below.
  const openSettingsSection = useCallback((section: SettingsSectionId) => {
    navigate({ panel: 'settings', route: { view: 'section', section } })
  }, [navigate])

  // Stable for DocumentsRoutePanel's memo: AppShell re-renders on every live delta.
  const openAssistantSettings = useCallback(() => openSettingsSection('assistant'), [openSettingsSection])

  // Opens Settings at a section and asks it to focus one control, so a tile or
  // the setup line lands on the exact dropdown rather than the section's top.
  const openSetupStep = useCallback((section: SettingsSectionId, control: string) => {
    requestSetupFocus(control)
    openSettingsSection(section)
  }, [openSettingsSection])

  // Mounted unconditionally (rules of hooks) but a no-op on the wall route
  // (`enabled: !isDisplay`) - see the hook's own doc comment. Never on the
  // wall path: no Mate UI is reachable there at all (isDisplay's early
  // return below is well before MateSheet/the Mate panel), so nothing
  // could ever be watched from that tab regardless, but this keeps that
  // explicit rather than incidental.
  useMateAnswerWatcher(viewedMateConversationIds, openMateConversationFromToast, !isDisplay)

  // The in-app help (ADR 0095): a right-hand sheet, rendered once here
  // beside the Mate sheet, opened by the header's contextual `?`, the
  // sidebar's Help item, or Settings' own Help button - each hands
  // openHelp a HelpTarget (or null for the contents page).
  const [helpOpen, setHelpOpen] = useState(false)
  const [helpTarget, setHelpTarget] = useState<HelpTarget | null>(null)
  // Same lazy-mount-on-first-open latch as mateSheetHasOpenedRef above:
  // HelpSheet fetches nothing before it has ever been opened (see
  // use-help.ts), so there is nothing lost by not mounting it until then,
  // and its back-stack history then survives later closes.
  const helpSheetHasOpenedRef = useRef(false)
  if (helpOpen) helpSheetHasOpenedRef.current = true

  // Who is at the screen. The wall display is never attended. A Home Screen
  // dashboard is unattended only while nothing is open over it: a sheet, a
  // tile's configuration dialog, layout editing or any other dialog on screen
  // means someone is using it. A dashboard in the browser's own full screen
  // is attended too (a reload would drop out of full screen), and so is a
  // Home Screen app on any panel other than the dashboard.
  const overlayOpen =
    mateSheetOpen || helpOpen || ribbonDialogOpen || layoutEditing || namingPageId !== null ||
    [embedDraft, gaugeDraft, gaugeGroupDraft, lampStripDraft, clusterDraft, poiMapDraft].some((d) => d !== null)
  const overlayOpenRef = useRef(false)
  overlayOpenRef.current = overlayOpen
  unattendedRef.current = isDisplay || (
    runningFromHomeScreen() && activePanel === null && !fullscreen.isFullscreen && !overlayOpenRef.current &&
    // Anything else that put a dialog or sheet up, in a tile or a panel.
    document.querySelector('[role="dialog"], [role="alertdialog"]') === null
  )
  // A reload held back for unsaved work or an open sheet goes ahead the
  // moment that clears, without waiting for the next health read.
  const guardCount = useSyncExternalStore(router.subscribe, () => router.getSnapshot().guardOwners.length)
  const unsavedNow = guardCount > 0 || matePanelHasDraft || mateSheetHasDraft
  const { retryHeldReload } = versionWatch
  useEffect(() => {
    if (!unsavedNow && !overlayOpen) retryHeldReload()
  }, [unsavedNow, overlayOpen, activePanel, fullscreen.isFullscreen, retryHeldReload])
  const openHelp = useCallback((target: HelpTarget | null) => {
    setHelpTarget(target)
    setHelpOpen(true)
  }, [])

  // App-wide voice (ADR 0093 voice phase, "App-wide voice"): mounted once
  // here, not in the Mate panel/sheet, so push-to-talk - and, once the
  // switch is on, "Hey Mate" - work from any page. `prime` only unlocks
  // speechSynthesis from push-to-talk's own tap (a user gesture); the actual
  // speaking of a reply happens in MateSheet, which owns its own
  // useSpeechOutput instance. Both voiceInput and wakeWord are anded with
  // `!isDisplay` here rather than in the hook itself - the wall display has
  // no microphone and isn't a control surface, and this is the one place
  // that already knows which shell is rendering.
  const mateSpeechOutput = useSpeechOutput()
  const mateVoice = useMateVoice({
    voiceInput: assistantVoiceConfig.voiceInput && !isDisplay,
    wakeWord: assistantVoiceConfig.wakeWord && !isDisplay,
    readAloud: assistantVoiceConfig.readAloud,
    canWrite,
    prime: mateSpeechOutput.prime,
    onQuestion: openMateBySpeech,
  })
  const { pushToTalk: mateVoicePushToTalk, cancel: mateVoiceCancel, listening: mateVoiceListening, error: mateVoiceError } = mateVoice
  // Drives the header mic's small dot and its title while wake mode is
  // actually running - mirrors the same condition useMateVoice itself gates
  // wake mode on, so the dot never claims to be listening when it isn't.
  const mateWakeActive = assistantVoiceConfig.wakeWord && assistantVoiceConfig.voiceInput && mateVoice.supported && canWrite && !isDisplay

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
    if (!assistantVoiceConfig.voiceInput || isDisplay) return

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
  }, [assistantVoiceConfig.voiceInput, isDisplay, mateVoicePushToTalk, mateVoiceCancel, mateVoiceListening])

  // Gates the router's address writes on the shell actually being shown
  // (mirrors the render gate further down): while auth is still resolving, or
  // a signalk install is waiting on sign-in, `canAdmin` reads permissive-false
  // rather than a real answer, and without this gate the router would rewrite
  // an admin's `/settings` deep link to `/` before they ever get to the login
  // form.
  const shellVisible = !auth.loading && auth.mode !== null && !(auth.mode === 'signalk' && auth.user === null)

  // Tells the router what it needs to write the address: which page is first,
  // which pages exist, and whether Settings is reachable. The router does the
  // rest (ADR 0171); App no longer mirrors the location into the bar itself.
  const firstPageId = firstDashboardPageId(pages)
  const knownPageIds = useMemo(() => (pagesLoading ? null : pages.map((p) => p.id)), [pages, pagesLoading])
  useEffect(() => {
    router.setContext({ firstPageId, knownPageIds, canAdmin, shellVisible })
  }, [router, firstPageId, knownPageIds, canAdmin, shellVisible])

  // If admin access ends (or was never established) while Settings happens
  // to be open, drop back to the dashboard. The router then sees `/settings`
  // as non-canonical (canAdmin false) and replaces it with `/`. Not a move
  // the operator asked for, so the guards do not get a say.
  useEffect(() => {
    if (shellVisible && !canAdmin && activePanel === 'settings') {
      navigate({ panel: null, pageId: null }, { force: true })
    }
  }, [shellVisible, canAdmin, activePanel, navigate])

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

  // The shell reads one field of vessel state itself: the navigation state,
  // which picks the wall's pages (useDisplayRotation below). Every tile and
  // page reads its own fields from the store (ADR 0215); the shell holds it
  // open so the last fix survives page switches and wall rotations.
  useRetainVesselState()
  const navigationState = useVesselStateSelector(selectNavigationState)
  // The shell draws no alarm itself. The banner, the Alarms page, the lamp
  // strips, the wall's pill and the AIS markers each read the alarms store
  // (ADR 0215); the shell only holds it open, so the list survives page
  // switches and wall rotations without an alarms tick re-rendering the shell.
  useRetainAlarms()
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
  // Only for deciding whether to offer SignalK discovery. Gated on `loading`
  // below so an unconfigured-looking empty address during the initial fetch
  // can't trigger the prompt spuriously.
  const { settings: currentSettings } = useSettingsForm()
  // The AIS Target Prioritizer plugin's webapp lives on the SignalK server
  // itself, so the collision alarm card's tuning link is only as good as
  // the configured address (ADR 0090); null when unconfigured, which
  // means no link renders.
  const collisionTuningHref = useMemo(
    () => buildCollisionTuningUrl(currentSettings.signalk?.address, currentSettings.signalk?.port),
    [currentSettings.signalk?.address, currentSettings.signalk?.port],
  )
  const { vessels: nearbyVessels, loading: nearbyVesselsLoading, lastUpdateAgeS: nearbyVesselsAgeS, maxRangeM: nearbyVesselsMaxRangeM } = useNearbyVessels()
  const { targets: radarTargets, radars: radarInfos, source: radarSource, loading: radarTargetsLoading } = useRadarTargets()
  const {
    tanks,
    loading: tanksLoading,
    error: tanksError,
    lastUpdateAgeS: tanksAgeS,
  } = useTanksState()
  const {
    lastUpdateAgeS: electricalLastUpdateAgeS,
    batterySocPercent,
    socPath,
    chargingCurrentA,
    chargingPowerW,
    solarOutputW,
    acOutputW,
    dc12vPowerW,
    voltageReadoutV,
    voltageReadoutLabel,
    generatorRealPowerW,
    generatorConfigured,
    generatorNone,
    houseBankConfigured,
    houseBankUnresolved,
    alternator0,
    alternator1,
    charger0CurrentA,
    charger0AcIn1CurrentA,
    charger0ChargingMode,
    charger0Error,
    batteryRatePercentPerHour,
    timeToGoHours,
  } = useElectricalState()
  // The bands follow the house bank's own path from the electrical state, so
  // they do not wait on the dawn projection (which needs a position fix).
  const socBands = useSocBands(alarmRules, socPath)
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
  const warningsFeed = useForecastWarnings(FORECAST_REFRESH_SECONDS)
  const { activeWarning: activeForecastWarning } = warningsFeed
  const wavesFeed = useWaveForecast(FORECAST_REFRESH_SECONDS)
  const {
    days: waveForecastDays,
    seaTemperatureF: waveSeaTemperatureF,
    loading: waveForecastLoading,
    error: waveForecastError,
  } = wavesFeed
  // Upper air keeps its own refresh cadence: the global models behind it run
  // four times a day, so polling it on the surface-forecast interval would
  // re-fetch the same numbers.
  const upperAirFeed = useUpperAir()
  // The anchor-watch record's own poll cadence is chosen inside the hook
  // itself (config/app-config.ts's ANCHOR_WATCH_ACTIVE/IDLE_REFRESH_SECONDS),
  // since it depends on whether a watch is currently active — not on this
  // component's unrelated vessel-state refresh setting.
  // The watch record: the shell reads whether a watch is set, the auto-raise
  // and the planning depth, none of which moves with the boat. The anchor
  // watch tile and page apply the live fix themselves (anchorWatchAt), so a
  // position tick does not re-render the shell.
  const anchorWatch = useAnchorWatch()
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
  // The Rode Planner's forecast wind band (frontend/src/lib/rode-plan.ts's
  // resolvePlanningWindBand), held here so it outlives the planner closing
  // and reopening. Deliberately not persisted: a band chosen for last night's
  // forecast must not silently drive tonight's recommendation. A reload
  // returns to the live seed.
  const [windBandId, setWindBandId] = useState<string | null>(null)
  // The planning depth is seeded from the depth at drop and editable from
  // there (ADR 0063). Hoisted here because the drawer and the Rode Planner
  // both plan off this number, and a component-local copy is how surfaces drifted apart. While anchored the
  // planning depth lives on the watch record (anchorWatch.planningDepthM, persisted server-side); this state only
  // covers the not-anchored "what-if" case, which has nowhere else to live
  // and is deliberately lost on reload. Cleared on every hasActiveAnchorWatch
  // transition below — without that, a pre-drop what-if would reappear once
  // the anchor comes up and is raised again.
  const [sessionPlanningDepth, setSessionPlanningDepth] = useState<{ depthM: number; tideHeightFt: number | null } | null>(null)
  // Item B: only the anchor-watch tile/drawer and the Nearby/POI map tile
  // read getSelfTrail/getAisTrails (AnchorWatchTile, AnchorWatchDrawer and
  // PoiMapTile below all take them as props) — every other page, including
  // most of the wall rotation, has nowhere for a trail to go. Gate the poll
  // on whichever of those is actually on screen, rather than running it
  // app-wide regardless.
  const activePageHasTrailConsumerWidget = effectiveWidgets.some(
    (w) => w.id === 'anchor-watch' || isPoiMapWidgetId(w.id),
  )
  const trailsEnabled = activePageHasTrailConsumerWidget || activePanel === 'anchor-watch'
  const { getSelfTrail, getAisTrails } = useServerTrails(5000, trailsEnabled)
  // The anchor swing trail has its own server-kept record (ADR 0199); only
  // the anchor watch tile and drawer draw it.
  const anchorTrailEnabled = effectiveWidgets.some((w) => w.id === 'anchor-watch') || activePanel === 'anchor-watch'
  const { getAnchorTrail } = useAnchorTrail(5000, anchorTrailEnabled)
  // The Nearby map's route layer: the route SignalK is navigating, combined
  // once here so the poi-map tile never has to know route activation exists -
  // it only draws whatever waypoints and next-index it's handed. null
  // whenever no route is active or it has no waypoints. `waypoints` is
  // traversalOrder's array, not necessarily the authored order, since
  // nextIndex is an index into that traversal order (see next-waypoint.ts's
  // own comment on why a reversed activation makes the two differ).
  const activeRoute = useMemo(() => {
    if (routeActivationStatus.state !== 'active') return null
    const waypoint = nextWaypoint(routeActivationStatus)
    const traversal = traversalOrder(routeActivationStatus)
    if (!waypoint || !traversal) return null
    return { name: routeActivationStatus.route.name.trim() !== '' ? routeActivationStatus.route.name : 'Active route', waypoints: traversal, nextIndex: waypoint.index }
  }, [routeActivationStatus])
  const depthTrend = useDepthTrend('3h', 60)
  // Item B: only the czone-switches widget reads this; poll it only while
  // the active page (or the wall's current page) actually has one placed.
  const activePageHasCZoneWidget = effectiveWidgets.some((w) => w.id === 'czone-switches')
  const { switches: czoneSwitches, loading: czoneLoading, pending: czonePending, error: czoneError, toggleSwitch: toggleCZone } = useCZoneSwitches(5, activePageHasCZoneWidget)
  const autopilot = useAutopilot()
  const isImperialDistance = uiConfig.distanceUnits === 'imperial'

  // Read at the click, not subscribed: the shell never renders on a fix.
  const handleDropAnchorHere = () => {
    const { latitude, longitude, depth } = getVesselStateSnapshot()
    if (latitude === null || longitude === null) return
    void anchorWatch.setAnchorHere(latitude, longitude, {
      planningDepthM: depth,
      planningTideHeightFt: tideHeightFtOrNull(tide),
    })
  }

  const hasActiveWindBulletin = Boolean(findActiveWindBulletin(activeForecastWarning))
  const hasActiveAnchorWatch = anchorWatch.active

  // Drives activePageId exactly the way a sidebar click or a deep link
  // does (ADR 0089, superseded by ADR 0110), so activePage/effectiveWidgets/
  // dashboardGrid/renderWidget all keep working unchanged whether the page
  // came from a click or from the rotation timer. Called unconditionally
  // (rules of hooks); `enabled` is what actually turns it off outside the
  // wall route, and it also gates on `wallDisplay` having resolved - every
  // wall boot briefly has `isDisplay` true and `wallDisplay` still null,
  // before the displays fetch and the slug match land.
  const displayRotation = useDisplayRotation({
    enabled: isDisplay && wallDisplay !== null,
    displayId: wallDisplay?.id ?? null,
    pages,
    navigationState,
    pinnedPageId: displayOptions.pageId,
    refetch: refetchPages,
    onShow: setActivePageId,
  })
  const { feedEmpty: displayFeedEmpty } = displayRotation
  // ADR 0110 §5b: bumped by DisplayRemoteController (rendered only on the
  // wall route, below) on every actual remote action - a step, a pause, a
  // resume - so DisplayRemoteToast flashes only for that, never for the
  // ordinary dwell-driven page change that also moves displayRotation's
  // own position.
  const [remoteAnnouncementId, setRemoteAnnouncementId] = useState(0)
  const bumpRemoteAnnouncement = useCallback(() => setRemoteAnnouncementId((n) => n + 1), [])

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
      // updatePlanningDepth now throws on a failed PATCH rather than
      // silently no-op'ing (use-anchor-watch.ts). The rejection is left to
      // propagate rather than caught here — AnchorRodePlanner's
      // commitPlanningDepth is what shows the toast (with the server's own
      // message and a Retry it can act on) and reverts the depth input to
      // the last-known-good server value, the same pattern the radius
      // stepper and the rode/conditions fields already use.
      return anchorWatch.updatePlanningDepth(depthM, tideHeightFt ?? -1)
    }
    setSessionPlanningDepth({ depthM, tideHeightFt })
    return Promise.resolve()
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
        widgets: effectiveWidgets.map((w) => (w.id === id ? { ...w, gauge, h: raiseGaugeHeight(w.h, gauge.display) } : w)),
      })
    } else {
      void updatePage(activePage.id, { widgets: [...effectiveWidgets, { ...gaugeDraft, gauge, h: raiseGaugeHeight(gaugeDraft.h, gauge.display) }] })
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

  // Tall: the canvas is 460x300 plus the tile chrome.
  const handleAddCluster = useCallback(() => {
    const maxY = effectiveWidgets.reduce((max, w) => Math.max(max, w.y + w.h), 0)
    setClusterDraft({
      id: newClusterWidgetId(effectiveWidgets),
      x: 0, y: maxY, ...CLUSTER_DEFAULT_SIZE,
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

  // Item D: the shell still re-renders on every autopilot, electrical, solar,
  // tanks, nearby-vessel, radar and anchor-watch tick, and on each polled
  // forecast (gauge values, alarms and vessel state no longer: ADR 0215 moved
  // them into the tiles; vessel state still re-renders it when the navigation
  // state changes). renderWidget below runs fresh on every one of those
  // renders (it is deliberately not memoized itself —
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
  // globalThis.Map, the builtin collection - not a lucide-react icon.
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
  const openForecastPanel = useCallback(() => navigate.toPanel('forecast'), [navigate])
  const openAlarmsPanel = useCallback(() => navigate.toPanel('alarms'), [navigate])
  const openAnchorWatchPanel = useCallback(() => navigate.toPanel('anchor-watch'), [navigate])
  const openRibbonDialog = useCallback(() => setRibbonDialogOpen(true), [])

  // Not wrapped in useCallback: exhaustive-deps reports ~58 dependencies here
  // (essentially the entire polled-data surface of the component —
  // electrical, tanks, nearby-vessels, anchor watch, forecasts, etc.), several of
  // which change every few seconds independently. Memoizing would just
  // recreate the reference on nearly every render anyway, so it buys no real
  // stabilization — left as a plain function per the task's own guidance for
  // this case.
  const renderWidget = (widget: DashboardLayoutItem): ReactNode => {
    const { id } = widget
    if (isClusterWidgetId(id)) {
      if (!widget.cluster) return null
      return (
        <EngineClusterHost
          config={widget.cluster}
          ctx={equipmentContext}
          tanks={tanks}
          editing={layoutEditing}
          onConfigure={configureHandlerFor(id, setClusterDraft)}
        />
      )
    }

    if (isLampStripWidgetId(id)) {
      if (!widget.lamps) return null
      return (
        <LampStripHost
          config={widget.lamps}
          ctx={equipmentContext}
          editing={layoutEditing}
          onConfigure={configureHandlerFor(id, setLampStripDraft)}
          onOpenAlarms={openAlarmsPanel}
        />
      )
    }

    if (isGaugeGroupWidgetId(id)) {
      if (!widget.gaugeGroup) return null
      return (
        <GaugeGroupHost
          config={widget.gaugeGroup}
          ctx={equipmentContext}
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
        <LivePoiMapTile
          config={widget.poiMap}
          editing={layoutEditing}
          onConfigure={configureHandlerFor(id, setPoiMapDraft)}
          activeRoute={activeRoute}
          nearbyVessels={nearbyVessels}
          getSelfTrail={getSelfTrail}
          isDarkTheme={isDarkTheme}
          forceDark={activePage?.skin === 'instrument'}
          distanceUnits={uiConfig.distanceUnits}
          interactive={!isDisplay}
        />
      )
    }

    switch (id) {
      case 'vessel':
        return <MarineHeader />
      case 'wind':
        return (
          <LiveWindTile />
        )
      case 'depth-tide':
        return (
          <LiveDepthTideTile
            isImperialDistance={isImperialDistance}
            depthTrend={depthTrend}
            tide={tide}
            onOpen={layoutEditing ? undefined : openForecastPanel}
          />
        )
      case 'position':
        return (
          <LivePositionTile />
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
          <LiveClockTile
            routeStatus={routeActivationStatus}
            sunriseTime={forecast[0]?.sunriseTime ?? null}
            sunsetTime={forecast[0]?.sunsetTime ?? null}
            moonPhase={forecast[0]?.moonPhase ?? null}
          />
        )
      case 'current-conditions':
        return (
          <LiveCurrentConditionsTile
            weather={weather}
            forecast={forecast}
            nextHour={forecastNextHour}
            distanceUnits={uiConfig.distanceUnits}
          />
        )
      case 'forecast-conditions':
        return (
          <ForecastConditionsTile
            forecast={forecast}
            waveForecastDays={waveForecastDays}
            waveLoading={waveForecastLoading}
            waveError={waveForecastError}
            units={uiConfig.distanceUnits}
          />
        )
      case 'anchor-watch':
        return (
          <LiveAnchorWatchTile
            watch={anchorWatch}
            isImperial={isImperialDistance}
            vesselTrail={getAnchorTrail}
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
            onFullscreen={openAnchorWatchPanel}
            placemarks={placemarks}
            onPlacemarkCreate={createPlacemark}
            onPlacemarkRemove={removePlacemark}
            tide={tide}
            interactive={!isDisplay}
          />
        )
      case 'tanks':
        return (
          <TanksTile
            tanks={tanks}
            loading={tanksLoading}
            error={tanksError}
            lastUpdateAgeS={tanksAgeS}
          />
        )
      case 'route':
        return (
          <LiveRouteTile status={routeActivationStatus} />
        )
      case 'nearby-vessels':
        return (
          <LiveNearbyVesselsTile
            vessels={nearbyVessels}
            loading={nearbyVesselsLoading}
            distanceUnits={uiConfig.distanceUnits}
            lastUpdateAgeS={nearbyVesselsAgeS}
            maxRangeM={nearbyVesselsMaxRangeM}
          />
        )
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
            voltageReadoutV={voltageReadoutV}
            voltageReadoutLabel={voltageReadoutLabel}
            charger0CurrentA={charger0CurrentA}
            charger0AcIn1CurrentA={charger0AcIn1CurrentA}
            charger0ChargingMode={charger0ChargingMode}
            charger0Error={charger0Error}
            batteryRatePercentPerHour={batteryRatePercentPerHour}
            timeToGoHours={timeToGoHours}
            lastUpdateAgeS={electricalLastUpdateAgeS}
            socBands={socBands}
            overnight={overnight}
            houseBankConfigured={houseBankConfigured}
            houseBankUnresolved={houseBankUnresolved}
            // The wall display shares this render; a kiosk has no Settings to go to.
            onOpenRoles={isDisplay ? undefined : () => openSetupStep('signalk', 'role-house_bank')}
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
        return <LiveAlternatorTile port={alternator0} starboard={alternator1} />
      case 'generator':
        return (
          <LiveGeneratorTile
            generatorRealPowerW={generatorRealPowerW}
            generatorConfigured={generatorConfigured}
            generatorNone={generatorNone}
            onOpenRoles={isDisplay ? undefined : () => openSetupStep('signalk', 'role-generator')}
            batterySocPercent={batterySocPercent}
            batteryRatePercentPerHour={batteryRatePercentPerHour}
            readOnly={!canWrite}
          />
        )
      case 'czone-switches':
        return <CZoneSwitchesTile switches={czoneSwitches} loading={czoneLoading} pending={czonePending} onToggle={toggleCZone} error={czoneError} readOnly={!canWrite} />
      case 'autopilot':
        return (
          <LiveAutopilotTile
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
    { label: 'Indicators…', category: 'custom', onSelect: handleAddLampStrip },
    { label: 'Gauge Group…', category: 'custom', onSelect: handleAddGaugeGroup },
    { label: 'Embed…', category: 'custom', onSelect: handleAddEmbed },
    { label: 'Nearby map…', category: 'navigation', onSelect: handleAddPoiMap },
  ]

  // The display the active page is actually assigned to (ADR 0110), not
  // wallDisplay - this is the ordinary authoring case (any page, on any
  // screen this session happens to be viewing), while wallDisplay only ever
  // means "the screen the wall route itself resolved to".
  const activePageDisplay = displays.find((d) => d.id === activePage?.display_id) ?? null

  const handleDisplayPatch = (id: string, patch: DisplayPatch) => {
    const page = pages.find((p) => p.id === id)
    void updatePage(id, patch).then((saved) => {
      // The server clears a page's hero the instant a display is assigned
      // (ADR 0110) - correct, and left alone here (AGENTS.md's fallback
      // policy is about not masking that, not about second-guessing it).
      // But a silent clear an operator only discovers later by reopening
      // the page reads as a bug, so this names what happened once the save
      // actually lands. Only for an assignment (a non-empty display_id) on
      // a page that had a hero to lose - never on a clear, and never when
      // there was nothing to clear in the first place.
      if (saved && patch.display_id && page?.hero) {
        const targetName = displays.find((d) => d.id === patch.display_id)?.name ?? 'the display'
        toast(`${page.name} is now on ${targetName}. Its hero tile was cleared.`)
      }
    })
  }

  // ADR 0110 §6: the "Duplicate to…" toolbar action. Client-side, through
  // the same POST /api/dashboard-pages createPage already wraps - widgets
  // are deep-cloned (multi-instance widget ids need no re-minting;
  // validateDashboardWidgets only enforces uniqueness within a page), skin/
  // dwell/condition copy unconditionally, and hero copies only onto a
  // plain Dashboard copy - the server rejects a hero on a page carrying a
  // display_id, so sending one for a display target would just fail the
  // create outright. Finishes with ADR 0107's exact new-page flow (switch
  // to it, start naming, force layout editing), same as "New Page" below.
  // An unattended wall that loses the displays fetch at boot - a transient
  // network blip, a backend still starting - would otherwise sit on the
  // "no display configured" card forever with nobody there to reload it.
  // The rotation already polls its way out of an empty feed; this is the
  // same recovery one level up, and it stops as soon as a fetch succeeds.
  useEffect(() => {
    if (!isDisplay || displaysError === null) return
    const timer = setInterval(() => { void refetchDisplays() }, DISPLAY_RECOVERY_POLL_MS)
    return () => clearInterval(timer)
  }, [isDisplay, displaysError, refetchDisplays])

  // ADR 0107's create-and-name flow, applied to a screen: make one with
  // workable defaults and land the operator in its editor rather than asking
  // for a name up front in a dialog this ADR just removed. The default name
  // steps past any it would collide with, because the slug is derived from
  // it and a duplicate slug is refused rather than auto-suffixed (ADR 0110).
  const handleCreateDisplay = async () => {
    const taken = new Set(displays.map((d) => d.name))
    let name = 'New display'
    for (let n = 2; taken.has(name); n += 1) name = `New display ${n}`
    const created = await createDisplay({ name, width: 1920, height: 1080, scale: 1, rotate: 0 })
    // Only while the index is still on screen: the create is async.
    if (created && router.getSnapshot().location.panel === 'wall-displays') {
      navigate({ panel: 'wall-displays', route: { view: 'editor', slug: created.slug } })
    }
  }

  // Both duplicate paths build the same copy; they differ only in what
  // happens afterwards, which is a property of where you started, not of
  // the copy. From a page's own toolbar you follow the new page (ADR 0107);
  // from a display editor the copy lands on a *different* screen, so
  // following it would throw you out of the rotation you are composing.
  const duplicatePageToDisplay = (pageId: string, displayId: string | null) => {
    const source = pages.find((p) => p.id === pageId)
    if (!source) return Promise.resolve(null)
    const init: CreatePageInit = {
      widgets: structuredClone(source.widgets),
      skin: source.skin,
      dwell_seconds: source.dwell_seconds,
      show_when: source.show_when,
    }
    if (displayId) {
      init.display_id = displayId
      // An ordinary Dashboard page has no dwell to copy, and the server
      // rejects a page that arrives on a display without one. Default it
      // the same way PageDisplaySelect does when it first assigns a page,
      // so duplicating a normal page onto a screen works at all.
      init.dwell_seconds = source.dwell_seconds || DEFAULT_DWELL_SECONDS
    } else if (source.hero) {
      init.hero = source.hero
    }
    return createPage('Untitled page', init)
  }

  const handleDuplicateToDisplay = (pageId: string, displayId: string | null) => {
    void duplicatePageToDisplay(pageId, displayId).then((created) => {
      if (created) {
        setActivePageId(created.id)
        setNamingPageId(created.id)
        setLayoutEditing(true)
      }
    })
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
      {/* The layout toolbar (ADR 0107): page name field, Add Tile, Ribbon,
          Skin, Hero, Wall display, Duplicate to…, Delete page, in that fixed
          order. Replaces both the old "Layout Mode — Drag to rearrange" pill
          that used to sit here and the separate control row that used to
          sit below the grid. */}
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
          displays={displays}
          onDisplayPatch={handleDisplayPatch}
          onManageDisplays={() => navigate.toPanel('wall-displays')}
          onDuplicateToDisplay={handleDuplicateToDisplay}
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

          Never rendered on the wall (ADR 0089, carried into ADR 0110): on a
          1920x360 strip it ran about a third of the height and pushed a
          seven-row page below the fold. The wall display gets nothing here
          for free; a page that wants lamps on the wall adds its own
          lamp-strip widget, sized and placed like any other tile, same as
          it would for any other page-specific status row. */}
      {!isDisplay && ribbon && (
        <div data-testid="dashboard-ribbon" className="w-full min-w-0">
          <LampStripHost
            config={ribbon}
            ctx={equipmentContext}
            editing={layoutEditing}
            onConfigure={openRibbonDialog}
            onOpenAlarms={openAlarmsPanel}
          />
        </div>
      )}

      {/* An empty page shows a prompt instead of an unexplained blank stretch
          (ADR 0107) — never on the wall, where there's nothing to click and
          no operator watching to click it. A page carrying a hero always
          shows the grid: the hero row itself is content, even when it's the
          only widget on the page. Gated on !pagesLoading too: before the
          initial GET /api/dashboard-pages resolves there is no active page
          yet either, which reads the same as "empty" — without this the
          prompt flashed on every load, not just on a genuinely empty page. */}
      {!pagesLoading && effectiveWidgets.length === 0 && !activePage?.hero ? (
        <EmptyPagePrompt editing={layoutEditing} canEditLayout={canEditLayout} onOpenHelp={openHelp} isKiosk={isDisplay} />
      ) : (
        // relative so DisplayFoldGuide (ADR 0110, superseding ADR 0089) can
        // position itself against exactly the content the wall route shows:
        // the grid alone. The ribbon sits outside this container (above)
        // precisely because it no longer counts against the fold budget —
        // it never reaches the wall at all. minHeight guarantees the guide's
        // own `absolute` line (drawn at foldPx from the top of this box) has
        // something behind it even on a tall canvas whose content doesn't
        // reach that far down yet.
        <div className="relative" style={activePageDisplay ? { minHeight: displayFoldPx(activePageDisplay) ?? undefined } : undefined}>
          <DashboardBentoGrid
            widgets={effectiveWidgets}
            editing={layoutEditing}
            // The server rejects a hero on a page that carries a display_id
            // (ADR 0110) - undefined here, not activePage?.hero, so the grid
            // never renders a hero row the backend would have refused to
            // persist in the first place.
            heroId={activePage?.display_id ? undefined : activePage?.hero}
            renderWidget={renderWidget}
            onRemoveWidget={handleRemoveWidget}
            onDuplicateWidget={handleDuplicateWidget}
            onLayoutSettle={handleLayoutSettle}
            // A wall page's height is fixed by the panel, not by a scrolling
            // viewport, so its rows sit closer together on a narrow strip.
            // Taken from the page's own assigned display, not the route, so
            // the helm browser authoring the page lays it out at the same
            // geometry the wall will render it at and the fold guide stays
            // honest.
            rowMargin={activePageDisplay ? displayRowMargin(activePageDisplay) : undefined}
          />

          {/* Authoring aid, not a wall-display feature: only shown while
              editing a page that is itself assigned to a display with a
              measured canvas (a zero-canvas display has no fold to guide
              against - lib/displays.ts's displayFoldPx), so laying it out on
              the ordinary desktop dashboard shows exactly where that
              screen's canvas cuts off before saving. */}
          {layoutEditing && activePageDisplay && displayFoldPx(activePageDisplay) !== null && (
            <DisplayFoldGuide
              topPx={displayFoldPx(activePageDisplay) as number}
              heightPx={activePageDisplay.height}
              displayName={activePageDisplay.name}
            />
          )}
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
          <LiveForecastPage
            canWrite={canWrite}
            boat={buildForecastDrawerProps({
              weather: weatherFeed,
              waves: wavesFeed,
              upperAir: upperAirFeed,
              warnings: warningsFeed,
              unit: uiConfig.distanceUnits as 'imperial' | 'metric',
            })}
          />
        )
      case 'alarms':
        return (
          <LiveAlarmsDrawer
            rules={alarmRules}
            loading={alarmRulesLoading}
            error={alarmRulesError}
            createRule={createAlarmRule}
            updateRule={updateAlarmRule}
            deleteRule={deleteAlarmRule}
            collisionTuningUrl={collisionTuningHref}
            forecastWarnings={activeForecastWarning}
          />
        )
      case 'radar':
        return <LiveRadarDrawer />
      case 'assistant':
        return (
          <AssistantDrawer
            canWrite={canWrite}
            onOpenSettings={openAssistantSettings}
            initialConversationId={matePanelConversationId}
            onActiveConversationChange={reportMatePanelConversation}
            onHasDraftChange={setMatePanelHasDraft}
          />
        )
      case 'wall-displays': {
        // ADR 0112: the index and the per-display editor are one panel, the
        // same way DocumentsPanel owns its own folder drill-down. Which one
        // renders is the presence of a slug, and the editor resolves it
        // itself rather than App keeping a second piece of state in step.
        const wallDisplaysRoute = appLocation.panel === 'wall-displays' ? appLocation.route : { view: 'index' as const }
        const editing = wallDisplaysRoute.view === 'editor'
          ? displays.find((d) => d.slug === wallDisplaysRoute.slug) ?? null
          : null
        if (wallDisplaysRoute.view === 'editor') {
          return (
            <DisplayEditorPanel
              display={editing}
              pages={pages}
              displays={displays}
              onBack={() => navigate({ panel: 'wall-displays', route: { view: 'index' } })}
              onUpdate={async (id, patch) => {
                const updated = await updateDisplay(id, patch)
                // The route names the display by slug, and the slug is one
                // of the fields this panel edits: without following it, a
                // rename resolves to nothing and the editor flips to "could
                // not be found" on a display that is fine.
                // Only while the editor is still what is on screen: the save
                // is async, and the operator may have moved on meanwhile.
                if (updated && updated.id === editing?.id && router.getSnapshot().location.panel === 'wall-displays') {
                  navigate({ panel: 'wall-displays', route: { view: 'editor', slug: updated.slug } }, { replace: true })
                }
                return updated
              }}
              onDelete={async (id) => {
                // deleteDisplay refreshes the page list, so the released
                // pages drop their display_id without a wire-up here.
                return deleteDisplay(id)
              }}
              onDisplayPatch={handleDisplayPatch}
              onDuplicateToDisplay={(pageId, targetDisplayId) => {
                // Unlike the layout toolbar's duplicate, this copy lands on
                // a different screen than the one being edited, so ADR
                // 0107's follow-the-new-page flow would throw the operator
                // out of the rotation they are composing. Report it instead.
                const target = displays.find((d) => d.id === targetDisplayId)
                void duplicatePageToDisplay(pageId, targetDisplayId).then((created) => {
                  if (created && target) toast.success(`Copied to ${target.name}.`)
                })
              }}
              onCreatePage={async (displayId) => await createPage('Untitled page', {
                widgets: [],
                display_id: displayId,
                dwell_seconds: DEFAULT_DWELL_SECONDS,
              })}
              onOpenPage={(id) => {
                if (navigate({ panel: null, pageId: id })) setLayoutEditing(true)
              }}
              onReorder={reorderPages}
              reordering={reordering}
              canWrite={canWrite}
            />
          )
        }
        return (
          <WallDisplaysPanel
            displays={displays}
            pages={pages}
            loading={displaysLoading}
            error={displaysError}
            onRetry={() => { void refetchDisplays() }}
            onOpenDisplay={(id) => {
              const target = displays.find((d) => d.id === id)
              if (target) navigate({ panel: 'wall-displays', route: { view: 'editor', slug: target.slug } })
            }}
            onCreateDisplay={() => { void handleCreateDisplay() }}
            onOpenHelp={openHelp}
            canWrite={canWrite}
          />
        )
      }
      case 'documents': {
        // ADR 0115 §2: the listing and the Details page are one panel, the
        // same index/editor split ADR 0112's wall-displays branch above
        // already uses - which one renders is the route's view, resolved
        // by DocumentsRoutePanel, which also owns the Details page's
        // unsaved-changes guard (ADR 0171).
        const documentDeepLinkId = documentsLeftOnceRef.current
          ? null
          : (initialLocation.panel === 'documents' && initialLocation.route.view === 'browse'
            ? initialLocation.route.documentId
            : null)
        return (
          <DocumentsRoutePanel
            initialDocumentId={documentDeepLinkId}
            onOpenHelp={openHelp}
            // ADR 0120: the toolbar's Mate failure line links straight to
            // Settings → Assistant, the same openSettingsSection helper
            // AssistantDrawer's own "Open Mate settings" button uses above.
            onOpenAssistantSettings={openAssistantSettings}
            // Ask Mate about a selection (ask-mate-selection.tsx): same
            // openMate the header's Sparkles button and SettingsPage's own
            // onAskMate already call, always starting a fresh conversation.
            onAskMate={openMate}
            onDetailsTitleChange={setDocumentDetailsTitle}
          />
        )
      }
      case 'passages':
        return <PassagesRoutePanel units={uiConfig.distanceUnits} canWrite={canWrite} isDarkTheme={isDarkTheme} />
      case 'inventory':
        return <InventoryRoutePanel canWrite={canWrite} />
      case 'settings':
        return <SettingsRoutePanel onAskMate={openMate} onOpenHelp={openHelp} />
      case 'anchor-watch':
        return (
          <>
            {/* The drawer's fix, depth, current, wind and the vessel's size
                come from vessel state inside LiveAnchorWatchDrawer, which
                also applies the live fix to the watch record; see its comment
                on the GPS fallback and the -1/-1 sentinel. */}
            <LiveAnchorWatchDrawer
              watch={anchorWatch}
              placemarks={placemarks}
              onPlacemarkCreate={createPlacemark}
              onPlacemarkRemove={removePlacemark}
              anchorLat={anchorWatch.anchorLat}
              anchorLon={anchorWatch.anchorLon}
              radiusMeters={anchorWatch.radiusMeters}
              bowOffsetM={anchorWatch.bowOffsetM}
              bowOffsetApplied={anchorWatch.bowOffsetApplied}
              bowOffsetReason={anchorWatch.bowOffsetReason}
              headingAtSetDeg={anchorWatch.headingAtSetDeg}
              placeName={anchorWatch.placeName}
              anchorSetAt={anchorWatch.setAt}
              anchorStateKnown={anchorWatch.loaded}
              error={anchorWatch.error}
              vesselTrail={getAnchorTrail}
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
              onRadiusChange={anchorWatch.updateRadius}
              adjustAnchor={anchorWatch.adjustAnchor}
              onClearAnchor={anchorWatch.clearAnchor}
              isImperial={isImperialDistance}
              onDropAnchor={handleDropAnchorHere}
              rodeDeployedM={anchorWatch.rodeDeployedM}
              seaState={anchorWatch.seaState}
              seabedType={anchorWatch.seabedType}
              tide={tide}
              anchorConfig={anchorConfig}
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

  // ADR 0142: the CRUD pattern library's dev-only gallery - reuses the
  // ordinary auth gates above (a boat running signalk auth still needs to
  // sign in to reach it) but bypasses the whole dashboard shell below
  // (sidebar, header, panel routing) entirely, the same "reuse dashboardGrid
  // directly" reasoning the wall display's own early return just below
  // uses for the opposite reason. patternsGalleryRequested is `false` in a
  // production build (import.meta.env.DEV statically replaced), so this
  // branch - and the dynamic import() PatternsGallery names - never runs
  // there.
  if (patternsGalleryRequested) {
    return (
      <Suspense
        fallback={
          <div className="flex min-h-screen items-center justify-center bg-background text-sm text-muted-foreground">
            Loading patterns…
          </div>
        }
      >
        <PatternsGallery />
      </Suspense>
    )
  }

  // The wall display (ADR 0110, superseding ADR 0089) reuses dashboardGrid
  // directly rather than the ordinary shell: no sidebar, no header, no
  // SidebarProvider. toastRef already null-checks everywhere it's read and
  // no tile calls useSidebar, so nothing downstream depends on
  // SidebarProvider being mounted. This also means the help (ADR 0095)
  // needs no separate wall gating - the header `?`, the sidebar Help item
  // and <HelpSheet> itself are all declared below this return and never
  // reached on an unattended screen.
  if (isDisplay) {
    // Still resolving which displays exist at all - a beat before the slug
    // can even be checked, never long enough to need more than a quiet
    // placeholder.
    if (displaysLoading) {
      return (
        <div className="flex min-h-screen items-center justify-center bg-background text-sm text-muted-foreground">
          Loading…
        </div>
      )
    }

    // No slug (bare /display) or a slug naming no configured display: an
    // explicit diagnostic, never a fallback to "the first display" - that
    // would silently put one screen's geometry on another (AGENTS.md's
    // fallback policy). Names every configured display and its URL so
    // fixing a stale or mistyped snap command is a matter of reading this
    // screen, not guessing.
    if (!wallDisplay) {
      return (
        <div className="flex min-h-screen flex-col items-center justify-center gap-3 bg-background px-6 text-center">
          <p className="text-sm font-semibold text-foreground">
            {wallDisplaySlug
              ? `No display is configured at "${wallDisplaySlug}".`
              : 'This address needs a display slug: /display/<slug>.'}
          </p>
          {displays.length > 0 ? (
            <ul className="flex flex-col gap-1 text-xs text-muted-foreground">
              {displays.map((d) => (
                <li key={d.id}>{d.name}: /display/{d.slug}</li>
              ))}
            </ul>
          ) : (
            <p className="text-xs text-muted-foreground">
              {displaysError
                ? 'Could not load the configured displays. Retrying…'
                : 'No displays are configured yet. Add one from the Wall displays dialog.'}
            </p>
          )}
        </div>
      )
    }

    return (
      <LiveDisplayShell
        display={wallDisplay}
        preview={displayOptions.preview}
        overlay={
          <>
            <DisplayRemoteToast
              announcementId={remoteAnnouncementId}
              pageName={activePage?.name ?? ''}
              position={displayRotation.position}
              paused={displayRotation.paused}
            />
            <DisplayUpdatedNotice current={versionWatch.current} />
          </>
        }
      >
        {pagesError && pages.length === 0 ? (
          // Distinct from the empty-feed message below: a failed fetch is a
          // wall that cannot see its pages, not a wall with none assigned,
          // and the rotation is already polling its way back. With pages
          // already loaded the wall keeps showing them instead.
          <div className="flex h-full items-center justify-center text-sm text-muted-foreground">
            Could not load dashboard pages. Retrying…
          </div>
        ) : displayFeedEmpty && !displayOptions.pageId ? (
          <div className="flex h-full items-center justify-center text-sm text-muted-foreground">
            No pages are assigned to {wallDisplay.name} yet.
          </div>
        ) : (
          <>
            {dashboardGrid}
            {pagesError && <RefreshFailedOverlay reason={pagesError} />}
          </>
        )}
        <DisplayRemoteController rotation={displayRotation} onAction={bumpRemoteAnnouncement} />
      </LiveDisplayShell>
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

      {!isFullscreenDashboard && (
      <Sidebar collapsible="icon">
        <SidebarContent>
          <SidebarMenu>
            <SidebarMenuItem>
              <SidebarMenuButton isActive={activePanel === null} onClick={() => navigate.toPanel(null)} tooltip="Dashboard">
                <LayoutDashboard />
                <span>Dashboard</span>
              </SidebarMenuButton>
            </SidebarMenuItem>
            {/* A page assigned to a display (ADR 0110) has its own row in
                the "Wall displays" group below, nested under its display -
                it never appears here too. */}
            {dashboardSubListPages.length > 0 && (
              <SidebarMenuSub>
                {dashboardSubListPages.map((page) => (
                  <SidebarMenuSubItem key={page.id}>
                    <SidebarMenuSubButton
                      render={<button type="button" />}
                      isActive={activePanel === null && page.id === activePageId}
                      onClick={() => {
                        // The router moves to the page; the hook also
                        // remembers it for next time straight away.
                        if (navigate({ panel: null, pageId: page.id })) setActivePageId(page.id)
                      }}
                    >
                      <span className="min-w-0 flex-1 truncate">{page.name}</span>
                    </SidebarMenuSubButton>
                  </SidebarMenuSubItem>
                ))}
              </SidebarMenuSub>
            )}
            {visiblePanelNavItems.map(({ id, label, icon: Icon }) => (
              <SidebarMenuItem key={id}>
                <SidebarMenuButton
                  isActive={activePanel === id}
                  // Mate always opens on a fresh empty chat (code-review
                  // finding): the router's resume(assistant) is exactly that,
                  // where every other panel returns to where it was left.
                  onClick={() => navigate.toPanel(id)}
                  tooltip={label}
                >
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
            {/* ADR 0095: opens the contents page of the in-app help. Never
                `isActive` (it's a sheet over whatever's on screen, not a
                panel of its own) and carries no PanelId or URL - ADR 0074
                keeps sheets out of the address bar. */}
            <SidebarMenuItem>
              <SidebarMenuButton tooltip="Help" onClick={() => openHelp(null)}>
                <BookOpen />
                <span>Help</span>
              </SidebarMenuButton>
            </SidebarMenuItem>
          </SidebarMenu>
        </SidebarContent>
        <SidebarFooter>
          <SidebarVersion
            build={versionWatch.current}
            error={versionWatch.readError}
            updateAvailable={versionWatch.available !== null}
            onReload={requestReload}
          />
        </SidebarFooter>
        <SidebarRail />
      </Sidebar>
      )}

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
        {/* ADR 0142: the window scrolls, so a dirty form's SaveBar (portalled into this
            header) would scroll away with it the moment the operator edits a field
            lower down the page. While - and only while - a save bar is showing, the
            header sticks to the top; :has() keeps every other page's header exactly
            as it was. */}
        {!isFullscreenDashboard && (
        <header className="relative z-60 flex h-14 shrink-0 items-center gap-2 border-b px-2 sm:px-4 lg:h-16 has-[[data-slot=save-bar]]:sticky has-[[data-slot=save-bar]]:top-0 has-[[data-slot=save-bar]]:bg-background">
          <div className="flex min-w-0 flex-1 items-center gap-2">
            <SidebarTrigger className="-ml-1" />
            <Separator orientation="vertical" className="mr-2 hidden h-4 sm:block" />
            <Breadcrumb className="min-w-0">
              <BreadcrumbList className="flex-nowrap">
                {activePanel === null ? (
                  <BreadcrumbItem>
                    <BreadcrumbPage>Dashboard</BreadcrumbPage>
                  </BreadcrumbItem>
                ) : (() => {
                  // Panels are siblings of Dashboard in the sidebar, not its
                  // children, so the root crumb is the panel's own name.
                  // Settings and Inventory additionally carry their active
                  // section as a second crumb, mirroring the SectionNav each
                  // of them hosts; every other panel is a single crumb.
                  const panelLabel = PANEL_NAV_ITEMS.find((item) => item.id === activePanel)?.label
                  const sectionLabel = activePanelSectionLabel(appLocation)
                  if (sectionLabel === null) {
                    return (
                      <BreadcrumbItem className="min-w-0">
                        <BreadcrumbPage className="truncate">{panelLabel}</BreadcrumbPage>
                      </BreadcrumbItem>
                    )
                  }
                  return (
                    <>
                      {/* Below `sm` only the leaf crumb survives — the parent
                          is redundant with the sidebar, which is already on
                          this panel. Plain text, not a link: there is no
                          "top of Settings/Inventory" separate from a section
                          to navigate to. */}
                      <BreadcrumbItem className="hidden sm:inline-flex">
                        <span className="truncate">{panelLabel}</span>
                      </BreadcrumbItem>
                      <BreadcrumbSeparator className="hidden sm:block" />
                      <BreadcrumbItem className="min-w-0">
                        <BreadcrumbPage className="truncate">{sectionLabel}</BreadcrumbPage>
                      </BreadcrumbItem>
                    </>
                  )
                })()}
              </BreadcrumbList>
            </Breadcrumb>
          </div>
          <div className="flex min-w-0 shrink-0 items-center gap-2">
            {activePanel === null && (!pagesError || pages.length > 0) && (
              <>
                <DashboardPageSwitcher
                  pages={dashboardSubListPages}
                  allPages={pages}
                  activePageName={activePage?.name}
                  canWrite={canWrite}
                  onReorder={reorderPages}
                  reordering={reordering}
                  activePageId={activePageId}
                  onSelect={setActivePageId}
                  onCreate={() => {
                    // Named in place, not behind a dialog (ADR 0107): the page
                    // exists immediately as "Untitled page", and namingPageId
                    // is what starts its name field empty and focused.
                    void createPage('Untitled page', { widgets: [] }).then((p) => {
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
                    control is absent rather than present-but-inert. Also hidden while
                    full screen: the header holding it is what full screen removes, so
                    there is no chrome left to toggle from. */}
                {canEditLayout && !fullscreen.isFullscreen && (
                  <LayoutModeToggle editing={layoutEditing} onToggle={() => setLayoutEditing((prev) => !prev)} />
                )}
                {/* Full screen (MDN Fullscreen API): fullscreens the whole
                    document, header and sidebar included, so the grid gets
                    the entire screen - see isFullscreenDashboard above for
                    the chrome-hiding half of this and the floating "Exit
                    full screen" button it renders in the header's place.
                    Not rendered at all when unsupported (`fullscreen.supported`
                    is feature-detected via `document.fullscreenEnabled`,
                    which iPhone Safari lacks) rather than shown disabled. */}
                {fullscreen.supported && (
                  <Button
                    variant="ghost"
                    size="icon"
                    aria-label="Full screen"
                    title="Full screen"
                    onClick={() => {
                      setLayoutEditing(false)
                      fullscreen.enter()
                    }}
                  >
                    <Maximize className="h-4 w-4" />
                  </Button>
                )}
              </>
            )}
            {/* The "settings for this page" shortcut: only the panels in
                PANEL_SETTINGS_SECTIONS have a matching Settings section for
                it to open, so it's absent everywhere else - Dashboard,
                Settings itself, and every panel with no section of its own
                (Documents, Inventory, Forecast, Wall displays). Hidden
                below `sm`: on a phone the right cluster is already full, and
                the breadcrumb is the slack absorber, so this button squeezed
                the page name down to one letter. Settings stays one sidebar
                tap away there. */}
            {(() => {
              const sectionId = activePanel ? PANEL_SETTINGS_SECTIONS[activePanel] : undefined
              if (!sectionId) return null
              const sectionLabel = SETTINGS_SECTIONS.find((section) => section.id === sectionId)?.label ?? ''
              return (
                <Button
                  variant="ghost"
                  size="icon"
                  className="hidden sm:inline-flex"
                  aria-label={`${sectionLabel} settings`}
                  title={`${sectionLabel} settings`}
                  onClick={() => openSettingsSection(sectionId)}
                >
                  <Settings className="h-4 w-4" />
                </Button>
              )
            })()}
            {/* ADR 0095: the contextual help - lands on the current
                screen's page (and heading, for the three dashboard
                sub-panels that share features/dashboard). `title` is how
                neighbouring header buttons carry a tooltip, same as this
                one's neighbours below. */}
            <Button
              variant="ghost"
              size="icon"
              aria-label="Open help"
              title="Help for this screen"
              onClick={() => openHelp(helpTargetFor(appLocation))}
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
                session the same way write controls are elsewhere. ADR 0122:
                with no speech API at all, the button is hidden entirely
                (`unsupportedReason === 'no-api'`) rather than shown disabled
                - matching the rule components/dictation.tsx's DictateButton
                already follows for an in-field mic. An insecure origin still
                shows it, disabled, with MicOff and a `title` naming why -
                unlike a field's mic, this one's reason stays a tooltip: it
                sits in the header rather than a touch-first form, and the
                app's own https link is already covered in Talk to Mate. */}
            {assistantVoiceConfig.voiceInput && canWrite && mateVoice.unsupportedReason !== 'no-api' && (
              <div className="flex items-center gap-2">
                <Button
                  variant={mateVoiceListening ? 'default' : 'ghost'}
                  size="icon"
                  aria-label="Talk to Mate"
                  aria-pressed={mateVoiceListening}
                  disabled={!mateVoice.supported}
                  title={
                    !mateVoice.supported
                      ? 'Voice input needs the app opened over https'
                      : mateWakeActive ? 'Listening for Hey Mate' : undefined
                  }
                  className="relative"
                  onClick={mateVoicePushToTalk}
                >
                  {mateVoice.supported ? <Mic className="h-4 w-4" /> : <MicOff className="h-4 w-4" />}
                  {mateWakeActive && (
                    // The dot needs to read against whichever background the
                    // button itself is wearing right now - primary-on-primary
                    // would vanish the moment the button fills solid while
                    // actually listening.
                    <span
                      className={cn(
                        'absolute right-1 top-1 h-1.5 w-1.5 rounded-full',
                        mateVoiceListening ? 'bg-primary-foreground' : 'bg-primary',
                      )}
                      aria-hidden="true"
                    />
                  )}
                </Button>
                {mateVoiceListening && (
                  <span className="text-xs text-muted-foreground">
                    {mateVoice.armed ? 'Mate is listening…' : mateVoice.interim !== '' ? mateVoice.interim : 'Listening…'}
                  </span>
                )}
              </div>
            )}
            <VesselStatusBar
              isDark={isDarkTheme}
              themeMode={themeState?.mode}
              autoUntil={themeState?.autoUntil}
              onToggleDarkMode={toggleDarkMode}
              username={auth.mode === 'signalk' ? auth.user?.username ?? null : null}
              onLogout={auth.mode === 'signalk' ? () => { void auth.logout() } : undefined}
            />
          </div>
          {/* ADR 0142: a dirty form's SaveBar portals in here and covers the
              header (Polaris' contextual save bar) - the header is `relative`,
              so the slot's absolute inset-0 is exactly its box. Empty (and
              display:none) whenever nothing is dirty. */}
          <SaveBarSlot />
        </header>
        )}
        {/* First-run setup: one quiet line under the header on the dashboard
            while a step is undone. Never on a wall display (which returns
            before this shell) and never for a read-only session. */}
        {!isFullscreenDashboard && (
          <SetupLine
            enabled={!isDisplay && canWrite && activePanel === null}
            onOpen={(step) => openSetupStep(step.target.section as SettingsSectionId, step.target.control)}
          />
        )}
        {/* Full screen, touch-first exit affordance: the header (and its
            "Exit full screen" counterpart) is gone in this state, so a
            screen with no Esc key needs its own way back. Esc and the
            browser's own chrome still work too - useFullscreen's
            `fullscreenchange` listener is what notices either one.

            Pinned top-right, where the header's own control was. The live
            alarm banner below (data-testid="alarm-banner-stack") becomes the
            top-most thing on screen once the header is gone, and its
            View/Acknowledge buttons (alarm-banner.tsx) are always the
            trailing, right-aligned child of its row, so the stack takes
            pr-12 while full screen to keep them clear of this 40px button.
            Padding the width rather than offsetting by the banner's height
            holds however many alarms are shown or how their text wraps. */}
        {isFullscreenDashboard && (
          <Button
            variant="outline"
            size="icon"
            aria-label="Exit full screen"
            title="Exit full screen"
            className="fixed right-2 top-2 z-60 border-border bg-card/80 text-muted-foreground"
            onClick={() => fullscreen.exit()}
          >
            <Minimize className="h-4 w-4" />
          </Button>
        )}
        {/* Swipe-paging toast: names the page a left/right swipe just
            landed on, plus its position in the same sub-list order the
            header's page switcher uses ("2 / 4"). role="status" +
            aria-live="polite" rather than a visual-only toast, since a
            screen reader user swiping the same gesture should hear the
            same confirmation a sighted operator sees. Self-clears via
            goToDashboardPageOffset's timer, or immediately if full screen
            ends first (the effect above). */}
        {isFullscreenDashboard && swipePageToast && (
          <div
            role="status"
            aria-live="polite"
            className="pointer-events-none fixed left-1/2 top-4 z-60 -translate-x-1/2 rounded-full border border-border bg-card/90 px-4 py-2 text-center shadow-lg"
          >
            <p className="font-display text-xs font-semibold uppercase tracking-[0.14em] text-foreground">
              {swipePageToast.name}
            </p>
            <p className="text-xs uppercase tracking-[0.14em] text-muted-foreground">
              {swipePageToast.index} / {swipePageToast.total}
            </p>
          </div>
        )}
        <div className="flex min-h-0 flex-1 flex-col px-2 py-2">
          <div className={cn('mx-auto flex w-full flex-1 min-h-0 flex-col gap-4', !isFullscreenDashboard && 'max-w-[1800px]')}>
            <ConnectionBanner />
            {/* z-55 keeps a live alarm above the Mate sheet's backdrop (z-50)
                while staying under the dashboard header, which sits at
                z-60 same as the page selector and every other top-level
                header control. (impeccable critique 2026-09-12, P1)

                Dropdown menus are z-60 too (components/ui/dropdown-menu.tsx),
                so they draw above this banner. Popovers (components/ui/
                popover.tsx) and the select popup (components/ui/select.tsx)
                sit at z-85, above the sheet and dialog layers below, because
                a popover's or a select's trigger commonly lives inside one of
                them (the note editor's Link/Image buttons in the documents
                viewer sheet are the case that exposed this for popovers, a
                maintenance rule dialog's Item picker the case for select)
                and the popup still needs to clear it (ADR 0139). Either way
                the popup portals out of the sheet's own stacking context
                rather than nesting inside it, so its z-index is compared
                against the banner directly and clears it regardless of the
                sheet (ADR 0107). The sheet's own panel (components/ui/
                sheet.tsx) is z-70, above the banner and dropdown menus —
                only its backdrop is z-50 — and dialogs (components/ui/
                dialog.tsx, alert-dialog.tsx) sit at z-70/z-80, that same
                layer again or higher. Tooltips (components/ui/tooltip.tsx)
                sit at z-90, the topmost layer above all of these, because a
                tooltip can be anchored to a trigger inside any of them and
                must remain readable. */}
            <div className={cn('relative z-55', isFullscreenDashboard && 'pr-12')} data-testid="alarm-banner-stack">
              <LiveAlarmBanner
                onOpen={openAlarmsPanel}
                forecastWarnings={activeForecastWarning}
              />
            </div>

            <div ref={swipeContainerRef} data-testid="dashboard-swipe-surface" className="min-h-0 flex-1">
              {activePanel === null ? (
                <>
                  {pagesError && pages.length > 0 && <RefreshFailedOverlay reason={pagesError} onRetry={() => { void refetchPages() }} />}
                  {pagesError && pages.length === 0 && <LoadError reason={pagesError} hasRows={false} onRetry={() => { void refetchPages() }} className="px-1 pb-2" />}
                  {dashboardGrid}
                </>
              ) : (
                <div className="h-full min-h-0 overflow-y-auto rounded-lg border bg-card p-4">
                  {/* One boundary for every panel, not eight. `key={activePanel}`
                      forces a fresh Suspense instance on every panel switch, so
                      it never keeps a previous panel's boundary state - each
                      panel always gets its own fallback while its own chunk
                      loads, not whatever state the boundary was last left in. */}
                  <LazyBoundary resetKey={activePanel} onChunkError={versionWatch.reportChunkFailure}>
                    <Suspense key={activePanel} fallback={renderPanelFallback(panelFallbackLabel(activePanel))}>
                      {activePanelContent}
                    </Suspense>
                  </LazyBoundary>
                </div>
              )}
            </div>
          </div>
        </div>
      </SidebarInset>

      <LiveSignalKDiscoveryPrompt />


      <NavigationGuardDialog />

      {/* Lazy-loaded, and not mounted at all until first opened - see
          mateSheetHasOpenedRef above. `fallback={null}` is fine here: the
          sheet itself is a Sheet primitive that renders nothing (no overlay,
          no panel) until `open` is true, so there is nothing that should be
          showing while its chunk loads on this very first open. */}
      {mateSheetHasOpenedRef.current && (
        <LazyBoundary fallback={null} onChunkError={versionWatch.reportChunkFailure}>
        <Suspense fallback={null}>
          <MateSheet
            open={mateSheetOpen}
            onOpenChange={(open) => {
              setMateSheetOpen(open)
              if (!open) {
                setMateSheetQuestion(undefined)
                setMateSheetNewConversation(false)
                setMateSheetSpoken(false)
              }
            }}
            initialQuestion={mateSheetQuestion}
            newConversation={mateSheetNewConversation}
            spoken={mateSheetSpoken}
            screen={mateScreen}
            canWrite={canWrite}
            readAloud={assistantVoiceConfig.readAloud}
            onOpenPanel={(id) => {
              setMateSheetOpen(false)
              openMatePanel(id)
            }}
            onActiveConversationChange={setMateSheetConversationId}
            onHasDraftChange={setMateSheetHasDraft}
          />
        </Suspense>
        </LazyBoundary>
      )}

      {/* Same reasoning as the Mate sheet above - see helpSheetHasOpenedRef. */}
      {helpSheetHasOpenedRef.current && (
        <LazyBoundary fallback={null} onChunkError={versionWatch.reportChunkFailure}>
        <Suspense fallback={null}>
          <HelpSheet
            open={helpOpen}
            onOpenChange={setHelpOpen}
            target={helpTarget}
            onAskMate={(question) => {
              setHelpOpen(false)
              openMate(question)
            }}
          />
        </Suspense>
        </LazyBoundary>
      )}

      <Toaster isDarkTheme={isDarkTheme} />
      <UpdatedToast current={versionWatch.current} />
      <UpdateNotices watch={versionWatch} requestReload={requestReload} />
      <ReloadConfirmDialog
        open={reloadConfirmOpen}
        onCancel={() => setReloadConfirmOpen(false)}
        onConfirm={() => window.location.reload()}
      />
    </SidebarProvider>
  )
}
