import { Compass, Sailboat, Waves } from 'lucide-react'

import { MarineHeader } from '@/components/marine-header'
import { NearbyVesselsTile } from '@/components/nearby-vessels-tile'
import { useNearbyVessels } from '@/hooks/use-nearby-vessels'
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

  return (
    <div className="min-h-screen p-4 md:p-6">
      <div className="mx-auto flex max-w-[1800px] flex-col gap-4">
        <MarineHeader />

        <div className="grid gap-4 rounded-xl border bg-card/80 p-4 shadow-sm backdrop-blur-sm xl:grid-cols-[260px_minmax(560px,1fr)_320px_320px]">

          <aside className="space-y-4">
            <Tile title="Depth">
              <p className="mt-2 font-display text-6xl text-secondary">{depthValue}</p>
              <p className="text-sm text-muted-foreground">{depth !== null ? depthUnitLabel : 'unavailable'}</p>
            </Tile>
            <Tile title="Position">
              <p className="mt-2 font-mono text-sm">{formatCoordinate(latitude, true)}</p>
              <p className="font-mono text-sm">{formatCoordinate(longitude, false)}</p>
              <div className="mt-3 rounded-md bg-secondary/10 px-3 py-2 font-display text-2xl text-secondary">{formatHeading(headingTrue)}</div>
            </Tile>
            <NearbyVesselsTile vessels={nearbyVessels} loading={nearbyVesselsLoading} distanceUnits={uiConfig.distanceUnits} />
          </aside>

          <main className="rounded-lg border bg-card p-4">
            <div className="mb-3 flex items-center justify-between">
              <h1 className="font-display text-sm tracking-[0.24em] text-muted-foreground">Apparent Wind - Course Up</h1>
              <Button variant="outline" size="sm">
                <Compass className="h-4 w-4" />
                AWA {awaLabel}
              </Button>
            </div>
            <div className="grid place-items-center rounded-xl border bg-background/70 p-8">
              <div className="relative h-[420px] w-[420px] rounded-full border-2 border-border bg-card shadow-inner">
                <div className="absolute inset-0 grid place-items-center">
                  <div className="text-center">
                    <p className="font-display text-3xl text-primary">
                      {windSideLabel} {relativeAngleLabel}
                    </p>
                    <p className="font-display text-9xl leading-none text-primary">{apparentWindSpeedLabel}</p>
                    <p className="font-display text-4xl text-muted-foreground">kts</p>
                    <div className="mt-6 grid grid-cols-2 gap-2">
                      <div className="rounded-md border bg-background/70 px-3 py-2 text-left">
                        <p className="text-[11px] uppercase tracking-[0.16em] text-muted-foreground">Max Gust 10m</p>
                        <p className="font-display text-xl text-primary">{gust10mLabel}</p>
                      </div>
                      <div className="rounded-md border bg-background/70 px-3 py-2 text-left">
                        <p className="text-[11px] uppercase tracking-[0.16em] text-muted-foreground">Max Gust 1h</p>
                        <p className="font-display text-xl text-primary">{gust1hLabel}</p>
                      </div>
                    </div>
                  </div>
                </div>
                <div className="absolute left-1/2 top-8 h-[160px] w-[2px] -translate-x-1/2 bg-secondary" />
              </div>
            </div>
          </main>

          <aside className="space-y-4">
            <Tile title="Anchor Watch">
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
                <Button variant="secondary">Set Anchor</Button>
                <Button variant="outline">Drop Here (Use Current GPS)</Button>
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
          </aside>

          <aside className="space-y-4">
            <Tile title="Battery & Power">
              <div className="mt-2 flex items-end gap-2">
                <span className="font-display text-7xl text-primary">68</span>
                <span className="pb-2 text-3xl">%</span>
              </div>
              <p className="text-secondary">+24.8A / +663W</p>
              <div className="mt-4 space-y-2 text-sm">
                <div className="flex justify-between rounded-md bg-muted/50 px-3 py-2">
                  <span>Solar Output</span>
                  <span className="font-semibold">1868W</span>
                </div>
                <div className="flex justify-between rounded-md bg-muted/50 px-3 py-2">
                  <span>AC Output</span>
                  <span className="font-semibold">1017W</span>
                </div>
              </div>
            </Tile>
            <Tile title="Actions">
              <div className="mt-3 grid gap-2">
                <Button>
                  <Sailboat className="h-4 w-4" />
                  Set Anchor
                </Button>
                <Button variant="secondary">
                  <Waves className="h-4 w-4" />
                  Drop Here
                </Button>
              </div>
            </Tile>
          </aside>
        </div>
      </div>
    </div>
  )
}
