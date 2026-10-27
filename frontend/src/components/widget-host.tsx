import { memo, useCallback } from 'react'

import { EmbedTile } from '@/components/embed-tile'
import { EngineClusterHost, GaugeGroupHost, LampStripHost } from '@/components/equipment-tile-hosts'
import { GaugeTile } from '@/components/gauge-tile'
import { HotWaterTile } from '@/components/hot-water-tile'
import {
  LiveAlternatorTile,
  LiveAnchorWatchTile,
  LiveAutopilotTile,
  LiveBatteryPowerTile,
  LiveClockTile,
  LiveCurrentConditionsTile,
  LiveCZoneSwitchesTile,
  LiveDepthTideTile,
  LiveForecastConditionsTile,
  LiveGeneratorTile,
  LiveNearbyVesselsTile,
  LivePoiMapTile,
  LivePositionTile,
  LiveRadarTargetsTile,
  LiveRouteTile,
  LiveSolarTile,
  LiveTanksTile,
  LiveTodayNowTile,
  LiveWindTile,
} from '@/components/live-surfaces'
import { MarineHeader } from '@/components/marine-header'
import type { DistanceUnits } from '@/config/app-config'
import {
  isClusterWidgetId,
  isEmbedWidgetId,
  isGaugeGroupWidgetId,
  isGaugeWidgetId,
  isLampStripWidgetId,
  isPoiMapWidgetId,
  type DashboardLayoutItem,
  type DashboardWidgetId,
} from '@/lib/dashboard-widgets'
import type { EquipmentTileContext } from '@/lib/equipment-tiles'

/**
 * The shell's callbacks a tile can call, in one object the shell builds once
 * (ADR 0215 phase 7). Every member is stable, so the object is too, and a
 * WidgetHost's memo never sees it change.
 */
export interface WidgetHostActions {
  /** Opens the config dialog for the widget with this id. */
  onConfigure: (id: DashboardWidgetId) => void
  onOpenAlarms: () => void
  onOpenForecast: () => void
  onOpenAnchorWatch: () => void
  /** Undefined on the wall display, where nobody can act on a roles link. */
  onOpenHouseBankRoles: (() => void) | undefined
  onOpenGeneratorRoles: (() => void) | undefined
  onAnchorImageryToggle: (on: boolean) => void
  onRadarEchoToggle: (on: boolean) => void
}

export interface WidgetHostProps {
  /** The widget record: its id, geometry and config. */
  widget: DashboardLayoutItem
  /** Layout editing: the tiles show their gear and stop navigating. */
  editing: boolean
  /** False on the wall display, where maps take no pointer input. */
  interactive: boolean
  isDarkTheme: boolean
  /** The page wears the instrument skin, so maps draw dark whatever the theme. */
  forceDark: boolean
  distanceUnits: DistanceUnits
  /** The session may not write: switches, the generator and the autopilot are read-only. */
  readOnly: boolean
  /** Items, profiles and reporting sources for the equipment tiles. */
  ctx: EquipmentTileContext
  showAnchorImagery: boolean
  showRadarEcho: boolean
  actions: WidgetHostActions
}

/**
 * One tile on a dashboard page: picks the component for the widget's kind and
 * hands it its config, the shell's flags and stable callbacks. Live values never
 * pass through here; each tile reads its own (ADR 0215).
 *
 * Memoized, so a shell render that changes nothing about this widget, its
 * flags or the shell's callbacks stops here. The page list keeps a widget's
 * record while the server answers it unchanged (use-dashboard-pages.ts), so a
 * write to one tile reaches only that tile.
 */
