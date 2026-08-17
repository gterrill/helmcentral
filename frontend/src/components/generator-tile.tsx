import { memo, useCallback, useState } from 'react'

import { Button } from '@/components/ui/button'
import { Tile } from '@/components/ui/tile'

interface GeneratorTileProps {
  generatorState: string | null
  generatorManualStart: boolean
  generatorManualStartTimer: number
  generatorRunningByCondition: string | null
  generatorRuntime: number | null
  generatorRealPowerW: number | null
  batterySocPercent: number | null
  batteryRatePercentPerHour: number | null
  /**
   * Cosmetic-only role gate (ADR 0040 §frontend, backend `write` tier): the
   * server is the actual enforcement point for /api/generator/start|stop,
   * this just avoids offering a control below readwrite that would only 403.
   */
  readOnly?: boolean
}

function formatRuntime(seconds: number | null): string {
  if (seconds === null || seconds < 0) return '—'
  const h = Math.floor(seconds / 3600)
  const m = Math.floor((seconds % 3600) / 60)
  const s = Math.floor(seconds % 60)
  return `${h}:${String(m).padStart(2, '0')}:${String(s).padStart(2, '0')}`
}

function formatRunReason(condition: string | null): string | null {
  if (!condition || condition === 'manual' || condition === 'stopped') return null
  const labels: Record<string, string> = {
    'test run': 'Test Run',
    'loss of communication': 'Loss of Comms',
    soc: 'Auto: SoC',
    acload: 'Auto: AC Load',
    'battery current': 'Auto: Batt Current',
    'battery voltage': 'Auto: Batt Voltage',
    'inverter high temp': 'Auto: Inverter Temp',
    'inverter overload': 'Auto: Overload',
  }
  return labels[condition] ?? condition
}

