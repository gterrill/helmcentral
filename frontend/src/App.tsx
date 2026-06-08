import { ArrowDown, ArrowUp, CloudSun, Compass } from 'lucide-react'
import { useEffect, useState } from 'react'

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
import { useElectricalState } from '@/hooks/use-electrical-state'
import { useNearbyVessels } from '@/hooks/use-nearby-vessels'
import { useAnchorWatch } from '@/hooks/use-anchor-watch'
import { usePlaceName } from '@/hooks/use-place-name'
import { useTanksState } from '@/hooks/use-tanks-state'
import { useTideToday } from '@/hooks/use-tide-today'
import { useVesselState } from '@/hooks/use-vessel-state'
import { useServerTrails } from '@/hooks/use-server-trails'
import { useWeatherForecast } from '@/hooks/use-weather-forecast'
import { useWeatherToday } from '@/hooks/use-weather-today'
import { useCZoneSwitches } from '@/hooks/use-czone-switches'
import { useDepthTrend } from '@/hooks/use-depth-trend'
import { anchorConfig, uiConfig } from '@/config/app-config'
import { Button } from '@/components/ui/button'
import { Tile } from '@/components/ui/tile'

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

  const discharging = hours < 0
  const totalMinutes = Math.max(0, Math.round(Math.abs(hours) * 60))
  const hh = Math.floor(totalMinutes / 60)
  const mm = totalMinutes % 60

  return discharging
    ? `~${hh}h ${mm.toString().padStart(2, '0')}m to empty`
    : `~${hh}h ${mm.toString().padStart(2, '0')}m to full`
}

