import { ArrowDown, ArrowUp, CloudSun, Compass } from 'lucide-react'
import { useState } from 'react'

import { AnchorWatchTile } from '@/components/anchor-watch-tile'
import { MarineHeader } from '@/components/marine-header'
import { NearbyVesselsTile } from '@/components/nearby-vessels-tile'
import { RodeScopeTile } from '@/components/rode-scope-tile'
import { RadarDrawer } from '@/components/radar-drawer'
import { SignalKSettingsPanel } from '@/components/signalk-settings-panel'
import { TanksTile } from '@/components/tanks-tile'
import { BottomDrawer } from '@/components/ui/bottom-drawer'
import { ForecastDrawer } from '@/components/forecast-drawer'
import { useElectricalState } from '@/hooks/use-electrical-state'
import { useNearbyVessels } from '@/hooks/use-nearby-vessels'
import { useAnchorWatch } from '@/hooks/use-anchor-watch'
import { useTanksState } from '@/hooks/use-tanks-state'
import { useTideToday } from '@/hooks/use-tide-today'
import { useVesselState } from '@/hooks/use-vessel-state'
import { useWeatherForecast } from '@/hooks/use-weather-forecast'
import { useWeatherToday } from '@/hooks/use-weather-today'
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
  if (hours === null || !Number.isFinite(hours) || hours < 0) {
    return '—'
  }

  const totalMinutes = Math.max(0, Math.round(hours * 60))
  const hh = Math.floor(totalMinutes / 60)
  const mm = totalMinutes % 60

  return `~${hh}h ${mm.toString().padStart(2, '0')}m to full`
}

