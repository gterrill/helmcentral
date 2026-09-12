import { ArrowLeft, BookOpen, Sparkles } from 'lucide-react'
import { useEffect, useRef, useState } from 'react'

import { ManualMarkdown } from '@/components/manual-markdown'
import { Button } from '@/components/ui/button'
import { Sheet, SheetContent, SheetHeader, SheetTitle } from '@/components/ui/sheet'
import { Skeleton } from '@/components/ui/skeleton'
import { useManualPage } from '@/hooks/use-manual'
import {
  MANUAL_INDEX,
  manualBodyWithoutTitle,
  slugifyHeading,
  type ManualLink,
  type ManualTarget,
} from '@/lib/manual-links'

interface ManualSheetProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  /** Where to land when the sheet opens - null means the contents page. */
  target: ManualTarget | null
  onAskMate: (question: string) => void
}

// The group line under the title (Feature / How-to / Reference / Contents):
// derived from the page id's own directory, the same three-way split
// docs/ itself uses (AGENTS.md's Diátaxis table) plus the hand-written
// contents page.
function manualGroupLabel(pageId: string): string {
  if (pageId === 'index') return 'Contents'
  if (pageId.startsWith('features/')) return 'Feature'
  if (pageId.startsWith('how-to/')) return 'How-to'
  if (pageId.startsWith('reference/')) return 'Reference'
  return pageId
}

/**
 * The in-app manual (ADR 0095): a right-hand sheet, same chrome as
 * mate-sheet.tsx, that renders one page of the embedded operator manual at a
 * time with its own back stack. Opened by the header's contextual `?`
 * (landing on the current screen's page/heading) or the sidebar's Manual
 * item (landing on the contents page, `target: null`).
 */
export function ManualSheet({ open, onOpenChange, target, onAskMate }: ManualSheetProps) {
  const [history, setHistory] = useState<ManualTarget[]>([target ?? MANUAL_INDEX])

  // Resets to the caller's target every time the sheet opens - same idiom as
  // mate-sheet.tsx's own open-keyed effects. The sheet is modal while open
  // (Base UI's Dialog), so the header `?` and sidebar Manual item that
  // supply `target` can't be reached again until it closes: `open` alone is
  // the whole story for "a new target has arrived".
  useEffect(() => {
    if (open) setHistory([target ?? MANUAL_INDEX])
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open])

  const current = history[history.length - 1]
  const isIndex = current.page === 'index'
  // Fetches nothing before the sheet has ever been opened.
  const manual = useManualPage(open ? current.page : null)

  const bodyRef = useRef<HTMLDivElement>(null)
  const [missingHeading, setMissingHeading] = useState<string | null>(null)

  // Scrolls to the current target's heading once its page has loaded. A
  // heading that isn't actually on the page never fails silently - the
  // sheet scrolls to the top and says so, rather than leaving the operator
  // looking at a page that doesn't seem to match what they asked for.
  useEffect(() => {
    setMissingHeading(null)
    if (!manual.page) return
    if (!current.heading) {
      if (bodyRef.current) bodyRef.current.scrollTop = 0
      return
    }
    const slug = slugifyHeading(current.heading)
    const headingEl = bodyRef.current?.querySelector('#' + CSS.escape(slug))
    if (headingEl) {
      headingEl.scrollIntoView({ block: 'start' })
    } else {
      if (bodyRef.current) bodyRef.current.scrollTop = 0
      setMissingHeading(current.heading)
    }
  }, [manual.page, current.heading])

  const openContents = () => setHistory((prev) => [...prev, MANUAL_INDEX])

  const handleNavigate = (link: ManualLink) => {
    if (link.kind === 'anchor') {
      // Same page, different heading: replaces the current entry rather
      // than pushing, so an in-page jump doesn't grow the Back stack.
      setHistory((prev) => {
        const next = [...prev]
        next[next.length - 1] = { ...next[next.length - 1], heading: link.hash }
        return next
      })
    } else if (link.kind === 'page') {
      setHistory((prev) => [...prev, { page: link.page, heading: link.hash }])
    }
    // 'external' never reaches here - ManualMarkdown opens it directly.
  }

  const title = manual.loading || isIndex ? 'Manual' : manual.page?.title ?? 'Manual'
  const groupLabel = manual.loading ? '--' : manualGroupLabel(current.page)
  // A page whose title hasn't loaded yet (still loading, or failed) falls
  // back to the same question the contents page uses - always a coherent
  // sentence, never "How does the undefined page work?".
  const askMateQuestion =
    isIndex || !manual.page ? 'What can Helmcentral do?' : `How does the ${manual.page.title} page work?`

  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent side="right" className="flex h-full min-w-0 flex-col gap-4 sm:max-w-xl">
        {/* pr-10 reserves room for the primitive's own 40px close button,
            same reasoning as mate-sheet.tsx's header. */}
        <SheetHeader className="flex-row items-center justify-between space-y-0 pr-10">
          <div className="min-w-0">
            <SheetTitle className="truncate text-base">{title}</SheetTitle>
            <div className="truncate text-[11px] text-muted-foreground">{groupLabel}</div>
          </div>
          <div className="flex shrink-0 items-center gap-1">
            {history.length > 1 && (
              <Button
                variant="ghost"
                size="icon"
                aria-label="Back"
                title="Back"
                onClick={() => setHistory((prev) => prev.slice(0, -1))}
              >
                <ArrowLeft className="h-4 w-4" />
              </Button>
            )}
            {!isIndex && (
              <Button variant="ghost" size="icon" aria-label="Manual contents" title="Manual contents" onClick={openContents}>
                <BookOpen className="h-4 w-4" />
              </Button>
            )}
            <Button
              variant="ghost"
              size="icon"
              aria-label="Ask Mate about this page"
              title="Ask Mate about this page"
              onClick={() => onAskMate(askMateQuestion)}
            >
              <Sparkles className="h-4 w-4" />
            </Button>
          </div>
        </SheetHeader>

        {/* min-h-0 + flex-1 + overflow-y-auto: the same load-bearing flex
            chain note in mate-sheet.tsx applies - this is what gives the
            scrolling region a bounded height instead of growing forever. */}
        <div ref={bodyRef} className="min-h-0 min-w-0 flex-1 overflow-y-auto">
          {manual.loading ? (
            <div className="space-y-2" data-testid="manual-sheet-skeleton">
              <Skeleton className="h-4 w-3/4" />
              <Skeleton className="h-4 w-full" />
              <Skeleton className="h-4 w-5/6" />
            </div>
          ) : manual.error ? (
            <ManualLoadErrorCard
              error={manual.error}
              pageId={current.page}
              isIndex={isIndex}
              onRetry={manual.reload}
              onOpenContents={openContents}
            />
          ) : manual.page ? (
            <>
              {missingHeading && (
                <p className="mb-3 text-sm text-muted-foreground">Section "{missingHeading}" is not on this page</p>
              )}
              <ManualMarkdown
                content={manualBodyWithoutTitle(manual.page.body)}
                pageId={manual.page.id}
                onNavigate={handleNavigate}
              />
            </>
          ) : null}
        </div>
      </SheetContent>
    </Sheet>
  )
}

