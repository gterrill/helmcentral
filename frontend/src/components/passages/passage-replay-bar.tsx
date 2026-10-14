import { Map as MapIcon, Pause, Play } from 'lucide-react'

import { Button } from '@/components/ui/button'
import { REPLAY_SPEEDS, type ReplaySpeedId } from '@/hooks/use-replay'
import { cn } from '@/lib/utils'

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
}

export function PassageReplayBar({
  clock, total, playing, readout, speed, insetOn, disabled, onToggle, onScrub, onSpeed, onInsetChange,
}: PassageReplayBarProps) {
  return (
    <div className="flex flex-wrap items-center gap-2 border-t border-border bg-card p-2" data-testid="passage-replay-bar">
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
      <input
        type="range"
        aria-label="Replay position"
        className="min-w-24 flex-1 accent-primary"
        min={0}
        max={Math.max(1, Math.round(total))}
        step={1}
        value={Math.round(clock)}
        disabled={disabled}
        onChange={(e) => onScrub(Number(e.target.value))}
      />
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
      <Button
        type="button"
        size="sm"
        variant={insetOn ? 'default' : 'outline'}
        aria-pressed={insetOn}
        className={cn('ml-auto')}
        onClick={() => onInsetChange(!insetOn)}
      >
        <MapIcon className="h-4 w-4" data-icon="inline-start" aria-hidden="true" />
        Overview map
      </Button>
    </div>
  )
}
