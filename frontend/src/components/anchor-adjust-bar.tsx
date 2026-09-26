import { Minus, Plus } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { usePressRepeat } from '@/hooks/use-press-repeat'
import { formatRadiusDisplay } from '@/lib/anchor-adjust'
import { cn } from '@/lib/utils'

const METERS_PER_FOOT = 3.28084

function radiusValueDisplay(radiusM: number, isImperial: boolean): string {
  return String(Math.round(isImperial ? radiusM * METERS_PER_FOOT : radiusM))
}

/** One of the bar's two recommendation shortcuts ("Rode + LOA", "Planner swing") — tapping an enabled chip sets the draft radius straight to it. */
export interface AnchorAdjustChip {
  label: string
  valueM: number | null
  /** Shown as the disabled chip's own reason (fail visibly, no substitution) — set exactly when valueM is null. */
  reason: string | null
}

export interface AnchorAdjustBarProps {
  radiusM: number
  isImperial: boolean
  atMin: boolean
  atMax: boolean
  /** "Chain onboard + boat length" / "Chain onboard, without boat length" — shown as the disabled + button's own reason. */
  maxDisabledReason: string
  stepM: number
  onStepRadius: (deltaM: number) => void
  chips: AnchorAdjustChip[]
  onApplyChip: (valueM: number) => void
  warningActive: boolean
  /** True once the first Set tap has been absorbed by the warning and is waiting for the confirming second tap — owned by the host (anchor-watch-drawer.tsx) so Enter, which reaches this same commit path through the map's own keyboard handler, shares the identical confirm state. */
  confirmingSet: boolean
  committing: boolean
  onCancel: () => void
  onSet: () => void
  /** No-WebGL2 path: there is no map, so no crosshair/pan — only the radius controls apply, against the anchor's existing position. */
  noMapNotice?: boolean
  /** Set when the watch's committed radius, at the moment Adjust opened, was above the current chain+LOA ceiling (possible from before that ceiling existed) — the raw value that got clamped down to the ring/draft shown, so the operator knows it changed rather than silently seeing a smaller number than they last set. */
  aboveMaxOriginalRadiusM?: number | null
  /** There is no live boat position (no fix yet, or the GNSS gate is holding one back) — "Alarm would sound now" can't be evaluated, so this replaces it rather than leaving the check silently skipped. */
  noFixNotice?: boolean
}

/**
 * Adjust mode's bottom bar (ADR 0136) — rendered once by the host
 * (anchor-watch-drawer.tsx) regardless of whether a map is actually present,
 * so the radius controls, the chips, and Cancel/Set behave identically
 * whether or not WebGL2 is available. 48px touch targets throughout, for use
 * on a moving boat.
 */