interface ManualLoadErrorCardProps {
  error: { status: number; message: string }
  pageId: string
  isIndex: boolean
  onRetry: () => void
  onOpenContents: () => void
}

// Three distinct shapes, per ADR 0095: the manual isn't staged in this
// build (503, retryable once a rebuild stages it), this id doesn't exist
// (404, not retryable - only "go to the contents" makes sense, and not even
// that when the contents page is itself what 404'd), or anything else
// (a transient failure, retryable).
function ManualLoadErrorCard({ error, pageId, isIndex, onRetry, onOpenContents }: ManualLoadErrorCardProps) {
  if (error.status === 503) {
    return (
      <div className="space-y-3 rounded-lg border border-border bg-card p-4">
        <p className="text-sm text-foreground">The manual is not in this build.</p>
        <p className="text-sm text-muted-foreground">{error.message}</p>
        <div className="flex gap-2">
          <Button onClick={onRetry}>Try again</Button>
        </div>
      </div>
    )
  }

  if (error.status === 404) {
    return (
      <div className="space-y-3 rounded-lg border border-border bg-card p-4">
        <p className="text-sm text-foreground">This build's manual has no page "{pageId}".</p>
        {!isIndex && (
          <div className="flex gap-2">
            <Button variant="ghost" onClick={onOpenContents}>
              Open the contents
            </Button>
          </div>
        )}
      </div>
    )
  }

  return (
    <div className="space-y-3 rounded-lg border border-border bg-card p-4">
      <p className="text-sm text-foreground">Could not load the manual.</p>
      <p className="text-sm text-muted-foreground">{error.message}</p>
      <div className="flex gap-2">
        <Button onClick={onRetry}>Try again</Button>
      </div>
    </div>
  )
}