export function App() {
  const [isDrawerOpen, setIsDrawerOpen] = useState(false)
  const [activeDrawerTab, setActiveDrawerTab] = useState('forecast')

  // Detect dark theme by watching the <html> class list
  const [isDarkTheme, setIsDarkTheme] = useState(() =>
    document.documentElement.classList.contains('dark'),
  )
  useEffect(() => {
    const observer = new MutationObserver(() => {
      setIsDarkTheme(document.documentElement.classList.contains('dark'))
    })
    observer.observe(document.documentElement, { attributes: true, attributeFilter: ['class'] })
    return () => observer.disconnect()
  }, [])
  const {
    depth,
    currentDriftKts,
    currentSetDeg,
    navigationState,
    latitude,
    longitude,
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
  } = useVesselState(uiConfig.vesselStateRefreshSeconds)
  const { vessels: nearbyVessels, loading: nearbyVesselsLoading } = useNearbyVessels(uiConfig.vesselStateRefreshSeconds)
  const { tanks, loading: tanksLoading } = useTanksState(uiConfig.vesselStateRefreshSeconds)
  const {
    batterySocPercent,
    chargingCurrentA,
    chargingPowerW,
    solarOutputW,
    acOutputW,
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
    loading: forecastLoading,
    error: forecastError,
    refetch: refetchForecast,
  } = useWeatherForecast(uiConfig.vesselStateRefreshSeconds)
  const anchorWatch = useAnchorWatch(latitude, longitude, navigationState, uiConfig.vesselStateRefreshSeconds)
  const { getSelfTrail, getAisTrails } = useServerTrails(5000)
  const placeName = usePlaceName(latitude, longitude, uiConfig.vesselStateRefreshSeconds)
  const depthTrendPoints = useDepthTrend('2h', 60)
  const { switches: czoneSwitches, loading: czoneLoading, pending: czonePending, toggleSwitch: toggleCZone } = useCZoneSwitches(5)
  const isImperialDistance = uiConfig.distanceUnits === 'imperial'
  const depthValue =
    depth !== null
      ? isImperialDistance
        ? (depth * 3.28084).toFixed(1)
        : depth.toFixed(1)
      : '—'
  const depthUnitLabel = isImperialDistance ? 'feet' : 'metres'
  const awaLabel = windAngleApparentDeg !== null ? `${Math.round(windAngleApparentDeg).toString().padStart(3, '0')}°` : '---°'
  const headingLabel = formatHeading(headingTrue)
  const gust10mLabel = maxGust10mKts !== null ? `${maxGust10mKts.toFixed(1)} kts` : '—'
  const gust1hLabel = maxGust1hKts !== null ? `${maxGust1hKts.toFixed(1)} kts` : '—'
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
  const dc24vVoltageLabel = dc24vVoltageV !== null ? dc24vVoltageV.toFixed(2) : '—'
  const chargeRateLabel = batteryRatePercentPerHour !== null
    ? `${batteryRatePercentPerHour >= 0 ? '+' : ''}${batteryRatePercentPerHour.toFixed(1)}%/hr`
    : '—'
  const timeToGoLabel = formatTimeToGo(timeToGoHours)
  const timeToGoClass = timeToGoHours !== null && timeToGoHours < 0 ? 'text-amber-600' : 'text-secondary'
  const drawerTitle = activeDrawerTab === 'radar'
    ? 'Radar'
    : activeDrawerTab === 'settings'
      ? 'Settings'
    : activeDrawerTab === 'wind'
      ? 'Wind'
      : activeDrawerTab === 'tides'
        ? 'Tides'
        : activeDrawerTab === 'anchor-watch'
          ? 'Anchor Watch'
          : 'Forecast'

  return (
    <div className="min-h-screen p-4 pb-20 md:p-6 md:pb-24">
      <div className="mx-auto flex max-w-[1800px] flex-col gap-4">
        <MarineHeader />

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
              <div className="grid place-items-center rounded-xl border bg-background/70 p-4">
                <div className="w-full max-w-[320px] lg:max-w-[360px]">
                  <div className="mx-auto aspect-square w-full max-w-[280px] lg:max-w-[320px]">
                    <WindCompass
                      headingTrue={headingTrue}
                      windAngleApparentDeg={windAngleApparentDeg}
                      windSide={windSide}
                      windAngleRelativeDeg={windAngleRelativeDeg}
                      windSpeedKts={windSpeedApparentKts}
                    />
                  </div>
                  <div className="mt-3 grid grid-cols-2 gap-2">
                    <div className="rounded-md border bg-background/70 px-3 py-2 text-left">
                      <p className="text-[10px] uppercase tracking-[0.16em] text-muted-foreground">Max Gust 10m</p>
                      <p className="font-display text-lg text-primary">{gust10mLabel}</p>
                    </div>
                    <div className="rounded-md border bg-background/70 px-3 py-2 text-left">
                      <p className="text-[10px] uppercase tracking-[0.16em] text-muted-foreground">Max Gust 1h</p>
                      <p className="font-display text-lg text-primary">{gust1hLabel}</p>
                    </div>
                  </div>
                </div>
              </div>
            </section>

            <Tile title="Position">
              <div className="mt-2 flex items-start justify-between gap-4">
                <div>
                  <p className="font-mono text-sm">{formatCoordinate(latitude, true)}</p>
                  <p className="font-mono text-sm">{formatCoordinate(longitude, false)}</p>
                </div>
                <p className="shrink-0 font-mono text-sm text-muted-foreground">HDG {headingLabel}</p>
              </div>
              <div className="mt-3 truncate rounded-md bg-secondary/10 px-3 py-2 font-display text-2xl text-secondary">
                {placeName ?? '—'}
              </div>
            </Tile>
            <Tile title="Depth">
              <div className="mt-2 flex items-center gap-4">
                <p className="shrink-0 font-display text-6xl text-secondary">
                  {depthValue}
                  <span className="ml-2 align-baseline text-xl text-muted-foreground">{depth !== null ? depthUnitLabel : 'unavailable'}</span>
                </p>
                {(navigationState === 'anchored' || navigationState === 'moored') && (
                  <DepthSparkline
                    points={depthTrendPoints}
                    isImperial={isImperialDistance}
                    className="min-w-0 flex-1"
                  />
                )}
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
                  </div>
                </div>

                <div className="my-3 h-px bg-border/70" />

                <div className="grid grid-cols-[auto_1fr] gap-3">
                  <div>
                    <p className="text-[11px] uppercase tracking-[0.16em] text-muted-foreground">Tide Now</p>
                    <p className="mt-1 font-display text-5xl leading-none text-secondary">
                      {tide.current_tide_height_ft >= 0 ? tide.current_tide_height_ft.toFixed(1) : '—'}
                      <span className="ml-1 align-baseline text-lg text-muted-foreground">ft</span>
                    </p>
                    <p className="mt-1 inline-flex items-center gap-1 text-xs font-semibold text-secondary">
                      <ArrowUp className="h-3.5 w-3.5" />
                      {tide.tide_direction}
                    </p>
                  </div>
                  <div className="space-y-2 pt-1 text-sm">
                    <p className="inline-flex items-center gap-1 text-foreground">
                      <ArrowUp className="h-4 w-4 text-secondary" />
                      High Today {new Date(tide.high_tide_time).toLocaleTimeString('en-US', { hour: 'numeric', minute: '2-digit' })}
                      <span className="text-muted-foreground">{tide.high_tide_height_ft >= 0 ? `${tide.high_tide_height_ft.toFixed(1)}ft` : '—ft'}</span>
                    </p>
                    <p className="inline-flex items-center gap-1 text-foreground">
                      <ArrowDown className="h-4 w-4 text-amber-600" />
                      Low Today {new Date(tide.low_tide_time).toLocaleTimeString('en-US', { hour: 'numeric', minute: '2-digit' })}
                      <span className="text-muted-foreground">{tide.low_tide_height_ft >= 0 ? `${tide.low_tide_height_ft.toFixed(1)}ft` : '—ft'}</span>
                    </p>
                  </div>
                </div>
              </Tile>
            </div>
          </aside>

          <aside className="space-y-4">
            {navigationState === 'motoring' && (
              <AlternatorTile
                port={alternator0}
                starboard={alternator1}
              />
            )}

            {navigationState !== 'motoring' && (
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
                  <p className="text-[11px] uppercase tracking-[0.16em] text-muted-foreground">Solar</p>
                  <p className="font-display text-4xl leading-none text-primary">
                    {solarOutputLabel}
                    <span className="ml-1 text-xl text-muted-foreground">W</span>
                  </p>
                </div>
                <div className="rounded-md border bg-background/60 px-3 py-2">
                  <p className="text-[11px] uppercase tracking-[0.16em] text-muted-foreground">AC Draw</p>
                  <p className="font-display text-4xl leading-none text-primary">
                    {acOutputLabel}
                    <span className="ml-1 text-xl text-muted-foreground">W</span>
                  </p>
                </div>
              </div>

              <div className="mt-2 rounded-md border bg-background/60 px-3 py-2">
                <p className="text-[11px] uppercase tracking-[0.16em] text-muted-foreground">Time To Go / Charge Rate</p>
                <p className={`mt-1 font-display text-4xl leading-none ${timeToGoClass}`}>
                  {timeToGoLabel} {chargeRateLabel}
                </p>
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

              <div className="mt-2 rounded-md border bg-background/60 px-3 py-2">
                <div className="flex items-start justify-between gap-3">
                  <div>
                    <p className="text-[11px] uppercase tracking-[0.16em] text-muted-foreground">8PM Forecast</p>
                    <p className="mt-1 text-xs text-muted-foreground">-7m generator needed</p>
                  </div>
                  <p className="font-display text-4xl leading-none text-secondary">94%</p>
                </div>
              </div>
            </Tile>

            <GeneratorTile
              generatorState={generatorState}
              generatorManualStart={generatorManualStart}
              generatorManualStartTimer={generatorManualStartTimer}
              generatorRunningByCondition={generatorRunningByCondition}
              generatorRuntime={generatorRuntime}
              generatorRealPowerW={generatorRealPowerW}
            />
            <TanksTile tanks={tanks} loading={tanksLoading} />
            <CZoneSwitchesTile switches={czoneSwitches} loading={czoneLoading} pending={czonePending} onToggle={toggleCZone} />
          </aside>
        </div>

        {/* Bottom Drawers */}
        <BottomDrawer
          isOpen={isDrawerOpen}
          onOpen={() => setIsDrawerOpen(true)}
          onClose={() => setIsDrawerOpen(false)}
          title={drawerTitle}
          tabs={[
            { id: 'forecast', label: 'Forecast' },
            { id: 'tides', label: 'Tides' },
            { id: 'wind', label: 'Wind' },
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
              loading={forecastLoading}
              error={forecastError}
              onRetry={refetchForecast}
              unit={uiConfig.distanceUnits as 'imperial' | 'metric'}
            />
            </div>
          )}
          {activeDrawerTab === 'tides' && (
            <div className="px-6 py-4">
              <div className="py-8 text-center text-muted-foreground">Tide details coming soon</div>
            </div>
          )}
          {activeDrawerTab === 'wind' && (
            <div className="px-6 py-4">
              <div className="py-8 text-center text-muted-foreground">Wind details coming soon</div>
            </div>
          )}
          {activeDrawerTab === 'radar' && (
            <div className="px-6 py-4">
              <RadarDrawer latitude={latitude} longitude={longitude} />
            </div>
          )}
          {activeDrawerTab === 'settings' && (
            <div className="px-6 py-4">
              <SignalKSettingsPanel />
            </div>
          )}
          {activeDrawerTab === 'anchor-watch' && (
            anchorWatch.anchorLat !== null && anchorWatch.anchorLon !== null && latitude !== null && longitude !== null
              ? (
                <AnchorWatchDrawer
                  vesselLat={latitude}
                  vesselLon={longitude}
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
                  onAnchorReposition={anchorWatch.updatePosition}
                  onRadiusChange={anchorWatch.updateRadius}
                  onClearAnchor={anchorWatch.clearAnchor}
                  isImperial={isImperialDistance}
                />
              )
              : <div className="px-6 py-8 text-center text-muted-foreground">No anchor watch active</div>
          )}
        </BottomDrawer>
      </div>
    </div>
  )
}
