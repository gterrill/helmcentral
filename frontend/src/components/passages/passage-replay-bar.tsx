import { Map as MapIcon, Pause, Play, Video } from 'lucide-react'
import type { ReactNode } from 'react'

import { Button } from '@/components/ui/button'
import { REPLAY_SPEEDS, type ReplaySpeedId } from '@/hooks/use-replay'
import { cn } from '@/lib/utils'

/** A camera shot drawn as a mark on the scrubber. */
export interface ShotTick {
  id: string
  /** 0..1 along the scrubber. */
  fraction: number
  label: string
  selected: boolean
}

export interface PassageReplayBarProps {
  clock: number
  total: number
  playing: boolean
  /** Boat-local date and time of the replay position, zone labelled. */
  readout: string
  speed: ReplaySpeedId
  insetOn: boolean
  disabled: boolean
  onToggle: () => void
  onScrub: (clock: number) => void
  onSpeed: (speed: ReplaySpeedId) => void
  onInsetChange: (on: boolean) => void
  shotTicks?: ShotTick[]
  cameraOpen?: boolean
  onCameraToggle?: () => void
  onTickClick?: (id: string) => void
  /** The camera shot editor, shown under the bar while the Camera button is on. */
  editor?: ReactNode
}

export function PassageReplayBar({
  clock, total, playing, readout, speed, insetOn, disabled, onToggle, onScrub, onSpeed, onInsetChange,
  shotTicks = [], cameraOpen = false, onCameraToggle, onTickClick, editor,
}: PassageReplayBarProps) {
  return (
    <div className="border-t border-border bg-card" data-testid="passage-replay-bar">
    <div className="flex flex-wrap items-center gap-2 p-2">
      <Button
        type="button"
        size="icon"
        variant="outline"
        disabled={disabled}
        aria-label={playing ? 'Pause replay' : 'Play replay'}
        onClick={onToggle}
      >
        {playing ? <Pause className="h-4 w-4" aria-hidden="true" /> : <Play className="h-4 w-4" aria-hidden="true" />}
      </Button>
      <div className="relative flex min-w-24 flex-1 items-center">
        <input
          type="range"
          aria-label="Replay position"
          className="w-full accent-primary"
          min={0}
          max={Math.max(1, Math.round(total))}
          step={1}
          value={Math.round(clock)}
          disabled={disabled}
          onChange={(e) => onScrub(Number(e.target.value))}
        />
        {shotTicks.map((t) => (
          <button
            key={t.id}
            type="button"
            aria-label={t.label}
            aria-pressed={t.selected}
            disabled={disabled}
            className={cn(
              'absolute top-0 h-2.5 w-2 -translate-x-1/2 rounded-b-sm border border-background',
              t.selected ? 'bg-foreground' : 'bg-primary',
            )}
            style={{ left: `${Math.max(0, Math.min(1, t.fraction)) * 100}%` }}
            onClick={() => onTickClick?.(t.id)}
          />
        ))}
      </div>
      <span className="min-w-36 truncate text-xs tabular-nums text-foreground" data-testid="replay-readout">{readout}</span>
      <span className="flex gap-1" role="group" aria-label="Replay speed">
        {REPLAY_SPEEDS.map((s) => (
          <Button
            key={s.id}
            type="button"
            size="sm"
            variant={speed === s.id ? 'default' : 'outline'}
            aria-pressed={speed === s.id}
            onClick={() => onSpeed(s.id)}
          >
            {s.label}
          </Button>
        ))}
      </span>
      {onCameraToggle && (
        <Button
          type="button"
          size="sm"
          variant={cameraOpen ? 'default' : 'outline'}
          aria-pressed={cameraOpen}
          className="ml-auto"
          onClick={onCameraToggle}
        >
          <Video className="h-4 w-4" data-icon="inline-start" aria-hidden="true" />
          Camera
        </Button>
      )}
      <Button
        type="button"
        size="sm"
        variant={insetOn ? 'default' : 'outline'}
        aria-pressed={insetOn}
        className={cn(!onCameraToggle && 'ml-auto')}
        onClick={() => onInsetChange(!insetOn)}
      >
        <MapIcon className="h-4 w-4" data-icon="inline-start" aria-hidden="true" />
        Overview map
      </Button>
    </div>
    {cameraOpen && editor}
    </div>
  )
}
