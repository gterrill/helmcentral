interface DisplayFoldGuideProps {
  /** Pixels from the top of the display's inner canvas - displayFoldPx(display)
   * from lib/displays.ts, which already accounts for the pixel-shift reserve. */
  topPx: number
  /** The display's own configured height, e.g. 360 or 720 - what the label
   * quotes. Not always equal to `topPx`: displayFoldPx trims room for the
   * pixel shift, but the operator wants to see the display's actual height,
   * not the trimmed fold budget. */
  heightPx: number
  /** e.g. "Flybridge" or "Saloon TV" - whichever display the page being
   * authored is assigned to. */
  displayName: string
}

/**
 * A layout aid, not a wall-display feature: shown only in layout mode on a
 * page assigned to a display (App.tsx), so an operator authoring on the helm
 * browser can see exactly where that screen's canvas cuts off before saving.
 */
export function DisplayFoldGuide({ topPx, heightPx, displayName }: DisplayFoldGuideProps) {
  return (
    <div
      data-testid="display-fold"
      className="pointer-events-none absolute inset-x-0 z-10 border-t-2 border-dashed border-amber-500/70"
      style={{ top: topPx }}
    >
      <span className="absolute right-0 top-1 rounded-b-md bg-amber-500/10 px-2 py-0.5 text-[10px] font-semibold uppercase tracking-[0.08em] text-amber-500">
        {displayName} fold, {heightPx} px
      </span>
    </div>
  )
}
