import type { ReactNode } from 'react'

import { cn } from '@/lib/utils'

// ADR 0142: the details/edit page shell - a main column plus an optional
// aside (quick facts, related records, a summary card). `minmax(0,1fr)` on
// the main column (AGENTS.md's layout-resiliency rule) is what lets it
// shrink instead of forcing the aside off-screen; single column below `lg`
// is the default grid behaviour (no aside column exists at all until the
// `lg:` override adds one), not a second layout bolted on afterward. The
// aside renders as a semantic `<aside>` (the ARIA "complementary" landmark)
// so a caller's own tests, and assistive tech, can find it without a
// bespoke test id.

export interface DetailsLayoutProps {
  aside?: ReactNode
  children: ReactNode
  className?: string
  /** Where the aside sits relative to the main column below `lg`, where the
   * grid collapses to one column and DOM order becomes visual order too.
   * 'end' (default) keeps the aside after the main content, matching its
   * own role as supplementary detail. 'start' puts it first - e.g. a
   * "Status" card an operator should see before scrolling into the form
   * itself. Implemented with CSS `order`, so it has no effect at `lg` and
   * above, where the aside is already its own column independent of DOM
   * order. */
  asidePosition?: 'start' | 'end'
}

export function DetailsLayout({ aside, children, className, asidePosition = 'end' }: DetailsLayoutProps) {
  const asideFirst = asidePosition === 'start' && !!aside
  return (
    <div className={cn('grid gap-4', aside ? 'lg:grid-cols-[minmax(0,1fr)_20rem]' : undefined, className)}>
      <div className={cn('flex min-w-0 flex-col gap-4', asideFirst && 'order-last lg:order-none')}>
        {children}
      </div>
      {aside && (
        <aside className={cn('flex min-w-0 flex-col gap-4', asideFirst && 'order-first lg:order-none')}>
          {aside}
        </aside>
      )}
    </div>
  )
}
