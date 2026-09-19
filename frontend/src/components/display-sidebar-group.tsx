import { Anchor, ExternalLink, MonitorPlay } from 'lucide-react'
import {
  SidebarMenuItem,
  SidebarMenuButton,
  SidebarMenuSub,
  SidebarMenuSubButton,
  SidebarMenuSubItem,
} from '@/components/ui/sidebar'
import type { Display } from '@/lib/displays'

export interface WallPageGlyphPage {
  display_id?: string
  dwell_seconds?: number
  show_when?: 'always' | 'anchored' | 'motoring' | 'sailing' | 'moored'
}

/**
 * Ported from dashboard-page-switcher.tsx's `KioskPageGlyph` (ADR 0089) and
 * renamed for ADR 0110's vocabulary. It only has one place left to render:
 * a page assigned to a display can no longer appear in the switcher's own
 * popover (that would be a wall page pretending to be a dashboard page), so
 * the glyph moved here, nested under its display instead.
 */
export function WallPageGlyph({ page }: { page: WallPageGlyphPage }) {
  if (!page.display_id || !page.dwell_seconds) return null
  const whenSuffix = page.show_when && page.show_when !== 'always' ? `, while ${page.show_when}` : ''
  return (
    <span
      className="inline-flex shrink-0 items-center gap-0.5 text-[10px] text-muted-foreground"
      title={`${page.dwell_seconds}s on this display${whenSuffix}`}
    >
      {page.dwell_seconds}s
      {page.show_when === 'anchored' && <Anchor className="h-3 w-3" aria-hidden="true" />}
    </span>
  )
}

export interface DisplaySidebarPage {
  id: string
  name: string
  display_id?: string
  dwell_seconds?: number
  show_when?: WallPageGlyphPage['show_when']
}

interface DisplaySidebarGroupProps {
  displays: Display[]
  /**
   * Every dashboard page — this group does its own filtering by
   * `display_id` per display, so pass the same full list App.tsx already
   * fetches rather than pre-slicing it per display.
   */
  pages: DisplaySidebarPage[]
  activePageId: string | null
  /**
   * True while the plain dashboard/page navigation is what's showing
   * (App.tsx's `activePanel === null`) — a wall page row is only ever
   * "active" in that state, the same rule the old Dashboard sub-list used.
   */
  dashboardActive: boolean
  /** Navigates to `page.id` the ordinary way — a wall page is still an
   * editable dashboard page at `/dashboard/<id>`, not a route of its own. */
  onSelectPage: (id: string) => void
  /** Opens the displays-management dialog (displays-dialog.tsx). */
  onOpenDisplaysDialog: () => void
}

/**
 * The sidebar group that replaces the old single "Wall display" link
 * (ADR 0110 §6 "The new sidebar section"): each display gets its own row
 * with a link that opens `/display/<slug>` in a new tab, and that display's
 * own pages nest under it. The group header always renders — even with zero
 * displays configured — because it's the only way to reach the dialog that
 * creates the first one; a control that can only say no is a dead end.
 */
export function DisplaySidebarGroup({
  displays,
  pages,
  activePageId,
  dashboardActive,
  onSelectPage,
  onOpenDisplaysDialog,
}: DisplaySidebarGroupProps) {
  return (
    <SidebarMenuItem>
      <SidebarMenuButton onClick={onOpenDisplaysDialog} tooltip="Wall displays">
        <MonitorPlay />
        <span>Wall displays</span>
      </SidebarMenuButton>
      {displays.length > 0 && (
        <SidebarMenuSub>
          {displays.map((display) => {
            const displayPages = pages.filter((page) => page.display_id === display.id)
            return (
              <SidebarMenuSubItem key={display.id}>
                <SidebarMenuSubButton
                  render={<a href={`/display/${display.slug}`} target="_blank" rel="noopener noreferrer" />}
                >
                  <span className="min-w-0 flex-1 truncate">{display.name}</span>
                  <ExternalLink className="h-3 w-3 shrink-0" aria-hidden="true" />
                </SidebarMenuSubButton>
                {displayPages.length > 0 && (
                  <SidebarMenuSub>
                    {displayPages.map((page) => (
                      <SidebarMenuSubItem key={page.id}>
                        <SidebarMenuSubButton
                          render={<button type="button" />}
                          isActive={dashboardActive && page.id === activePageId}
                          onClick={() => onSelectPage(page.id)}
                        >
                          <span className="min-w-0 flex-1 truncate">{page.name}</span>
                          <WallPageGlyph page={page} />
                        </SidebarMenuSubButton>
                      </SidebarMenuSubItem>
                    ))}
                  </SidebarMenuSub>
                )}
              </SidebarMenuSubItem>
            )
          })}
        </SidebarMenuSub>
      )}
    </SidebarMenuItem>
  )
}