export const GeneratorTile = memo(function GeneratorTile({
  generatorState,
  generatorManualStart,
  generatorManualStartTimer,
  generatorRunningByCondition,
  generatorRuntime,
  generatorRealPowerW,
  batterySocPercent,
  batteryRatePercentPerHour,
  readOnly = false,
}: GeneratorTileProps) {
  const [timedRunEnabled, setTimedRunEnabled] = useState(false)
  const [timerHours, setTimerHours] = useState(1)
  const [timerMinutes, setTimerMinutes] = useState(0)
  const [pending, setPending] = useState(false)

  // generatorState reflects the auto-generator-start controller's own state
  // machine, which doesn't necessarily know the generator is running if it
  // was started manually at the panel rather than through that controller.
  // Real measured power output is a direct electrical reading, so it's
  // trusted as evidence of "running" regardless of how it was started.
  const isRunning =
    generatorState === 'running' ||
    generatorState === 'warm up' ||
    generatorState === 'warm uo' ||
    generatorState === 'cool down' ||
    (generatorRealPowerW !== null && generatorRealPowerW > 0)

  const powerLabel =
    generatorRealPowerW !== null && generatorRealPowerW > 0
      ? Math.round(generatorRealPowerW).toString()
      : '0'

  const runReason = formatRunReason(generatorRunningByCondition)

  // Projects where the battery's state of charge will land by the time the
  // manual-start timer finishes, using the same net charge-rate the battery
  // tile already computes (covers generator + solar + alternator + loads
  // combined, which is what actually determines the battery's trajectory).
  // Only shown while genuinely charging - a negative/zero/unknown rate would
  // make "at finish" a misleading thing to project.
  const projectedSocPercent =
    batterySocPercent !== null && batteryRatePercentPerHour !== null && batteryRatePercentPerHour > 0
      ? Math.max(0, Math.min(100, batterySocPercent + batteryRatePercentPerHour * (generatorManualStartTimer / 3600)))
      : null

  const handleStart = useCallback(async () => {
    setPending(true)
    try {
      const timerSeconds = timedRunEnabled ? timerHours * 3600 + timerMinutes * 60 : 0
      await fetch('/api/generator/start', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ timer_seconds: timerSeconds }),
      })
    } catch (err) {
      console.error('Generator start failed:', err)
    } finally {
      setPending(false)
    }
  }, [timedRunEnabled, timerHours, timerMinutes])

  const handleStop = useCallback(async () => {
    setPending(true)
    try {
      await fetch('/api/generator/stop', { method: 'POST' })
    } catch (err) {
      console.error('Generator stop failed:', err)
    } finally {
      setPending(false)
    }
  }, [])

  return (
    <Tile title="Generator">
      <div className="mt-2 grid grid-cols-1 gap-2 text-center md:grid-cols-2">
        <div className={`rounded-md border bg-background/60 px-2 py-2 ${isRunning ? 'border-gauge-secondary/30' : ''}`}>
          <p className="text-[10px] uppercase tracking-[0.16em] text-muted-foreground">Power</p>
          <p className={`font-display text-2xl tabular-nums ${isRunning ? 'text-gauge-secondary' : 'text-muted-foreground'}`}>
            {powerLabel}<span className="ml-1 text-base text-muted-foreground">W</span>
          </p>
        </div>

        <div className={`rounded-md border bg-background/60 px-2 py-2 ${isRunning ? 'border-muted/40' : ''}`}>
          <p className="text-[10px] uppercase tracking-[0.16em] text-muted-foreground">Runtime</p>
          <p className="font-display text-2xl tabular-nums text-gauge-secondary">
            {formatRuntime(generatorRuntime)}
          </p>
        </div>
      </div>

      {runReason && (
        <div className="mt-2 flex flex-wrap items-center gap-2">
          <span className="text-[11px] text-muted-foreground">{runReason}</span>
        </div>
      )}

      {/* Start / Stop button */}
      <div className="mt-3 flex justify-end">
        <Button
          size="default"
          variant={isRunning ? 'outline' : 'default'}
          className={`h-10 min-w-[64px] shrink-0 antialiased transition-colors${isRunning ? ' border-red-300 text-red-500 hover:bg-red-50 dark:border-red-700 dark:text-red-400 dark:hover:bg-red-950' : ''}`}
          disabled={pending || readOnly}
          onClick={isRunning ? handleStop : handleStart}
        >
          {pending ? '…' : isRunning ? 'Stop' : 'Start'}
        </Button>
      </div>

      {/* Remaining timer while running, plus a battery forecast for the end of it */}
      {isRunning && generatorManualStart && generatorManualStartTimer > 0 && (
        <div className={`mt-3 grid gap-2 ${projectedSocPercent !== null ? 'grid-cols-2' : 'grid-cols-1'}`}>
          <div className="rounded-md border bg-background/60 px-3 py-2">
            <p className="text-[11px] uppercase tracking-[0.16em] text-muted-foreground">Timer Remaining</p>
            <p className="mt-0.5 font-mono text-sm tabular-nums text-gauge-secondary">
              {formatRuntime(generatorManualStartTimer)}
            </p>
          </div>
          {projectedSocPercent !== null && (
            <div className="rounded-md border bg-background/60 px-3 py-2">
              <p className="text-[11px] uppercase tracking-[0.16em] text-muted-foreground">Battery at Finish</p>
              <p className="mt-0.5 font-mono text-sm tabular-nums text-gauge-secondary">
                {Math.round(projectedSocPercent)}%
              </p>
            </div>
          )}
        </div>
      )}

      {/* Timed run toggle — only when stopped */}
      {!isRunning && (
        <div className="mt-3 rounded-md border bg-background/60 px-3 py-2.5">
          <div className="flex items-center justify-between">
            <p className="text-[11px] uppercase tracking-[0.16em] text-muted-foreground">Timed Run</p>
            <button
              role="switch"
              aria-checked={timedRunEnabled}
              className="relative inline-flex h-5 w-9 cursor-pointer items-center rounded-full transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-1"
              style={{ background: timedRunEnabled ? 'hsl(var(--primary))' : 'hsl(var(--muted))' }}
              onClick={() => setTimedRunEnabled((v) => !v)}
            >
              <span
                className="inline-block h-4 w-4 rounded-full bg-white shadow-sm transition-transform"
                style={{ transform: timedRunEnabled ? 'translateX(18px)' : 'translateX(2px)' }}
              />
            </button>
          </div>
          {timedRunEnabled && (
            <div className="mt-2.5 flex items-center gap-3">
              <div className="flex items-center gap-1.5">
                <input
                  type="number"
                  min={0}
                  max={23}
                  value={timerHours}
                  onChange={(e) =>
                    setTimerHours(Math.max(0, Math.min(23, parseInt(e.target.value, 10) || 0)))
                  }
                  className="w-14 rounded-md border bg-background px-2 py-1.5 text-center font-mono text-sm tabular-nums focus:outline-none focus:ring-2 focus:ring-ring"
                />
                <span className="text-sm text-muted-foreground">h</span>
              </div>
              <div className="flex items-center gap-1.5">
                <input
                  type="number"
                  min={0}
                  max={59}
                  step={15}
                  value={timerMinutes}
                  onChange={(e) =>
                    setTimerMinutes(Math.max(0, Math.min(59, parseInt(e.target.value, 10) || 0)))
                  }
                  className="w-14 rounded-md border bg-background px-2 py-1.5 text-center font-mono text-sm tabular-nums focus:outline-none focus:ring-2 focus:ring-ring"
                />
                <span className="text-sm text-muted-foreground">m</span>
              </div>
              <span className="text-xs text-muted-foreground">
                = {timerHours}h {String(timerMinutes).padStart(2, '0')}m
              </span>
            </div>
          )}
        </div>
      )}
    </Tile>
  )
})
