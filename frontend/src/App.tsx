import { Anchor, ArrowDown, ArrowUp, CloudSun, Compass, Sailboat, Waves, X } from 'lucide-react'

import { MarineHeader } from '@/components/marine-header'
import { NearbyVesselsTile } from '@/components/nearby-vessels-tile'
import { TanksTile } from '@/components/tanks-tile'
import { useElectricalState } from '@/hooks/use-electrical-state'
import { useNearbyVessels } from '@/hooks/use-nearby-vessels'
import { useTanksState } from '@/hooks/use-tanks-state'
import { useVesselState } from '@/hooks/use-vessel-state'
import { uiConfig } from '@/config/app-config'
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

export function App() {
  const {
    depth,
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
  } = useElectricalState(5)
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

  return (
    <div className="min-h-screen p-4 md:p-6">
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
                <div className="relative h-[300px] w-[300px] rounded-full border-2 border-border bg-card shadow-inner lg:h-[330px] lg:w-[330px]">
                  <div className="absolute inset-0 grid place-items-center">
                    <div className="text-center">
                      <p className="font-display text-2xl text-primary lg:text-3xl">
                        {windSideLabel} {relativeAngleLabel}
                      </p>
                      <p className="font-display text-7xl leading-none text-primary lg:text-8xl">{apparentWindSpeedLabel}</p>
                      <p className="font-display text-3xl text-muted-foreground">kts</p>
                      <div className="mt-4 grid grid-cols-2 gap-2">
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
                  <div className="absolute left-1/2 top-8 h-[120px] w-[2px] -translate-x-1/2 bg-secondary lg:h-[135px]" />
                </div>
              </div>
            </section>

            <Tile title="Depth">
              <p className="mt-2 font-display text-6xl text-secondary">{depthValue}</p>
              <p className="text-sm text-muted-foreground">{depth !== null ? depthUnitLabel : 'unavailable'}</p>
            </Tile>
            <Tile title="Position">
              <p className="mt-2 font-mono text-sm">{formatCoordinate(latitude, true)}</p>
              <p className="font-mono text-sm">{formatCoordinate(longitude, false)}</p>
              <div className="mt-3 rounded-md bg-secondary/10 px-3 py-2 font-display text-2xl text-secondary">{formatHeading(headingTrue)}</div>
            </Tile>
            <Tile title="Today & Now">
              <div className="mt-2 grid grid-cols-[auto_1fr_auto] items-center gap-3">
                <div className="grid h-12 w-12 place-items-center rounded-full bg-amber-100 text-amber-600">
                  <CloudSun className="h-7 w-7" />
                </div>
                <div>
                  <div className="flex items-end gap-1">
                    <p className="font-display text-5xl leading-none text-amber-700">75</p>
                    <p className="pb-1 text-xl font-semibold text-amber-700">°F</p>
                  </div>
                  <p className="text-sm font-semibold uppercase tracking-[0.1em] text-foreground">Mostly Clear</p>
                  <p className="text-xs text-muted-foreground">↑77° ↓70°</p>
                </div>
                <div className="text-right">
                  <p className="font-display text-xl leading-none text-secondary">15.7 kts NE</p>
                  <p className="mt-1 text-xs text-muted-foreground">8% precip</p>
                </div>
              </div>

              <div className="my-3 h-px bg-border/70" />

              <div className="grid grid-cols-[auto_1fr] gap-3">
                <div>
                  <p className="text-[11px] uppercase tracking-[0.16em] text-muted-foreground">Tide Now</p>
                  <p className="mt-1 font-display text-5xl leading-none text-secondary">1.5</p>
                  <p className="-mt-1 text-lg font-semibold text-secondary">ft</p>
                  <p className="mt-1 inline-flex items-center gap-1 text-xs font-semibold text-secondary">
                    <ArrowUp className="h-3.5 w-3.5" />
                    Rising
                  </p>
                </div>
                <div className="space-y-2 pt-1 text-sm">
                  <p className="inline-flex items-center gap-1 text-foreground">
                    <ArrowUp className="h-4 w-4 text-secondary" />
                    High Today 12:57 PM
                    <span className="text-muted-foreground">1.9ft</span>
                  </p>
                  <p className="inline-flex items-center gap-1 text-foreground">
                    <ArrowDown className="h-4 w-4 text-amber-600" />
                    Low Today 7:11 PM
                    <span className="text-muted-foreground">-0.1ft</span>
                  </p>
                </div>
              </div>
            </Tile>
          </aside>

          <aside className="space-y-4">
            <Tile title="Anchor Watch" icon={<Anchor className="h-3.5 w-3.5" />}>
              <div className="mt-2 grid grid-cols-3 gap-2 text-center">
                <div className="rounded-md bg-muted/50 px-2 py-2">
                  <p className="text-[10px] uppercase tracking-[0.16em] text-muted-foreground">Distance</p>
                  <p className="font-display text-2xl text-primary">121</p>
                  <p className="text-[11px] text-muted-foreground">ft</p>
                </div>
                <div className="rounded-md bg-muted/50 px-2 py-2">
                  <p className="text-[10px] uppercase tracking-[0.16em] text-muted-foreground">Bearing</p>
                  <p className="font-display text-2xl text-secondary">72</p>
                  <p className="text-[11px] text-muted-foreground">deg</p>
                </div>
                <div className="rounded-md bg-muted/50 px-2 py-2">
                  <p className="text-[10px] uppercase tracking-[0.16em] text-muted-foreground">Radius</p>
                  <p className="font-display text-2xl text-secondary">160</p>
                  <p className="text-[11px] text-muted-foreground">ft</p>
                </div>
              </div>
              <div className="mt-3 rounded-md border bg-background/60 px-3 py-2 text-sm">
                <p className="text-muted-foreground">Anchor Position</p>
                <p className="font-mono text-xs">N 25 29.181&apos; W 76 38.213&apos;</p>
              </div>
              <div className="mt-3 grid gap-2">
                <Button className="h-11 bg-amber-600 text-amber-50 hover:bg-amber-700">
                  <Sailboat className="h-4 w-4" />
                  Set Anchor
                </Button>
                <Button className="h-11 border border-teal-500 bg-teal-600 text-teal-50 hover:bg-teal-700">
                  <Waves className="h-4 w-4" />
                  Drop Here (Use Current GPS)
                </Button>
                <Button variant="outline" className="h-11 text-muted-foreground">
                  <X className="h-4 w-4" />
                  Clear
                </Button>
              </div>
            </Tile>

            <Tile title="Rode & Scope">
              <div className="mt-2 grid grid-cols-2 gap-2 text-sm">
                <div className="rounded-md bg-muted/50 px-3 py-2">
                  <p className="text-[10px] uppercase tracking-[0.16em] text-muted-foreground">Chain Counter</p>
                  <p className="font-display text-2xl text-primary">122</p>
                  <p className="text-[11px] text-muted-foreground">ft</p>
                </div>
                <div className="rounded-md bg-muted/50 px-3 py-2">
                  <p className="text-[10px] uppercase tracking-[0.16em] text-muted-foreground">Drop Deduct</p>
                  <p className="font-display text-2xl text-primary">5</p>
                  <p className="text-[11px] text-muted-foreground">ft</p>
                </div>
              </div>
              <div className="mt-3 grid grid-cols-3 gap-2 text-center">
                <div className="rounded-md border bg-background/60 px-2 py-2">
                  <p className="text-[10px] uppercase tracking-[0.16em] text-muted-foreground">Rode</p>
                  <p className="font-display text-2xl text-secondary">117</p>
                  <p className="text-[11px] text-muted-foreground">ft</p>
                </div>
                <div className="rounded-md border bg-background/60 px-2 py-2">
                  <p className="text-[10px] uppercase tracking-[0.16em] text-muted-foreground">Total+BOW</p>
                  <p className="font-display text-2xl text-secondary">17.5</p>
                  <p className="text-[11px] text-muted-foreground">ft</p>
                </div>
                <div className="rounded-md border bg-background/60 px-2 py-2">
                  <p className="text-[10px] uppercase tracking-[0.16em] text-muted-foreground">Scope</p>
                  <p className="font-display text-2xl text-secondary">6.7</p>
                  <p className="text-[11px] text-muted-foreground">:1</p>
                </div>
              </div>
            </Tile>

            <NearbyVesselsTile vessels={nearbyVessels} loading={nearbyVesselsLoading} distanceUnits={uiConfig.distanceUnits} />
          </aside>

          <aside className="space-y-4">
            <Tile title="Battery & Power">
              <div className="mt-1 grid grid-cols-[1fr_1.4fr] gap-2">
                <div className="flex items-end gap-2 rounded-md border bg-background/60 px-3 py-3">
                  <span className="font-display text-7xl leading-none text-primary">{socLabel}</span>
                  <span className="pb-2 text-3xl leading-none text-foreground">%</span>
                </div>
                <div className="rounded-md border bg-background/60 px-3 py-3">
                  <p className="text-[11px] uppercase tracking-[0.16em] text-muted-foreground">Charging</p>
                  <p className="font-display text-5xl leading-none text-secondary">{chargingCurrentLabel}A</p>
                  <p className="mt-1 font-display text-4xl leading-none text-secondary">{chargingPowerLabel}W</p>
                </div>
              </div>

              <div className="mt-2 h-1.5 rounded-full bg-muted/60">
                <div className="h-full rounded-full bg-primary" style={{ width: socBarWidth }} />
              </div>

              <div className="mt-3 grid grid-cols-2 gap-2 text-sm">
                <div className="rounded-md border bg-background/60 px-3 py-2">
                  <p className="text-[11px] uppercase tracking-[0.16em] text-muted-foreground">Solar Output</p>
                  <p className="font-display text-4xl leading-none text-primary">{solarOutputLabel}</p>
                  <p className="text-sm text-muted-foreground">W</p>
                </div>
                <div className="rounded-md border bg-background/60 px-3 py-2">
                  <p className="text-[11px] uppercase tracking-[0.16em] text-muted-foreground">AC Output</p>
                  <p className="font-display text-4xl leading-none text-primary">{acOutputLabel}</p>
                  <p className="text-sm text-muted-foreground">W</p>
                </div>
              </div>

              <div className="mt-2 grid grid-cols-2 gap-2 text-sm">
                <div className="rounded-md border bg-background/60 px-3 py-2">
                  <p className="text-[11px] uppercase tracking-[0.16em] text-muted-foreground">24V DC Power</p>
                  <p className="font-display text-4xl leading-none text-foreground">{dc12vPowerLabel}</p>
                  <p className="text-sm text-muted-foreground">W {dc12vCurrentLabel}A</p>
                </div>
                <div className="rounded-md border bg-background/60 px-3 py-2">
                  <p className="text-[11px] uppercase tracking-[0.16em] text-muted-foreground">24V Voltage</p>
                  <p className="font-display text-4xl leading-none text-secondary">{dc24vVoltageLabel}</p>
                  <p className="text-sm text-muted-foreground">V</p>
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
                <p className="mt-1 font-display text-4xl leading-none text-secondary">~4h 38m to full +6.7%/hr</p>
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
      </div>
    </div>
  )
}
