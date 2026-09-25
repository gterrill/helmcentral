import {
  Anchor,
  BellRing,
  BookOpen,
  CircleHelp,
  CloudSun,
  FileText,
  LayoutDashboard,
  Mic,
  MicOff,
  Radar as RadarIcon,
  Route,
  Settings,
  Sparkles,
  MonitorPlay,
  Wrench,
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
import { ForecastConditionsTile } from '@/components/forecast-conditions-tile'
import { WindTile } from '@/components/wind-tile'
import { MarineHeader } from '@/components/marine-header'
import { VesselStatusBar } from '@/components/vessel-status-bar'
import { AlarmBanner } from '@/components/alarm-banner'
import { NearbyVesselsTile } from '@/components/nearby-vessels-tile'
import { RadarTargetsTile } from '@/components/radar-targets-tile'
// Not lazy: prefetchNoteEditor() is a plain function that only reaches into
// the editor-vendor chunk through a dynamic import() of its own (see that
// file's doc comment) — importing the function itself here costs nothing.
import { prefetchNoteEditor } from '@/components/note-editor'
import type { SettingsPageHandle } from '@/components/settings/settings-page'
import type { DocumentDetailsPageHandle } from '@/components/document-details-page'
import type { InventoryPanelHandle } from '@/components/inventory/inventory-panel'
import type { SettingsSectionId } from '@/components/settings/settings-nav'
import type { InventorySectionId } from '@/components/inventory/inventory-nav'

const AlarmsDrawer = lazy(() => import('@/components/alarms-drawer').then((mod) => ({ default: mod.AlarmsDrawer })))
const AnchorWatchDrawer = lazy(() => import('@/components/anchor-watch-drawer').then((mod) => ({ default: mod.AnchorWatchDrawer })))
const AssistantDrawer = lazy(() => import('@/components/assistant-drawer').then((mod) => ({ default: mod.AssistantDrawer })))
const DocumentsPanel = lazy(() => import('@/components/documents-panel').then((mod) => ({ default: mod.DocumentsPanel })))
const DocumentDetailsPage = lazy(() => import('@/components/document-details-page').then((mod) => ({ default: mod.DocumentDetailsPage })))
const InventoryPanel = lazy(() => import('@/components/inventory/inventory-panel').then((mod) => ({ default: mod.InventoryPanel })))
const ForecastDrawer = lazy(() => import('@/components/forecast-drawer').then((mod) => ({ default: mod.ForecastDrawer })))
const RadarDrawer = lazy(() => import('@/components/radar-drawer').then((mod) => ({ default: mod.RadarDrawer })))
const RoutePlannerDrawer = lazy(() => import('@/components/route-planner-drawer').then((mod) => ({ default: mod.RoutePlannerDrawer })))
const SettingsPage = lazy(() => import('@/components/settings/settings-page').then((mod) => ({ default: mod.SettingsPage })))
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
import { DashboardBentoGrid } from '@/components/dashboard-bento-grid'
import { LayoutModeToggle } from '@/components/layout-mode-toggle'
import { LayoutToolbar } from '@/components/layout-toolbar'
import { EmptyPagePrompt } from '@/components/empty-page-prompt'
import type { AddTileMultiInstanceEntry } from '@/components/add-tile-picker'
import { Toaster } from '@/components/ui/sonner'
import { useRoutes } from '@/hooks/use-routes'
import { useDashboardRouteId } from '@/hooks/use-dashboard-route'
import { useDashboardPages, type DashboardPage, type CreatePageInit } from '@/hooks/use-dashboard-pages'
import { useDashboardRibbon } from '@/hooks/use-dashboard-ribbon'
import { useActiveDashboardPageId } from '@/hooks/use-active-dashboard-page'
import { useDisplayRotation, type UseDisplayRotationResult } from '@/hooks/use-display-rotation'
import { useDisplayRemote } from '@/hooks/use-display-remote'
import { useDisplays } from '@/hooks/use-displays'
import { parseDisplayOptions, displayFoldPx, displayRowMargin, DEFAULT_DWELL_SECONDS, DISPLAY_RECOVERY_POLL_MS } from '@/lib/displays'
import { nextWaypoint, traversalOrder, computeClockTripEta } from '@/lib/next-waypoint'
import { DisplayShell } from '@/components/display-shell'
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
  inventoryEditorClosedBy,
  inventoryWorkClosedBy,
  type AppLocation,
  type PanelId,
} from '@/lib/app-location'
import { screenContextFor } from '@/lib/mate-screen'
import { helpTargetFor, type HelpTarget } from '@/lib/help-links'
import { cn } from '@/lib/utils'