export function App() {
  const [isDrawerOpen, setIsDrawerOpen] = useState(false)
  const [activeDrawerTab, setActiveDrawerTab] = useState('forecast')
  const {
    depth,
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
    dc12vCurrentA,
    dc24vVoltageV,
    acLoadsW,
    chargeRatePercentPerHour,
    timeToFullHours,
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
  const isImperialDistance = uiConfig.distanceUnits === 'imperial'
  const depthValue =
    depth !== null
      ? isImperialDistance
        ? (depth * 3.28084).toFixed(1)
        : depth.toFixed(1)
      : '—'
  const depthUnitLabel = isImperialDistance ? 'feet' : 'metres'
  const awaLabel = windAngleApparentDeg !== null ? `${Math.round(windAngleApparentDeg).toString().padStart(3, '0')}°` : '---°'
  const windSideLabel = windSide ? windSide.toUpperCase() : '—'
  const relativeAngleLabel = windAngleRelativeDeg !== null ? `${Math.round(windAngleRelativeDeg)}°` : '—'
  const apparentWindSpeedLabel = windSpeedApparentKts !== null ? Math.round(windSpeedApparentKts).toString() : '—'
  const gust10mLabel = maxGust10mKts !== null ? `${maxGust10mKts.toFixed(1)} kts` : '—'
  const gust1hLabel = maxGust1hKts !== null ? `${maxGust1hKts.toFixed(1)} kts` : '—'
  const socLabel = batterySocPercent !== null ? Math.round(batterySocPercent).toString() : '—'
  const socBarWidth = `${Math.max(0, Math.min(100, batterySocPercent ?? 0))}%`
  const chargingCurrentLabel = chargingCurrentA !== null ? `+${chargingCurrentA.toFixed(1)}` : '—'
  const chargingPowerLabel = chargingPowerW !== null ? `+${Math.round(chargingPowerW)}` : '—'
  const solarOutputLabel = solarOutputW !== null ? Math.round(solarOutputW).toString() : '—'
  const acOutputLabel = acOutputW !== null ? Math.round(acOutputW).toString() : '—'
  const dc12vPowerLabel = dc12vPowerW !== null ? Math.round(dc12vPowerW).toString() : '—'
  const dc12vCurrentLabel = dc12vCurrentA !== null ? dc12vCurrentA.toFixed(1) : '—'
  const dc24vVoltageLabel = dc24vVoltageV !== null ? dc24vVoltageV.toFixed(2) : '—'
  const acLoadsLabel = acLoadsW !== null ? `${Math.round(acLoadsW)}W` : '—'
  const chargeRateLabel = chargeRatePercentPerHour !== null ? `+${chargeRatePercentPerHour.toFixed(1)}%/hr` : '—'
  const timeToGoLabel = formatTimeToGo(timeToFullHours)
  const drawerTitle = activeDrawerTab === 'radar'
    ? 'Radar'
    : activeDrawerTab === 'settings'
      ? 'Settings'
    : activeDrawerTab === 'wind'
      ? 'Wind'
      : activeDrawerTab === 'tides'
        ? 'Tides'
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
                  <div className="relative mx-auto h-[240px] w-[240px] rounded-full border-2 border-border bg-card shadow-inner lg:h-[280px] lg:w-[280px]">
                    <div className="absolute inset-0 grid place-items-center">
                      <div className="text-center">
                        <p className="font-display text-xl text-primary lg:text-2xl">
                          {windSideLabel} {relativeAngleLabel}
                        </p>
                        <p className="font-display text-6xl leading-none text-primary lg:text-7xl">
                          {apparentWindSpeedLabel}
                          <span className="ml-1 align-baseline text-2xl text-muted-foreground lg:text-3xl">kts</span>
                        </p>
                      </div>
                    </div>
                    <div className="absolute left-1/2 top-6 h-[96px] w-[2px] -translate-x-1/2 bg-secondary lg:h-[112px]" />
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

            <Tile title="Depth">
              <p className="mt-2 font-display text-6xl text-secondary">
                {depthValue}
                <span className="ml-2 align-baseline text-xl text-muted-foreground">{depth !== null ? depthUnitLabel : 'unavailable'}</span>
              </p>
            </Tile>
            <Tile title="Position">
              <p className="mt-2 font-mono text-sm">{formatCoordinate(latitude, true)}</p>
              <p className="font-mono text-sm">{formatCoordinate(longitude, false)}</p>
              <div className="mt-3 rounded-md bg-secondary/10 px-3 py-2 font-display text-2xl text-secondary">{formatHeading(headingTrue)}</div>
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
            <AnchorWatchTile
              watch={anchorWatch}
              lat={latitude}
              lon={longitude}
              isImperial={isImperialDistance}
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

            <NearbyVesselsTile vessels={nearbyVessels} loading={nearbyVesselsLoading} distanceUnits={uiConfig.distanceUnits} />
          </aside>

          <aside className="space-y-4">
            <Tile title="Battery & Power">
              <div className="mt-1 grid grid-cols-[minmax(0,1.2fr)_minmax(0,1fr)] gap-2">
                <div className="flex min-w-0 items-end gap-2 rounded-md border bg-background/60 px-3 py-3">
                  <span className="font-display text-6xl leading-none tabular-nums text-primary md:text-7xl">{socLabel}</span>
                  <span className="shrink-0 pb-2 text-2xl leading-none text-foreground md:text-3xl">%</span>
                </div>
                <div className="min-w-0 rounded-md border bg-background/60 px-3 py-3">
                  <p className="text-[11px] uppercase tracking-[0.16em] text-muted-foreground">Charging</p>
                  <p className="font-display text-4xl leading-none text-secondary md:text-5xl">{chargingCurrentLabel}A</p>
                  <p className="mt-1 font-display text-3xl leading-none text-secondary md:text-4xl">{chargingPowerLabel}W</p>
                </div>
              </div>

              <div className="mt-2 h-1.5 rounded-full bg-muted/60">
                <div className="h-full rounded-full bg-primary" style={{ width: socBarWidth }} />
              </div>

              <div className="mt-3 grid grid-cols-2 gap-2 text-sm">
                <div className="rounded-md border bg-background/60 px-3 py-2">
                  <p className="text-[11px] uppercase tracking-[0.16em] text-muted-foreground">Solar Output</p>
                  <p className="font-display text-4xl leading-none text-primary">
                    {solarOutputLabel}
                    <span className="ml-1 text-xl text-muted-foreground">W</span>
                  </p>
                </div>
                <div className="rounded-md border bg-background/60 px-3 py-2">
                  <p className="text-[11px] uppercase tracking-[0.16em] text-muted-foreground">AC Output</p>
                  <p className="font-display text-4xl leading-none text-primary">
                    {acOutputLabel}
                    <span className="ml-1 text-xl text-muted-foreground">W</span>
                  </p>
                </div>
              </div>

              <div className="mt-2 grid grid-cols-2 gap-2 text-sm">
                <div className="rounded-md border bg-background/60 px-3 py-2">
                  <p className="text-[11px] uppercase tracking-[0.16em] text-muted-foreground">24V DC Power</p>
                  <p className="font-display text-4xl leading-none text-foreground">
                    {dc12vPowerLabel}
                    <span className="ml-1 text-xl text-muted-foreground">W</span>
                    <span className="ml-3 text-2xl text-muted-foreground">{dc12vCurrentLabel}A</span>
                  </p>
                </div>
                <div className="rounded-md border bg-background/60 px-3 py-2">
                  <p className="text-[11px] uppercase tracking-[0.16em] text-muted-foreground">24V Voltage</p>
                  <p className="font-display text-4xl leading-none text-secondary">
                    {dc24vVoltageLabel}
                    <span className="ml-1 text-xl text-muted-foreground">V</span>
                  </p>
                </div>
              </div>

              <div className="mt-2 rounded-md border bg-background/60 px-3 py-2">
                <div className="flex items-end justify-between">
                  <p className="text-[11px] uppercase tracking-[0.16em] text-muted-foreground">Emporia AC Loads</p>
                  <p className="font-display text-3xl leading-none text-primary">{acLoadsLabel}</p>
                </div>
                <p className="mt-1 text-xs text-muted-foreground">Outlet Salon/Cockpit 112W · Starlink 90W</p>
              </div>

              <div className="mt-2 rounded-md border bg-background/60 px-3 py-2">
                <p className="text-[11px] uppercase tracking-[0.16em] text-muted-foreground">Time To Go / Charge Rate</p>
                <p className="mt-1 font-display text-4xl leading-none text-secondary">
                  {timeToGoLabel} {chargeRateLabel}
                </p>
              </div>

              <div className="mt-2 rounded-md border bg-background/60 px-3 py-2">
                <div className="flex items-center justify-between gap-2">
                  <div className="inline-flex items-center gap-2 text-sm uppercase tracking-[0.16em] text-muted-foreground">
                    <span className="h-2.5 w-2.5 rounded-full bg-muted-foreground/40" />
                    Generator
                  </div>
                  <span className="text-sm font-semibold uppercase tracking-[0.12em] text-muted-foreground">Standby</span>
                  <Button size="sm" variant="outline" className="h-9 min-w-20">
                    Start
                  </Button>
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

            <TanksTile tanks={tanks} loading={tanksLoading} />
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
            { id: 'settings', label: 'Settings' },
          ]}
          activeTab={activeDrawerTab}
          onTabChange={setActiveDrawerTab}
        >
          {activeDrawerTab === 'forecast' && (
            <ForecastDrawer
              forecast={forecast}
              loading={forecastLoading}
              error={forecastError}
              onRetry={refetchForecast}
              unit={uiConfig.distanceUnits as 'imperial' | 'metric'}
            />
          )}
          {activeDrawerTab === 'tides' && (
            <div className="py-8 text-center text-muted-foreground">Tide details coming soon</div>
          )}
          {activeDrawerTab === 'wind' && (
            <div className="py-8 text-center text-muted-foreground">Wind details coming soon</div>
          )}
          {activeDrawerTab === 'radar' && (
            <RadarDrawer latitude={latitude} longitude={longitude} />
          )}
          {activeDrawerTab === 'settings' && (
            <SignalKSettingsPanel />
          )}
        </BottomDrawer>
      </div>
    </div>
  )
}
