import { Plus } from 'lucide-react'

import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import {
  DASHBOARD_WIDGET_CATEGORY,
  DASHBOARD_WIDGET_IDS,
  DASHBOARD_WIDGET_LABELS,
  WIDGET_CATEGORIES,
  type BuiltinWidgetId,
  type WidgetCategory,
} from '@/lib/dashboard-widgets'

/**
 * A multi-instance addition (Gauge, Gauge Group, Engine Cluster, an equipment
 * profile, Indicators, Embed, Nearby map…) offered from the same menu as the
 * built-in widgets. These aren't in DASHBOARD_WIDGET_IDS — App.tsx opens a
 * config dialog for each rather than placing a widget straight away — so the
 * picker takes them as plain entries instead of trying to fold them into the
 * built-in list.
 */
export interface AddTileMultiInstanceEntry {
  label: string
  category: WidgetCategory
  onSelect: () => void
}

interface AddTilePickerProps {
  /** Ids already on the page — these built-ins render disabled (ADR 0107). */
  placedWidgetIds: readonly string[]
  onAddWidget: (id: BuiltinWidgetId) => void
  multiInstanceEntries: readonly AddTileMultiInstanceEntry[]
}

/**
 * The grouped Add Tile menu (ADR 0107). Every built-in widget shows here
 * always, with the ones already on the page greyed out and labelled rather
 * than missing, so it's obvious why a click does nothing. Built on the
 * shadcn/Base UI dropdown menu rather than the plain Popover the old flat
 * list used, for its grouping and keyboard navigation.
 */
export function AddTilePicker({ placedWidgetIds, onAddWidget, multiInstanceEntries }: AddTilePickerProps) {
  const placed = new Set(placedWidgetIds)

  return (
    <DropdownMenu>
      <DropdownMenuTrigger className="inline-flex w-fit items-center gap-1 rounded-md border border-border bg-background/70 px-3 py-1.5 text-xs font-semibold uppercase tracking-[0.1em] text-muted-foreground hover:border-primary/40 hover:text-primary">
        <Plus className="h-3.5 w-3.5" />
        Add Tile
      </DropdownMenuTrigger>
      {/* w-64, not the old popover's w-56: "Nearby Vessels" plus a trailing
          "On page" hint needs the extra room to stay on one line. Scrolling
          and the z-60 stacking above the alarm banner both come from
          DropdownMenuContent itself (components/ui/dropdown-menu.tsx). */}
      <DropdownMenuContent className="w-64" align="start">
        {WIDGET_CATEGORIES.map(({ id: categoryId, label }, index) => {
          const builtins = DASHBOARD_WIDGET_IDS.filter((id) => DASHBOARD_WIDGET_CATEGORY[id] === categoryId)
          const extras = multiInstanceEntries.filter((entry) => entry.category === categoryId)
          if (builtins.length === 0 && extras.length === 0) return null

          return (
            <div key={categoryId}>
              {index > 0 && <DropdownMenuSeparator />}
              <DropdownMenuGroup>
                <DropdownMenuLabel>{label}</DropdownMenuLabel>
                {builtins.map((id) => {
                  const isPlaced = placed.has(id)
                  return (
                    <DropdownMenuItem
                      key={id}
                      disabled={isPlaced}
                      onClick={() => {
                        if (!isPlaced) onAddWidget(id)
                      }}
                    >
                      <span className="min-w-0 flex-1 truncate">{DASHBOARD_WIDGET_LABELS[id]}</span>
                      {isPlaced && (
                        <span className="shrink-0 text-[10px] uppercase tracking-wider text-muted-foreground">On page</span>
                      )}
                    </DropdownMenuItem>
                  )
                })}
                {extras.map((entry) => (
                  <DropdownMenuItem key={entry.label} onClick={entry.onSelect}>
                    {entry.label}
                  </DropdownMenuItem>
                ))}
              </DropdownMenuGroup>
            </div>
          )
        })}
      </DropdownMenuContent>
    </DropdownMenu>
  )
}
