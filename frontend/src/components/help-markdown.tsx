import { lazy, Suspense } from 'react'

import type { HelpLink } from '@/lib/help-links'

// Same kiosk bundle-split reasoning as assistant-markdown.tsx: this keeps
// react-markdown's module graph (and the regex lookbehind literal in its
// mdast-util-gfm-autolink-literal dependency, unparseable by a pre-16.4
// Safari) out of the entry chunk. The Help sheet never mounts on the kiosk
// route in the first place, but a lazy import here still stops help
// content from pulling that graph back into the shared bundle every other
// route loads.
const HelpMarkdownImpl = lazy(() => import('./help-markdown-impl'))

interface HelpMarkdownProps {
  content: string
  /** The help page id this content came from - relative links (e.g.
   * "alarms.md" or "../reference/configuration.md#…") are resolved against
   * it, the same way a browser resolves a relative href against the current
   * document's URL. */
  pageId: string
  onNavigate: (link: HelpLink) => void
  /** Fired after every render this content mounts - see
   * help-markdown-impl.tsx's own comment on the prop. */
  onRendered?: () => void
}

export function HelpMarkdown({ content, pageId, onNavigate, onRendered }: HelpMarkdownProps) {
  return (
    <Suspense fallback={<div className="min-w-0 text-sm leading-relaxed text-muted-foreground" />}>
      <HelpMarkdownImpl content={content} pageId={pageId} onNavigate={onNavigate} onRendered={onRendered} />
    </Suspense>
  )
}
