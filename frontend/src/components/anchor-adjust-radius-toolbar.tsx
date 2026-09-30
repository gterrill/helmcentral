import { Minus, Plus } from 'lucide-react'
import { usePressRepeat } from '@/hooks/use-press-repeat'
import { formatRadiusDisplay, radiusDisplayValue } from '@/lib/anchor-adjust'
import { cn } from '@/lib/utils'

export interface AnchorAdjustRadiusToolbarProps {
  radiusM: number
  isImperial: boolean
  atMin: boolean
  atMax: boolean
  /** "Chain onboard + boat length" / "Chain onboard, without boat length" — the + button's own disabled reason. */
  maxDisabledReason: string
  stepM: number
  onStepRadius: (deltaM: number) => void
  /**
   * 'overlay' (default): dark scrim matching the map's own corner controls
   * (bg-black/65, the same as the zoom/satellite/recentre buttons it sits
   * under). 'panel': themed surface tokens (bg-card/text-foreground), for
   * the no-WebGL2 inline fallback (anchor-watch-drawer.tsx), which has no
   * map/imagery behind it to justify the raw-colour overlay exception.
   */
  variant?: 'overlay' | 'panel'
  className?: string
}

/**
 * Adjust mode's radius stepper (ADR 0143): a horizontal −/readout/+ row, reused both
 * inside AnchorWatchMap's own top-right control column (slid out directly
 * under the Adjust icon) and inline in the no-WebGL2 fallback
 * (anchor-watch-drawer.tsx), which has no map to put a control stack on at
 * all. 40px+ touch targets throughout, for use on a moving boat. Hold-to-
 * repeat (hooks/use-press-repeat.ts) on both buttons; keyboard activation
 * (Enter/Space -> a synthetic click with detail 0) is handled the same way
 * the buttons themselves used to under the old bottom bar (code-review
 * finding carried forward): a real pointer/touch press already stepped via
 * onPointerDown, so only a detail-0 click (keyboard) steps again here.
 */
export function AnchorAdjustRadiusToolbar({
  radiusM,
  isImperial,
  atMin,
  atMax,
  maxDisabledReason,
  stepM,
  onStepRadius,
  variant = 'overlay',
  className,
}: AnchorAdjustRadiusToolbarProps) {
  const decrement = usePressRepeat(() => onStepRadius(-stepM), atMin)
  const increment = usePressRepeat(() => onStepRadius(stepM), atMax)

  const handleKeyboardClick = (deltaM: number) => (event: React.MouseEvent<HTMLButtonElement>) => {
    if (event.detail === 0) onStepRadius(deltaM)
  }

  const overlay = variant === 'overlay'
  const buttonClass = cn(
    'flex h-10 w-10 shrink-0 items-center justify-center rounded-md active:scale-95 disabled:pointer-events-none disabled:opacity-40',
    overlay ? 'text-white hover:bg-white/15' : 'text-foreground hover:bg-accent',
  )

  return (
    <div
      data-testid="anchor-adjust-radius-toolbar"
      className={cn(
        'flex flex-row items-center gap-0.5 rounded-lg p-1 shadow-sm backdrop-blur-sm',
        overlay ? 'bg-black/65 text-white' : 'border border-border bg-card text-foreground',
        className,
      )}
    >
      <button
        type="button"
        aria-label="Decrease radius"
        disabled={atMin}
        title={atMin ? `Minimum ${formatRadiusDisplay(radiusM, isImperial)}` : undefined}
        className={buttonClass}
        onPointerDown={decrement.onPointerDown}
        onPointerUp={decrement.onPointerUp}
        onPointerLeave={decrement.onPointerLeave}
        onPointerCancel={decrement.onPointerCancel}
        onClick={handleKeyboardClick(-stepM)}
      >
        <Minus className="h-4 w-4" />
      </button>

      <p className="min-w-12 select-none whitespace-nowrap px-1 text-center leading-none">
        <span
          data-testid="anchor-adjust-radius-value"
          className={cn('text-sm font-semibold tabular-nums', overlay ? 'text-white' : 'text-gauge-primary')}
        >
          {radiusDisplayValue(radiusM, isImperial)}
        </span>
        <span
          data-testid="anchor-adjust-radius-unit"
          className={cn('ml-1 text-xs', overlay ? 'text-white/80' : 'text-muted-foreground')}
        >
          {isImperial ? 'ft' : 'm'}
        </span>
      </p>

      <button
        type="button"
        aria-label="Increase radius"
        disabled={atMax}
        title={atMax ? maxDisabledReason : undefined}
        className={buttonClass}
        onPointerDown={increment.onPointerDown}
        onPointerUp={increment.onPointerUp}
        onPointerLeave={increment.onPointerLeave}
        onPointerCancel={increment.onPointerCancel}
        onClick={handleKeyboardClick(stepM)}
      >
        <Plus className="h-4 w-4" />
      </button>
    </div>
  )
}
