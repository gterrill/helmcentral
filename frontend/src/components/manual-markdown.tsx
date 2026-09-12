import { lazy, Suspense } from 'react'

import type { ManualLink } from '@/lib/manual-links'

// Same kiosk bundle-split reasoning as assistant-markdown.tsx: this keeps
// react-markdown's module graph (and the regex lookbehind literal in its
// mdast-util-gfm-autolink-literal dependency, unparseable by a pre-16.4
// Safari) out of the entry chunk. The Manual sheet never mounts on the kiosk
// route in the first place, but a lazy import here still stops manual
// content from pulling that graph back into the shared bundle every other
// route loads.
const ManualMarkdownImpl = lazy(() => import('./manual-markdown-impl'))

interface ManualMarkdownProps {
  content: string
  /** The manual page id this content came from - relative links (e.g.
   * "alarms.md" or "../reference/configuration.md#…") are resolved against
   * it, the same way a browser resolves a relative href against the current
   * document's URL. */
  pageId: string
  onNavigate: (link: ManualLink) => void
  /** Fired after every render this content mounts - see
   * manual-markdown-impl.tsx's own comment on the prop. */
  onRendered?: () => void
}

export function ManualMarkdown({ content, pageId, onNavigate, onRendered }: ManualMarkdownProps) {
  return (
    <Suspense fallback={<div className="min-w-0 text-sm leading-relaxed text-muted-foreground" />}>
      <ManualMarkdownImpl content={content} pageId={pageId} onNavigate={onNavigate} onRendered={onRendered} />
    </Suspense>
  )
}
