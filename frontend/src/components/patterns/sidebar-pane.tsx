import { ChevronLeft, ChevronRight, type LucideIcon } from 'lucide-react'

import { Button } from '@/components/ui/button'
import { SidebarHeader } from '@/components/ui/sidebar'

// The header and the collapsed rail shared by the Rode Planner (a right-hand
// pane) and TreeLayout (a left-hand one), so the two read as one control.
// The chevron points toward the edge the pane hides into: a left pane hides
// with a left chevron and its rail opens with a right one, and a right pane
// the reverse.

type Side = 'left' | 'right'

export interface SidebarPaneHeaderProps {
  side: Side
  icon: LucideIcon
  /** The pane's name, shown beside the icon. */
  label: string
  /** The hide button's accessible name and title. */
  hideLabel: string
  /** The id of the pane content the hide button controls. */
  controlsId: string
  onHide: () => void
}

export function SidebarPaneHeader({ side, icon: Icon, label, hideLabel, controlsId, onHide }: SidebarPaneHeaderProps) {
  const Chevron = side === 'left' ? ChevronLeft : ChevronRight
  return (
    <SidebarHeader className="flex-row items-center justify-between border-b">
      <span className="inline-flex items-center gap-1.5 px-1 text-[10px] font-semibold uppercase tracking-[0.16em] text-muted-foreground">
        <Icon className="h-3.5 w-3.5 shrink-0" aria-hidden="true" />
        {label}
      </span>
      <Button
        type="button"
        variant="ghost"
        size="icon-sm"
        aria-label={hideLabel}
        title={hideLabel}
        aria-expanded
        aria-controls={controlsId}
        onClick={onHide}
      >
        <Chevron className="h-4 w-4" aria-hidden="true" />
      </Button>
    </SidebarHeader>
  )
}

export interface SidebarPaneRailProps {
  side: Side
  icon: LucideIcon
  /** The rail's accessible name and title. */
  label: string
  onShow: () => void
}

export function SidebarPaneRail({ side, icon: Icon, label, onShow }: SidebarPaneRailProps) {
  const Chevron = side === 'left' ? ChevronRight : ChevronLeft
  // No aria-controls: the pane's content is not mounted while this shows.
  // The strip runs the height of the list; only its top is the button, so a
  // stray tap beside a long list does not reopen the pane.
  return (
    <div className="w-10 shrink-0 rounded-xl border bg-sidebar text-sidebar-foreground">
      <button
        type="button"
        aria-expanded={false}
        aria-label={label}
        title={label}
        onClick={onShow}
        className="flex w-full flex-col items-center gap-2 rounded-xl py-3 hover:bg-sidebar-accent"
      >
        <Icon className="h-4 w-4" aria-hidden="true" />
        <Chevron className="h-4 w-4" aria-hidden="true" />
      </button>
    </div>
  )
}
