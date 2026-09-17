import { Star } from 'lucide-react'
import { widgetDisplayName, type DashboardLayoutItem } from '@/lib/dashboard-widgets'

export interface HeroablePage {
  id: string
  name: string
  hero?: string
  widgets: DashboardLayoutItem[]
}

interface PageHeroSelectProps {
  page: HeroablePage | null
  onSetHero: (id: string, hero: string) => void
}

/**
 * The active page's hero widget (ADR 0072), as a layout control — the same
 * placement and shape as PageSkinSelect right beside it: a page-level
 * property belongs where an operator already is when deciding what the page
 * looks like, which puts it in layout mode alongside Add Widget rather than
 * three clicks away in the page-switcher popover.
 */
export function PageHeroSelect({ page, onSetHero }: PageHeroSelectProps) {
  if (!page) return null

  return (
    // A label wrapping the select, so the whole chip is the hit target and the
    // border can respond to focus inside it. Styled as the sibling of
    // PageSkinSelect and Add Widget, because that is what it is.
    <label className="inline-flex w-fit items-center gap-1.5 rounded-md border border-border bg-background/70 px-3 py-1.5 text-xs font-semibold uppercase tracking-[0.1em] text-muted-foreground transition-colors focus-within:border-primary/40 hover:border-primary/40 hover:text-primary">
      <Star className="h-3.5 w-3.5 shrink-0" aria-hidden="true" />
      {/* The name says which page, not just which setting: the board carries
          several and this edits whichever one is on screen. */}
      <select
        aria-label={`Hero widget for ${page.name}`}
        className="cursor-pointer bg-transparent text-xs font-semibold uppercase tracking-[0.1em] outline-hidden"
        value={page.hero ?? ''}
        onChange={(e) => onSetHero(page.id, e.target.value)}
      >
        <option value="">No hero</option>
        {page.widgets.map((w) => (
          <option key={w.id} value={w.id}>{widgetDisplayName(w)}</option>
        ))}
      </select>
    </label>
  )
}
