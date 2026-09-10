interface KioskFoldGuideProps {
  /** Pixels from the top of the kiosk root, e.g. KIOSK_FOLD_PX from lib/kiosk.ts. */
  topPx: number
}

/**
 * A layout aid, not a kiosk feature: shown only in layout mode on a page
 * flagged for kiosk (App.tsx), so an operator authoring on the helm browser
 * can see exactly where the wall's 360px strip cuts off before saving.
 */
export function KioskFoldGuide({ topPx }: KioskFoldGuideProps) {
  return (
    <div
      data-testid="kiosk-fold"
      className="pointer-events-none absolute inset-x-0 z-10 border-t-2 border-dashed border-amber-500/70"
      style={{ top: topPx }}
    >
      <span className="absolute right-0 top-1 rounded-b-md bg-amber-500/10 px-2 py-0.5 text-[10px] font-semibold uppercase tracking-[0.08em] text-amber-500">
        Kiosk fold, 360 px
      </span>
    </div>
  )
}