export const WidgetHost = memo(function WidgetHost({
  widget,
  editing,
  interactive,
  isDarkTheme,
  forceDark,
  distanceUnits,
  readOnly,
  ctx,
  showAnchorImagery,
  showRadarEcho,
  actions,
}: WidgetHostProps) {
  // One stable handler per widget id: the tiles below are memoized too, and
  // an arrow built here on every render would defeat them.
  const { onConfigure: configure } = actions
  const onConfigure = useCallback(() => configure(widget.id), [configure, widget.id])
  const { id } = widget
  if (isClusterWidgetId(id)) {
    if (!widget.cluster) return null
    return (
      <EngineClusterHost
        config={widget.cluster}
        ctx={ctx}
        editing={editing}
        onConfigure={onConfigure}
      />
    )
  }

  if (isLampStripWidgetId(id)) {
    if (!widget.lamps) return null
    return (
      <LampStripHost
        config={widget.lamps}
        ctx={ctx}
        editing={editing}
        onConfigure={onConfigure}
        onOpenAlarms={actions.onOpenAlarms}
      />
    )
  }

  if (isGaugeGroupWidgetId(id)) {
    if (!widget.gaugeGroup) return null
    return (
      <GaugeGroupHost
        config={widget.gaugeGroup}
        ctx={ctx}
        editing={editing}
        onConfigure={onConfigure}
      />
    )
  }

  if (isGaugeWidgetId(id)) {
    if (!widget.gauge) return null
    return (
      <GaugeTile
        config={widget.gauge}
        editing={editing}
        onConfigure={onConfigure}
      />
    )
  }

  if (isEmbedWidgetId(id)) {
    return (
      <EmbedTile
        config={widget.embed}
        editing={editing}
        onConfigure={onConfigure}
        isDarkTheme={isDarkTheme}
      />
    )
  }

  if (isPoiMapWidgetId(id)) {
    if (!widget.poiMap) return null
    return (
      <LivePoiMapTile
        config={widget.poiMap}
        editing={editing}
        onConfigure={onConfigure}
        isDarkTheme={isDarkTheme}
        forceDark={forceDark}
        distanceUnits={distanceUnits}
        interactive={interactive}
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
          isImperialDistance={distanceUnits === 'imperial'}
          onOpen={editing ? undefined : actions.onOpenForecast}
        />
      )
    case 'position':
      return (
        <LivePositionTile />
      )
    case 'today-now':
      return (
        <LiveTodayNowTile
          distanceUnits={distanceUnits}
          onOpen={editing ? undefined : actions.onOpenForecast}
        />
      )
    case 'clock':
      return <LiveClockTile />
    case 'current-conditions':
      return <LiveCurrentConditionsTile distanceUnits={distanceUnits} />
    case 'forecast-conditions':
      return <LiveForecastConditionsTile units={distanceUnits} />
    case 'anchor-watch':
      return (
        <LiveAnchorWatchTile
          isImperial={distanceUnits === 'imperial'}
          isDarkTheme={isDarkTheme}
          showImageryLayer={showAnchorImagery}
          onImageryToggle={actions.onAnchorImageryToggle}
          showRadarEcho={showRadarEcho}
          onRadarEchoToggle={actions.onRadarEchoToggle}
          onFullscreen={actions.onOpenAnchorWatch}
          interactive={interactive}
        />
      )
    case 'tanks':
      return <LiveTanksTile />
    case 'route':
      return <LiveRouteTile />
    case 'nearby-vessels':
      return <LiveNearbyVesselsTile distanceUnits={distanceUnits} />
    case 'radar-targets':
      return <LiveRadarTargetsTile distanceUnits={distanceUnits} />
    case 'battery-power':
      return <LiveBatteryPowerTile onOpenRoles={actions.onOpenHouseBankRoles} />
    case 'solar':
      return <LiveSolarTile />
    case 'alternator':
      return <LiveAlternatorTile />
    case 'generator':
      return <LiveGeneratorTile onOpenRoles={actions.onOpenGeneratorRoles} readOnly={readOnly} />
    case 'czone-switches':
      return <LiveCZoneSwitchesTile readOnly={readOnly} />
    case 'autopilot':
      return <LiveAutopilotTile readOnly={readOnly} />
    case 'hot-water':
      return <HotWaterTile />
    default:
      return null
  }
})
