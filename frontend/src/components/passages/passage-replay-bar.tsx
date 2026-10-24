import { Map as MapIcon, Minus, Pause, Play, Plus, Signpost, SlidersHorizontal, Video } from 'lucide-react'
import type { ReactNode } from 'react'

import { Button } from '@/components/ui/button'
import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover'
import { REPLAY_SPEEDS, type ReplaySpeedId } from '@/hooks/use-replay'
import {
  INSET_CORNERS, INSET_ZOOM_STEP_MAX, INSET_ZOOM_STEP_MIN, clampInsetStep, insetStepLabel, type InsetView,
} from '@/lib/passage-inset-view'
import { cn } from '@/lib/utils'

/** A camera shot drawn as a mark on the scrubber. */
export interface ShotTick {
  id: string
  /** 0..1 along the scrubber. */
  fraction: number
  label: string
  /** Hover text: the shot's number and time. */
  title?: string
  selected: boolean
}

/** A sign drawn as a mark on the scrubber: a diamond, so it reads apart from a camera shot. */
export interface SignTick {
  id: string
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
  /** Where the overview map sits and how close it looks. */
  insetView?: InsetView
  /** Saves a new corner or zoom step. Without it the placement controls stay out of the bar. */
  onInsetViewChange?: (view: InsetView) => void
  shotTicks?: ShotTick[]
  cameraOpen?: boolean
  onCameraToggle?: () => void
  onTickClick?: (id: string) => void
  /** The camera shot editor, shown under the bar while the Camera button is on. */
  editor?: ReactNode
  signTicks?: SignTick[]
  signsOpen?: boolean
  onSignsToggle?: () => void
  onSignTickClick?: (id: string) => void
  /** The sign editor, shown under the bar while the Signs button is on. */
  signsEditor?: ReactNode
}

export function PassageReplayBar({
  clock, total, playing, readout, speed, insetOn, disabled, onToggle, onScrub, onSpeed, onInsetChange,
  insetView, onInsetViewChange,
  shotTicks = [], cameraOpen = false, onCameraToggle, onTickClick, editor,
  signTicks = [], signsOpen = false, onSignsToggle, onSignTickClick, signsEditor,
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
      <div className={cn('relative flex min-w-24 flex-1 items-center', onSignsToggle && 'pb-5', shotTicks.length > 0 && 'pt-3')}>
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
            title={t.title}
            aria-pressed={t.selected}
            disabled={disabled}
            className={cn(
              'absolute -translate-x-1/2 rounded-b-sm border border-background',
              t.selected ? 'top-1 z-10 h-3 w-3 bg-foreground ring-2 ring-ring ring-offset-1 ring-offset-background' : 'top-2 h-2.5 w-2 bg-primary',
            )}
            style={{ left: `${Math.max(0, Math.min(1, t.fraction)) * 100}%` }}
            onClick={() => onTickClick?.(t.id)}
          />
        ))}
        {signTicks.map((t) => (
          <button
            key={t.id}
            type="button"
            aria-label={t.label}
            aria-pressed={t.selected}
            disabled={disabled}
            className="group absolute bottom-0 flex h-5 w-5 -translate-x-1/2 items-center justify-center bg-transparent focus-visible:outline-hidden"
            style={{ left: `${Math.max(0, Math.min(1, t.fraction)) * 100}%` }}
            onClick={() => onSignTickClick?.(t.id)}
          >
            <span
              aria-hidden="true"
              className={cn(
                'h-3 w-3 rotate-45 rounded-xs border group-focus-visible:ring-2 group-focus-visible:ring-ring',
                t.selected ? 'border-foreground bg-foreground' : 'border-primary bg-card',
              )}
            />
          </button>
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
      {onSignsToggle && (
        <Button
          type="button"
          size="sm"
          variant={signsOpen ? 'default' : 'outline'}
          aria-pressed={signsOpen}
          className={cn(!onCameraToggle && 'ml-auto')}
          onClick={onSignsToggle}
        >
          <Signpost className="h-4 w-4" data-icon="inline-start" aria-hidden="true" />
          Signs
        </Button>
      )}
      <span className={cn('flex items-center gap-1', !onCameraToggle && !onSignsToggle && 'ml-auto')}>
        <Button
          type="button"
          size="sm"
          variant={insetOn ? 'default' : 'outline'}
          aria-pressed={insetOn}
          onClick={() => onInsetChange(!insetOn)}
        >
          <MapIcon className="h-4 w-4" data-icon="inline-start" aria-hidden="true" />
          Overview map
        </Button>
        {insetOn && insetView && onInsetViewChange && (
          <InsetPlacement view={insetView} onChange={onInsetViewChange} />
        )}
      </span>
    </div>
    {cameraOpen && editor}
    {signsOpen && signsEditor}
    </div>
  )
}

/** Corner and zoom step of the overview map, in a small popover beside its button. */
function InsetPlacement({ view, onChange }: { view: InsetView; onChange: (view: InsetView) => void }) {
  return (
    <Popover>
      <PopoverTrigger
        render={<Button type="button" size="icon-sm" variant="outline" aria-label="Place overview map" />}
      >
        <SlidersHorizontal className="h-4 w-4" aria-hidden="true" />
      </PopoverTrigger>
      <PopoverContent align="end" side="top" className="flex w-56 flex-col gap-3 p-3">
        <div className="flex flex-col gap-1.5">
          <span className="text-[10px] font-medium uppercase tracking-wider text-muted-foreground">Corner</span>
          <div role="group" aria-label="Overview map corner" className="grid grid-cols-2 gap-1">
            {INSET_CORNERS.map((c) => (
              <Button
                key={c.id}
                type="button"
                size="sm"
                variant={view.corner === c.id ? 'default' : 'outline'}
                aria-pressed={view.corner === c.id}
                onClick={() => onChange({ ...view, corner: c.id })}
              >
                {c.label}
              </Button>
            ))}
          </div>
        </div>
        <div className="flex flex-col gap-1.5">
          <span className="text-[10px] font-medium uppercase tracking-wider text-muted-foreground">Zoom</span>
          <div role="group" aria-label="Overview map zoom" className="flex items-center justify-between gap-2">
            <Button
              type="button"
              size="sm"
              variant="outline"
              aria-label="Zoom out"
              disabled={view.zoomOffset <= INSET_ZOOM_STEP_MIN}
              onClick={() => onChange({ ...view, zoomOffset: clampInsetStep(view.zoomOffset - 1) })}
            >
              <Minus className="h-4 w-4" data-icon="inline-start" aria-hidden="true" />
              Wider
            </Button>
            <span data-testid="inset-zoom-step" className="min-w-6 text-center text-xs tabular-nums text-foreground">
              {insetStepLabel(view.zoomOffset)}
            </span>
            <Button
              type="button"
              size="sm"
              variant="outline"
              aria-label="Zoom in"
              disabled={view.zoomOffset >= INSET_ZOOM_STEP_MAX}
              onClick={() => onChange({ ...view, zoomOffset: clampInsetStep(view.zoomOffset + 1) })}
            >
              <Plus className="h-4 w-4" data-icon="inline-start" aria-hidden="true" />
              Closer
            </Button>
          </div>
        </div>
      </PopoverContent>
    </Popover>
  )
}
