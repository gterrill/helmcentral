import type { ManualTarget } from '@/lib/manual-links'
import { CREATE_PAGE_MANUAL_TARGET } from '@/lib/manual-links'

interface EmptyPagePromptProps {
  /** Layout mode is on — the operator can act on this prompt right now. */
  editing: boolean
  /** Whether the screen is wide enough for layout mode to exist at all (the
   * `lg` breakpoint DashboardBentoGrid itself gates on). */
  canEditLayout: boolean
  onOpenManual: (target: ManualTarget) => void
  /** Never shown on the wall display — there is nothing there to click, and
   * an unattended screen should never sit on an instruction meant for an
   * operator with a mouse. */
  isKiosk?: boolean
}

const PROMPT_CLASSNAME = 'flex min-h-40 flex-col items-center justify-center gap-2 rounded-xl border border-dashed border-border bg-background/40 px-6 py-10 text-center text-sm text-muted-foreground'

/**
 * Stands in for the grid on a page with nothing on it yet (ADR 0107). Before
 * this, an empty page was just a blank stretch of background — nothing told
 * a first-time operator that Add Widget was the way in.
 */
export function EmptyPagePrompt({ editing, canEditLayout, onOpenManual, isKiosk = false }: EmptyPagePromptProps) {
  if (isKiosk) return null

  if (editing) {
    return (
      <div className={PROMPT_CLASSNAME}>
        <p>
          Create your personalized page. Use the Add Widget button to get started.
          Drag widgets to rearrange.
        </p>
        <button
          type="button"
          onClick={() => onOpenManual(CREATE_PAGE_MANUAL_TARGET)}
          className="text-xs font-semibold uppercase tracking-[0.1em] text-primary hover:underline"
        >
          How to build a page
        </button>
      </div>
    )
  }

  if (canEditLayout) {
    return (
      <div className={PROMPT_CLASSNAME}>
        <p>Nothing on this page yet. Press Edit to add widgets.</p>
      </div>
    )
  }

  return (
    <div className={PROMPT_CLASSNAME}>
      <p>Nothing on this page yet. Widgets are added on a screen at least 1024px wide.</p>
    </div>
  )
}
