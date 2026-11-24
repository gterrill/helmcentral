import { Zap } from 'lucide-react'
import { memo } from 'react'

import { FigureBox } from '@/components/ui/figure-box'
import { Tile } from '@/components/ui/tile'
import { useElectricalFields } from '@/hooks/use-electrical-state'
import { useVesselStateSelector, type VesselStateValue } from '@/hooks/use-vessel-state'

interface AlternatorData {
  currentA: number | null
  voltageV: number | null
  powerW: number | null
  temperatureC: number | null
}

interface AlternatorTileProps {
  port: AlternatorData
  starboard: AlternatorData
  /** Null when vessel state has not said, or no engine is set up: the tile claims neither running nor stopped. */
  enginesRunning: boolean | null
}

function hasAlternatorData(data: AlternatorData | null | undefined): boolean {
  if (!data) return false
  return data.currentA !== null || data.voltageV !== null || data.powerW !== null || data.temperatureC !== null
}

function tempClass(tempC: number | null): string {
  if (tempC === null) return 'text-gauge-secondary'
  if (tempC >= 100) return 'text-red-500'
  if (tempC >= 80) return 'text-amber-500'
  return 'text-gauge-secondary'
}

function AlternatorColumn({ label, data }: { label?: string; data: AlternatorData }) {
  const powerLabel = data.powerW !== null ? Math.round(data.powerW).toString() : '—'
  const currentLabel = data.currentA !== null ? data.currentA.toFixed(1) : '—'
  const voltageLabel = data.voltageV !== null ? data.voltageV.toFixed(1) : '—'
  const tempLabel = data.temperatureC !== null ? `${Math.round(data.temperatureC)}°` : null

  return (
    <div className="flex min-w-0 flex-col gap-2">
      {label && <p className="text-[10px] uppercase tracking-[0.20em] text-muted-foreground">{label}</p>}

      {/* Power */}
      <FigureBox
        label="Output"
        value={powerLabel}
        unit="W"
        narrow="stack"
        className="py-3 @max-2xs:py-1"
        valueClassName="text-4xl text-gauge-secondary @max-2xs:text-2xl"
        unitClassName="text-xs"
        // Below 18rem the readings under Output would stack four boxes per
        // alternator, twelve rows for two. They become lines in Output's box.
        subClassName="hidden @max-2xs:block"
        sub={
          <>
            <span className="block">{currentLabel} A</span>
            <span className="block">{voltageLabel} V</span>
            {tempLabel !== null && <span className={`block ${tempClass(data.temperatureC)}`}>{tempLabel}C</span>}
          </>
        }
      />

      <div className="grid grid-cols-2 gap-2 @max-2xs:hidden">
        <FigureBox
          label="Amps"
          value={currentLabel}
          unit="A"
          className="px-2"
          labelClassName="tracking-[0.14em]"
          valueClassName="text-lg text-foreground"
          unitClassName="ml-0.5 text-xs"
        />
        <FigureBox
          label="Volts"
          value={voltageLabel}
          unit="V"
          className="px-2"
          labelClassName="tracking-[0.14em]"
          valueClassName="text-lg text-foreground"
          unitClassName="ml-0.5 text-xs"
        />
      </div>

      {/* Temperature — only when available */}
      {tempLabel !== null && (
        <FigureBox
          className="@max-2xs:hidden"
          label="Temp"
          value={tempLabel}
          unit="C"
          valueClassName={`text-xl ${tempClass(data.temperatureC)}`}
          unitClassName="ml-0.5 text-xs"
        />
      )}
    </div>
  )
}

export const AlternatorTile = memo(function AlternatorTile({ port, starboard, enginesRunning }: AlternatorTileProps) {
  const hasPort = hasAlternatorData(port)
  const hasStarboard = hasAlternatorData(starboard)
  const isSingleAlternator = (hasPort && !hasStarboard) || (!hasPort && hasStarboard)
  const singleData = hasPort ? port : starboard

  return (
    <Tile title={isSingleAlternator ? 'Alternator' : 'Alternators'} className="@container" icon={<Zap className="h-3.5 w-3.5 text-gauge-secondary" />}>
      {enginesRunning === null ? (
        <div className="mt-1 flex h-24 items-center justify-center rounded-md border bg-background/60">
          <p className="text-sm text-muted-foreground">--</p>
        </div>
      ) : enginesRunning ? (
        isSingleAlternator ? (
          <div className="mt-1">
            <AlternatorColumn data={singleData} />
          </div>
        ) : (
          <div className="mt-1 grid grid-cols-2 gap-4 @max-2xs:grid-cols-1">
            <AlternatorColumn label="Port" data={port} />
            <AlternatorColumn label="Starboard" data={starboard} />
          </div>
        )
      ) : (
        <div className="mt-1 flex h-24 items-center justify-center rounded-md border bg-background/60">
          <p className="text-sm text-muted-foreground">{isSingleAlternator ? 'Engine not running' : 'Engines not running'}</p>
        </div>
      )}
    </Tile>
  )
})

const selectEnginesRunning = (state: VesselStateValue) => state.enginesRunning
const ALTERNATOR_FIELDS = ['alternator0', 'alternator1'] as const

/**
 * The alternator tile: the readings from the electrical stream, shown while an
 * engine runs. Whether one runs is null until vessel state says, and the tile
 * then claims neither.
 */
export const LiveAlternatorTile = memo(function LiveAlternatorTile() {
  const enginesRunning = useVesselStateSelector(selectEnginesRunning)
  const { alternator0, alternator1 } = useElectricalFields(ALTERNATOR_FIELDS)
  return <AlternatorTile port={alternator0} starboard={alternator1} enginesRunning={enginesRunning} />
})