export function AnchorAdjustBar({
  radiusM,
  isImperial,
  atMin,
  atMax,
  maxDisabledReason,
  stepM,
  onStepRadius,
  chips,
  onApplyChip,
  warningActive,
  confirmingSet,
  committing,
  onCancel,
  onSet,
  noMapNotice = false,
  aboveMaxOriginalRadiusM = null,
  noFixNotice = false,
}: AnchorAdjustBarProps) {
  const decrement = usePressRepeat(() => onStepRadius(-stepM))
  const increment = usePressRepeat(() => onStepRadius(stepM))

  // Spelled out under the controls, not left in hover titles: on the helm
  // touchscreen a title never shows, and a disabled control with no visible
  // reason reads as broken.
  const disabledReasons = [
    ...(atMin ? [`Minimum ${formatRadiusDisplay(radiusM, isImperial)}`] : []),
    ...(atMax ? [`Maximum: ${maxDisabledReason}`] : []),
    ...chips.filter((chip) => chip.valueM === null && chip.reason).map((chip) => `${chip.label}: ${chip.reason}`),
  ]

  return (
    <div
      data-testid="anchor-adjust-bar"
      className="flex shrink-0 flex-col gap-3 border-t border-border bg-card px-3 py-3 sm:px-4"
    >
      {noMapNotice && (
        <p className="text-center text-xs text-muted-foreground">Moving the anchor needs the map</p>
      )}

      {aboveMaxOriginalRadiusM !== null && (
        <p className="text-center text-[11px] text-muted-foreground">
          Was {formatRadiusDisplay(aboveMaxOriginalRadiusM, isImperial)}, above the maximum
        </p>
      )}

      <div className="flex items-center justify-center gap-4">
        <Button
          type="button"
          variant="outline"
          size="icon"
          className="h-12 w-12 shrink-0"
          aria-label="Decrease radius"
          disabled={atMin}
          title={atMin ? 'Minimum alarm radius' : undefined}
          onPointerDown={decrement.onPointerDown}
          onPointerUp={decrement.onPointerUp}
          onPointerLeave={decrement.onPointerLeave}
          onPointerCancel={decrement.onPointerCancel}
        >
          <Minus className="h-5 w-5" />
        </Button>

        <div className="min-w-0 text-center">
          <p className="text-[10px] uppercase tracking-[0.16em] text-muted-foreground">Radius</p>
          <p className="font-display text-4xl leading-none tabular-nums text-gauge-primary">
            <span data-testid="anchor-adjust-radius-value">{radiusValueDisplay(radiusM, isImperial)}</span>
            <span data-testid="anchor-adjust-radius-unit" className="ml-2 align-baseline text-lg text-muted-foreground">
              {isImperial ? 'ft' : 'm'}
            </span>
          </p>
        </div>

        <Button
          type="button"
          variant="outline"
          size="icon"
          className="h-12 w-12 shrink-0"
          aria-label="Increase radius"
          disabled={atMax}
          title={atMax ? maxDisabledReason : undefined}
          onPointerDown={increment.onPointerDown}
          onPointerUp={increment.onPointerUp}
          onPointerLeave={increment.onPointerLeave}
          onPointerCancel={increment.onPointerCancel}
        >
          <Plus className="h-5 w-5" />
        </Button>
      </div>

      <div className="flex flex-wrap items-center justify-center gap-2">
        {chips.map((chip) => (
          <Button
            key={chip.label}
            type="button"
            variant="secondary"
            size="sm"
            className="h-12 min-w-0"
            disabled={chip.valueM === null}
            title={chip.valueM === null ? chip.reason ?? undefined : undefined}
            onClick={() => { if (chip.valueM !== null) onApplyChip(chip.valueM) }}
          >
            <span className="truncate">
              {chip.label}
              {chip.valueM !== null && (
                <span className="ml-1 text-muted-foreground">{formatRadiusDisplay(chip.valueM, isImperial)}</span>
              )}
            </span>
          </Button>
        ))}
      </div>

      {disabledReasons.length > 0 && (
        <p data-testid="anchor-adjust-reasons" className="text-center text-[11px] text-muted-foreground">
          {disabledReasons.join(' · ')}
        </p>
      )}

      {warningActive && (
        <div
          role="alert"
          data-testid="anchor-adjust-warning"
          className="rounded-md border border-amber-500/50 bg-amber-500/10 px-3 py-2 text-center text-xs font-semibold text-amber-700 dark:border-amber-400/40 dark:bg-amber-950 dark:text-amber-400"
        >
          Alarm would sound now
        </div>
      )}

      {noFixNotice && (
        <p className="text-center text-[11px] text-muted-foreground">
          No GPS fix: can&apos;t check the boat against this circle
        </p>
      )}

      <div className="flex items-center justify-center gap-3">
        <Button type="button" variant="outline" size="lg" className="h-12 flex-1 max-w-40" onClick={onCancel}>
          Cancel
        </Button>
        <Button
          type="button"
          size="lg"
          className={cn('h-12 flex-1 max-w-52', warningActive && confirmingSet && 'bg-destructive text-destructive-foreground hover:bg-destructive/90')}
          disabled={committing}
          onClick={onSet}
        >
          {warningActive && confirmingSet ? 'Set anyway' : `Set ${formatRadiusDisplay(radiusM, isImperial)}`}
        </Button>
      </div>
    </div>
  )
}
