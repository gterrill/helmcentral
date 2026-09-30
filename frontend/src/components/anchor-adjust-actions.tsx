import { Button } from '@/components/ui/button'
import { formatRadiusDisplay } from '@/lib/anchor-adjust'
import { cn } from '@/lib/utils'

export interface AnchorAdjustActionsProps {
  isImperial: boolean
  atMin: boolean
  atMax: boolean
  radiusM: number
  /** "Chain onboard + boat length" / "Chain onboard, without boat length". */
  maxDisabledReason: string
  warningActive: boolean
  /** True once the first Save tap has been absorbed by the warning and is waiting for the confirming second tap. */
  confirmingSet: boolean
  committing: boolean
  /** Discards the draft — same path as the old Cancel. */
  onCancel: () => void
  /** Commits the draft — same path as the old Set. */
  onSave: () => void
  /** No-WebGL2 path: there is no map, so no crosshair/pan — only the radius toolbar applies, against the anchor's existing position. */
  noMapNotice?: boolean
  /** Set when the watch's committed radius, at the moment Adjust opened, was above the current chain+LOA ceiling — the raw value that got clamped down to the draft shown. */
  aboveMaxOriginalRadiusM?: number | null
  /** There is no live boat position — "Alarm would sound now" can't be evaluated, so this replaces it. */
  noFixNotice?: boolean
  /**
   * 'overlay' (default): floats over the map (bottom-centre, dark scrim for
   * contrast against arbitrary chart imagery, matching the map's own metric
   * overlay). 'panel': themed surface tokens, for the no-WebGL2 inline
   * fallback, which has nothing to overlay.
   */
  variant?: 'overlay' | 'panel'
  className?: string
}

/**
 * Adjust mode's safety notices and Cancel/Save (ADR 0143, renamed from
 * Cancel/Set): rendered as a floating overlay at the bottom of the map when
 * a map is present, and inline in the no-WebGL2 fallback otherwise — same
 * component, same logic, reused rather than duplicated (anchor-adjust-
 * radius-toolbar.tsx gets the same treatment for the −/readout/+ stepper).
 */
export function AnchorAdjustActions({
  isImperial,
  atMin,
  atMax,
  radiusM,
  maxDisabledReason,
  warningActive,
  confirmingSet,
  committing,
  onCancel,
  onSave,
  noMapNotice = false,
  aboveMaxOriginalRadiusM = null,
  noFixNotice = false,
  variant = 'overlay',
  className,
}: AnchorAdjustActionsProps) {
  const overlay = variant === 'overlay'

  // Spelled out as visible text, not left in hover titles: on the helm
  // touchscreen a title never shows, and a disabled control with no visible
  // reason reads as broken. The radius toolbar itself is a narrow corner
  // control with no room for this, so it surfaces here instead — still
  // "somewhere readable", just not on the button itself.
  const disabledReasons = [
    ...(atMin ? [`Minimum ${formatRadiusDisplay(radiusM, isImperial)}`] : []),
    ...(atMax ? [`Maximum: ${maxDisabledReason}`] : []),
  ]

  const noticeClass = overlay ? 'text-white/90' : 'text-muted-foreground'

  return (
    <div
      data-testid="anchor-adjust-actions"
      className={cn(
        'flex flex-col items-center gap-2',
        overlay && 'rounded-lg bg-black/80 px-3 py-2 text-white shadow-lg backdrop-blur-sm',
        className,
      )}
    >
      {noMapNotice && <p className={cn('text-center text-xs', noticeClass)}>Moving the anchor needs the map</p>}

      {aboveMaxOriginalRadiusM !== null && (
        <p className={cn('text-center text-xs', noticeClass)}>
          Was {formatRadiusDisplay(aboveMaxOriginalRadiusM, isImperial)}, above the maximum
        </p>
      )}

      {disabledReasons.length > 0 && (
        <p data-testid="anchor-adjust-reasons" className={cn('text-center text-xs', noticeClass)}>
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
        <p className={cn('text-center text-xs', noticeClass)}>
          No GPS fix: can&apos;t check the boat against this circle
        </p>
      )}

      <div className="flex items-center justify-center gap-3">
        <Button type="button" variant="outline" size="lg" className="h-12 flex-1 max-w-40 text-foreground" onClick={onCancel}>
          Cancel
        </Button>
        <Button
          type="button"
          size="lg"
          className={cn('h-12 flex-1 max-w-40', warningActive && confirmingSet && 'bg-destructive text-destructive-foreground hover:bg-destructive/90')}
          disabled={committing}
          onClick={onSave}
        >
          {warningActive && confirmingSet ? 'Save anyway' : 'Save'}
        </Button>
      </div>
    </div>
  )
}