/**
 * Ordered the way the boat is actually run, not the order the panels were
 * built in: what you want at a glance underway first (Alarms, Anchor Watch,
 * Forecast, Radar, Routes), then the reference material you go and look
 * something up in (Documents, Inventory, Mate), then the rows you touch once
 * and leave alone (Wall displays, Settings). ADR 0123 put Inventory right
 * after Documents rather than after Routes - the equipment registry links
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
const PANEL_NAV_ITEMS: Array<{ id: PanelId; label: string; icon: typeof CloudSun }> = [
  { id: 'alarms', label: 'Alarms', icon: BellRing },
  { id: 'anchor-watch', label: 'Anchor Watch', icon: Anchor },
  { id: 'forecast', label: 'Forecast', icon: CloudSun },
  { id: 'radar', label: 'Radar', icon: RadarIcon },
  { id: 'routes', label: 'Routes', icon: Route },
  { id: 'documents', label: 'Documents', icon: FileText },
  { id: 'inventory', label: 'Inventory', icon: Wrench },
  { id: 'assistant', label: 'Mate', icon: Sparkles },
  { id: 'wall-displays', label: 'Wall displays', icon: MonitorPlay },
  { id: 'settings', label: 'Settings', icon: Settings },
]

const ANCHOR_IMAGERY_ENABLED_KEY = 'anchorWatch.imagery.enabled'
const ANCHOR_RADAR_ECHO_ENABLED_KEY = 'anchorWatch.radarEcho.enabled'

// ADR 0124: the max wait requestIdleCallback (or its setTimeout fallback)
// is given before firing the note editor's own chunk prefetch regardless of
// how busy the browser stays - generous, since nothing on screen is waiting
// on this the way embed-tile.tsx's own mount deferral is.
const NOTE_EDITOR_PREFETCH_IDLE_TIMEOUT_MS = 2000

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
  // ADR 0115 §2 review finding: the Details page holds its own explicit
  // Save/Discard draft (document-details-page.tsx), and used to let any
  // navigation throw it away unasked. This is that page's half of exactly
  // the same guard settingsDirty/settingsPageRef already give Settings,
  // generalized below rather than reinvented.
  const [documentDetailsDirty, setDocumentDetailsDirty] = useState(false)
  const documentDetailsPageRef = useRef<DocumentDetailsPageHandle>(null)
  // ADR 0123: the Equipment editor's own half of the same guard, wired
  // through InventoryPanel the same way documentDetailsDirty/
  // documentDetailsPageRef wire through DocumentDetailsPage directly -
  // InventoryPanel forwards whichever child (EquipmentEditor) is actually
  // mounted, see its own imperative handle.
  const [inventoryDirty, setInventoryDirty] = useState(false)
  // Release-fixes code-review finding: Stocktake's scan events/live NFC
  // session and the bin page's quick-add draft, reported the same way
  // inventoryDirty is (onDirtyChange) - see requestWithinInventory's own
  // comment for why this shares that guard rather than getting a second one.
  const [inventoryHasWork, setInventoryHasWork] = useState(false)
  // Wording for what inventoryHasWork actually holds (BinQuickAdd's own
  // onHasWorkChange doc comment) - only meaningful alongside inventoryHasWork
  // true, cleared by the same effect that clears inventoryHasWork itself
  // below. Read at the moment a navigation is stashed (stashPendingNavigation
  // below), not live by the dialog - see pendingNavigationDetail's own
  // comment for why.
  const [inventoryWorkDetail, setInventoryWorkDetail] = useState<string | null>(null)
  const inventoryPanelRef = useRef<InventoryPanelHandle>(null)
  const [pendingNavigation, setPendingNavigation] = useState<(() => void) | null>(null)
  // Release-fixes code-review finding: which dialog form (pendingNavigationKind)
  // and, for a quick-add-work reason, what to say (pendingNavigationDetail) -
  // captured by stashPendingNavigation at the moment a navigation is stashed,
  // not recomputed from live state while the dialog is open. Recomputing
  // it live used to work only because nothing was ever assumed to change
  // inventoryDirty/inventoryHasWork/inventorySection while the underlying
  // page sits inert behind the modal - true for a page the operator can no
  // longer click into, false for an async save already in flight when the
  // dialog opened (BinQuickAdd's own Save, kicked off just before Back was
  // pressed): its success still lands and clears inventoryHasWork, which
  // used to flip the dialog from Leave/Stay to the "Unsaved changes"/Save and
  // Continue form for a page that was never dirty in that sense - and whose
  // ref (inventoryPanelRef) has no editor mounted to save with Save and
  // Continue's own onClick.
  const [pendingNavigationKind, setPendingNavigationKind] = useState<'dirty' | 'stocktake-work' | 'quick-add-work'>('dirty')
  const [pendingNavigationDetail, setPendingNavigationDetail] = useState<string | null>(null)
  const [isSavingBeforeNavigate, setIsSavingBeforeNavigate] = useState(false)
  const [saveAndContinueError, setSaveAndContinueError] = useState<string | null>(null)

  // requestNavigate/requestBackFromDocumentDetails/requestWithinInventory and
  // their shared stashPendingNavigation/inventoryPendingReason/
  // dirtyPageLabel/handleSaveAndContinue live further down (after
  // inventoryNewEquipmentPreset) - inventoryPendingReason needs
  // inventorySection, declared there, and `const` bindings can't be read
  // before that declaration runs.

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

  // Hoisted ahead of useDarkMode (rather than left beside displayOptions
  // below, where it used to live) so the wall dark-theme override just below
  // has it in scope. isDisplay depends only on activePanel, which is already
  // set by this point, so nothing about moving it changes what it means.
  const isDisplay = activePanel === 'display'
  const [storedIsDarkTheme, toggleDarkMode] = useDarkMode()
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
  const { routes, loading: routesLoading, error: routesError, createRoute, updateRoute, deleteRoute } = useRoutes()
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
  // ADR 0110: the wall display records themselves. Fetched here (not inside
  // the wall branch further down) because the ordinary shell needs the list
  // too — the sidebar's display group, the layout toolbar's display picker
  // and duplicate-to-display popover, and the displays-management dialog all
  // render regardless of which route is active.
  const { displays, loading: displaysLoading, error: displaysError, refetch: refetchDisplays, createDisplay, updateDisplay, deleteDisplay } = useDisplays()
  const [activePageId, setActivePageId] = useActiveDashboardPageId(pages, initialLocation.pageId, !isDisplay)
  const activePage = pages.find((p) => p.id === activePageId) ?? null
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
  // rather than app state, so it's parsed once here the same way
  // initialLocation is, and never written back to the URL — see the early
  // returns in the sync effect and the popstate handler below. isDisplay
  // itself is declared earlier, beside useDarkMode, so the wall dark-theme
  // override there can read it.
  const [displayOptions] = useState(() => parseDisplayOptions(globalThis.location?.search ?? ''))
  // Read off initialLocation, not activePanel/the live location bar, for the
  // same reason displayOptions is parsed once: the wall never navigates, so
  // its slug can only ever be the one it was loaded with. Deliberately no
  // fallback to "the first display" when the slug is missing or unknown —
  // that would silently put one display's geometry on another screen, the
  // masking fallback AGENTS.md forbids. The wall branch below renders an
  // explicit diagnostic instead.
  const wallDisplay = isDisplay ? displays.find((d) => d.slug === initialLocation.displaySlug) ?? null : null
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
  // ADR 0115 §2: which document's Details page is open (the
  // `/documents/<id>` route, app-location.ts's documentEditId), or null for
  // the ordinary listing. Same precedent as ADR 0112's wallDisplaysSlug just
  // below - one panel owning an index/editor split, seeded from the deep
  // link the same way.
  const [documentsEditId, setDocumentsEditId] = useState<string | null>(initialLocation.documentEditId ?? null)

  // documentDetailsDirty (declared up with settingsDirty) is only meaningful
  // while the Details page is actually mounted and reporting it via
  // onDirtyChange - mirrors settingsDirty's own clearing effect above, kept
  // as a separate effect down here (rather than folded into that one)
  // because it needs documentsEditId, which isn't declared until this point
  // in the component. "No longer rendered" is leaving the 'documents' panel
  // entirely OR documentsEditId going back to null - both unmount
  // DocumentDetailsPage (see documentsLeftOnceRef's own comment below on
  // why documentsEditId, not just activePanel, decides what's on screen
  // here), and DocumentDetailsPage stops calling onDirtyChange the moment it
  // unmounts, so nothing else would ever reset this otherwise.
  useEffect(() => {
    if (activePanel !== 'documents' || documentsEditId === null) {
      setDocumentDetailsDirty(false)
    }
  }, [activePanel, documentsEditId])

  // Revision "one panel, not three" (2026-09-20): which section (a document
  // node in a Manual-kind folder's tree) Documents has open in its reading
  // pane, mirrored into `?section=` the same way documentsFolderId mirrors
  // into `?folder=` - continuously, since picking a section is exactly as
  // bookmarkable/shareable as opening one used to be on the deleted
  // /manuals route. A note itself (unfiled or filed) opens through the
  // existing `?document=` deep link below instead of a field of its own -
  // it's a document with kind='note', addressed the same way any other one
  // is.
  const [documentsSectionId, setDocumentsSectionId] = useState<string | null>(initialLocation.documentSectionId ?? null)
  // ADR 0112: which display the management panel is editing, or null for its
  // index. Seeded from the deep link the same way documentsFolderId is.
  const [wallDisplaysSlug, setWallDisplaysSlug] = useState<string | null>(initialLocation.displayEditSlug ?? null)
  // ADR 0123: the Inventory panel's own section (the InventoryNav shape,
  // same contract as settingsSection) and, within the Equipment section, its
  // index/editor split - same precedent as documentsEditId/wallDisplaysSlug
  // just above. inventoryCreatingEquipment is the "New item" draft
  // (app-location.ts's own doc comment on equipmentEditId): it never has a
  // URL of its own, so it's local App state rather than something
  // applyAppLocation/the sync effect below ever reads from or writes to a
  // parsed location - a page reload always resolves to a real id or the
  // index, never mid-draft.
  const [inventorySection, setInventorySection] = useState<InventorySectionId>(initialLocation.inventorySection ?? 'equipment')
  const [inventoryEquipmentEditId, setInventoryEquipmentEditId] = useState<string | null>(initialLocation.equipmentEditId ?? null)
  const [inventoryCreatingEquipment, setInventoryCreatingEquipment] = useState(false)
  // ADR 0127: the Locations section's bin page - `/inventory/bins/<code>`,
  // what a tag tap or a Locations-section click opens. null is "not on a
  // bin page" (the ordinary Locations index), the same convention
  // wallDisplaysSlug/documentsEditId use for their own "nothing open" state.
  const [inventoryBinCode, setInventoryBinCode] = useState<string | null>(initialLocation.binCode ?? null)
  // ADR 0127: the bin/zone the bin page's "Full item" button last asked
  // for - local UI state, like inventoryCreatingEquipment above, never
  // serialised to the URL (a "New item" draft has none of its own either
  // way). Read once by EquipmentEditor when a brand new draft mounts
  // (InventoryPanel's own newEquipmentPreset prop).
  const [inventoryNewEquipmentPreset, setInventoryNewEquipmentPreset] = useState<{ zoneId?: string; binId?: string } | null>(null)

  // Stashes a navigation the same way every guard below does, capturing which
  // dialog form to show (and, for quick-add-work, what to say) at THIS
  // moment - see pendingNavigationKind/pendingNavigationDetail's own comment
  // on why that has to happen here rather than being read live off
  // inventoryDirty/inventoryHasWork by the dialog itself.
  const stashPendingNavigation = useCallback((
    navigate: () => void,
    kind: 'dirty' | 'stocktake-work' | 'quick-add-work',
    detail: string | null = null,
  ) => {
    setPendingNavigationKind(kind)
    setPendingNavigationDetail(detail)
    setPendingNavigation(() => navigate)
  }, [])

  // Which of inventoryDirty's dirty-editor reason or inventoryHasWork's two
  // has-work reasons is actually true right now, for whichever guard below
  // is about to stash a navigation while activePanel === 'inventory'. Pulled
  // out into its own function (rather than inlined at each of the three call
  // sites that need it) purely so it can be called at each of them without
  // repeating the same three-way branch.
  const inventoryPendingReason = useCallback((): { kind: 'dirty' | 'stocktake-work' | 'quick-add-work'; detail: string | null } => {
    if (!inventoryDirty && inventoryHasWork) {
      return inventorySection === 'stocktake'
        ? { kind: 'stocktake-work', detail: null }
        : { kind: 'quick-add-work', detail: inventoryWorkDetail }
    }
    return { kind: 'dirty', detail: null }
  }, [inventoryDirty, inventoryHasWork, inventorySection, inventoryWorkDetail])

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
      stashPendingNavigation(navigate, 'dirty')
      return false
    }
    // ADR 0115 §2's Details-page guard, generalized from the Settings guard
    // above: any navigation to a DIFFERENT panel while a dirty Details page
    // is open stashes it the same way. Navigating to 'documents' itself is
    // deliberately not caught here - the one call site that actually leaves
    // Details while staying on the 'documents' panel (the page's own
    // breadcrumb Back, and a Back/Forward landing on a different Details id
    // or the bare listing) has no "leaving documents" signal for this check
    // to key off, so it goes through its own small guard instead
    // (requestBackFromDocumentDetails, right below).
    if (activePanel === 'documents' && targetPanel !== 'documents' && documentDetailsDirty) {
      stashPendingNavigation(navigate, 'dirty')
      return false
    }
    // ADR 0123: same generalization a third time, for a dirty Equipment
    // editor. Navigating to 'inventory' itself is deliberately not caught
    // here for the same reason documents' own check above isn't - switching
    // sections or going back to the index while staying on the 'inventory'
    // panel has no "leaving inventory" signal for this check to key off, so
    // it goes through requestWithinInventory instead.
    // Release-fixes code-review finding: this used to check inventoryDirty
    // alone - leaving Inventory from the sidebar (or any other panel) while
    // Stocktake or the bin page's quick-add held real work went straight
    // through with no prompt, the same gap onSectionChange/onOpenBin had
    // before requestWithinInventory picked up inventoryHasWork below.
    if (activePanel === 'inventory' && targetPanel !== 'inventory' && (inventoryDirty || inventoryHasWork)) {
      const reason = inventoryPendingReason()
      stashPendingNavigation(navigate, reason.kind, reason.detail)
      return false
    }
    navigate()
    return true
  }, [activePanel, settingsDirty, documentDetailsDirty, inventoryDirty, inventoryHasWork, inventoryPendingReason, stashPendingNavigation])

  // The page's own breadcrumb Back (document-details-page.tsx's onBack) and
  // a Back/Forward that changes which Details page - or none - is open both
  // stay on the 'documents' panel, so requestNavigate's targetPanel check
  // above can never see a difference to catch for either of them. Same
  // stash-and-return-false shape as requestNavigate, just without the panel
  // comparison it can't use here.
  const requestBackFromDocumentDetails = useCallback((navigate: () => void): boolean => {
    if (documentDetailsDirty) {
      stashPendingNavigation(navigate, 'dirty')
      return false
    }
    navigate()
    return true
  }, [documentDetailsDirty, stashPendingNavigation])

  // ADR 0123's equivalent of requestBackFromDocumentDetails above - covers
  // both ways an Equipment editor can close without changing `activePanel`:
  // InventoryNav switching to a different section, and the editor's own
  // Back/onCreated/onDeleted (all routed through InventoryPanel's
  // onEquipmentEditIdChange/onCreatingEquipmentChange props, wired at the
  // 'inventory' case below).
  // Release-fixes code-review finding: this used to guard only inventoryDirty
  // (the Equipment editor) - Stocktake's own scan events/live NFC session and
  // the bin page's quick-add draft can hold just as much work an operator
  // would not want silently cleared, and nothing routed Open/Full item
  // through this at all. inventoryHasWork is their shared "has work" flag
  // (StocktakeSection/BinQuickAdd's own onHasWorkChange, wired below), and
  // guarding it here - the same stash-and-let-the-dialog-decide shape
  // inventoryDirty already used - covers both without a second guard
  // function. The dialog itself shows whichever form stashPendingNavigation
  // captured (pendingNavigationKind, by the dialog below).
  const requestWithinInventory = useCallback((navigate: () => void): boolean => {
    if (inventoryDirty || inventoryHasWork) {
      const reason = inventoryPendingReason()
      stashPendingNavigation(navigate, reason.kind, reason.detail)
      return false
    }
    navigate()
    return true
  }, [inventoryDirty, inventoryHasWork, inventoryPendingReason, stashPendingNavigation])

  // Which dirty page pendingNavigation (if any) is guarding, for the
  // dialog's copy and for handleSaveAndContinue below - derived from
  // activePanel rather than stored alongside the stashed navigate() itself.
  // The three dirty pages are mutually exclusive (activePanel is exactly one
  // panel at a time), and activePanel stays exactly where it was when
  // requestNavigate/requestBackFromDocumentDetails/requestWithinInventory
  // stashed the navigation, right up until the dialog resolves one way or
  // the other. Unlike pendingNavigationKind/pendingNavigationDetail, this one
  // is safe to read live: activePanel cannot change while the underlying
  // page is still on screen (behind the modal), only inventoryDirty/
  // inventoryHasWork/inventorySection can, from an async completion - which
  // is exactly the race those two are captured against instead.
  const dirtyPageLabel = activePanel === 'settings' ? 'Settings' : activePanel === 'inventory' ? 'Inventory' : 'Details'

  const handleSaveAndContinue = useCallback(async () => {
    setIsSavingBeforeNavigate(true)
    setSaveAndContinueError(null)
    try {
      if (activePanel === 'settings') {
        await settingsPageRef.current?.save()
      } else if (activePanel === 'inventory') {
        await inventoryPanelRef.current?.save()
      } else {
        await documentDetailsPageRef.current?.save()
      }
      pendingNavigation?.()
      setPendingNavigation(null)
    } catch (err) {
      // Stay on the page so the user can fix it and retry. Every page
      // renders its own error banner too, but this dialog is modal and
      // covers it - without repeating the reason here, a rejected save
      // (e.g. POST /api/settings refusing an unreachable SignalK address, a
      // document PATCH rejecting a duplicate title, or an equipment PUT
      // rejecting a zone/bin that disagree) looks like the button simply
      // did nothing.
      setSaveAndContinueError(err instanceof Error ? err.message : `Unable to save the ${dirtyPageLabel} page`)
    } finally {
      setIsSavingBeforeNavigate(false)
    }
  }, [pendingNavigation, activePanel, dirtyPageLabel])

  // inventoryDirty (declared up with settingsDirty) is only meaningful while
  // the Equipment editor is actually mounted and reporting it via
  // onDirtyChange - same reasoning and shape as documentDetailsDirty's own
  // clearing effect just above, kept separate for the same reason: it needs
  // state that isn't declared until this point. "No longer rendered" is
  // leaving the 'inventory' panel entirely, leaving the Equipment section,
  // OR both equipmentEditId and creatingEquipment going back to their
  // nothing-open state - any of those unmounts EquipmentEditor, which stops
  // calling onDirtyChange the moment it does.
  useEffect(() => {
    if (
      activePanel !== 'inventory'
      || inventorySection !== 'equipment'
      || (inventoryEquipmentEditId === null && !inventoryCreatingEquipment)
    ) {
      setInventoryDirty(false)
    }
  }, [activePanel, inventorySection, inventoryEquipmentEditId, inventoryCreatingEquipment])
  // inventoryHasWork/inventoryWorkDetail (declared up with inventoryDirty)
  // are only meaningful while Stocktake or the bin page's quick-add are
  // actually mounted and reporting them - same reasoning as inventoryDirty's
  // own clearing effect just above. Leaving the 'inventory' panel, leaving
  // Stocktake, or leaving the bin page (inventoryBinCode back to null)
  // unmounts whichever of the two was reporting, which stops calling
  // onHasWorkChange the moment it does.
  useEffect(() => {
    if (
      activePanel !== 'inventory'
      || !(inventorySection === 'stocktake' || (inventorySection === 'locations' && inventoryBinCode !== null))
    ) {
      setInventoryHasWork(false)
      setInventoryWorkDetail(null)
    }
  }, [activePanel, inventorySection, inventoryBinCode])
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
  // documentsEditId check, DocumentsPanel unmounting for the Details page
  // and remounting on the way back (activePanelContent switches between two
  // different component types under the one Suspense boundary - see the
  // comment on that Suspense below) would still see the latch un-tripped
  // and hand the fresh mount initialLocation.documentId again, reopening a
  // Mate attachment chip's ?document= viewer the operator had already seen
  // and dismissed before ever visiting Details.
  const documentsLeftOnceRef = useRef(false)
  if (activePanel !== 'documents' || documentsEditId !== null) documentsLeftOnceRef.current = true
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
    onQuestion: openMate,
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
      setActivePageId(knownPageId ?? firstDashboardPageId(pages))
    }
    if (loc.panel === 'settings') {
      setSettingsSection(loc.section ?? 'general')
    }
    if (loc.panel === 'assistant') {
      setMatePanelConversationId(loc.conversationId ?? null)
    }
    if (loc.panel === 'wall-displays') {
      setWallDisplaysSlug(loc.displayEditSlug ?? null)
    }
    if (loc.panel === 'documents') {
      setDocumentsFolderId(loc.documentFolderId ?? null)
      setDocumentsEditId(loc.documentEditId ?? null)
      setDocumentsSectionId(loc.documentSectionId ?? null)
    }
    if (loc.panel === 'inventory') {
      setInventorySection(loc.inventorySection ?? 'equipment')
      setInventoryEquipmentEditId(loc.equipmentEditId ?? null)
      // A location-driven change always resolves to a real id or the index,
      // never a mid-draft "New item" - see inventoryCreatingEquipment's own
      // doc comment above.
      setInventoryCreatingEquipment(false)
      setInventoryBinCode(loc.binCode ?? null)
    }
  }, [pages, pagesLoading, setActivePageId])

  // The single writer of window.location (ADR 0074). Chosen over pushing at
  // each of the ~10 existing setActivePanel call sites (sidebar, page
  // sub-items, breadcrumb, tile onOpen, alarm banner, anchor-watch
  // auto-close) because it needs no call-site churn and covers programmatic
  // changes too (e.g. the active page disappearing out from under a user).
  useEffect(() => {
    if (!shellVisible) return
    // /display/<slug> owns its own query string (just `?page=`) rather than
    // app state, and never navigates anywhere else - writing to history here
    // would fight the device's fixed URL for no benefit to anyone looking at
    // a screen with no back button.
    if (isDisplay) return
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

    const ctx = { firstPageId: firstDashboardPageId(pages), knownPageIds: pagesLoading ? null : pages.map((p) => p.id), canAdmin }
    const firstPageChanged = previousFirstPageIdRef.current !== ctx.firstPageId
    previousFirstPageIdRef.current = ctx.firstPageId
    const next = formatAppLocation({
      panel: activePanel,
      pageId: activePageId,
      section: settingsSection,
      conversationId: activePanel === 'assistant' ? matePanelConversationId : null,
      displayEditSlug: activePanel === 'wall-displays' ? wallDisplaysSlug : null,
      documentFolderId: activePanel === 'documents' ? documentsFolderId : null,
      documentEditId: activePanel === 'documents' ? documentsEditId : null,
      documentSectionId: activePanel === 'documents' ? documentsSectionId : null,
      // inventoryCreatingEquipment never reaches here - a "New item" draft
      // has no URL of its own (its own doc comment above), so the bar shows
      // the Equipment index until the operator actually saves.
      inventorySection: activePanel === 'inventory' ? inventorySection : undefined,
      equipmentEditId: activePanel === 'inventory' ? inventoryEquipmentEditId : null,
      binCode: activePanel === 'inventory' ? (inventoryBinCode ?? undefined) : undefined,
    }, ctx)
    // documents is the one panel whose canonical URL can carry a query
    // string (?folder=/?document=/?section=) - pathname alone is never
    // enough to tell it apart from its own bare path, so this compares
    // against pathname+search (harmless everywhere else: no other
    // panel/location ever has one).
    const path = window.location.pathname + window.location.search
    if (next === path) return // popstate, or a clean deep link, already put us here

    // Normalise (replace) rather than add a history entry for a path this
    // effect didn't itself write — that only ever happens on first load, or
    // when the page list/admin role resolve to something that makes the
    // current bar non-canonical.
    const replace = first || firstPageChanged || !isCanonicalAppPath(path, { firstPageId: ctx.firstPageId, knownPageIds: ctx.knownPageIds, canAdmin })
    window.history[replace ? 'replaceState' : 'pushState'](null, '', next)
  }, [
    shellVisible, isDisplay, activePanel, activePageId, settingsSection, matePanelConversationId,
    documentsFolderId, documentsEditId, documentsSectionId, wallDisplaysSlug,
    inventorySection, inventoryEquipmentEditId, inventoryBinCode, pages, pagesLoading, canAdmin,
  ])

  // Handles Back/Forward. Goes through requestNavigate so a dirty Settings
  // (or, ADR 0115 §2, a dirty Documents Details) page still gets to veto the
  // navigation exactly as a sidebar click would - window.location has
  // already moved to the previous entry by the time this fires, so without
  // the re-push below, a guarded Back would leave the bar on the
  // destination while the guard dialog (and the dirty page itself) stay on
  // screen.
  useEffect(() => {
    const handlePopState = () => {
      if (!shellVisible) return
      // /display/<slug> never pushes or replaces history (see the sync
      // effect above), so there is nothing here for it to react to; a bare
      // `return` also means the device's own back/forward gestures, if it
      // has any, don't fight the fixed URL it was launched with.
      if (isDisplay) return
      // Documents (?folder=) is the one location whose canonical form needs
      // the query string too - see the sync effect above's own comment.
      const path = window.location.pathname + window.location.search
      const ctx = { firstPageId: firstDashboardPageId(pages), knownPageIds: pagesLoading ? null : pages.map((p) => p.id), canAdmin }
      const parsed = parseAppLocation(path)
      if (!isCanonicalAppPath(path, ctx)) {
        // So the sync effect's own equality check holds once it runs off
        // the state change requestNavigate is about to (maybe) apply.
        window.history.replaceState(null, '', formatAppLocation(parsed, ctx))
      }
      // ADR 0115 §2 review finding: a Back/Forward that stays on the
      // 'documents' panel but changes (or clears) which Details page is
      // open never differs in `targetPanel` the way every other navigation
      // this guard covers does - requestNavigate's panel-level check can't
      // see it, for exactly the same reason the page's own breadcrumb Back
      // can't (see requestBackFromDocumentDetails's own comment). This is
      // the single most common way Back actually leaves a dirty Details
      // page (opening it always pushes a new entry over the listing, so one
      // Back press lands here, not on some other panel), so it's routed
      // through that same small guard instead of requestNavigate.
      const leavingDetailsWithinDocuments = activePanel === 'documents' && parsed.panel === 'documents'
        && (parsed.documentEditId ?? null) !== documentsEditId
      // ADR 0123: the same "stays on the panel, so requestNavigate's own
      // targetPanel check can't see it" case a third time - a Back/Forward
      // that takes the Equipment editor off screen. The comparison itself
      // lives in app-location.ts (inventoryEditorClosedBy) because getting it
      // right needs the "New item" draft, which has no URL, and a section
      // switch, which leaves the record id untouched on both sides; see that
      // function's own note on the two holes an id-only check left.
      const leavingInventoryEditorWithinInventory = activePanel === 'inventory'
        && inventoryEditorClosedBy(
          {
            section: inventorySection,
            equipmentEditId: inventoryEquipmentEditId,
            creating: inventoryCreatingEquipment,
          },
          parsed,
        )
      // Release-fixes code-review finding: this popstate handler only ever
      // asked inventoryEditorClosedBy (the Equipment editor's own case) -
      // Back off Stocktake or a bin page's quick-add draft never asked
      // inventoryWorkClosedBy anything at all, so it fell all the way through
      // to requestNavigate, whose own targetPanel check can't see a same-
      // panel move either. inventoryWorkClosedBy is that function's exact
      // counterpart for inventoryHasWork - see its own doc comment
      // (app-location.ts).
      const leavingInventoryWorkWithinInventory = activePanel === 'inventory'
        && inventoryWorkClosedBy(
          { section: inventorySection, binCode: inventoryBinCode },
          parsed,
        )
      const navigated = leavingDetailsWithinDocuments
        ? requestBackFromDocumentDetails(() => applyAppLocation(parsed))
        : (leavingInventoryEditorWithinInventory || leavingInventoryWorkWithinInventory)
          ? requestWithinInventory(() => applyAppLocation(parsed))
          : requestNavigate(parsed.panel, () => applyAppLocation(parsed))
      if (!navigated) {
        // Guarded: the browser already moved off whichever page is actually
        // still on screen (Settings, a dirty Documents Details page, or a
        // dirty Equipment editor), so push its own URL back - the bar has to
        // agree with what's still rendered while the confirmation dialog is
        // up. Built the same way the sync effect above builds `next`, rather
        // than hardcoded to Settings, now that this guard also covers the
        // other two.
        window.history.pushState(null, '', formatAppLocation({
          panel: activePanel,
          pageId: activePageId,
          section: settingsSection,
          conversationId: activePanel === 'assistant' ? matePanelConversationId : null,
          displayEditSlug: activePanel === 'wall-displays' ? wallDisplaysSlug : null,
          documentFolderId: activePanel === 'documents' ? documentsFolderId : null,
          documentEditId: activePanel === 'documents' ? documentsEditId : null,
          inventorySection: activePanel === 'inventory' ? inventorySection : undefined,
          equipmentEditId: activePanel === 'inventory' ? inventoryEquipmentEditId : null,
          binCode: activePanel === 'inventory' ? (inventoryBinCode ?? undefined) : undefined,
        }, ctx))
      }
    }
    window.addEventListener('popstate', handlePopState)
    return () => window.removeEventListener('popstate', handlePopState)
  }, [
    shellVisible, isDisplay, requestNavigate, requestBackFromDocumentDetails, requestWithinInventory, applyAppLocation,
    activePanel, activePageId, settingsSection, matePanelConversationId, wallDisplaysSlug,
    documentsFolderId, documentsEditId, inventorySection, inventoryEquipmentEditId, inventoryBinCode, pages, pagesLoading, canAdmin,
  ])

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
    windSpeedTrueKts,
    windAngleTrueDeg,
    windSideTrue,
    windAngleTrueRelativeDeg,
    windDirectionTrueDeg,
    maxGustKts,
    maxGustTrueKts,
    maxTrueWindKts1h,
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
    nextHour: forecastNextHour,
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
  // most of the wall rotation, has nowhere for a trail to go. Gate the poll
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
  // The clock wall-display tile's trip-ETA line (ADR 0092, ADR 0125):
  // computeClockTripEta (lib/next-waypoint.ts) picks between a
  // Helmcentral-activated route's FINAL waypoint (etaToRouteEnd) and a bare
  // Course API destination (etaToDestination, GET /api/routes/active's
  // `destination`, backend/route_activation.go) - the latter covers both an
  // inactive chartplotter go-to AND a route activated somewhere other than
  // Helmcentral, so the tile still reports something rather than showing
  // nothing just because the trip wasn't planned inside Helmcentral. See
  // that function's own doc comment for the full decision.
  const clockTripEta = useMemo(
    () => computeClockTripEta(routeActivationStatus, routes, latitude, longitude, speedOverGroundKts, new Date()),
    [routeActivationStatus, routes, latitude, longitude, speedOverGroundKts],
  )
  // The Nearby map's route layer (this cycle's own addition): the same
  // routeActivationStatus/routes/nextWaypoint pieces as clockTripEta
  // above, combined once here so the poi-map tile never has to know route
  // activation exists - it only draws whatever waypoints and next-index it's
  // handed. null whenever no route is active, its id isn't in `routes`, or
  // the route has no waypoints. `waypoints` is traversalOrder's array, not
  // necessarily route.waypoints' authored order, since nextIndex is an index
  // into that traversal order (see next-waypoint.ts's own comment on why a
  // reversed activation makes the two differ).
  const activeRoute = useMemo(() => {
    if (!routeActivationStatus || routeActivationStatus.state !== 'active' || routeActivationStatus.routeId === null) return null
    const route = routes.find((r) => r.id === routeActivationStatus.routeId)
    if (!route) return null
    const waypoint = nextWaypoint(route, routeActivationStatus)
    const traversal = traversalOrder(route, routeActivationStatus)
    if (!waypoint || !traversal) return null
    return { name: route.name, waypoints: traversal, nextIndex: waypoint.index }
  }, [routeActivationStatus, routes])
  const depthTrend = useDepthTrend('3h', 60)
  // Item B: only the czone-switches widget reads this; poll it only while
  // the active page (or the wall's current page) actually has one placed.
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
          activeRoute={activeRoute}
          gnssCriticalAlert={gnssCriticalAlert}
          positionLastUpdateAgeS={positionLastUpdateAgeS}
          nearbyVessels={nearbyVessels}
          aisCollisionAlarms={aisCollisionAlarms}
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
          <WindTile
            lastUpdateAgeS={windLastUpdateAgeS}
            headingTrue={headingTrue}
            windAngleApparentDeg={windAngleApparentDeg}
            windSide={windSide}
            windAngleRelativeDeg={windAngleRelativeDeg}
            windSpeedApparentKts={windSpeedApparentKts}
            windSpeedTrueKts={windSpeedTrueKts}
            windAngleTrueDeg={windAngleTrueDeg}
            windSideTrue={windSideTrue}
            windAngleTrueRelativeDeg={windAngleTrueRelativeDeg}
            windDirectionTrueDeg={windDirectionTrueDeg}
            currentSetDeg={currentSetDeg}
            currentDriftKts={currentDriftKts}
            currentDriftImpactKts={currentDriftImpactKts}
            maxGustKts={maxGustKts}
            maxGustTrueKts={maxGustTrueKts}
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
            tripEta={clockTripEta}
          />
        )
      case 'current-conditions':
        return (
          <CurrentConditionsTile
            depth={depth}
            depthLastUpdateAgeS={depthLastUpdateAgeS}
            windSpeedTrueKts={windSpeedTrueKts}
            windDirectionTrueDeg={windDirectionTrueDeg}
            maxTrueWindKts1h={maxTrueWindKts1h}
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
            interactive={!isDisplay}
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
    if (created) setWallDisplaysSlug(created.slug)
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
          onManageDisplays={() => requestNavigate('wall-displays', () => setActivePanel('wall-displays'))}
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
            forecastWarnings={activeForecastWarning}
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
      case 'wall-displays': {
        // ADR 0112: the index and the per-display editor are one panel, the
        // same way DocumentsPanel owns its own folder drill-down. Which one
        // renders is the presence of a slug, and the editor resolves it
        // itself rather than App keeping a second piece of state in step.
        const editing = wallDisplaysSlug !== null
          ? displays.find((d) => d.slug === wallDisplaysSlug) ?? null
          : null
        if (wallDisplaysSlug !== null) {
          return (
            <DisplayEditorPanel
              display={editing}
              pages={pages}
              displays={displays}
              onBack={() => setWallDisplaysSlug(null)}
              onUpdate={async (id, patch) => {
                const updated = await updateDisplay(id, patch)
                // The route names the display by slug, and the slug is one
                // of the fields this panel edits: without following it, a
                // rename resolves to nothing and the editor flips to "could
                // not be found" on a display that is fine.
                if (updated && updated.id === editing?.id) setWallDisplaysSlug(updated.slug)
                return updated
              }}
              onDelete={async (id) => {
                const released = await deleteDisplay(id)
                // Without this the released pages keep a stale display_id
                // and show in neither the Dashboard list nor any display
                // until a reload.
                if (released) await refetchPages()
                return released
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
              onOpenPage={(id) => requestNavigate(null, () => {
                setActivePanel(null)
                setActivePageId(id)
                setLayoutEditing(true)
              })}
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
              if (target) setWallDisplaysSlug(target.slug)
            }}
            onCreateDisplay={() => { void handleCreateDisplay() }}
            onOpenHelp={openHelp}
            canWrite={canWrite}
          />
        )
      }
      case 'documents': {
        // ADR 0115 §2: the listing and the Details page are one
        // panel, the same index/editor split ADR 0112's wall-displays branch
        // above already uses - which one renders is just the presence of
        // documentsEditId, resolved by the Details page itself rather than
        // App keeping a second piece of resolved-document state in step.
        if (documentsEditId !== null) {
          return (
            <DocumentDetailsPage
              ref={documentDetailsPageRef}
              documentId={documentsEditId}
              onDirtyChange={setDocumentDetailsDirty}
              // Routed through requestBackFromDocumentDetails rather than a
              // bare setDocumentsEditId(null) - review finding: this call
              // site stays on the 'documents' panel, so requestNavigate's
              // own targetPanel check (used everywhere else) would never
              // catch a dirty Details page being left this way.
              onBack={() => { requestBackFromDocumentDetails(() => setDocumentsEditId(null)) }}
            />
          )
        }
        const documentDeepLinkId = documentsLeftOnceRef.current ? null : (initialLocation.documentId ?? null)
        return (
          <DocumentsPanel
            initialFolderId={documentsFolderId}
            onFolderChange={setDocumentsFolderId}
            initialDocumentId={documentDeepLinkId}
            onEditDocument={setDocumentsEditId}
            initialSectionId={documentsSectionId}
            onSectionChange={setDocumentsSectionId}
            onOpenHelp={openHelp}
            // ADR 0120: the toolbar's Mate failure line links straight to
            // Settings → Assistant, the same requestNavigate wiring
            // AssistantDrawer's own "Open Mate settings" button uses below.
            onOpenAssistantSettings={() => requestNavigate('settings', () => {
              setSettingsSection('assistant')
              setActivePanel('settings')
            })}
            // Ask Mate about a selection (ask-mate-selection.tsx): same
            // openMate the header's Sparkles button and SettingsPage's own
            // onAskMate already call, always starting a fresh conversation.
            onAskMate={openMate}
          />
        )
      }
      case 'inventory':
        return (
          <InventoryPanel
            ref={inventoryPanelRef}
            activeSectionId={inventorySection}
            onSectionChange={(id) => { requestWithinInventory(() => setInventorySection(id)) }}
            equipmentEditId={inventoryEquipmentEditId}
            creatingEquipment={inventoryCreatingEquipment}
            // Opening an item or starting a new one enters the editor, so
            // there is no draft to discard yet and nothing to guard. Both
            // callbacks are reachable from OUTSIDE the Equipment section too
            // - the bin page's own item rows/"Full item" button and
            // Stocktake's photo grid (ADR 0127) - so both have to switch
            // inventorySection to 'equipment' and clear inventoryBinCode
            // themselves, not just set the id/creating flag. Review finding:
            // InventoryPanel only ever renders the editor when
            // activeSectionId === 'equipment' (its own showEquipmentEditor
            // check), so pressing Open from a bin page used to set
            // inventoryEquipmentEditId while activeSectionId stayed
            // 'locations' - nothing appeared, and the bin's own binCode
            // stayed set underneath a URL that had already moved to
            // /inventory/equipment/<id>. Clearing binCode here is what makes
            // the sync effect below produce that URL as a NEW history entry
            // rather than leaving the bar disagreeing with what's on screen,
            // so the browser's own Back returns to the bin.
            // Release-fixes code-review finding: these used to set section/
            // id state directly, with no guard at all - reachable not just
            // from the Equipment index (nothing to lose there) but from the
            // bin page's own item rows/"Full item" and Stocktake's photo
            // grid, where switching straight to 'equipment' silently threw
            // away a stocktake pass's scans or a staged quick-add.
            // requestWithinInventory now also checks inventoryHasWork (its
            // own comment above), so this is a no-op everywhere neither
            // reason applies - which is everywhere except those two cases.
            onOpenEquipment={(id) => {
              requestWithinInventory(() => {
                setInventorySection('equipment')
                setInventoryBinCode(null)
                setInventoryEquipmentEditId(id)
              })
            }}
            onNewEquipment={(preset) => {
              requestWithinInventory(() => {
                setInventorySection('equipment')
                setInventoryBinCode(null)
                setInventoryNewEquipmentPreset(preset ?? null)
                setInventoryCreatingEquipment(true)
              })
            }}
            // Back is the only exit that can throw away typed work, so it is
            // the only one guarded - and it is ONE guarded call that clears
            // both pieces of state together. Two calls would not work:
            // requestWithinInventory stashes a single pending navigation, so
            // the second would overwrite the first and Discard would run a
            // no-op with the dirty editor still open.
            onCloseEditor={() => {
              requestWithinInventory(() => {
                setInventoryEquipmentEditId(null)
                setInventoryCreatingEquipment(false)
              })
            }}
            // A create or delete that already succeeded has nothing left to
            // discard, and the editor is still reporting dirty at the moment
            // it calls these (its draft is never re-baselined - it unmounts
            // or reloads instead). Routing them through the guard therefore
            // popped "unsaved changes" straight after a successful save, and
            // on a delete offered a Save that would PUT to a record the
            // server had just dropped.
            onEquipmentCreated={(id) => {
              setInventoryDirty(false)
              setInventoryCreatingEquipment(false)
              setInventoryEquipmentEditId(id)
            }}
            onEquipmentDeleted={(message) => {
              setInventoryDirty(false)
              setInventoryEquipmentEditId(null)
              setInventoryCreatingEquipment(false)
              // 2026-09-25 amendment: the item is gone either way - message
              // is only ever the server's own "the item was deleted, but a
              // photo file/document could not be removed" warning, shown on
              // the destination (the Equipment index) rather than keeping
              // the editor open on a record that no longer exists.
              if (message) toast.error(message)
            }}
            onDirtyChange={setInventoryDirty}
            onHasWorkChange={(work, detail) => {
              setInventoryHasWork(work)
              setInventoryWorkDetail(detail ?? null)
            }}
            onOpenHelp={openHelp}
            canWrite={canWrite}
            binCode={inventoryBinCode}
            // Opening a bin, like opening an equipment item, never discards
            // anything by itself - but it CAN leave a dirty Equipment
            // editor on screen if the operator navigates to a bin from
            // there (a Locations-section click, or a deep link), so it
            // still goes through the same guard onSectionChange uses.
            onOpenBin={(code) => {
              requestWithinInventory(() => {
                setInventorySection('locations')
                setInventoryBinCode(code)
              })
            }}
            // Release-fixes code-review finding: this used to be a plain
            // setter on the theory that the bin page holds no draft to
            // discard - true of the Equipment editor, but not of the
            // quick-add form living on this same page (ADR 0127), which can
            // hold a staged name/photos or a photo still queued for Retry.
            // Routed through the same guard onOpenBin/onSectionChange use.
            onCloseBin={() => { requestWithinInventory(() => { setInventoryBinCode(null) }) }}
            newEquipmentPreset={inventoryNewEquipmentPreset}
          />
        )
      case 'settings':
        return (
          <SettingsPage
            ref={settingsPageRef}
            onDirtyChange={setSettingsDirty}
            activeSectionId={settingsSection}
            onSectionChange={setSettingsSection}
            onOpenHelp={openHelp}
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
            {initialLocation.displaySlug
              ? `No display is configured at "${initialLocation.displaySlug}".`
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
      <DisplayShell display={wallDisplay} alarms={alarms}>
        {pagesError ? (
          // Distinct from the empty-feed message below: a failed fetch is a
          // wall that cannot see its pages, not a wall with none assigned,
          // and the rotation is already polling its way back.
          <div className="flex h-full items-center justify-center text-sm text-muted-foreground">
            Could not load dashboard pages. Retrying…
          </div>
        ) : displayFeedEmpty && !displayOptions.pageId ? (
          <div className="flex h-full items-center justify-center text-sm text-muted-foreground">
            No pages are assigned to {wallDisplay.name} yet.
          </div>
        ) : (
          dashboardGrid
        )}
        <DisplayRemoteController rotation={displayRotation} onAction={bumpRemoteAnnouncement} />
        <DisplayRemoteToast
          announcementId={remoteAnnouncementId}
          pageName={activePage?.name ?? ''}
          position={displayRotation.position}
          paused={displayRotation.paused}
        />
      </DisplayShell>
    )
  }

  // ADR 0110: a page assigned to a display leaves the Dashboard sub-list
  // (and the header's DashboardPageSwitcher below) for the sidebar's own
  // "Wall displays" group, nested under the display it's actually on.
  const dashboardSubListPages = pages.filter((p) => !p.display_id)

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
                      onClick={() => requestNavigate(null, () => {
                        setActivePanel(null)
                        setActivePageId(page.id)
                      })}
                    >
                      <span className="min-w-0 flex-1 truncate">{page.name}</span>
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
                    control is absent rather than present-but-inert. */}
                {canEditLayout && (
                  <LayoutModeToggle editing={layoutEditing} onToggle={() => setLayoutEditing((prev) => !prev)} />
                )}
              </>
            )}
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
              onClick={() => openHelp(helpTargetFor({ panel: activePanel, section: settingsSection }))}
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
              <AlarmBanner
                alarms={alarms}
                onOpen={openAlarmsPanel}
                onAcknowledge={acknowledgeAlarm}
                forecastWarnings={activeForecastWarning}
              />
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
          {pendingNavigationKind === 'dirty' ? (
            <>
              <AlertDialogHeader>
                <AlertDialogTitle>Unsaved changes</AlertDialogTitle>
                <AlertDialogDescription>
                  {/* dirtyPageLabel: 'Settings', 'Details' (ADR 0115 §2) or
                      'Inventory' (ADR 0123) - whichever page's guard actually
                      stashed this navigation. */}
                  You have unsaved changes on the {dirtyPageLabel} page. Save them before leaving, or discard them?
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
            </>
          ) : (
            // Release-fixes code-review finding: Stocktake's scans and a
            // staged quick-add have nothing to Save - offering that button
            // anyway (or the "Unsaved changes" copy, which implies one) would
            // be offering an action that does not exist. Leave/Stay instead,
            // worded for what is actually about to be cleared.
            <>
              <AlertDialogHeader>
                <AlertDialogTitle>
                  {pendingNavigationKind === 'stocktake-work' ? 'Leave stocktake?' : 'Leave this bin?'}
                </AlertDialogTitle>
                <AlertDialogDescription>
                  {pendingNavigationKind === 'stocktake-work'
                    ? 'The scans from this pass will be cleared.'
                    // Release-fixes code-review finding: falls back to the
                    // generic copy only when BinQuickAdd didn't give a more
                    // specific one (pendingNavigationDetail, captured by
                    // stashPendingNavigation) - a photo still queued for
                    // Retry after a partial-failure save leaves the form's
                    // own name/photo fields empty, so "the name and photos
                    // you have added" would be describing nothing.
                    : (pendingNavigationDetail ?? 'The name and photos you have added will be cleared.')}
                </AlertDialogDescription>
              </AlertDialogHeader>
              <AlertDialogFooter>
                <AlertDialogCancel onClick={() => setPendingNavigation(null)}>Stay</AlertDialogCancel>
                <AlertDialogAction
                  onClick={() => {
                    pendingNavigation?.()
                    setPendingNavigation(null)
                  }}
                >
                  Leave
                </AlertDialogAction>
              </AlertDialogFooter>
            </>
          )}
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

      {/* Same reasoning as the Mate sheet above - see helpSheetHasOpenedRef. */}
      {helpSheetHasOpenedRef.current && (
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
      )}

      <Toaster isDarkTheme={isDarkTheme} />
    </SidebarProvider>
  )
}
