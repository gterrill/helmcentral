import { Fragment, useEffect, useRef } from 'react'

import { Card, CardContent } from '@/components/ui/card'
import { cn } from '@/lib/utils'

export interface SectionNavGroup<Id extends string> {
  /** Omitted for a page with a single, unlabelled group of sections -
   * present for a page whose sections fall into named groups (e.g.
   * Settings' "Boat & app" / "Connections", Inventory's "Inventory" /
   * "Servicing"). */
  label?: string
  items: Array<{ id: Id; label: string }>
}

interface SectionNavProps<Id extends string> {
  groups: Array<SectionNavGroup<Id>>
  activeId: Id
  onSelect: (id: Id) => void
  'aria-label': string
}

/**
 * The second of the app's two nav patterns: the sidebar lists PLACES the
 * operator goes; this is the in-page nav for a page with several SECTIONS
 * of one job (Settings, Inventory). A pure controlled list - no state of
 * its own, no scroll-spy, no fetches. The host page owns which section is
 * active and mirrors it to the URL (ADR 0074).
 *
 * `md:sticky md:top-4 md:self-start` keeps the card only as tall as its own
 * items rather than stretching to the height of its flex sibling (the
 * section content), and keeps it in view while a long section scrolls.
 *
 * Below `md` this collapses to a horizontal scroll strip: group headings
 * hide and every item sits flat in one row, so grouping is a desktop-only
 * affordance and never breaks the compact layout on a phone or the helm
 * tablet.
 */
export function SectionNav<Id extends string>({
  groups,
  activeId,
  onSelect,
  'aria-label': ariaLabel,
}: SectionNavProps<Id>) {
  // Below `md` the items sit in a sideways strip, and a deep link to a late
  // section (/settings/logs) would otherwise open with the active item out of
  // sight. Scroll the strip itself, never the page, and only when it actually
  // overflows, so the desktop column is untouched.
  const stripRef = useRef<HTMLDivElement>(null)
  useEffect(() => {
    const strip = stripRef.current
    if (!strip || strip.scrollWidth <= strip.clientWidth) return
    const active = strip.querySelector<HTMLElement>('[aria-current="true"]')
    if (!active) return
    strip.scrollLeft = Math.max(0, active.offsetLeft - (strip.clientWidth - active.offsetWidth) / 2)
  }, [activeId])

  return (
    <nav aria-label={ariaLabel} className="md:sticky md:top-4 md:self-start">
      <Card className="w-full gap-0 py-2 md:w-48">
        <CardContent
          ref={stripRef}
          data-testid="section-nav-strip"
          className="relative flex flex-row gap-1 overflow-x-auto px-2 md:flex-col md:overflow-visible"
        >
          {groups.map((group, groupIndex) => {
            const buttons = group.items.map((item) => {
              const active = item.id === activeId
              return (
                <button
                  key={item.id}
                  type="button"
                  onClick={() => onSelect(item.id)}
                  aria-current={active ? 'true' : undefined}
                  className={cn(
                    'whitespace-nowrap rounded-md px-3 py-2 text-left text-sm font-medium transition-colors',
                    active
                      ? 'bg-primary/10 text-primary'
                      : 'text-muted-foreground hover:bg-muted hover:text-foreground',
                  )}
                >
                  {item.label}
                </button>
              )
            })

            if (!group.label) {
              return <Fragment key={`group-${groupIndex}`}>{buttons}</Fragment>
            }

            // `contents` unwraps this div from layout (both the mobile flat
            // row and the desktop column keep coming straight from
            // CardContent's own flex), while still giving the group a real
            // DOM node to carry `role="group"` on. The heading inside is a
            // plain span, never a button, so it is not part of tab order.
            return (
              <div key={group.label} role="group" aria-label={group.label} className="contents">
                <span
                  className={cn(
                    'hidden px-3 pb-1 text-[10px] font-medium tracking-wider text-muted-foreground uppercase md:block',
                    groupIndex > 0 && 'pt-3',
                  )}
                >
                  {group.label}
                </span>
                {buttons}
              </div>
            )
          })}
        </CardContent>
      </Card>
    </nav>
  )
}
