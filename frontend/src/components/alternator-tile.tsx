import { Zap } from 'lucide-react'

import { Tile } from '@/components/ui/tile'

interface AlternatorData {
  currentA: number | null
  voltageV: number | null
  powerW: number | null
  temperatureC: number | null
}

interface AlternatorTileProps {
  port: AlternatorData
  starboard: AlternatorData
  enginesRunning: boolean
}

function tempClass(tempC: number | null): string {
  if (tempC === null) return 'text-gauge-secondary'
  if (tempC >= 100) return 'text-red-500'
  if (tempC >= 80) return 'text-amber-500'
  return 'text-gauge-secondary'
}

function AlternatorColumn({ label, data }: { label: string; data: AlternatorData }) {
  const powerLabel = data.powerW !== null ? Math.round(data.powerW).toString() : '—'
  const currentLabel = data.currentA !== null ? data.currentA.toFixed(1) : '—'
  const voltageLabel = data.voltageV !== null ? data.voltageV.toFixed(1) : '—'
  const tempLabel = data.temperatureC !== null ? `${Math.round(data.temperatureC)}°` : null

  return (
    <div className="flex flex-col gap-2">
      <p className="text-[10px] uppercase tracking-[0.20em] text-muted-foreground">{label}</p>

      {/* Power */}
      <div className="rounded-md border bg-background/60 px-3 py-3">
        <p className="text-[10px] uppercase tracking-[0.16em] text-muted-foreground">Output</p>
        <div className="flex items-baseline gap-1">
          <span className="font-display text-4xl tabular-nums leading-none text-gauge-secondary">{powerLabel}</span>
          <span className="text-xs leading-none text-muted-foreground">W</span>
        </div>
      </div>

      {/* Current + Voltage */}
      <div className="grid grid-cols-2 gap-2">
        <div className="rounded-md border bg-background/60 px-2 py-2">
          <p className="text-[9px] uppercase tracking-[0.14em] text-muted-foreground">Amps</p>
          <div className="flex items-baseline gap-0.5">
            <span className="font-display text-lg tabular-nums leading-none text-foreground">{currentLabel}</span>
            <span className="text-[10px] leading-none text-muted-foreground">A</span>
          </div>
        </div>
        <div className="rounded-md border bg-background/60 px-2 py-2">
          <p className="text-[9px] uppercase tracking-[0.14em] text-muted-foreground">Volts</p>
          <div className="flex items-baseline gap-0.5">
            <span className="font-display text-lg tabular-nums leading-none text-foreground">{voltageLabel}</span>
            <span className="text-[10px] leading-none text-muted-foreground">V</span>
          </div>
        </div>
      </div>

      {/* Temperature — only when available */}
      {tempLabel !== null && (
        <div className="rounded-md border bg-background/60 px-3 py-2">
          <p className="text-[10px] uppercase tracking-[0.16em] text-muted-foreground">Temp</p>
          <div className="flex items-baseline gap-0.5">
            <span className={`font-display text-xl tabular-nums leading-none ${tempClass(data.temperatureC)}`}>{tempLabel}</span>
            <span className="text-[10px] leading-none text-muted-foreground">C</span>
          </div>
        </div>
      )}
    </div>
  )
}

export function AlternatorTile({ port, starboard, enginesRunning }: AlternatorTileProps) {
  return (
    <Tile title="Alternators" icon={<Zap className="h-3.5 w-3.5 text-gauge-secondary" />}>
      {enginesRunning ? (
        <div className="mt-1 grid grid-cols-2 gap-4">
          <AlternatorColumn label="Port" data={port} />
          <AlternatorColumn label="Starboard" data={starboard} />
        </div>
      ) : (
        <div className="mt-1 flex h-24 items-center justify-center rounded-md border bg-background/60">
          <p className="text-sm text-muted-foreground">Engines not running</p>
        </div>
      )}
    </Tile>
  )
}
